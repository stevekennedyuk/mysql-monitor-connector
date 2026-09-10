# Installation and enrollment

This guide is for the Linux administrator installing the connector at a customer site.

## 1. Choose a safe host

Prefer a dedicated internal management host or small VM. The connector may run on a web server, but a public-facing web server has a larger attack surface. A compromise of the connector host can expose the locally stored MySQL credential.

The host should:

- run a maintained, systemd-based Linux distribution;
- be dedicated to administration or monitoring where practical;
- have accurate time from NTP, because signed jobs and certificates expire;
- reach the monitoring service on outbound TCP 443;
- reach only the intended MySQL hosts on their internal port, normally TCP 3306; and
- not accept inbound connections for the connector.

## 2. Firewall rules

| Source | Destination | Port | Direction | Purpose |
|---|---|---:|---|---|
| Connector | Monitoring service hostname | TCP 443 | Outbound | Enrollment, job polling, and results |
| Connector | Explicit MySQL target addresses | TCP 3306 or configured port | Internal outbound | Monitoring queries |
| Internet or control plane | Connector | Any | Inbound | Not required; deny |

Restrict rules by destination where the firewall supports it. Do not expose MySQL or add an inbound port-forward to the connector.

If an HTTPS proxy is required, the connector honours `HTTPS_PROXY` and `NO_PROXY`. Create `/etc/mysql-monitor-connector/environment`:

```text
HTTPS_PROXY=http://proxy.customer.example:8080
NO_PROXY=localhost,127.0.0.1
```

Set ownership to `root:mysql-monitor` and mode `0640`. The supplied systemd unit loads this optional file. Do not place proxy passwords in it unless the customer accepts that local secret-storage model. Mutual TLS normally requires an HTTP `CONNECT` tunnel. The connector deliberately fails if a TLS-inspection proxy substitutes an untrusted certificate; do not bypass certificate validation.

## 3. Obtain and verify the binary

Official releases should publish:

- Linux `amd64` and `arm64` binaries;
- SHA-256 checksums;
- detached signatures or TUF metadata; and
- release notes identifying security fixes and supported upgrade paths.

Until signed releases exist, build from a reviewed source commit:

```sh
make check
make release
sha256sum build/mysql-monitor-connector-linux-amd64
sha256sum build/mysql-monitor-connector-linux-arm64
```

Do not install using a shell script piped directly from the Internet.

## 4. Install the service

Run from the repository root, choosing the correct architecture:

```sh
sudo ./packaging/install.sh ./build/mysql-monitor-connector-linux-amd64
```

Use `mysql-monitor-connector-linux-arm64` on 64-bit ARM Linux.

The installer creates:

- `/usr/local/bin/mysql-monitor-connector`;
- the unprivileged `mysql-monitor` account and group;
- `/etc/mysql-monitor-connector` for identity, targets, and credentials;
- `/var/lib/mysql-monitor-connector` for replay-protection state; and
- `/etc/systemd/system/mysql-monitor-connector.service`.

It does not start the service before enrollment and target configuration are complete.

## 5. Enrol the connector

In the service portal, create a connector and obtain a single-use, short-lived enrollment token. Save it without a trailing explanation or other text:

```sh
sudo install -m 0600 /path/to/downloaded-token /root/connector-enrollment-token
sudo mysql-monitor-connector enroll \
  --service-url https://connect.example.com \
  --token-file /root/connector-enrollment-token
```

For a service using a private HTTPS CA, add `--ca-file /path/to/service-ca.pem`. Do not use this option to trust a certificate obtained from an unverified source.

Enrollment generates the connector private key locally, submits only a certificate signing request, validates the returned client certificate, and records the control plane's Ed25519 job-signing public key. The private key must never be uploaded to the service.

After enrollment:

```sh
sudo chown -R root:mysql-monitor /etc/mysql-monitor-connector
sudo find /etc/mysql-monitor-connector -type d -exec chmod 0750 {} \;
sudo find /etc/mysql-monitor-connector -type f -exec chmod 0640 {} \;
```

Securely remove the enrollment-token file. The server must also have atomically invalidated the token, so copying it should not permit another enrollment.

## 6. Install the MySQL CA

Obtain the CA certificate through a trusted customer-administrator channel. It must be the CA that issued the MySQL server certificate, not a certificate downloaded opportunistically from the database connection.

```sh
sudo install -m 0640 -o root -g mysql-monitor mysql-ca.pem \
  /etc/mysql-monitor-connector/mysql-ca.pem
```

The TLS server name supplied in the next step must match a DNS name in the MySQL server certificate's Subject Alternative Name extension.

## 7. Add a target

Create the dedicated account first using [MySQL setup](mysql-setup.md). Then add the target. This command disables terminal echo and restores it even if interrupted:

```sh
sudo sh -c '
  trap "stty echo" EXIT INT TERM
  stty -echo
  printf "MySQL monitoring password: " >&2
  mysql-monitor-connector target add \
    --name production \
    --address db01.internal:3306 \
    --user mysql_monitor \
    --tls-ca /etc/mysql-monitor-connector/mysql-ca.pem \
    --tls-server-name db01.internal \
    --password-stdin
  printf "\n" >&2
'
sudo chown -R root:mysql-monitor /etc/mysql-monitor-connector
sudo chmod -R o-rwx /etc/mysql-monitor-connector
```

Target names may contain letters, digits, `.`, `_`, and `-`; they must start with a letter or digit. The name is the `target_id` used by control-plane jobs.

For MySQL on the same host, a Unix socket can be used instead of TCP TLS:

```sh
sudo sh -c '
  trap "stty echo" EXIT INT TERM
  stty -echo
  mysql-monitor-connector target add \
    --name local \
    --network unix \
    --address /run/mysqld/mysqld.sock \
    --user mysql_monitor \
    --password-stdin
'
sudo chown -R root:mysql-monitor /etc/mysql-monitor-connector
sudo chmod -R o-rwx /etc/mysql-monitor-connector
```

Do not place passwords on the command line, in shell history, in a target JSON file, or in the service environment.

## 8. Test and start

Run the connectivity test as the service user so it exercises the actual filesystem permissions:

```sh
sudo -u mysql-monitor /usr/local/bin/mysql-monitor-connector target test \
  --name production
sudo systemctl enable --now mysql-monitor-connector
sudo systemctl status mysql-monitor-connector
sudo journalctl -u mysql-monitor-connector --since today
```

The logs should show `connector started` and must not contain the MySQL password. Confirm in the service portal that the expected connector identity is online.

## 9. Post-install checks

- Confirm there is no connector listening socket with `ss -lntp`.
- Confirm outbound traffic is limited to the service and configured MySQL targets.
- Run `systemd-analyze security mysql-monitor-connector.service` and review distribution-specific warnings.
- Confirm `/etc/mysql-monitor-connector` is not included in broadly accessible backups.
- Record the connector owner, host, target scope, certificate expiry, and credential-rotation date.
- Configure alerts for unexpected disconnects, repeated signature failures, and certificate-expiry risk.
