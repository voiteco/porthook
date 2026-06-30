# Gateway

License: AGPL-3.0-only

The gateway is the public edge service.

Responsibilities:

- Accept public HTTP traffic.
- Route hostnames to active tunnels.
- Maintain agent tunnel connections.
- Forward requests and responses.
- Enforce limits.
- Expose health and metrics endpoints.

Default local listeners:

- Public HTTP: `:8080`
- Agent WebSocket: `:8081`

Run locally:

```sh
go run ./server/gateway/cmd/porthook-gateway
```

## Runtime Configuration

| Environment variable | Default | Description |
| --- | --- | --- |
| `PORTHOOK_ADDR` | `:8080` | Public HTTP listener address. |
| `PORTHOOK_AGENT_ADDR` | `:8081` | Agent WebSocket listener address. |
| `PORTHOOK_ROOT_DOMAIN` | `localhost` | Root domain used for subdomain routing. |
| `PORTHOOK_PUBLIC_URL` | `http://localhost:8080` | Base public URL printed by the agent. |
| `PORTHOOK_STATIC_TOKEN` | `dev-token` | Static agent authentication token. |
| `PORTHOOK_CONTROL_PLANE_URL` | empty | Optional control-plane base URL for token validation. Static token auth is used when empty. |
| `PORTHOOK_CONTROL_PLANE_TOKEN` | empty | Bearer token used when calling control-plane validation and reserved-subdomain authorization endpoints. Required when `PORTHOOK_CONTROL_PLANE_URL` is set. |
| `PORTHOOK_CONTROL_PLANE_TIMEOUT` | `5s` | Timeout for control-plane token validation and reserved-subdomain authorization requests. |
| `PORTHOOK_MAX_BODY_BYTES` | `1048576` | Maximum public request body forwarded through a tunnel. |
| `PORTHOOK_MAX_CONCURRENT_STREAMS` | `64` | Maximum concurrent public requests per tunnel. |
| `PORTHOOK_RATE_LIMIT_RPS` | `60` | Maximum public requests per second per tunnel. |
| `PORTHOOK_RATE_LIMIT_BURST` | `120` | Per-tunnel burst capacity before rate limiting returns `429`. |
| `PORTHOOK_STREAM_CHUNK_BYTES` | `32768` | Maximum HTTP body chunk size for tunnel messages. |
| `PORTHOOK_READ_HEADER_TIMEOUT` | `5s` | HTTP request header read timeout. |
| `PORTHOOK_READ_TIMEOUT` | `30s` | HTTP request read timeout. |
| `PORTHOOK_WRITE_TIMEOUT` | `35s` | HTTP response write timeout. |
| `PORTHOOK_IDLE_TIMEOUT` | `120s` | HTTP keep-alive idle timeout. |
| `PORTHOOK_HANDSHAKE_TIMEOUT` | `10s` | Agent authentication and tunnel registration timeout. |
| `PORTHOOK_STREAM_TIMEOUT` | `30s` | Public request round-trip timeout over the tunnel. |
| `PORTHOOK_WS_WRITE_TIMEOUT` | `10s` | WebSocket message write timeout. |
| `PORTHOOK_WS_PING_INTERVAL` | `15s` | WebSocket keepalive ping interval. |
| `PORTHOOK_WS_PONG_TIMEOUT` | `5s` | WebSocket keepalive pong timeout. |
| `PORTHOOK_SHUTDOWN_TIMEOUT` | `5s` | Graceful shutdown timeout. |

Duration values use Go duration syntax such as `500ms`, `10s`, or `1m`.

## Operational Endpoints

The public listener exposes:

- `GET /healthz`
- `GET /readyz`
- `GET /metrics`
- `GET /api/v1/tunnels`

Metrics use Prometheus text format and include active tunnels, public requests, token validation attempts, authentication failures, and tunnel registrations. `GET /api/v1/tunnels` returns active tunnel summaries for dashboard visibility and omits local target URLs.

Gateway logs are structured text logs written to stdout. Operational logs include an `event` field such as `gateway.public_request`, `gateway.tunnel_registered`, `gateway.agent_auth_failed`, and `gateway.agent_keepalive_failed`.

Public request logs include method, host, path, whether a query string was present, route outcome, status, tunnel ID, stream ID, byte counts, duration, remote IP, and optional `request_id` from `X-Request-ID` or `X-Correlation-ID`. Raw query strings and token values are not logged.

When `PORTHOOK_CONTROL_PLANE_URL` is configured, requested subdomains require an existing control-plane reservation owned by the validated token. Agents that omit `--subdomain` still receive random subdomains without a reservation.
