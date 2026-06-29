# Gateway

License: AGPL-3.0-only

The gateway is the public edge service.

Responsibilities:

- Accept public HTTP traffic.
- Route hostnames to active tunnels.
- Maintain agent tunnel connections.
- Forward requests and responses.
- Enforce limits.
- Expose health endpoints.

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
| `PORTHOOK_CONTROL_PLANE_TOKEN` | empty | Bearer token used when calling the control-plane validation endpoint. Required when `PORTHOOK_CONTROL_PLANE_URL` is set. |
| `PORTHOOK_CONTROL_PLANE_TIMEOUT` | `5s` | Timeout for control-plane token validation requests. |
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

Gateway logs are structured text logs written to stdout. Public request logs include route outcome, status, tunnel ID, stream ID, byte counts, and duration. Token values are not logged.
