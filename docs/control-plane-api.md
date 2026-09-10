# Control-plane API contract

All runtime endpoints require the connector's client certificate. The service must derive the connector and tenant from that certificate and verify that they match the URL and stored record.

## Enrollment

`POST /v1/connectors/enroll`

Request:

```json
{"token":"single-use secret","csr":"-----BEGIN CERTIFICATE REQUEST-----..."}
```

Successful response (`201 Created`):

```json
{
  "tenant_id": "tenant-uuid",
  "connector_id": "connector-uuid",
  "certificate": "PEM encoded connector certificate",
  "ca": "PEM encoded service CA chain",
  "job_signing_public_key": "unpadded base64url Ed25519 public key"
}
```

The server must validate and atomically consume the enrollment token and sign the submitted CSR. It must not generate or receive the connector private key.

## Poll

`GET /v1/connectors/{connector_id}/jobs/next?wait=45`

Return `204 No Content` when no job is available, or a signed envelope:

```json
{
  "payload": "base64url bytes of the exact compact job JSON",
  "signature": "base64url Ed25519 signature over those payload bytes"
}
```

Example decoded payload:

```json
{
  "job_id": "job-uuid",
  "tenant_id": "tenant-uuid",
  "connector_id": "connector-uuid",
  "target_id": "production",
  "operation": "collect.statement_digests.v1",
  "parameters": {"limit": 100},
  "issued_at": "2026-09-10T12:00:00Z",
  "expires_at": "2026-09-10T12:05:00Z",
  "sequence": 42,
  "max_runtime_ms": 10000,
  "max_rows": 1000,
  "max_result_bytes": 1048576,
  "nonce": "at-least-128-bits-of-randomness"
}
```

Sequences are strictly increasing per connector. Job lifetimes must not exceed 15 minutes. Do not put SQL, database addresses, credentials, or file paths in jobs.

## Result

`POST /v1/connectors/{connector_id}/jobs/{job_id}/result`

The body contains `status`, timestamps, column names, rows, a truncation marker, and a controlled error code. The control plane should make result submission idempotent by job ID and reject a result belonging to another connector or tenant.

## Operational requirements

- Disable TLS early data on job and result endpoints.
- Rate-limit enrollment and runtime endpoints independently.
- Keep the public relay separate from the job-signing key.
- Rotate short-lived connector certificates and support immediate revocation.
- Never log enrollment tokens, complete job bodies, SQL-derived result rows, or client certificate private material.
