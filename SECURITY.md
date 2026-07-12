# Security Policy

## Supported Versions

Currently (pre-`1.0.0`): security fixes target the latest minor release line and the latest `main` branch on a best-effort basis.

| Version | Supported |
| --- | --- |
| `0.18.x` | Best effort |
| `main` | Best effort |
| `0.17.x` and older | No |

### Once `v1.0.0` Ships

- The latest `1.x` minor release receives full support, including security fixes.
- The previous `1.x` minor release receives security fixes only, for 90 days after the next minor release ships.
- Upgrading across `1.x` minor releases never requires a database migration to roll back or a code change to adopt (see [docs/COMPATIBILITY.md](./docs/COMPATIBILITY.md)), so staying on the latest `1.x` minor release is the recommended and lowest-effort way to stay supported.
- A `2.0` release, if one is ever needed, is out of scope for this policy and would define its own supported-version table.

## Deprecation Policy

Within `1.x`, nothing documented in [docs/COMPATIBILITY.md](./docs/COMPATIBILITY.md) as guaranteed is removed or silently changed. If a capability, environment variable, API field, or configuration default ever needs to be deprecated in favor of a replacement:

1. The replacement ships first, alongside the deprecated form, with both working.
2. The deprecation is recorded in `CHANGELOG.md` under a `### Deprecated` heading, naming the removal target (never sooner than the next major version) and the migration step.
3. `configcheck` and relevant CLI/log output warn when a deprecated form is in use, naming the replacement.
4. Removal happens only at a major version boundary, never within `1.x`.

## Migration Policy

Per-version upgrade steps, required actions, and rollback procedures are documented in [docs/UPGRADING.md](./UPGRADING.md) for every release so far; each new release adds its own section there. [docs/COMPATIBILITY.md](./docs/COMPATIBILITY.md) states what's guaranteed to keep working without any migration step at all. Database migrations are additive-only within `1.x` (see [docs/RELIABILITY.md](./docs/RELIABILITY.md)'s Migration Serialization section), so downgrading code without reverting the database is supported.

## Reporting a Vulnerability

Do not open a public GitHub issue for a suspected vulnerability.

Use GitHub private vulnerability reporting for this repository when available. If that is not available, contact the maintainers privately through the repository owner's published contact channel before sharing details publicly.

Include as much of the following as you can:

- Affected component or path, such as `agent/`, `protocol/`, `server/gateway/`, `server/control-plane/`, `server/dashboard/`, or `deploy/`.
- Affected commit, branch, version, or deployment configuration.
- Vulnerability class and expected impact.
- Reproduction steps or a minimal proof of concept.
- Relevant logs, with tokens, credentials, hostnames, IP addresses, and personal data removed.
- Whether the issue is already public or known to third parties.

## Scope

Security-sensitive areas include:

- tunnel authentication and session handling
- token generation, storage, validation, and logging
- hostname and subdomain routing
- request forwarding between gateway and agent
- WebSocket or other tunnel transport behavior
- control-plane and dashboard authentication or authorization
- TLS, reverse proxy, and deployment defaults
- handling of secrets, logs, headers, and request metadata

See [docs/THREAT_MODEL.md](./docs/THREAT_MODEL.md) for the supported v1 trust boundaries, attacker model, security requirements, and accepted risks.

## Disclosure Expectations

Please give maintainers time to investigate and fix confirmed issues before public disclosure. Do not access, modify, or exfiltrate data that is not yours, and do not perform destructive testing against systems you do not control.

We aim to acknowledge valid private reports within 7 days and provide status updates as the issue is triaged.

## Security-Fix Release Policy

A confirmed security fix is released as a patch on every currently supported line (see Supported Versions above): the latest minor release and, once `v1.0.0` ships, the previous minor release if it's still within its 90-day security-fix window. The GitHub Release notes and `CHANGELOG.md` entry for a security patch describe the impact and affected versions without a working exploit; a GitHub Security Advisory is published for the reporter's credit and a CVE requested when the finding warrants one. Reporters are credited unless they ask not to be.
