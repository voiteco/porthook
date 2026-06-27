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
- Optional control-plane and dashboard components after the tunnel core works.

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
- Custom domains.
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

The MVP has two required binaries:

- `porthook-agent`: local CLI agent.
- `porthook-gateway`: public gateway server.

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

Control messages are JSON.

Request and response bodies currently use whole-body JSON payloads for simplicity. The protocol should move to chunked or binary body streaming once the single request path is stable.

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
    "agent_version": "0.1.0"
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

## 10. Stream Messages

Initial HTTP stream message types:

```text
http.request
http.response
http.stream.error
```

### 10.1 Request Payload

```json
{
  "method": "POST",
  "path": "/webhook",
  "query": "source=test",
  "header": {
    "content-type": ["application/json"]
  },
  "body": "base64-encoded body in JSON"
}
```

### 10.2 Response Payload

```json
{
  "status": 200,
  "header": {
    "content-type": ["application/json"]
  },
  "body": "base64-encoded body in JSON"
}
```

## 11. Gateway Design

Gateway responsibilities:

- Listen for public HTTP requests.
- Listen for agent WebSocket connections.
- Authenticate agents.
- Register active tunnel sessions.
- Route hostnames to tunnels.
- Multiplex concurrent streams over agent connections.
- Enforce request size, timeout, and concurrency limits.
- Return clear errors for inactive tunnels.
- Expose health endpoints.

### 11.1 HTTP Listener

Routing rule:

```text
{subdomain}.{root_domain}
```

If the hostname does not match an active tunnel, return `404`.

If the tunnel exists but is disconnected, return `503`.

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
porthook http <port>
porthook http <port> --server <url>
porthook http <port> --token <token>
porthook http <port> --subdomain <name>
porthook version
```

Future commands:

```text
porthook login
porthook logout
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
PORTHOOK_ROOT_DOMAIN=porthook.example
PORTHOOK_PUBLIC_URL=https://porthook.example
PORTHOOK_STATIC_TOKEN=dev-token
PORTHOOK_MAX_BODY_BYTES=1048576
PORTHOOK_READ_HEADER_TIMEOUT=5s
PORTHOOK_READ_TIMEOUT=30s
PORTHOOK_WRITE_TIMEOUT=35s
PORTHOOK_IDLE_TIMEOUT=120s
PORTHOOK_HANDSHAKE_TIMEOUT=10s
PORTHOOK_STREAM_TIMEOUT=30s
PORTHOOK_WS_WRITE_TIMEOUT=10s
PORTHOOK_WS_PING_INTERVAL=15s
PORTHOOK_WS_PONG_TIMEOUT=5s
PORTHOOK_SHUTDOWN_TIMEOUT=5s
```

Agent environment variables:

```text
PORTHOOK_SERVER_URL=https://porthook.example
PORTHOOK_TOKEN=dev-token
PORTHOOK_HANDSHAKE_TIMEOUT=10s
PORTHOOK_REQUEST_TIMEOUT=30s
PORTHOOK_MAX_RESPONSE_BODY_BYTES=1048576
PORTHOOK_WS_WRITE_TIMEOUT=10s
PORTHOOK_WS_PING_INTERVAL=15s
PORTHOOK_WS_PONG_TIMEOUT=5s
```

Static token authentication is acceptable for the first proof of concept.

Duration values use Go duration syntax such as `500ms`, `10s`, or `1m`.

## 15. Health and Operations

Gateway endpoints:

```text
GET /healthz
GET /readyz
```

`/healthz` should return success when the process is alive.

`/readyz` should return success when the gateway can accept public requests and agent connections.

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

- Should control and body messages share one WebSocket frame format from day one?
- Should binary body frames be implemented before public alpha?
- Should the first gateway expose one port or separate public and agent ports?
- Should the root Go module include all components or should modules be split early?
- Which WebSocket library should be used?
