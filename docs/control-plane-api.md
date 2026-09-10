# Control-plane API and security contract

This is a security boundary, not only a wire-format description. The service must fail closed when identity, authorization, signature, sequence, expiry, size, or state checks fail.

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

Enrollment tokens must contain at least 128 bits of randomness, expire quickly, be stored as one-way hashes where practical, be bound to a tenant and pending connector record, and become unusable in the same transaction that issues the certificate. Rate-limit failures without logging token values.

Issue short-lived client certificates with a connector-specific identity and the client-authentication extended key usage. Store the CA private key in managed key infrastructure and separate certificate issuance authority from the public relay.

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

Sequences are strictly increasing per connector. Job lifetimes must not exceed 15 minutes. Generate a unique nonce with at least 128 bits of randomness. Do not put SQL, database addresses, credentials, executable content, or file paths in jobs.

The signing service must authorize the tenant, connector, locally known target name, operation, parameters, and limits before signing. The public polling relay should be able to deliver already signed jobs but should not possess the signing key or permission to construct arbitrary jobs.

## Result

`POST /v1/connectors/{connector_id}/jobs/{job_id}/result`

The body contains `status`, timestamps, column names, rows, a truncation marker, and a controlled error code. The control plane should make result submission idempotent by job ID and reject a result belonging to another connector or tenant.

Enforce a server-side body limit no larger than the connector and tenant policy. Validate column and row structure, timestamps, status values, and UTF-8 before storage. Never deserialize connector data into executable objects or render it as unescaped HTML.

## Operational requirements

- Disable TLS early data on job and result endpoints.
- Prefer TLS 1.3 and permit TLS 1.2 only for required compatibility; disable older protocols and weak suites.
- Rate-limit enrollment and runtime endpoints independently.
- Keep the public relay separate from the job-signing key.
- Rotate short-lived connector certificates and support immediate revocation.
- Never log enrollment tokens, complete job bodies, SQL-derived result rows, or client certificate private material.
- Derive identity from the authenticated client certificate and compare it to every URL, job, queue, and storage key.
- Use explicit per-tenant authorization in every storage and cache operation; do not rely only on user-interface filtering.
- Make result submission idempotent and reject cross-connector or cross-tenant job IDs.
- Record an immutable audit event for enrollment, revocation, job creation, approval, signature, delivery, completion, and result access.
- Apply encryption at rest, documented retention and deletion, backup protection, and privacy controls to monitoring results.
- Alert on repeated authentication, signature, sequence, revocation, and result-validation failures.

## Certificate and signing-key rotation

The current connector does not yet implement automatic certificate or job-signing-key rotation. A production protocol must define renewal before certificate expiry, rotate the connector key where feasible, authenticate trust-key changes through an uncompromised root, support overlap during rollout, and reject rollback to previously revoked keys. Do not deliver a replacement signing key solely in a job signed by the key suspected of compromise.

## Availability and delivery semantics

Polling and result endpoints should support bounded retries with jitter and idempotency. The current connector persists the sequence before executing a job and does not yet have a durable result spool; a crash or upload failure can therefore leave a job without a result. The control plane must time out that lease and report it as interrupted, never reuse or lower the connector sequence, and avoid interpreting silence as successful execution.

## Prohibited capabilities

The service protocol must not add arbitrary SQL, cloud-supplied database addresses, raw TCP forwarding, shell commands, file upload/download, dynamic plugins, or automatic tuning mutations. If a future product requires database changes, design a separate approval and credential boundary with an exact change preview, short-lived privileges, maintenance-window enforcement, before/after evidence, and rollback.
