# Threat Model

Porthook is a self-hosted reverse-tunnel system. This document records the v1 security model, the boundaries that must be preserved, and the risks intentionally accepted for the supported single-node deployment.

## Scope

The supported deployment contains a public gateway, an agent-facing WebSocket endpoint, a control plane, a dashboard, a Postgres database, and an operator-managed reverse proxy. It supports HTTP, HTTPS supplied by the reverse proxy, streaming responses, WebSocket traffic, wildcard subdomains, reserved subdomains, access policies, and configured custom domains.

This model does not cover hosted-service tenancy, organization management, payment systems, multi-region routing, raw TCP or UDP tunnels, or multi-gateway high availability.

## Assets

- Agent, admin, validator, and service credentials.
- Tunnel ownership, reserved-subdomain ownership, and access-policy secrets.
- Public request and response data while it is being forwarded.
- Operator-only audit history, request history, metrics, runtime diagnostics, and backups.
- Custom-domain verification tokens and domain mappings.
- Postgres data, migration history, and retention configuration.
- Availability of the gateway, control plane, tunnel connections, and public routes.

## Trust Boundaries

| Boundary | Trusted side | Untrusted side | Required control |
| --- | --- | --- | --- |
| Internet to reverse proxy | Operator proxy and certificate configuration | Public clients | TLS termination, host routing, request size and timeout limits. |
| Public listener to tunnel | Gateway routing and policy evaluation | Request paths, headers, bodies, and hosts | Transparent application forwarding after route selection; no operational metadata on tunnel hosts. |
| Agent connection | Authenticated agent and gateway protocol implementation | Internet clients and invalid agents | Static or control-plane token validation, protocol negotiation, bounded frames and streams. |
| Control plane | Scoped administrator, validator service credential, and Postgres | Browser clients, agents, and public requests | Endpoint-specific authorization, strict input handling, audit records, and private service access. |
| Reverse proxy client identity | Explicitly configured proxy peers | Direct clients and forged forwarded headers | Trusted-proxy allowlist and consistent client-IP resolution. |
| Operator workstation and backups | Operator-controlled systems | Public repository and public network | No credentials in repository, checksum verification, access control, and encrypted storage chosen by the operator. |

## Attacker Capabilities

An attacker may:

- Send arbitrary HTTP, WebSocket, and malformed protocol input to public and agent-facing endpoints.
- Attempt token guessing, token replay, scope escalation, hostname takeover, request smuggling, and header spoofing.
- Open slow or abandoned connections, generate large bodies, and cause reconnect churn.
- Register lookalike or invalid subdomains, probe custom-domain verification, and send traffic to configured domain names.
- Read public repository history and release artifacts.
- Obtain a low-privilege agent token or scoped admin token and attempt to access unrelated functions.

The attacker cannot modify the operator's reverse-proxy configuration, control-plane database, release signing identity, or private operator systems unless a separate operator compromise occurs.

## Security Requirements

### Authentication And Authorization

- Agent and service credentials are compared without exposing plaintext values in logs, API summaries, traces, exports, or metrics.
- Administrative APIs require scoped admin credentials. Bootstrap credentials are recovery-only operational secrets.
- User-selected access-policy passwords require slow, versioned password hashes. High-entropy generated tokens may use one-way lookup hashes.
- Authentication failures are rate-limited without blocking normal public tunnel traffic.

### Public Traffic And Management Data

- Wildcard tunnel hosts must forward application paths without reserving health, metrics, or API routes.
- Health, readiness, metrics, tunnel inventory, diagnostics, and request history belong on a private management surface.
- The control plane mediates operator access to gateway management data with a dedicated service credential and scoped admin authorization.

### Client Identity

- Only configured reverse proxies may supply forwarded client-address information.
- Direct clients cannot select an allowlist identity with `X-Forwarded-For` or related headers.
- The selected client address is used consistently for policy evaluation, logs, audit events, traces, and rate-limit keys.

### Protocol And Resource Bounds

- Protocol versions negotiate explicit capabilities and reject unsupported combinations with actionable errors.
- WebSocket and streaming traffic has frame-size, buffering, concurrency, request, idle, and lifetime limits.
- Cancellation and disconnects release stream resources promptly.
- Unsafe hop-by-hop HTTP state is not forwarded through the tunnel.

### TLS And Domains

- The operator-managed reverse proxy terminates TLS and preserves the original host, request ID, and trusted client identity required by the gateway.
- Custom domains require explicit operator configuration, DNS verification, and certificate coverage before routing traffic.
- Certificates and private keys are never committed to the repository or bundled into release artifacts.

## Abuse Cases And Mitigations

| Abuse case | Mitigation | Follow-up block |
| --- | --- | --- |
| A tunnel host probes `/metrics` or `/api/v1/*` for operational data. | Isolate gateway management listener and mediate access through the control plane. | Block 3 |
| A direct client forges `X-Forwarded-For` to bypass an IP allowlist. | Trust only configured proxies and select the first untrusted address in the chain. | Block 4 |
| A leaked or weak user password is recovered from a fast hash. | Use Argon2id or bcrypt with versioned verification and lazy legacy upgrades. | Block 4 |
| A slow client or malformed frame consumes unbounded memory or goroutines. | Bound frames, buffers, streams, deadlines, and cancellation paths. | Block 5 |
| A mixed-version agent silently misinterprets a streamed or WebSocket message. | Capability negotiation, compatibility tests, and explicit version failures. | Block 5 |
| A proxy routes a custom domain without the intended certificate or host preservation. | Test a real Caddy TLS edge and document domain, certificate, and rollback procedures. | Block 6 |
| A dependency, source change, or secret reaches the repository unnoticed. | Required race, fuzz-seed, vulnerability, CodeQL, dependency-update, and secret-scan checks. | Block 2 |

## Accepted v1 Risks

- A single gateway and control plane remain availability bottlenecks; v1 does not provide high availability or cross-region failover.
- The operator is responsible for network isolation of private management endpoints, reverse-proxy configuration, TLS issuance, DNS, host firewall rules, database encryption, and backup retention.
- Static tokens are supported for self-hosted bootstrap and agent authentication; operators must use generated high-entropy values, rotation, and least-privilege scopes.
- Public tunnel users can expose an unsafe local service. Porthook controls tunnel authorization and transport boundaries, but cannot make the local application secure.
- Denial-of-service resistance is bounded by configured limits and the capacity of a single operator deployment; Blocks 5 and 8 define the enforced limits and measured envelope.
- DNS and certificate issuance are external dependencies. A valid custom-domain mapping does not guarantee that external DNS, certificates, or network routes are safe or available.

## Review Triggers

Update this model when a public protocol capability, authentication method, management endpoint, client-identity rule, custom-domain flow, release artifact, or supported deployment topology changes. Findings that invalidate a stated requirement block the relevant release until resolved or explicitly deferred outside the supported v1 scope.
