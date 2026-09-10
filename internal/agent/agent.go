package agent

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/example/mysql-monitor-connector/internal/collectors"
	"github.com/example/mysql-monitor-connector/internal/config"
	"github.com/example/mysql-monitor-connector/internal/mysqltarget"
	"github.com/example/mysql-monitor-connector/internal/protocol"
)

type runner struct {
	configDir    string
	config       config.Config
	client       *http.Client
	publicKey    ed25519.PublicKey
	logger       *slog.Logger
	lastSequence uint64
}

func Run(ctx context.Context, configDir string, logger *slog.Logger) error {
	cfg, err := config.Load(configDir)
	if err != nil {
		return err
	}
	client, err := newHTTPClient(cfg)
	if err != nil {
		return err
	}
	publicKeyBytes, err := base64.RawURLEncoding.DecodeString(cfg.JobSigningPublicKey)
	if err != nil || len(publicKeyBytes) != ed25519.PublicKeySize {
		return errors.New("job signing public key is invalid")
	}
	r := &runner{configDir: configDir, config: cfg, client: client, publicKey: ed25519.PublicKey(publicKeyBytes), logger: logger}
	if err := os.MkdirAll(cfg.DataDir, 0750); err != nil {
		return err
	}
	r.lastSequence, err = loadSequence(filepath.Join(cfg.DataDir, "last-sequence"))
	if err != nil {
		return err
	}
	logger.Info("connector started", "connector_id", cfg.ConnectorID)
	return r.loop(ctx)
}

func (r *runner) loop(ctx context.Context) error {
	backoff := time.Second
	for ctx.Err() == nil {
		job, err := r.poll(ctx)
		if err != nil {
			r.logger.Warn("poll failed", "error", err)
			if !sleepContext(ctx, jitter(backoff)) {
				break
			}
			if backoff < time.Minute {
				backoff *= 2
			}
			continue
		}
		backoff = time.Second
		if job == nil {
			continue
		}
		if err := protocol.ValidateJob(*job, r.config.TenantID, r.config.ConnectorID, r.lastSequence, time.Now()); err != nil {
			r.logger.Warn("rejected signed job", "job_id", job.JobID, "error", err)
			continue
		}
		if err := storeSequence(filepath.Join(r.config.DataDir, "last-sequence"), job.Sequence); err != nil {
			return err
		}
		r.lastSequence = job.Sequence
		if !collectors.Known(job.Operation) {
			_ = r.postResult(ctx, *job, protocol.Result{JobID: job.JobID, Status: "rejected", StartedAt: time.Now(), FinishedAt: time.Now(), ErrorCode: "unknown_operation", Error: "operation is not installed on this connector"})
			continue
		}
		result := r.execute(ctx, *job)
		if err := r.postResult(ctx, *job, result); err != nil {
			r.logger.Warn("result upload failed", "job_id", job.JobID, "error", err)
		}
	}
	return nil
}

func (r *runner) poll(ctx context.Context) (*protocol.Job, error) {
	endpoint, err := endpointURL(r.config.ServiceURL, "v1", "connectors", r.config.ConnectorID, "jobs", "next")
	if err != nil {
		return nil, err
	}
	q := endpoint.Query()
	q.Set("wait", strconv.Itoa(r.config.PollWaitSeconds))
	endpoint.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "mysql-monitor-connector/dev")
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNoContent {
		io.Copy(io.Discard, resp.Body)
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("poll returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, protocol.MaxEnvelopeBytes+1))
	if err != nil {
		return nil, err
	}
	job, err := protocol.DecodeAndVerify(body, r.publicKey)
	if err != nil {
		return nil, err
	}
	return &job, nil
}

func (r *runner) execute(parent context.Context, job protocol.Job) (result protocol.Result) {
	started := time.Now().UTC()
	result = protocol.Result{JobID: job.JobID, Status: "failed", StartedAt: started}
	defer func() { result.FinishedAt = time.Now().UTC() }()
	target, password, err := config.LoadTarget(r.configDir, job.TargetID)
	if err != nil {
		result.ErrorCode = "target_unavailable"
		result.Error = "configured target is unavailable"
		return result
	}
	db, err := mysqltarget.Open(target, password)
	for i := range password {
		password[i] = 0
	}
	if err != nil {
		result.ErrorCode = "target_configuration"
		result.Error = "target configuration could not be loaded"
		return result
	}
	defer db.Close()
	timeout := time.Duration(r.config.MaxQuerySeconds) * time.Second
	if job.MaxRuntimeMS > 0 && time.Duration(job.MaxRuntimeMS)*time.Millisecond < timeout {
		timeout = time.Duration(job.MaxRuntimeMS) * time.Millisecond
	}
	if timeout < time.Second {
		timeout = time.Second
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	maxRows := lowerPositive(job.MaxRows, r.config.MaxRows)
	maxBytes := lowerPositive(job.MaxResultBytes, r.config.MaxResultBytes)
	queryResult, err := collectors.Execute(ctx, db, job.Operation, job.Parameters, maxRows, maxBytes)
	if err != nil {
		r.logger.Warn("collector failed", "job_id", job.JobID, "target", job.TargetID, "operation", job.Operation, "error", err)
		result.ErrorCode = "collector_failed"
		result.Error = "collector execution failed"
		return result
	}
	result.Status = "completed"
	result.Columns = queryResult.Columns
	result.Rows = queryResult.Rows
	result.Truncated = queryResult.Truncated
	return result
}

func (r *runner) postResult(ctx context.Context, job protocol.Job, result protocol.Result) error {
	endpoint, err := endpointURL(r.config.ServiceURL, "v1", "connectors", r.config.ConnectorID, "jobs", job.JobID, "result")
	if err != nil {
		return err
	}
	body, err := json.Marshal(result)
	if err != nil {
		return err
	}
	if len(body) > r.config.MaxResultBytes+256*1024 {
		return errors.New("encoded result exceeds configured limit")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("result upload returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func newHTTPClient(cfg config.Config) (*http.Client, error) {
	cert, err := tls.LoadX509KeyPair(cfg.ClientCertificate, cfg.ClientKey)
	if err != nil {
		return nil, fmt.Errorf("load connector certificate: %w", err)
	}
	roots, err := x509.SystemCertPool()
	if err != nil {
		roots = x509.NewCertPool()
	}
	if cfg.ServiceCAFile != "" {
		caPEM, err := os.ReadFile(cfg.ServiceCAFile)
		if err != nil {
			return nil, err
		}
		if !roots.AppendCertsFromPEM(caPEM) {
			return nil, errors.New("service CA contains no certificates")
		}
	}
	transport := &http.Transport{
		Proxy:             http.ProxyFromEnvironment,
		TLSClientConfig:   &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}, RootCAs: roots},
		ForceAttemptHTTP2: true, MaxIdleConns: 2, MaxIdleConnsPerHost: 2, IdleConnTimeout: 90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 70 * time.Second,
	}
	return &http.Client{Transport: transport}, nil
}

func endpointURL(base string, parts ...string) (*url.URL, error) {
	u, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	u.RawQuery = ""
	u.Fragment = ""
	path := strings.TrimRight(u.Path, "/")
	for _, part := range parts {
		path += "/" + url.PathEscape(part)
	}
	u.Path = path
	return u, nil
}

func loadSequence(path string) (uint64, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	n, err := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid persisted job sequence: %w", err)
	}
	return n, nil
}

func storeSequence(path string, sequence uint64) error {
	return config.AtomicWrite(path, []byte(strconv.FormatUint(sequence, 10)+"\n"), 0640)
}
func lowerPositive(requested, limit int) int {
	if requested > 0 && requested < limit {
		return requested
	}
	return limit
}
func jitter(d time.Duration) time.Duration { return d + time.Duration(rand.Int64N(int64(d/4)+1)) }
func sleepContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
