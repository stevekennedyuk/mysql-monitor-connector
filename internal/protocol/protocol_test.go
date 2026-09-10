package protocol

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"
)

func TestDecodeVerifyAndValidate(t *testing.T) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	want := Job{JobID: "job-1", TenantID: "tenant-1", ConnectorID: "connector-1", TargetID: "db-1", Operation: "collect.server_identity.v1", IssuedAt: now.Add(-time.Second), ExpiresAt: now.Add(time.Minute), Sequence: 2, Nonce: "1234567890abcdef"}
	payload, _ := json.Marshal(want)
	envelope, _ := json.Marshal(Envelope{Payload: base64.RawURLEncoding.EncodeToString(payload), Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(privateKey, payload))})
	got, err := DecodeAndVerify(envelope, publicKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateJob(got, "tenant-1", "connector-1", 1, now); err != nil {
		t.Fatal(err)
	}
	if got.JobID != want.JobID {
		t.Fatalf("got %q want %q", got.JobID, want.JobID)
	}
}

func TestRejectsTamperedPayload(t *testing.T) {
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	payload := []byte(`{"job_id":"one"}`)
	signature := ed25519.Sign(privateKey, payload)
	payload[11] = 'x'
	envelope, _ := json.Marshal(Envelope{Payload: base64.RawURLEncoding.EncodeToString(payload), Signature: base64.RawURLEncoding.EncodeToString(signature)})
	if _, err := DecodeAndVerify(envelope, publicKey); err == nil {
		t.Fatal("tampered payload was accepted")
	}
}

func TestRejectsTrailingSignedJSON(t *testing.T) {
	publicKey, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	payload := []byte(`{"job_id":"one"} {}`)
	envelope, _ := json.Marshal(Envelope{Payload: base64.RawURLEncoding.EncodeToString(payload), Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(privateKey, payload))})
	if _, err := DecodeAndVerify(envelope, publicKey); err == nil {
		t.Fatal("trailing signed JSON was accepted")
	}
}

func TestRejectsReplayAndExpiredJobs(t *testing.T) {
	now := time.Now().UTC()
	job := Job{JobID: "j", TenantID: "t", ConnectorID: "c", TargetID: "db", Operation: "op", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute), Sequence: 4, Nonce: "1234567890abcdef"}
	if err := ValidateJob(job, "t", "c", 4, now); err == nil {
		t.Fatal("replayed sequence was accepted")
	}
	job.Sequence = 5
	job.ExpiresAt = now.Add(-time.Second)
	if err := ValidateJob(job, "t", "c", 4, now); err == nil {
		t.Fatal("expired job was accepted")
	}
}
