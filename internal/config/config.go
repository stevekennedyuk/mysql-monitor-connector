package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	DefaultQueryTimeout = 10 * time.Second
	MaxSecretBytes      = 16 * 1024
)

var targetNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,63}$`)

type Config struct {
	ServiceURL          string `json:"service_url"`
	TenantID            string `json:"tenant_id"`
	ConnectorID         string `json:"connector_id"`
	ClientCertificate   string `json:"client_certificate"`
	ClientKey           string `json:"client_key"`
	ServiceCAFile       string `json:"service_ca_file,omitempty"`
	JobSigningPublicKey string `json:"job_signing_public_key"`
	DataDir             string `json:"data_dir"`
	PollWaitSeconds     int    `json:"poll_wait_seconds"`
	MaxResultBytes      int    `json:"max_result_bytes"`
	MaxRows             int    `json:"max_rows"`
	MaxQuerySeconds     int    `json:"max_query_seconds"`
}

type Target struct {
	Name          string `json:"name"`
	Network       string `json:"network"`
	Address       string `json:"address"`
	User          string `json:"user"`
	Database      string `json:"database,omitempty"`
	TLSCAFile     string `json:"tls_ca_file,omitempty"`
	TLSServerName string `json:"tls_server_name,omitempty"`
}

func (c *Config) ApplyDefaults() {
	if c.PollWaitSeconds == 0 {
		c.PollWaitSeconds = 45
	}
	if c.MaxResultBytes == 0 {
		c.MaxResultBytes = 5 * 1024 * 1024
	}
	if c.MaxRows == 0 {
		c.MaxRows = 5000
	}
	if c.MaxQuerySeconds == 0 {
		c.MaxQuerySeconds = int(DefaultQueryTimeout.Seconds())
	}
}

func (c Config) Validate() error {
	u, err := url.Parse(c.ServiceURL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("service_url must be an HTTPS URL without credentials, query, or fragment")
	}
	if c.TenantID == "" || c.ConnectorID == "" {
		return errors.New("tenant_id and connector_id are required")
	}
	if c.ClientCertificate == "" || c.ClientKey == "" {
		return errors.New("client certificate and key are required")
	}
	if c.JobSigningPublicKey == "" {
		return errors.New("job signing public key is required")
	}
	if !filepath.IsAbs(c.DataDir) {
		return errors.New("data_dir must be absolute")
	}
	if c.PollWaitSeconds < 1 || c.PollWaitSeconds > 60 {
		return errors.New("poll_wait_seconds must be between 1 and 60")
	}
	if c.MaxResultBytes < 1024 || c.MaxResultBytes > 20*1024*1024 {
		return errors.New("max_result_bytes is outside the safe range")
	}
	if c.MaxRows < 1 || c.MaxRows > 10000 {
		return errors.New("max_rows is outside the safe range")
	}
	if c.MaxQuerySeconds < 1 || c.MaxQuerySeconds > 60 {
		return errors.New("max_query_seconds is outside the safe range")
	}
	return nil
}

func Load(dir string) (Config, error) {
	var c Config
	if err := readJSON(filepath.Join(dir, "config.json"), &c); err != nil {
		return c, err
	}
	c.ApplyDefaults()
	return c, c.Validate()
}

func ValidateTarget(t Target) error {
	if !targetNamePattern.MatchString(t.Name) {
		return errors.New("target name must be 1-64 safe filename characters")
	}
	if t.User == "" {
		return errors.New("target user is required")
	}
	switch t.Network {
	case "tcp":
		host, port, err := net.SplitHostPort(t.Address)
		if err != nil || host == "" || port == "" {
			return errors.New("TCP address must be host:port (IPv6 must use brackets)")
		}
		if t.TLSCAFile == "" || t.TLSServerName == "" {
			return errors.New("TCP targets require --tls-ca and --tls-server-name")
		}
		if !filepath.IsAbs(t.TLSCAFile) {
			return errors.New("tls_ca_file must be absolute")
		}
	case "unix":
		if !filepath.IsAbs(t.Address) {
			return errors.New("Unix socket address must be absolute")
		}
		if t.TLSCAFile != "" || t.TLSServerName != "" {
			return errors.New("TLS options are not used with Unix sockets")
		}
	default:
		return errors.New("network must be tcp or unix")
	}
	return nil
}

func AddTarget(dir string, target Target, password []byte) error {
	password = bytes.TrimSuffix(password, []byte("\n"))
	password = bytes.TrimSuffix(password, []byte("\r"))
	if err := ValidateTarget(target); err != nil {
		return err
	}
	if len(password) == 0 {
		return errors.New("password must not be empty")
	}
	if len(password) > MaxSecretBytes {
		return errors.New("password exceeds 16 KiB")
	}
	if bytes.IndexByte(password, 0) >= 0 {
		return errors.New("password contains a NUL byte")
	}
	if err := os.MkdirAll(filepath.Join(dir, "targets.d"), 0750); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, "secrets"), 0750); err != nil {
		return err
	}
	b, err := json.MarshalIndent(target, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if err := atomicWrite(filepath.Join(dir, "targets.d", target.Name+".json"), b, 0640); err != nil {
		return err
	}
	return atomicWrite(filepath.Join(dir, "secrets", target.Name+".password"), password, 0640)
}

func LoadTarget(dir, name string) (Target, []byte, error) {
	var target Target
	if !targetNamePattern.MatchString(name) {
		return target, nil, errors.New("invalid target name")
	}
	if err := readJSON(filepath.Join(dir, "targets.d", name+".json"), &target); err != nil {
		return target, nil, err
	}
	if target.Name != name {
		return target, nil, errors.New("target filename and embedded name differ")
	}
	if err := ValidateTarget(target); err != nil {
		return target, nil, err
	}
	secretPath := filepath.Join(dir, "secrets", name+".password")
	info, err := os.Stat(secretPath)
	if err != nil {
		return target, nil, fmt.Errorf("stat target secret: %w", err)
	}
	if info.Mode().Perm()&0007 != 0 {
		return target, nil, errors.New("target secret must not be accessible by other users")
	}
	secret, err := os.ReadFile(secretPath)
	if err != nil {
		return target, nil, fmt.Errorf("read target secret: %w", err)
	}
	if len(secret) == 0 || len(secret) > MaxSecretBytes {
		return target, nil, errors.New("target secret has invalid length")
	}
	return target, secret, nil
}

func ListTargets(dir string) ([]Target, error) {
	entries, err := os.ReadDir(filepath.Join(dir, "targets.d"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var targets []Target
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".json")
		t, _, err := LoadTarget(dir, name)
		if err != nil {
			return nil, fmt.Errorf("load target %q: %w", name, err)
		}
		targets = append(targets, t)
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].Name < targets[j].Name })
	return targets, nil
}

func readJSON(path string, dst any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("parse %s: trailing JSON", path)
	}
	return nil
}

func AtomicWrite(path string, data []byte, mode os.FileMode) error {
	return atomicWrite(path, data, mode)
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err := f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
