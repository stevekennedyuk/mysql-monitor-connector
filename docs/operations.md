# Operations, upgrades, and troubleshooting

## Local layout

| Path | Purpose | Expected ownership/mode |
|---|---|---|
| `/usr/local/bin/mysql-monitor-connector` | Connector executable | `root:root`, `0755` |
| `/etc/mysql-monitor-connector/config.json` | Connector identity and runtime limits | `root:mysql-monitor`, `0640` |
| `/etc/mysql-monitor-connector/connector-key.pem` | mTLS private key | `root:mysql-monitor`, `0640` |
| `/etc/mysql-monitor-connector/connector.pem` | mTLS certificate | `root:mysql-monitor`, `0640` |
| `/etc/mysql-monitor-connector/service-ca.pem` | Service CA | `root:mysql-monitor`, `0640` |
| `/etc/mysql-monitor-connector/targets.d/*.json` | Local target addresses and usernames | `root:mysql-monitor`, `0640` |
| `/etc/mysql-monitor-connector/secrets/*.password` | MySQL passwords | `root:mysql-monitor`, `0640` |
| `/etc/mysql-monitor-connector/environment` | Optional non-secret proxy settings | `root:mysql-monitor`, `0640` |
| `/var/lib/mysql-monitor-connector/last-sequence` | Replay-protection state | `mysql-monitor:mysql-monitor`, `0640` |

Do not put `/etc/mysql-monitor-connector` in a source repository, diagnostics bundle, container image, or support ticket.

## Runtime configuration

Enrollment writes `config.json`. Supported controls are:

| Field | Default | Allowed range/purpose |
|---|---:|---|
| `poll_wait_seconds` | 45 | 1–60 seconds |
| `max_result_bytes` | 5 MiB | 1 KiB–20 MiB |
| `max_rows` | 5000 | 1–10000 |
| `max_query_seconds` | 10 | 1–60 seconds |

Lower limits reduce performance and exfiltration risk. Stop the service before editing. Preserve valid JSON, ownership, and mode, then run the service and inspect logs for validation errors.

The local target address, user, TLS CA, and TLS server name cannot be changed by a cloud job. Re-run `target add` locally with the same name to replace a target atomically.

## Routine commands

```sh
sudo systemctl status mysql-monitor-connector
sudo systemctl restart mysql-monitor-connector
sudo journalctl -u mysql-monitor-connector --since today
sudo -u mysql-monitor mysql-monitor-connector target list
sudo -u mysql-monitor mysql-monitor-connector target test --name production
```

Logs contain job IDs, target names, operations, HTTP status codes, and detailed local collector errors. Treat them as customer-confidential operational metadata. The connector deliberately sends only controlled error messages to the cloud.

## Upgrade procedure

The MVP has no automatic updater. Until signed packages and update metadata are implemented:

1. Obtain the new binary and release notes through the official channel.
2. Verify its signature, SHA-256 checksum, architecture, and version.
3. Confirm the release supports the existing configuration and protocol version.
4. Run tests in a staging connector against representative database versions.
5. Stop the service.
6. Back up configuration through the customer's approved encrypted backup process. Do not send it to the vendor.
7. Install the new binary atomically with `install -m 0755`.
8. Start the service and verify polling, a low-impact collector, logs, and portal health.
9. Retain the previous verified binary briefly for operational rollback, unless it has a known security vulnerability.

Do not downgrade across a security boundary or install an older version solely because it is correctly signed. A production updater should use TUF-style version and expiration metadata to prevent rollback and freeze attacks.

## Backup and recovery

The connector can be re-enrolled and targets can be recreated, so prefer rebuilding over restoring an untrusted host. If configuration backups are required:

- encrypt them with customer-controlled keys;
- tightly restrict access and retention;
- exclude journals unless specifically needed;
- never back up the one-time enrollment token; and
- treat restore as cloning sensitive connector and MySQL identity.

Do not run two restored copies with the same connector certificate and sequence state. Re-enrol the replacement and revoke the old identity.

## Troubleshooting

### Enrollment fails

- Verify the service URL begins with `https://` and contains no credentials, query, or fragment.
- Confirm DNS, system time, outbound TCP 443, and proxy settings.
- Confirm the token is unexpired and unused.
- Validate a private CA was obtained through a trusted channel.
- A TLS-inspection proxy may break mTLS; request a pass-through exception instead of disabling validation.

### Service starts but the portal shows offline

- Inspect `journalctl -u mysql-monitor-connector`.
- Verify the systemd service can read all PEM and configuration files.
- Confirm the client certificate has not expired or been revoked.
- Check the proxy environment file and reload systemd after unit changes.
- Ensure the firewall permits the service hostname rather than a stale fixed IP where the service uses changing addresses.

### Target test fails

- Run the test as `mysql-monitor`, not root.
- Confirm internal DNS and TCP 3306 routing.
- Confirm the MySQL account host restriction matches the connector's actual source address.
- Verify the CA file and that `--tls-server-name` matches the certificate SAN.
- Check that MySQL requires and successfully negotiates TLS.
- Confirm secret files are not accessible by users outside the service group.

### A collector fails but connectivity succeeds

- Verify Performance Schema and the `sys` schema exist and are enabled.
- Compare the operation's required objects in [MySQL setup](mysql-setup.md) with `SHOW GRANTS`.
- Check whether the MySQL or MariaDB release exposes different columns or view names.
- Do not solve the problem by granting `ALL` or switching to `root`.
- Disable the incompatible operation until a version-specific collector is reviewed and shipped.

### Repeated signature or sequence failures

Treat unexplained signature failures as a security signal. Check time synchronization, connector identity, job-signing key rotation, duplicate/restored connector instances, and control-plane audit logs. Do not delete `last-sequence` merely to make jobs run; doing so weakens replay protection. Re-enrol through the documented recovery process if state is irreconcilable.

## Decommissioning

1. Stop and disable the connector service.
2. Revoke the connector identity in the control plane.
3. Lock or drop each associated MySQL account.
4. Preserve only audit records required by policy.
5. Remove connector configuration, keys, passwords, state, binary, and service definition using the customer's approved secure-deletion process.
6. Remove obsolete firewall and proxy exceptions.

Secure deletion semantics depend on the filesystem and storage platform; deletion or `shred` does not necessarily erase data from snapshots, SSDs, journals, or backups.
