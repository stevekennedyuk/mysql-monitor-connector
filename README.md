# MySQL Monitor Connector

A small, outbound-only Linux connector for a hosted MySQL monitoring service. It polls a control plane over HTTPS with mutual TLS, verifies every job with Ed25519, and runs only collectors compiled into the binary against locally configured MySQL targets.

The connector has no inbound listener. The cloud service cannot supply database addresses, credentials, SQL, file paths, or executable commands.

## Architecture

```text
Monitoring control plane
          ^
          | outbound HTTPS 443: mTLS, signed jobs and bounded results
          |
Customer connector
          |
          | internal MySQL TLS 3306, or a local Unix socket
          v
Customer MySQL servers
```

The repository contains the customer connector. A compatible service must implement the [control-plane API](docs/control-plane-api.md).

## Documentation

- [Installation and enrollment](docs/installation.md)
- [Preparing MySQL securely](docs/mysql-setup.md)
- [Security architecture and threat model](docs/security.md)
- [Operations, upgrades, and troubleshooting](docs/operations.md)
- [Control-plane API and server requirements](docs/control-plane-api.md)
- [Production-readiness checklist](docs/production-readiness.md)
- [Reporting security vulnerabilities](SECURITY.md)

Read the production-readiness checklist before deploying this connector against a production database. The current code is an MVP and deliberately does not support arbitrary SQL or database changes.

## Built-in operations

- `collect.server_identity.v1`
- `collect.global_status.v1`
- `collect.global_variables.v1`
- `collect.statement_digests.v1`
- `collect.table_io.v1`
- `collect.lock_waits.v1`

All operations are read-only and have fixed SQL in the connector. The cloud may lower row, byte, and time limits but cannot raise the connector's locally configured limits.

## Build and test

Go 1.23 or newer is required to build from source.

```sh
make check
make build
make release
```

`make build` produces a native development binary. `make release` produces statically linked Linux `amd64` and `arm64` binaries in `build/`.

## Quick start

After reviewing the full [installation guide](docs/installation.md):

```sh
sudo ./packaging/install.sh ./build/mysql-monitor-connector-linux-amd64
sudo mysql-monitor-connector enroll \
  --service-url https://connect.example.com \
  --token-file /root/connector-enrollment-token
sudo chgrp -R mysql-monitor /etc/mysql-monitor-connector
sudo chmod -R o-rwx /etc/mysql-monitor-connector
```

Prepare a dedicated MySQL account as described in [MySQL setup](docs/mysql-setup.md), add the target locally, test it, and then start the service.

## Project status

The connector implements the secure transport and execution boundary, but production deployment also requires a correctly implemented control plane, PKI and signing-key operations, certificate renewal, durable result delivery, signed release artifacts, MySQL-version integration testing, and an independent security review. See the [production-readiness checklist](docs/production-readiness.md).
