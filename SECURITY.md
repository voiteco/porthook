# Security Policy

## Supported Versions

Porthook is pre-1.0. Security fixes target the latest minor release line and the latest `main` branch on a best-effort basis.

| Version | Supported |
| --- | --- |
| `0.14.x` | Best effort |
| `main` | Best effort |
| `0.13.x` and older | No |

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
