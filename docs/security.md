# Security architecture and threat model

## Security objective

The connector permits a hosted monitoring service to collect bounded diagnostic data without opening an inbound customer firewall rule and without giving the service a general-purpose network tunnel or SQL console.

The principal containment rule is: compromise of the public relay or control plane must not automatically provide arbitrary SQL execution or arbitrary access to the customer's internal network.

## Trust boundaries

```text
Customer administrator
  | local enrollment and target configuration
  v
Connector host ── verified MySQL TLS ──> configured database targets
  |
  └── outbound HTTPS/mTLS ──> public relay ──> control plane/job signer
```

The connector trusts:

- the local root administrator and operating system;
- the connector's locally stored configuration and trust roots;
- the service certificate chain and enrolled client identity;
- the pinned Ed25519 job-signing public key; and
- MySQL certificates issued by the locally configured MySQL CA.

It does not trust job transport alone. Every job is verified independently after mTLS transport authentication.

## Implemented controls

### Outbound-only network design

The connector has no server socket. It initiates HTTPS long polls and result uploads on TCP 443. Database addresses are stored locally and cannot be supplied by a job, preventing the service from using the connector as an internal scanner or generic TCP proxy.

### Enrollment and connector identity

Enrollment tokens must be single-use, short-lived, random, and tenant-bound. The connector creates an ECDSA P-256 private key locally and sends only a CSR. It validates that the returned certificate:

- matches the generated key;
- chains to the returned service CA;
- is currently valid; and
- permits TLS client authentication.

The runtime presents that certificate through mTLS. The server must derive tenant and connector identity from the authenticated certificate instead of trusting URL or JSON fields.

### Signed jobs and replay resistance

Jobs are Ed25519-signed over the exact payload bytes. The connector rejects unknown JSON fields, invalid signatures, tenant or connector mismatches, expired jobs, jobs issued too far in the future, lifetimes over 15 minutes, invalid nonces, and sequence numbers not greater than persisted state.

The monotonic sequence is persisted before execution. This favours at-most-once behaviour: after a crash, a read-only job may be reported as interrupted rather than executed twice. The control plane must expire the lease rather than lowering or reusing a sequence.

### No arbitrary SQL

The operation catalogue and SQL text are compiled into the binary. Jobs contain only an operation identifier and tightly decoded parameters. Unknown parameters fail closed. Multi-statement execution and parameter interpolation are disabled in the MySQL driver.

Do not add a generic `execute_sql`, MySQL protocol tunnel, command shell, plugin loader, file reader, or cloud-supplied target feature. Such a change would invalidate the threat model.

### Workload containment

The connector enforces local maximums for query duration, result rows, result bytes, HTTP bodies, connection-pool size, and job lifetime. A job may request smaller limits but cannot increase local limits. Collection failures return controlled cloud errors; detailed database errors remain in the customer journal.

### Credential custody

MySQL credentials are accepted only through standard input and are stored separately from target metadata. They are never included in cloud jobs or results. Secret files must be readable only by `root` and the `mysql-monitor` service group.

File permissions do not protect secrets from root or a compromised connector process. For higher-assurance deployments, add systemd encrypted credentials or a narrowly scoped external secret provider. Do not claim that encrypting a password with a key stored beside it protects against host compromise.

### Transport security

- Service connections require HTTPS, certificate validation, and mTLS after enrollment.
- TLS 1.2 is the minimum; TLS 1.3 is preferred where negotiated.
- TCP MySQL targets require a local CA and verified server name.
- A local Unix socket is supported when the connector and MySQL share a host.
- There is no insecure-skip-verify option and no plaintext TCP fallback.
- TLS 1.3 early data should be disabled on control-plane job and result endpoints.

### Process isolation

The supplied systemd unit uses an unprivileged account, an empty capability set, `NoNewPrivileges`, protected system and home paths, private devices and temporary storage, restricted address families, and a single writable state directory. Test the policy on every supported distribution with `systemd-analyze security`; hardening is not a substitute for patching the host.

## Data classification and privacy

Monitoring results can include:

- hostnames and server versions;
- schemas, tables, users, and topology;
- normalized statement text;
- workload volumes and timing; and
- session and lock relationships.

Treat results as customer confidential. The control plane should apply per-tenant encryption at rest, strict tenant scoping, MFA and RBAC, short retention, deletion workflows, access audit logs, and regional/data-processing controls appropriate to customer agreements.

Prefer statement digests over complete SQL. Never collect application table rows. New collectors require a privacy review as well as a database-security review.

## Threats and mitigations

| Threat | Primary mitigations | Residual risk |
|---|---|---|
| Internet attacker | TLS validation, mTLS, signed jobs, expiry and replay checks | CA or cryptographic implementation compromise |
| Public relay compromise | Relay separated from signing key; connector verifies signatures | Relay can deny service or observe data unless further payload encryption is added |
| Control-plane compromise | Fixed collectors, local targets, DB least privilege, resource bounds | Attacker with signing access can run every enabled collector |
| Malicious/replayed job | Exact-byte signature, identity binding, expiry, nonce, monotonic sequence | Clock failure can reject legitimate work |
| SQL injection | No cloud SQL, fixed queries, strict parameters, no multi-statements | A defective built-in collector still runs with monitor privileges |
| Internal-network pivot | Targets configured locally; no generic proxy | Compromised host has its ordinary network reachability |
| Connector-host compromise | Dedicated host/user, file permissions, systemd sandbox, limited DB account | MySQL password and connector key can be stolen by root/process compromise |
| Excessive monitoring load | Local time/row/byte/connection limits | Some Performance Schema queries may still be costly on unusual installations |
| Malicious update | Signed artifacts/TUF required before auto-update | Current MVP does not implement updates |

## Control-plane security requirements

- Separate public relay, authorization, job construction, and signing responsibilities.
- Keep signing keys in a managed HSM/KMS where possible; log every signature request.
- Require MFA and least-privilege RBAC for customer and operator actions.
- Bind each job to one tenant, connector, target, allowed operation, sequence, and short expiry.
- Make result submission idempotent and tenant-bound.
- Rate-limit enrollment, polling, and result uploads independently.
- Do not log enrollment tokens, certificate private material, complete results, or credentials.
- Support immediate connector revocation and alert on revoked-certificate use.
- Maintain a customer-visible audit history of job creator, approver, target, operation, timing, status, and result access.
- Do not automatically execute tuning changes. Any future mutation path needs separate credentials, explicit approval, an exact preview, maintenance-window controls, before/after capture, and rollback.

## Incident response

### Suspected connector-host compromise

1. Isolate the host without destroying evidence.
2. Revoke the connector certificate in the control plane.
3. Lock the connector's MySQL accounts from a trusted administration path.
4. Preserve journal, control-plane audit, authentication, and network logs.
5. Rotate MySQL credentials, connector identity, enrollment credentials, and any proxy credential present on the host.
6. Rebuild on a clean, patched host; do not simply restart the old binary.
7. Review collected data access and jobs executed during the affected period.

### Suspected service signing-key compromise

1. Stop job issuance and revoke the signing key.
2. Notify customers to stop connectors or block outbound 443 to the service.
3. Determine all jobs signed by the affected key and disclose their operations and targets.
4. Rotate the trust key through an authenticated, audited recovery mechanism—not an ordinary job signed by the compromised key.
5. Re-enrol connectors if the trust chain cannot be proven intact.

## Security references

- [OWASP Transport Layer Security Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Transport_Layer_Security_Cheat_Sheet.html)
- [MySQL encrypted connection configuration](https://dev.mysql.com/doc/refman/8.4/en/using-encrypted-connections.html)
- [MySQL Performance Schema](https://dev.mysql.com/doc/refman/8.4/en/performance-schema.html)
- [MySQL sys schema](https://dev.mysql.com/doc/refman/8.4/en/sys-schema.html)
- [The Update Framework](https://theupdateframework.io/docs/overview/)
