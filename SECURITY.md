# Security policy

## Reporting a vulnerability

Do not disclose a suspected vulnerability in a public GitHub issue, discussion, pull request, or social-media post.

Repository owners should enable GitHub private vulnerability reporting under **Settings → Security → Code security and analysis**. Once enabled, use the repository's **Security → Advisories → Report a vulnerability** workflow to report issues privately.

Include:

- the affected commit or version;
- connector and operating-system configuration relevant to the issue;
- reproducible steps or a minimal proof of concept;
- expected and actual security impact;
- whether credentials or customer data may have been exposed; and
- a safe way to contact the reporter.

Do not include real customer credentials, private keys, enrollment tokens, database contents, or identifiable query samples. Use generated test data.

## Supported versions

There is not yet a production-supported release. The current `main` branch is an MVP under active development. Repository owners must publish a supported-version table and security-fix policy before production use.

## Coordinated disclosure

The maintainers should acknowledge reports privately, establish an impact assessment and remediation timeline, prepare patched signed artifacts, coordinate customer notification where necessary, and publish an advisory after affected users have had a reasonable opportunity to update.
