package enroll

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/example/mysql-monitor-connector/internal/config"
)

type Options struct{ ConfigDir, ServiceURL, TokenFile, CAFile, DataDir string }
type request struct {
	Token string `json:"token"`
	CSR   string `json:"csr"`
}
type response struct {
	TenantID            string `json:"tenant_id"`
	ConnectorID         string `json:"connector_id"`
	Certificate         string `json:"certificate"`
	CA                  string `json:"ca"`
	JobSigningPublicKey string `json:"job_signing_public_key"`
}

func Run(ctx context.Context, opts Options) error {
	u, err := url.Parse(opts.ServiceURL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("--service-url must be an HTTPS URL without credentials, query, or fragment")
	}
	if opts.TokenFile == "" {
		return errors.New("--token-file is required")
	}
	tokenBytes, err := os.ReadFile(opts.TokenFile)
	if err != nil {
		return fmt.Errorf("read enrollment token: %w", err)
	}
	token := strings.TrimSpace(string(tokenBytes))
	if len(token) < 16 || len(token) > 4096 {
		return errors.New("enrollment token has invalid length")
	}

	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: "mysql-monitor-connector"}}, privateKey)
	if err != nil {
		return err
	}
	csr := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})
	body, _ := json.Marshal(request{Token: token, CSR: string(csr)})

	transport := &http.Transport{
		Proxy:             http.ProxyFromEnvironment,
		TLSClientConfig:   &tls.Config{MinVersion: tls.VersionTLS12},
		ForceAttemptHTTP2: true,
	}
	if opts.CAFile != "" {
		caPEM, err := os.ReadFile(opts.CAFile)
		if err != nil {
			return fmt.Errorf("read service CA: %w", err)
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(caPEM) {
			return errors.New("service CA file contains no certificates")
		}
		transport.TLSClientConfig.RootCAs = roots
	}
	client := &http.Client{Transport: transport, Timeout: 30 * time.Second}
	endpoint := strings.TrimRight(opts.ServiceURL, "/") + "/v1/connectors/enroll"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("enrollment request: %w", err)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024+1))
	if err != nil {
		return err
	}
	if len(responseBody) > 1024*1024 {
		return errors.New("enrollment response is too large")
	}
	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("enrollment rejected with HTTP %d", resp.StatusCode)
	}
	var enrolled response
	decoder := json.NewDecoder(bytes.NewReader(responseBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&enrolled); err != nil {
		return fmt.Errorf("decode enrollment response: %w", err)
	}
	if enrolled.TenantID == "" || enrolled.ConnectorID == "" || enrolled.Certificate == "" || enrolled.CA == "" || enrolled.JobSigningPublicKey == "" {
		return errors.New("enrollment response is incomplete")
	}

	keyDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	if err := validateEnrollment([]byte(enrolled.Certificate), keyPEM, []byte(enrolled.CA)); err != nil {
		return fmt.Errorf("validate enrollment certificate: %w", err)
	}
	jobKey, err := base64.RawURLEncoding.DecodeString(enrolled.JobSigningPublicKey)
	if err != nil || len(jobKey) != ed25519.PublicKeySize {
		return errors.New("enrollment returned an invalid Ed25519 job-signing key")
	}
	certPath := filepath.Join(opts.ConfigDir, "connector.pem")
	keyPath := filepath.Join(opts.ConfigDir, "connector-key.pem")
	caPath := filepath.Join(opts.ConfigDir, "service-ca.pem")
	if err := config.AtomicWrite(keyPath, keyPEM, 0640); err != nil {
		return err
	}
	if err := config.AtomicWrite(certPath, []byte(enrolled.Certificate), 0640); err != nil {
		return err
	}
	if err := config.AtomicWrite(caPath, []byte(enrolled.CA), 0640); err != nil {
		return err
	}
	final := config.Config{
		ServiceURL: opts.ServiceURL, TenantID: enrolled.TenantID, ConnectorID: enrolled.ConnectorID,
		ClientCertificate: certPath, ClientKey: keyPath, ServiceCAFile: caPath,
		JobSigningPublicKey: enrolled.JobSigningPublicKey, DataDir: opts.DataDir,
	}
	final.ApplyDefaults()
	if err := final.Validate(); err != nil {
		return fmt.Errorf("invalid enrollment response: %w", err)
	}
	configJSON, _ := json.MarshalIndent(final, "", "  ")
	return config.AtomicWrite(filepath.Join(opts.ConfigDir, "config.json"), append(configJSON, '\n'), 0640)
}

func validateEnrollment(certPEM, keyPEM, caPEM []byte) error {
	keyPair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return errors.New("certificate does not match the locally generated private key")
	}
	if len(keyPair.Certificate) == 0 {
		return errors.New("certificate chain is empty")
	}
	leaf, err := x509.ParseCertificate(keyPair.Certificate[0])
	if err != nil {
		return errors.New("connector certificate is malformed")
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return errors.New("service CA contains no certificates")
	}
	intermediates := x509.NewCertPool()
	for _, der := range keyPair.Certificate[1:] {
		cert, err := x509.ParseCertificate(der)
		if err != nil {
			return errors.New("connector certificate chain is malformed")
		}
		intermediates.AddCert(cert)
	}
	_, err = leaf.Verify(x509.VerifyOptions{
		Roots:         roots,
		Intermediates: intermediates,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	})
	if err != nil {
		return errors.New("connector certificate is not a valid client certificate signed by the service CA")
	}
	return nil
}
