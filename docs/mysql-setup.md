# Preparing MySQL securely

Use a dedicated monitoring account for the connector. Never configure a MySQL `root`, application, replication-applier, backup, or schema-owner account.

## Requirements

- Enable MySQL TLS and prefer `require_secure_transport=ON`.
- Use a server certificate with the database DNS name in its Subject Alternative Name.
- Restrict the MySQL account to the connector host address, not `%`.
- Use a generated, unique password and the server's current recommended authentication plugin.
- Set a low connection limit; the connector itself opens at most two connections per job.
- Grant only the schemas needed by enabled collectors.

The connector requires verified TLS for every TCP target. It will not connect with `PREFERRED`, an unverified certificate, or plaintext fallback.

## Example MySQL 8 account

Substitute the connector's internal source address and generate the password using the customer's approved password manager:

```sql
CREATE USER 'mysql_monitor'@'10.20.30.40'
  IDENTIFIED BY 'replace-with-a-generated-secret'
  REQUIRE SSL
  WITH MAX_USER_CONNECTIONS 2;

GRANT SELECT ON performance_schema.*
  TO 'mysql_monitor'@'10.20.30.40';

GRANT SELECT ON sys.*
  TO 'mysql_monitor'@'10.20.30.40';

SHOW GRANTS FOR 'mysql_monitor'@'10.20.30.40';
```

Do not put the real password in a checked-in SQL file. Run account creation through a protected DBA session and then supply the password directly to `target add --password-stdin`.

The schema-level `SELECT` grants above are a portable starting point for the implemented collector catalogue. A customer with stricter requirements can grant only the referenced views and tables after testing against its exact MySQL release. The `sys` schema can require access to underlying Performance Schema objects. Some lock visibility may require additional privileges on particular MySQL versions; do not add `PROCESS` automatically, because it can expose other sessions' query text.

## Collector access

| Operation | Objects used | Sensitivity |
|---|---|---|
| `collect.server_identity.v1` | Server version and selected system variables | Hostname and topology metadata |
| `collect.global_status.v1` | `SHOW GLOBAL STATUS` | Aggregate workload counters |
| `collect.global_variables.v1` | `performance_schema.global_variables` | Configuration, paths, and topology metadata |
| `collect.statement_digests.v1` | `performance_schema.events_statements_summary_by_digest` | Normalized statement text and schema names |
| `collect.table_io.v1` | `sys.schema_table_statistics` | Schema/table names and access patterns |
| `collect.lock_waits.v1` | `sys.innodb_lock_waits` | Session identifiers and locked object names |

Statement digests normally replace literals with markers, but object names and SQL structure can still be confidential. Enable and retain this data only with customer approval.

## Verify TLS

Before adding the connector target, test with a MySQL client that performs CA and hostname verification:

```sh
mysql \
  --host=db01.internal \
  --port=3306 \
  --user=mysql_monitor \
  --ssl-mode=VERIFY_IDENTITY \
  --ssl-ca=/etc/mysql-monitor-connector/mysql-ca.pem
```

Inside the session, verify TLS and grants:

```sql
SHOW STATUS LIKE 'Ssl_cipher';
SHOW GRANTS;
```

An empty `Ssl_cipher` indicates that TLS is not active. Do not proceed with a TCP target in that condition.

## Reduce monitoring overhead

Performance Schema is intended for monitoring, but enabling every instrument and long-history consumer can add overhead. Enable only the instruments required by the product, benchmark on a representative workload, use conservative collection intervals, and enforce the connector's time, row, byte, and concurrency limits.

## Password rotation

1. Create or set the new secret in MySQL through an approved DBA channel.
2. Stop the connector if the old and new passwords cannot overlap.
3. Re-run `target add` with the same target name and the new password; each local target and secret file is replaced atomically.
4. Restore `root:mysql-monitor` ownership and remove permissions for other users.
5. Run `target test` as `mysql-monitor`.
6. Restart the service and revoke the old credential if a staged account was used.

## Revocation

If the connector host is compromised, immediately lock or drop its MySQL account, revoke the connector identity in the control plane, preserve relevant logs, and replace the connector host and all credentials. Rotating only the cloud certificate is insufficient because the MySQL password is stored locally.
