# Gateway

License: AGPL-3.0-only

The gateway is the public edge service.

Responsibilities:

- Accept public HTTP traffic.
- Route hostnames to active tunnels.
- Maintain agent tunnel connections.
- Forward requests and responses.
- Enforce limits.
- Enforce access policies for reserved subdomains when connected to a control plane.
- Resolve custom domain mappings when connected to a control plane.
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
| `PORTHOOK_CONTROL_PLANE_TIMEOUT` | `5s` | Timeout for control-plane token validation, reserved-subdomain authorization, custom domain lookup, and access policy evaluation requests. |
| `PORTHOOK_CUSTOM_DOMAIN_CACHE_TTL` | `30s` | Time to cache custom-domain lookup hits and misses. Set to `0s` to disable caching. |
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
| `PORTHOOK_REQUEST_LOG_LIMIT` | `500` | Number of recent public request log entries kept in memory. Set `0` to disable the in-memory request log endpoint. |
| `PORTHOOK_REQUEST_LOG_DATABASE_URL` | empty | Optional Postgres connection URL for durable gateway request log ingestion. If empty, request logs are kept only in memory. |
| `PORTHOOK_REQUEST_LOG_WRITE_TIMEOUT` | `1s` | Timeout for writing a public request log entry to the durable store. |
| `PORTHOOK_HEALTHCHECK_URL` | derived from `PORTHOOK_ADDR` | Optional explicit URL used by `porthook-gateway healthcheck`. |
| `PORTHOOK_HEALTHCHECK_TIMEOUT` | `2s` | HTTP timeout used by `porthook-gateway healthcheck`. |
| `PORTHOOK_OTEL_ENABLED` | `false` | Enable OpenTelemetry tracing. |
| `PORTHOOK_OTEL_EXPORTER` | `none` | Trace exporter: `otlp`, `otlp-http`, `otlp-grpc`, `stdout`, `console`, or `none`. |
| `PORTHOOK_OTEL_PROTOCOL` | `http/protobuf` | OTLP protocol when using `PORTHOOK_OTEL_EXPORTER=otlp`: `http/protobuf` or `grpc`. |

Duration values use Go duration syntax such as `500ms`, `10s`, or `1m`.

## Operational Endpoints

The public listener exposes:

- `GET /healthz`
- `GET /readyz`
- `GET /metrics`
- `GET /api/v1/tunnels`
- `GET /api/v1/tunnels/{id}`
- `GET /api/v1/runtime`
- `GET /api/v1/request-logs`

Metrics use Prometheus text format and include active tunnels, process uptime, public request totals, selected public request outcome counters, custom domain lookup results, access policy denials/errors, token validation attempts, authentication failures, and tunnel registration successes/failures. The metrics endpoint supports browser reads for the self-hosted dashboard. `GET /api/v1/tunnels` returns active tunnel summaries for dashboard visibility and omits local target URLs. `GET /api/v1/tunnels/{id}` returns a single active tunnel with public metadata, stream counts, uptime, and a recent request summary; it also omits local target URLs. `GET /api/v1/runtime` returns safe gateway runtime metadata, limits, timeouts, request-log buffer usage, stream totals, and counters without exposing tokens, local targets, or control-plane URLs.

Use `porthook doctor --gateway http://localhost:8080` to check gateway health, readiness, and tunnel API reachability from the CLI. Use `porthook tunnels list --gateway http://localhost:8080` and `porthook tunnels show --gateway http://localhost:8080 <tunnel-id>` to inspect active tunnels. Add `--control-plane` and an admin token to `porthook doctor` to include control-plane checks.

The container image includes `porthook-gateway healthcheck`, which calls `/readyz` through the local public listener. Set `PORTHOOK_HEALTHCHECK_URL` only when the listener cannot be reached through the derived localhost URL.

`GET /api/v1/request-logs` returns public request summaries. Without `PORTHOOK_REQUEST_LOG_DATABASE_URL`, it reads from the in-memory ring buffer. When `PORTHOOK_REQUEST_LOG_DATABASE_URL` is set, the gateway writes public request summaries to Postgres and reads the endpoint from that durable store. The response is newest-first and supports `?limit=N`, `subdomain`, `method`, `host`, `path`, `status`, `outcome`, `request_id`, `tunnel_id`, `since`, `until`, and `cursor`. Time filters use RFC3339 timestamps, and `limit` is applied after filtering. Responses include `request_logs`, echoed `filters`, and `next_cursor` when another page is available. Entries include method, host, path, query presence, remote IP, request ID, subdomain, tunnel ID, stream ID, status, outcome, byte counts, duration, and an optional error string. Raw query strings and authorization values are not returned.

Gateway logs are structured text logs written to stdout. Operational logs include an `event` field such as `gateway.public_request`, `gateway.tunnel_registered`, `gateway.agent_auth_failed`, and `gateway.agent_keepalive_failed`.

Public request logs include method, host, path, whether a query string was present, route outcome, custom-domain routing state, status, tunnel ID, stream ID, byte counts, duration, remote IP, and optional `request_id` from `X-Request-ID` or `X-Correlation-ID`. Raw query strings and token values are not logged.

When `PORTHOOK_CONTROL_PLANE_URL` is configured, requested subdomains require an existing control-plane reservation owned by the validated token. Agents that omit `--subdomain` still receive random subdomains without a reservation.

Reserved subdomains may also have public access policies managed by the control plane. The gateway evaluates those policies before forwarding public requests to the agent. Supported policy modes are `public`, `basic_auth`, `bearer_token`, and `ip_allowlist`. Basic and bearer credentials used for gateway access are consumed at the gateway and are not forwarded to the local service.

Custom domains are resolved through the control plane when a public request host does not match `{subdomain}.{PORTHOOK_ROOT_DOMAIN}`. A custom domain maps to a reserved subdomain, so the agent still registers the reserved name and any access policy on that reservation applies to both the wildcard hostname and mapped custom hostnames.

OpenTelemetry tracing is disabled by default. See [../../docs/OBSERVABILITY.md](../../docs/OBSERVABILITY.md) for OTLP and stdout trace exporter configuration.
