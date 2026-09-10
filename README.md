# MySQL Monitor Connector

A small, outbound-only Linux connector for a hosted MySQL monitoring service. It polls a control plane over HTTPS with mutual TLS, verifies every job with Ed25519, and executes only collectors compiled into the binary against locally configured MySQL targets.

This repository contains the customer-side connector. The service must implement the API in [docs/control-plane-api.md](docs/control-plane-api.md).

## Security properties

- No inbound listener or firewall rule.
- No cloud-supplied database address, credentials, SQL, files, or executable commands.
- MySQL credentials remain on the customer host.
- TCP MySQL targets require a CA and verified TLS server name.
- Signed jobs have tenant/connector binding, expiry, nonce, and a persistent monotonic sequence.
- Fixed query catalogue, prepared values, one statement per operation, and bounded execution/results.
- Dedicated unprivileged systemd service with no Linux capabilities.

This is an MVP, not a complete production release. Before production use, add certificate renewal, a durable encrypted result spool, signed update metadata, integration tests against supported MySQL/MariaDB versions, external security review, and operational key-management procedures.

## Build and test

Requires Go 1.23 or newer.

```sh
make check
make build
make release
```

`make build` produces a native development binary. `make release` produces statically linked Linux `amd64` and `arm64` binaries.

## Install

Copy a release archive to the customer system and verify its published signature and checksum. Do not install through a shell piped from the Internet.

```sh
sudo ./packaging/install.sh ./build/mysql-monitor-connector-linux-amd64
```

Use `mysql-monitor-connector-linux-arm64` instead on a 64-bit ARM Linux host.

Enroll using a single-use token stored in a protected file:

```sh
sudo mysql-monitor-connector enroll \
  --service-url https://connect.example.com \
  --token-file /root/connector-enrollment-token
sudo chown -R root:mysql-monitor /etc/mysql-monitor-connector
sudo chmod -R o-rwx /etc/mysql-monitor-connector
```

Add a TCP target. The password is read from standard input, never an argument:

```sh
sudo sh -c 'stty -echo; mysql-monitor-connector target add \
  --name production \
  --address db01.internal:3306 \
  --user monitor \
  --tls-ca /etc/mysql-monitor-connector/mysql-ca.pem \
  --tls-server-name db01.internal \
  --password-stdin; stty echo'
sudo chown -R root:mysql-monitor /etc/mysql-monitor-connector
```

Test and start it:

```sh
sudo -u mysql-monitor mysql-monitor-connector target test --name production
sudo systemctl enable --now mysql-monitor-connector
```

For a local database, `--network unix --address /run/mysqld/mysqld.sock` is supported.

## Built-in operations

- `collect.server_identity.v1`
- `collect.global_status.v1`
- `collect.global_variables.v1`
- `collect.statement_digests.v1`
- `collect.table_io.v1`
- `collect.lock_waits.v1`

The last three collectors require Performance Schema or the MySQL `sys` schema. Grant only the privileges required by the enabled operations. Do not use a root or application account.
