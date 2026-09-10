package protocol

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

const MaxEnvelopeBytes = 256 * 1024

type Envelope struct {
	Payload   string `json:"payload"`
	Signature string `json:"signature"`
}

type Job struct {
	JobID          string          `json:"job_id"`
	TenantID       string          `json:"tenant_id"`
	ConnectorID    string          `json:"connector_id"`
	TargetID       string          `json:"target_id"`
	Operation      string          `json:"operation"`
	Parameters     json.RawMessage `json:"parameters,omitempty"`
	IssuedAt       time.Time       `json:"issued_at"`
	ExpiresAt      time.Time       `json:"expires_at"`
	Sequence       uint64          `json:"sequence"`
	MaxRuntimeMS   int             `json:"max_runtime_ms,omitempty"`
	MaxRows        int             `json:"max_rows,omitempty"`
	MaxResultBytes int             `json:"max_result_bytes,omitempty"`
	Nonce          string          `json:"nonce"`
}

type Result struct {
	JobID      string    `json:"job_id"`
	Status     string    `json:"status"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
	Columns    []string  `json:"columns,omitempty"`
	Rows       [][]any   `json:"rows,omitempty"`
	Truncated  bool      `json:"truncated,omitempty"`
	ErrorCode  string    `json:"error_code,omitempty"`
	Error      string    `json:"error,omitempty"`
}

func DecodeAndVerify(data []byte, publicKey ed25519.PublicKey) (Job, error) {
	var job Job
	if len(data) > MaxEnvelopeBytes {
		return job, errors.New("job envelope is too large")
	}
	var envelope Envelope
	envelopeDecoder := json.NewDecoder(bytes.NewReader(data))
	envelopeDecoder.DisallowUnknownFields()
	if err := envelopeDecoder.Decode(&envelope); err != nil {
		return job, fmt.Errorf("decode envelope: %w", err)
	}
	if err := requireEOF(envelopeDecoder); err != nil {
		return job, errors.New("job envelope contains trailing data")
	}
	payload, err := base64.RawURLEncoding.DecodeString(envelope.Payload)
	if err != nil {
		return job, errors.New("invalid payload encoding")
	}
	signature, err := base64.RawURLEncoding.DecodeString(envelope.Signature)
	if err != nil {
		return job, errors.New("invalid signature encoding")
	}
	if len(publicKey) != ed25519.PublicKeySize || !ed25519.Verify(publicKey, payload, signature) {
		return job, errors.New("invalid job signature")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&job); err != nil {
		return job, fmt.Errorf("decode signed job: %w", err)
	}
	if err := requireEOF(decoder); err != nil {
		return job, errors.New("signed job contains trailing data")
	}
	return job, nil
}

func requireEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return errors.New("expected end of JSON")
	}
	return nil
}

func ValidateJob(job Job, tenantID, connectorID string, lastSequence uint64, now time.Time) error {
	if job.JobID == "" || len(job.JobID) > 128 {
		return errors.New("invalid job_id")
	}
	if job.TenantID != tenantID || job.ConnectorID != connectorID {
		return errors.New("job identity does not match this connector")
	}
	if job.TargetID == "" || len(job.TargetID) > 64 {
		return errors.New("invalid target_id")
	}
	if job.Operation == "" || len(job.Operation) > 128 {
		return errors.New("invalid operation")
	}
	if len(job.Nonce) < 16 || len(job.Nonce) > 256 {
		return errors.New("invalid nonce")
	}
	if job.Sequence <= lastSequence {
		return errors.New("job sequence is not newer than persisted state")
	}
	if job.IssuedAt.After(now.Add(2 * time.Minute)) {
		return errors.New("job issue time is in the future")
	}
	if !job.ExpiresAt.After(now) {
		return errors.New("job has expired")
	}
	if job.ExpiresAt.Sub(job.IssuedAt) > 15*time.Minute {
		return errors.New("job lifetime exceeds 15 minutes")
	}
	return nil
}
