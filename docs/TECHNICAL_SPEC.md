# Porthook Technical Specification

Status: draft  
Audience: contributors and maintainers  
Repository: public `porthook`

## 1. Scope

This document describes the technical design for the public self-hosted Porthook product.

The first implementation target is an HTTP reverse tunnel MVP built from:

- A Go CLI agent.
- A Go gateway server.
- A public tunnel protocol.
- Docker Compose deployment.
- Control-plane token management, with dashboard components after the tunnel core works.

Private hosted-service implementation details belong outside this repository.

## 2. Technical Goals

- Expose a local HTTP service through a public gateway.
- Keep the agent behind NAT and firewalls by using outbound connections only.
- Route public hostnames to active tunnel sessions.
- Support concurrent HTTP requests over one persistent tunnel connection.
- Make the protocol inspectable and stable enough for future SDKs.
- Keep the first deployment path easy to run on one VPS.

## 3. Non-Goals for MVP

- Raw TCP tunnels.
- UDP tunnels.
- Multi-region routing.
- Persistent request history.
- Organization management.
- Hosted-service-only features.

## 4. Runtime Architecture

```text
Public HTTP client
    |
    v
Gateway HTTP listener
    |
    | host -> tunnel session
    v
Gateway tunnel multiplexer
    |
    | WebSocket transport
    v
Agent tunnel multiplexer
    |
    | local HTTP request
    v
Local service
```

The MVP has three required binaries:

- `porthook-agent`: local CLI agent.
- `porthook-gateway`: public gateway server.
- `porthook-control-plane`: token management API for self-hosted deployments.

The final user-facing CLI command should be named `porthook`.

## 5. Repository Structure

Planned structure:

```text
agent/
  cmd/porthook/
  internal/agent/
  internal/config/

protocol/
  messages/
  transport/
  stream/

server/
  gateway/
    cmd/porthook-gateway/
    internal/gateway/
    internal/registry/
    internal/limits/
  control-plane/
  dashboard/

deploy/
  compose/
  examples/

docs/
```

The repository may start with one Go module at the root for speed. If package boundaries become noisy, `agent`, `protocol`, and `server` can become separate modules later.

## 6. Transport Choice

Initial transport: WebSocket over TLS.

Reasons:

- Works through common outbound network restrictions.
- Easy to debug with standard tools.
- Supported by Go's ecosystem.
- Good enough for HTTP-only MVP.

Future candidates:

- HTTP/2 streams.
- QUIC.
- A TCP multiplexer such as yamux or smux.

The protocol package should avoid assuming WebSocket forever.

## 7. Protocol Overview

The protocol has two layers:

- Control messages for authentication, tunnel registration, and lifecycle.
- Stream messages for HTTP request and response forwarding.

Control messages and stream start/end messages are JSON.

HTTP request and response bodies use binary WebSocket frames tagged as `http.request.body` and `http.response.body`. The gateway and agent still understand the original whole-body `http.request` and `http.response` messages, plus JSON body chunk payloads, for compatibility. The public forwarding path uses `start/body/end` frames with binary body chunks.

Protocol negotiation is handled in the auth handshake:

- `auth.request` includes `protocol_version` and `capabilities`.
- `auth.ok` returns the gateway protocol version and capability list.
- The gateway rejects agents with missing/invalid protocol information as `unsupported_protocol`.
- The agent rejects gateways that do not report compatible protocol version/capabilities.

Required capabilities for stream-aware HTTP:

- `stream_start_end`
- `binary_body_frames`
- `stream_cancel`

Every forwarded HTTP request is assigned a stream ID created by the gateway.

## 8. Message Envelope

Draft JSON envelope:

```json
{
  "type": "http.request",
  "stream_id": "01J00000000000000000000000",
  "tunnel_id": "tun_123",
  "payload": {}
}
```

Required fields:

- `type`: message type.
- `stream_id`: required for stream messages.
- `tunnel_id`: required after registration.
- `payload`: message-specific object.

## 9. Control Messages

Initial message types:

```text
auth.request
auth.ok
auth.error
tunnel.register
tunnel.registered
tunnel.error
ping
pong
```

### 9.1 auth.request

Sent by agent after opening the WebSocket.

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

### 9.2 tunnel.register

Sent by agent after authentication.

```json
{
  "type": "tunnel.register",
  "payload": {
    "protocol": "http",
    "local_target": "http://localhost:3000",
    "requested_subdomain": "demo"
  }
}
```

### 9.3 tunnel.registered

Sent by gateway when the tunnel is active.

```json
{
  "type": "tunnel.registered",
  "payload": {
    "tunnel_id": "tun_123",
    "public_url": "https://demo.porthook.example",
    "subdomain": "demo"
  }
}
```

### 9.4 auth.ok

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

### 9.5 auth.error

```json
{
  "type": "auth.error",
  "payload": {
    "code": "unsupported_protocol",
    "message": "agent protocol version \"0.1\" is not supported, expected \"0.2\""
  }
}
```

## 10. Stream Messages

Initial HTTP stream message types:

```text
http.request
http.request.start
http.request.body
http.request.end
http.response
http.response.start
http.response.body
http.response.end
http.stream.error
http.stream.cancel
```

### 10.1 Request Start Payload

```json
{
  "method": "POST",
  "path": "/webhook",
  "query": "source=test",
  "headers": {
    "content-type": ["application/json"]
  },
  "content_length": 123
}
```

### 10.2 Body Chunk Frames

Used by `http.request.body` and `http.response.body`.

The primary transport is a binary WebSocket frame with Porthook body-frame metadata followed by raw body bytes. Implementations may also accept the JSON compatibility payload:

```json
{
  "data": "base64-encoded body chunk in JSON"
}
```

`http.request.end` and `http.response.end` use an empty payload.

### 10.3 Response Start Payload

```json
{
  "status": 200,
  "headers": {
    "content-type": ["application/json"]
  }
}
```

### 10.4 Stream Cancel Payload

Sent by the gateway when a public request no longer needs a local response.

```json
{
  "reason": "gateway stream timeout"
}
```

The agent should cancel the matching local HTTP request context and avoid sending a late response for that stream.

## 11. Gateway Design

Gateway responsibilities:

- Listen for public HTTP requests.
- Listen for agent WebSocket connections.
- Authenticate agents.
- Register active tunnel sessions.
- Route hostnames to tunnels.
- Multiplex concurrent streams over agent connections.
- Enforce request size, timeout, and concurrency limits.
- Send stream cancellation messages when a public request times out or is canceled.
- Return clear errors for inactive tunnels.
- Expose health endpoints.

### 11.1 HTTP Listener

Routing rule:

```text
{subdomain}.{root_domain}
```

If the hostname matches `{subdomain}.{root_domain}`, route directly to that subdomain.

If the hostname does not match the root domain and the gateway is connected to a control plane, validate it as a custom domain hostname and resolve active custom-domain mappings through `POST /api/v1/custom-domains/lookup`. Pending or failed custom-domain verification records are treated as unroutable.

If no route exists for the hostname, return `404`.

If the tunnel exists but is disconnected, return `503`.

The current gateway supports custom root domains through `PORTHOOK_ROOT_DOMAIN` and per-tunnel custom domains through control-plane mappings to reserved subdomains. Access policies are evaluated against the resolved reserved subdomain.

### 11.2 Tunnel Registry

The MVP can use an in-memory registry:

```text
subdomain -> tunnel session
tunnel_id -> tunnel session
connection_id -> agent connection
```

This means one gateway instance only.

Persistent state can be added later through Postgres and Redis.

### 11.3 Limits

MVP limits:

- Maximum request body size.
- Maximum response body size.
- Maximum concurrent streams per tunnel.
- Stream idle timeout.
- Agent connection idle timeout.

## 12. Agent Design

Agent responsibilities:

- Parse CLI flags and environment variables.
- Authenticate to the gateway.
- Register a local tunnel.
- Maintain the WebSocket connection.
- Forward gateway requests to the local target.
- Stream local responses back to the gateway.
- Print tunnel URL and request logs.
- Reconnect with backoff after transient failures.

### 12.1 CLI Commands

MVP commands:

```text
porthook login --server <url>
porthook login --server <url> --token <token>
porthook login --server <url> --token-stdin
porthook logout
porthook http <port>
porthook http <port> --server <url>
porthook http <port> --token <token>
porthook http <port> --subdomain <name>
porthook tokens create --control-plane <url> --name <name>
porthook tokens list --control-plane <url>
porthook tokens revoke --control-plane <url> <token-id>
porthook reserved create --control-plane <url> --name <name> --token-id <token-id>
porthook reserved list --control-plane <url>
porthook reserved delete --control-plane <url> <id-or-name>
porthook access create --control-plane <url> --reserved-subdomain-id <id> --mode <mode>
porthook access list --control-plane <url>
porthook access update --control-plane <url> <policy-id> --mode <mode>
porthook access delete --control-plane <url> <policy-id>
porthook tunnels list --control-plane <url>
porthook tunnels show --control-plane <url> <tunnel-id>
porthook history events --control-plane <url>
porthook history requests --control-plane <url>
porthook export --control-plane <url>
porthook version
```

`porthook tokens list` prints token summaries only. Summaries include creation time, last successful validation time when available, and revocation time when revoked.

`porthook tunnels list` and `porthook tunnels show` read active tunnel summaries through authenticated control-plane operator APIs. They require an admin token with `runtime_diagnostics` and do not expose local target URLs.

`porthook history events` and `porthook history requests` read paginated operational history from `GET /api/v1/events` and `GET /api/v1/gateway/request-logs`. They support the corresponding endpoint filters, `--cursor`, `--limit`, and `--json`; both require an admin token with `audit_history`.

`porthook export` writes a best-effort operational JSON snapshot for self-hosted debugging. Schema version 3 uses only authenticated control-plane APIs and includes safe summaries, diagnostics, audit events, active gateway tunnels and tunnel details, gateway runtime, metrics, request logs, and cursor/filter metadata. It records partial endpoint failures in an `errors` array and does not include plaintext agent tokens, policy secrets, or local target URLs.

Postgres-backed control-plane token, reservation, access policy, custom domain, audit event, and gateway request-log storage uses embedded versioned SQL migrations. The control plane and gateway apply their pending migrations at startup and record applied versions in `schema_migrations`.

The control plane exposes `GET /api/v1/status` for dashboard and automation checks. It returns JSON with readiness state and the binary version.

The control plane exposes `GET /api/v1/events` for dashboard audit visibility. The endpoint returns recent audit events, newest first, and omits plaintext tokens and access policy secrets. It accepts `limit`, `event`, `level`, `request_id`, `remote_ip`, `field`, `since`, `until`, and `cursor` query parameters; time filters use RFC3339 timestamps. Responses include `events`, echoed `filters`, and `next_cursor` when another page is available. Postgres-backed deployments store audit events durably; development deployments without a database use an in-memory ring buffer.

The dashboard and `porthook doctor` use same-origin control-plane operator APIs for gateway health, readiness, tunnel inventory, runtime, metrics, and request logs. Gateway runtime routes require `runtime_diagnostics`; request logs require `audit_history`.

The private gateway management listener exposes `GET /api/v1/tunnels`; the control plane proxies it as `GET /api/v1/gateway/tunnels` after scope authorization. The endpoint returns active tunnel summaries and omits local target URLs.

The private gateway management listener exposes `GET /api/v1/runtime`; the control plane proxies it as `GET /api/v1/gateway/runtime`. The endpoint returns safe uptime, request-log buffer usage, stream totals, selected limits/timeouts, and counters without local target URLs, tokens, or control-plane URLs.

The dashboard reads gateway Prometheus metrics through `GET /api/v1/gateway/metrics` on the control plane.

The private gateway management listener exposes `GET /api/v1/request-logs`; the control plane proxies it as `GET /api/v1/gateway/request-logs` after `audit_history` authorization. The endpoint returns public request summaries, newest first. Without `PORTHOOK_REQUEST_LOG_DATABASE_URL`, it reads from an in-memory ring buffer. With durable storage, it supports the same filters and pagination. Entries include path and `query_present`, but never raw query strings.

When the gateway is configured with `PORTHOOK_CONTROL_PLANE_URL`, requested subdomains are authorized through `POST /api/v1/reserved-subdomains/authorize`. Random subdomains do not require reservations.

When the gateway is configured with `PORTHOOK_CONTROL_PLANE_URL`, reserved subdomains can also be protected by access policies evaluated through `POST /api/v1/access-policies/evaluate`. Supported modes are `public`, `basic_auth`, `bearer_token`, and `ip_allowlist`.

Future commands:

```text
porthook tunnels
```

### 12.2 Local Forwarding

For each gateway request stream, the agent creates a local HTTP request to:

```text
http://localhost:{port}
```

The agent should preserve method, path, query, headers, and body where safe.

Hop-by-hop headers must be stripped.

## 13. Header Policy

The gateway should add:

```text
X-Forwarded-For
X-Forwarded-Host
X-Forwarded-Proto
X-Porthook-Tunnel-ID
```

The gateway and agent must strip hop-by-hop headers:

```text
Connection
Keep-Alive
Proxy-Authenticate
Proxy-Authorization
TE
Trailer
Transfer-Encoding
Upgrade
```

## 14. Configuration

Gateway environment variables:

```text
PORTHOOK_ADDR=:8080
PORTHOOK_AGENT_ADDR=:8081
PORTHOOK_MANAGEMENT_ADDR=:8082
PORTHOOK_MANAGEMENT_TOKEN=management-secret
PORTHOOK_TRUSTED_PROXIES=172.30.0.0/24
PORTHOOK_ROOT_DOMAIN=porthook.example
PORTHOOK_PUBLIC_URL=https://porthook.example
PORTHOOK_STATIC_TOKEN=dev-token
PORTHOOK_CONTROL_PLANE_URL=http://control-plane:8082
PORTHOOK_CONTROL_PLANE_TOKEN=validator-secret
PORTHOOK_CONTROL_PLANE_TIMEOUT=5s
PORTHOOK_CUSTOM_DOMAIN_CACHE_TTL=30s
PORTHOOK_CUSTOM_DOMAIN_MISS_TTL=5s
PORTHOOK_MAX_BODY_BYTES=1048576
PORTHOOK_WS_MESSAGE_MAX_BYTES=1048576
PORTHOOK_MAX_CONCURRENT_STREAMS=64
PORTHOOK_RATE_LIMIT_RPS=60
PORTHOOK_RATE_LIMIT_BURST=120
PORTHOOK_AUTH_ATTEMPT_LIMIT=5
PORTHOOK_AUTH_ATTEMPT_WINDOW=1m
PORTHOOK_STREAM_CHUNK_BYTES=32768
PORTHOOK_READ_HEADER_TIMEOUT=5s
PORTHOOK_READ_TIMEOUT=30s
PORTHOOK_WRITE_TIMEOUT=35s
PORTHOOK_IDLE_TIMEOUT=120s
PORTHOOK_HANDSHAKE_TIMEOUT=10s
PORTHOOK_STREAM_REQUEST_TIMEOUT=30s
PORTHOOK_STREAM_IDLE_TIMEOUT=60s
PORTHOOK_STREAM_MAX_LIFETIME=1h
PORTHOOK_WS_WRITE_TIMEOUT=10s
PORTHOOK_WS_PING_INTERVAL=15s
PORTHOOK_WS_PONG_TIMEOUT=5s
PORTHOOK_SHUTDOWN_TIMEOUT=5s
PORTHOOK_REQUEST_LOG_DATABASE_URL=postgres://...
PORTHOOK_REQUEST_LOG_RETENTION=720h
PORTHOOK_REQUEST_LOG_PRUNE_INTERVAL=1h
```

Agent environment variables:

```text
PORTHOOK_SERVER_URL=https://porthook.example
PORTHOOK_TOKEN=dev-token
PORTHOOK_CONFIG_PATH=
PORTHOOK_HANDSHAKE_TIMEOUT=10s
PORTHOOK_REQUEST_TIMEOUT=30s
PORTHOOK_MAX_RESPONSE_BODY_BYTES=1048576
PORTHOOK_WS_MESSAGE_MAX_BYTES=1048576
PORTHOOK_STREAM_CHUNK_BYTES=32768
PORTHOOK_WS_WRITE_TIMEOUT=10s
PORTHOOK_WS_PING_INTERVAL=15s
PORTHOOK_WS_PONG_TIMEOUT=5s
PORTHOOK_RECONNECT_INITIAL_DELAY=500ms
PORTHOOK_RECONNECT_MAX_DELAY=5s
PORTHOOK_RECONNECT_JITTER=250ms
```

Static token authentication is acceptable for the first proof of concept.

Control-plane environment variables:

```text
PORTHOOK_CONTROL_ADDR=:8082
PORTHOOK_CONTROL_ADMIN_TOKEN=...
PORTHOOK_CONTROL_VALIDATOR_TOKEN=...
PORTHOOK_TRUSTED_PROXIES=172.30.0.0/24
PORTHOOK_GATEWAY_MANAGEMENT_URL=http://gateway:8082
PORTHOOK_GATEWAY_MANAGEMENT_TOKEN=management-secret
PORTHOOK_DATABASE_URL=postgres://...
PORTHOOK_AUDIT_EVENT_RETENTION=2160h
PORTHOOK_AUDIT_EVENT_PRUNE_INTERVAL=1h
```

Duration values use Go duration syntax such as `500ms`, `10s`, `1m`, or `720h`. Request-log and audit-event pruning can be disabled by setting the matching retention or prune interval to `0s`.

The agent retries transient dial and WebSocket disconnect failures with exponential backoff and jitter. Authentication failures and explicit tunnel registration errors are terminal.

## 15. Health and Operations

Gateway management endpoints:

```text
GET /healthz
GET /readyz
GET /metrics
GET /api/v1/tunnels
GET /api/v1/tunnels/{id}
GET /api/v1/runtime
GET /api/v1/request-logs
```

`/healthz` should return success when the process is alive. Health and readiness are unauthenticated on the private management network; all data endpoints require `PORTHOOK_MANAGEMENT_TOKEN` when configured.

`/readyz` should return success when the gateway can accept public requests and agent connections.

The public listener has no Porthook operational routes. All paths, including `/healthz`, `/metrics`, and `/api/v1/*`, are forwarded to the selected tunnel.

## 16. Observability

MVP logs should include:

- Tunnel registration.
- Tunnel disconnect.
- Public request start.
- Public request completion.
- Stream errors.
- Authentication failures.
- WebSocket keepalive failures.
- Local agent request completion and failures.

Recommended fields:

```text
timestamp
level
component
outcome
tunnel_id
stream_id
method
host
path
request_bytes
response_bytes
status
duration_ms
error
```

Gateway and control-plane binaries should support optional OpenTelemetry tracing. The current trace MVP covers inbound HTTP server spans, gateway outbound control-plane HTTP client spans, W3C trace context propagation, and Porthook route/result attributes on public gateway request spans.

## 17. Security Baseline

MVP requirements:

- Token authentication for agents.
- No unauthenticated tunnel registration.
- Request and response size limits.
- Stream timeout.
- Hostname validation.
- Subdomain validation.
- Tunnel isolation in registry lookups.
- No logging of token values.

Subdomain names should allow only lowercase ASCII letters, numbers, and hyphens.

## 18. Testing Strategy

Unit tests:

- Protocol message encoding and decoding.
- Header filtering.
- Subdomain validation.
- Registry lookups.
- Limit enforcement.

Integration tests:

- Agent connects to gateway.
- Tunnel registration succeeds.
- Public request reaches local service.
- Concurrent requests complete.
- Unknown host returns `404`.
- Disconnected tunnel returns `503`.

Manual smoke test:

```text
1. Start local HTTP server on port 3000.
2. Start gateway with root domain mapped to localhost.
3. Start agent with static token.
4. Send request to gateway with Host header for tunnel subdomain.
5. Verify response body and logs.
```

## 19. Implementation Milestones

### Milestone 1: Single Request Proof

- Go module.
- Gateway HTTP listener.
- Agent WebSocket connection.
- Auth message.
- Tunnel registration.
- One request and one response round-trip.

### Milestone 2: Concurrent Streams

- Stream IDs.
- Multiple in-flight requests.
- Timeouts.
- Error propagation.

### Milestone 3: CLI Polish

- Friendly command output.
- Structured logs.
- Reconnect behavior.
- Version command.

### Milestone 4: Self-Hosted Packaging

- Dockerfile.
- Docker Compose.
- Example root domain configuration.
- TLS reverse proxy example.

### Milestone 5: Control Plane Foundation

- Token storage.
- Tunnel metadata.
- Reserved subdomains.
- Basic dashboard.

## 20. Open Technical Questions

- Should control and body messages share one WebSocket frame format later?
- Should the first gateway expose one port or separate public and agent ports?
- Should the root Go module include all components or should modules be split early?
- Which WebSocket library should be used?
