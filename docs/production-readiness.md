# Production-readiness checklist

The repository is a secure connector MVP, not a complete production service. Do not represent it as production-ready until every applicable item below has an owner, evidence, and an accepted residual-risk decision.

## Connector engineering

- [ ] Add automatic short-lived client-certificate renewal with key rotation and failure alerts.
- [ ] Add a bounded, encrypted, crash-safe result spool with idempotent retry.
- [ ] Define a safe job-acknowledgement and interrupted-job protocol.
- [ ] Add authenticated job-signing-key rotation and emergency recovery.
- [ ] Add versioned configuration migrations and rollback tests.
- [ ] Add Linux package builds for supported Debian/Ubuntu/RHEL-family releases.
- [ ] Publish SBOMs, provenance, signed checksums, and reproducible-build evidence.
- [ ] Implement signed, rollback-resistant updates or retain a documented manual process.
- [ ] Add resource controls such as systemd memory, CPU, task, and file-descriptor limits based on load testing.

## Database compatibility

- [ ] Integration-test every collector against each supported MySQL release.
- [ ] Decide explicitly whether MariaDB, Percona Server, Aurora MySQL, and managed services are supported.
- [ ] Maintain version-specific least-privilege grant templates.
- [ ] Benchmark collector overhead on large Performance Schema datasets.
- [ ] Verify cancellation behaviour for every supported driver/server combination.
- [ ] Define safe collection intervals, concurrency, and backoff defaults.

## Control plane

- [ ] Implement and test the documented API with strict mTLS identity binding.
- [ ] Place enrollment, CA, and job-signing keys in managed, audited key infrastructure.
- [ ] Separate public relay permissions from job construction and signing.
- [ ] Enforce tenant isolation in authorization, storage, queues, caches, logs, and backups.
- [ ] Add MFA, RBAC, approval policies, session security, and customer audit exports.
- [ ] Add connector revocation, certificate-expiry alerting, and compromised-key recovery.
- [ ] Add idempotent result ingestion, bounded parsing, retention, and deletion.
- [ ] Disable TLS early data for stateful endpoints and test TLS/proxy compatibility.

## Security assurance

- [ ] Produce a formal threat model and data-flow diagram for the deployed service.
- [ ] Complete independent code review, penetration testing, and dependency review.
- [ ] Enable private vulnerability reporting and publish a supported disclosure process.
- [ ] Run secret scanning, static analysis, dependency vulnerability scanning, and release-policy checks in CI.
- [ ] Conduct connector-host, cloud-control-plane, signing-key, and tenant-isolation incident exercises.
- [ ] Define security support periods and patch timelines.
- [ ] Document regional privacy, retention, subprocessors, backup, and deletion behaviour.

## Release gate

A release candidate should demonstrate:

1. A fresh install on every supported distribution and architecture.
2. Enrollment, renewal, revocation, and re-enrollment.
3. Successful least-privilege collection from every supported database version.
4. Failure closed under invalid TLS, signatures, identities, sequence, expiry, parameters, targets, and result sizes.
5. Recovery from network loss, proxy loss, process crash, disk full, certificate expiry, and control-plane outage.
6. No credentials or sensitive row data in connector, proxy, control-plane, or CI logs.
7. Signed artifact and update verification, including rejection of rollback and freeze scenarios.
