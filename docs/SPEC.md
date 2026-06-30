# Porthook Open-Source Product and Technical Specification

Status: draft  
Audience: contributors and maintainers

## 1. Mission

Porthook is an open-source reverse tunnel product that gives developers and teams a simple way to expose local services to the internet for development, testing, previews, and collaboration.

The product should feel small, reliable, inspectable, and easy to self-host.

This specification describes the public self-hosted product. Private commercial strategy, hosted-service implementation, pricing, internal operations, and non-public roadmap items belong outside this repository.

## 2. Target Users

Primary users:

- Backend and full-stack developers testing webhooks.
- Small teams sharing local preview environments.
- QA and support engineers reproducing issues against local builds.
- Open-source maintainers who want a self-hostable tunnel stack.

Secondary users:

- Platform teams that want an internally controlled alternative to hosted tunnel vendors.
- Agencies that need temporary public URLs for client previews.

## 3. Product Principles

- Open core, not bait-and-switch.
- Self-hosted must be useful without any hosted service.
- The first-run CLI experience should be excellent.
- Secure defaults matter more than feature volume.
- HTTP tunnels must be boringly reliable before adding TCP, UDP, or advanced routing.

## 4. Public Repository Scope

This public repository should include:

- Local agent.
- Gateway server.
- Control-plane API.
- Basic self-hosted dashboard.
- Docker Compose deployment.
- Documentation for wildcard DNS and TLS.
- Public protocol documentation.
- Contributor-facing roadmap.

This public repository should not include:

- Pricing strategy.
- Hosted-service infrastructure plans.
- Private production runbooks.
- Commercial roadmap items.
- Internal abuse-detection details.
- Investor, launch, or sales material.

See [REPOSITORY_BOUNDARY.md](./REPOSITORY_BOUNDARY.md) for the public/private split.

## 5. Licensing Model

Porthook uses a split-license model:

- Server-side components are AGPL-3.0-only.
- The CLI agent and protocol SDK are Apache-2.0.

Server-side components include:

- Gateway.
- Control plane.
- Dashboard.

Apache-2.0 components include:

- CLI agent.
- Shared protocol definitions.
- Client libraries and SDK packages.

This keeps the user-facing agent easy to distribute and integrate while protecting the network-accessible server core.

## 6. MVP Functional Requirements

The first MVP must support:

- One gateway process.
- One agent process.
- HTTP forwarding from a public hostname to a local port.
- Token-based agent authentication.
- Random subdomain assignment.
- Optional requested subdomain.
- Request and response forwarding.
- Basic request logs in the agent.
- Health endpoint for the gateway.
- Docker Compose self-hosting path.

The first MVP should support:

- Graceful tunnel reconnect.
- Per-tunnel connection limit.
- Basic rate limiting per tunnel.
- Structured logs.

The first MVP does not need:

- Raw TCP tunnels.
- UDP tunnels.
- Multi-region routing.
- Custom domains.
- Browser-based replay.
- Persistent request history.
- Organization management.
- Commercial hosting features.

## 7. Core User Flows

### 7.1 Start a Tunnel

```sh
printf '%s' "$PORTHOOK_TOKEN" | porthook login --server https://tunnel.example.com --token-stdin
porthook http 3000
```

Expected output:

```text
Tunnel established
Forwarding: https://a1b2c3.porthook.example -> http://localhost:3000
```

### 7.2 Request a Subdomain

```sh
porthook http 3000 --subdomain demo
```

Expected output:

```text
Tunnel established
Forwarding: https://demo.porthook.example -> http://localhost:3000
```

If the subdomain is unavailable, the agent should show a clear error and suggest a random name.

### 7.3 Use a Self-Hosted Gateway

```sh
porthook login --server https://tunnel.example.com
porthook http 3000 --server https://tunnel.example.com
```

In an interactive terminal, `porthook login` prompts for the token without echoing it. Scripts should pass tokens with `--token-stdin`.

The self-hosted path must not require any private hosted service.

## 8. Architecture

```text
Public client
    |
    v
Gateway HTTP listener
    |
    | hostname lookup
    v
Tunnel registry
    |
    | multiplexed stream
    v
Agent connection
    |
    v
Local service
```

The system has three runtime planes:

- Data plane: request forwarding between gateway and agent.
- Control plane: users, tokens, tunnel registration, limits, and metadata.
- Presentation plane: dashboard and local CLI output.

## 9. Components

### 9.1 Agent

Responsibilities:

- Authenticate with the gateway.
- Register requested tunnels.
- Maintain a persistent outbound connection.
- Forward gateway requests to local services.
- Stream responses back to the gateway.
- Print useful runtime logs.
- Reconnect when the network drops.

Preferred implementation language: Go.

### 9.2 Gateway

Responsibilities:

- Accept public HTTP and HTTPS requests.
- Resolve hostnames to active tunnel sessions.
- Enforce tunnel limits.
- Forward requests to the correct agent stream.
- Return clear errors for inactive or unknown tunnels.
- Expose health and metrics endpoints.

Preferred implementation language: Go.

### 9.3 Control Plane

Responsibilities:

- Issue and validate API tokens.
- Store users and tunnel metadata.
- Track reserved subdomains.
- Enforce account and tunnel limits.

Initial storage: Postgres.

Optional ephemeral state: Redis.

### 9.4 Dashboard

Responsibilities:

- Show active tunnels.
- Manage tokens.
- Show basic request logs.
- Reserve subdomains.
- Manage self-hosted instance settings.

The dashboard is useful, but not required for the first tunnel MVP.

## 10. Tunnel Protocol v0.2

Protocol negotiation now happens in `auth.request` and `auth.ok` to keep backward compatibility safe:

- `auth.request` carries `protocol_version` and `capabilities`.
- `auth.ok` returns gateway protocol capabilities.
- `auth.error` uses `unsupported_protocol` for incompatible clients.

Recommended transport: WebSocket over TLS.

Why WebSocket first:

- Easy to debug.
- Works through most outbound firewalls.
- Simple to terminate at the gateway.
- Good enough for HTTP-only MVP.

Future transports may include HTTP/2 streams, QUIC, or a custom TCP multiplexer.

### 10.1 Connection Lifecycle

1. Agent opens a WebSocket connection to the gateway.
2. Agent sends an authentication message.
3. Gateway validates the token.
4. Agent sends a tunnel registration message.
5. Gateway confirms the public hostname.
6. Gateway forwards public requests over logical streams.
7. Agent forwards each request to the local service.
8. Agent streams the response back.

### 10.2 Message Types

Control messages and stream start/end messages should be JSON.

HTTP body messages use binary WebSocket frames with stream metadata and raw body bytes. JSON body chunk payloads may be accepted for compatibility.

Initial control messages:

- `auth.request`
- `auth.ok`
- `auth.error`
- `tunnel.register`
- `tunnel.registered`
- `tunnel.error`
- `http.request`
- `http.request.start`
- `http.request.body`
- `http.request.end`
- `http.response`
- `http.response.start`
- `http.response.body`
- `http.response.end`
- `http.stream.error`
- `http.stream.cancel`
- `ping`
- `pong`

`auth.request` example:

```json
{
  "type": "auth.request",
  "payload": {
    "token": "secret-token",
    "agent_version": "0.1.0",
    "protocol_version": "0.2",
    "capabilities": [
      "stream_start_end",
      "binary_body_frames",
      "stream_cancel"
    ]
  }
}
```

`auth.ok` example:

```json
{
  "type": "auth.ok",
  "payload": {
    "protocol_version": "0.2",
    "capabilities": [
      "stream_start_end",
      "binary_body_frames",
      "stream_cancel"
    ]
  }
}
```

### 10.3 Stream Identity

Every forwarded request should have a stream ID created by the gateway.

The stream ID must be unique within one agent connection.

The protocol must allow concurrent request streams over one persistent tunnel connection.

The gateway may send `http.stream.cancel` when the public client disconnects, the stream times out, or the gateway no longer needs the response.

### 10.4 Header Handling

The gateway should preserve incoming request headers by default, with these additions:

- `X-Forwarded-For`
- `X-Forwarded-Host`
- `X-Forwarded-Proto`
- `X-Porthook-Tunnel-ID`

Hop-by-hop headers must be stripped according to HTTP rules.

## 11. HTTP Routing

The gateway routes by hostname:

```text
{subdomain}.{root_domain}
```

Examples:

- `a1b2c3.porthook.example`
- `demo.porthook.example`

For self-hosted deployments, the operator configures:

- Root domain.
- Wildcard DNS record.
- TLS certificate.
- Gateway public URL.

Unknown hostnames should return `404`.

Inactive tunnels should return `502` or `503` with a short plain-text error.

## 12. Security Requirements

### 12.1 Agent Authentication

Agents authenticate with a token.

Tokens should be scoped and revocable.

Initial token scope:

- Register HTTP tunnels.

Future token scopes:

- Reserve subdomains.
- Manage domains.
- Read request logs.
- Administer self-hosted instance.

### 12.2 Tunnel Isolation

One tunnel must not be able to read or write traffic from another tunnel.

Gateway routing must bind every public request to exactly one active tunnel session.

### 12.3 Public Access Control

The first MVP may expose tunnels publicly.

The next security milestone should add optional access control before traffic reaches the local service:

- Basic auth.
- Static bearer token.
- OAuth/OIDC later.
- IP allowlist.

### 12.4 Operator Safety

The open-source version should include basic controls for self-hosted operators:

- Request size limits.
- Connection limits.
- Rate limits.
- Clear operator configuration.
- Structured logs for incident review.

Private hosted-service abuse detection and enforcement details should not live in this public repository.

## 13. Observability

Minimum:

- Structured gateway logs.
- Structured agent logs.
- Gateway health endpoint.
- Active tunnel count.
- Request count.
- Error count.
- Prometheus text metrics.

Future:

- OpenTelemetry traces.
- Dashboard charts.

## 14. Data Model Draft

Initial persistent entities:

- User.
- API token.
- Tunnel session.
- Reserved subdomain.

Possible schema sketch:

```text
users
  id
  email
  created_at

api_tokens
  id
  user_id
  name
  token_hash
  scopes
  last_used_at
  revoked_at
  created_at

reserved_subdomains
  id
  owner_user_id
  name
  created_at

tunnel_sessions
  id
  owner_user_id
  subdomain
  target_url
  status
  connected_at
  disconnected_at
```

For the very first self-hosted MVP, this may start with static tokens in configuration before Postgres is introduced.

## 15. API Draft

Initial control-plane endpoints:

```text
GET  /api/v1/tokens
POST /api/v1/tokens
POST /api/v1/tokens/validate
DELETE /api/v1/tokens/{id}
GET  /api/v1/tunnels
GET  /api/v1/tunnels/{id}
POST /api/v1/reserved-subdomains
GET  /healthz
GET  /readyz
GET  /metrics
```

The tunnel transport endpoint:

```text
GET /api/v1/agent/connect
```

This endpoint upgrades to WebSocket.

## 16. CLI Draft

Commands:

```text
porthook login --server <url>
porthook login --server <url> --token <token>
porthook login --server <url> --token-stdin
porthook logout
porthook http <port>
porthook http <port> --subdomain <name>
porthook http <port> --server <url>
porthook tokens create --control-plane <url> --name <name>
porthook tokens list --control-plane <url>
porthook tokens revoke --control-plane <url> <token-id>
porthook tunnels
porthook version
```

Useful flags:

```text
--server
--token
--subdomain
--control-plane
--admin-token
--admin-token-stdin
--json
--host-header
--basic-auth
--log-format
```

## 17. Configuration

Gateway configuration:

```text
PORTHOOK_ROOT_DOMAIN=porthook.example
PORTHOOK_PUBLIC_URL=https://porthook.example
PORTHOOK_TOKEN_SECRET=...
PORTHOOK_CONTROL_PLANE_URL=http://control-plane:8082
PORTHOOK_CONTROL_PLANE_TOKEN=validator-secret
PORTHOOK_CONTROL_PLANE_TIMEOUT=5s
PORTHOOK_CONTROL_ADMIN_TOKEN=...
PORTHOOK_CONTROL_VALIDATOR_TOKEN=...
PORTHOOK_DATABASE_URL=...
PORTHOOK_REDIS_URL=...
PORTHOOK_RATE_LIMIT_RPS=60
PORTHOOK_RATE_LIMIT_BURST=120
```

Agent configuration:

```text
PORTHOOK_SERVER_URL=https://porthook.example
PORTHOOK_TOKEN=...
PORTHOOK_CONFIG_PATH=
```

## 18. Deployment

The first supported self-hosting path should be Docker Compose.

Minimum services:

- Gateway.
- Control plane.
- Postgres.

Optional services:

- Redis.
- Dashboard.
- Caddy or Traefik for TLS.

The deployment docs must explain:

- Wildcard DNS.
- TLS options.
- Token setup.
- Local agent connection.

## 19. Roadmap

### Milestone 0: Repository Foundation

- README.
- Product and technical spec.
- License map.
- Initial repository structure.

### Milestone 1: Tunnel Proof of Concept

- Go agent.
- Go gateway.
- WebSocket connection.
- Single HTTP request forwarding.
- Manual token configuration.

### Milestone 2: MVP

- Concurrent request streams.
- Random subdomains.
- Requested subdomains.
- Structured logs.
- Docker Compose.
- Basic rate limits.

### Milestone 3: Developer Experience

- Polished CLI output.
- Better reconnect behavior.
- Request log view.
- Binary releases.
- Installation docs.

### Milestone 4: Control Plane and Dashboard

- User accounts.
- Token management.
- Active tunnel list.
- Reserved subdomains.
- Basic dashboard.

### Milestone 5: Public Hardening

- Production deployment examples.
- Custom domain documentation.
- Access control.
- Audit-friendly logs.
- Security review checklist.

## 20. Open Questions

- Should the first production transport remain WebSocket or move to HTTP/2 streams quickly?
- Should self-hosted MVP start with static tokens or Postgres-backed tokens?
- Should the dashboard live inside `server/` or become a separate top-level app later?
- What should the final product name be?
