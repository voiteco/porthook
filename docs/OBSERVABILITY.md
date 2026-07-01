# Observability

Porthook exposes Prometheus text metrics and optional OpenTelemetry traces for the self-hosted gateway and control plane.

Metrics remain available without extra configuration:

- gateway: `GET /metrics` on the public listener
- control plane: `GET /metrics` on the control-plane listener

OpenTelemetry tracing is disabled by default. Enable it only when you intentionally send traces to an OTLP collector or want local stdout trace debugging.

## Trace Coverage

The OpenTelemetry MVP records:

- gateway public HTTP request spans
- gateway agent WebSocket handshake request spans
- control-plane HTTP request spans
- gateway outbound HTTP client spans for token validation, reserved-subdomain authorization, access policy evaluation, and custom-domain lookup

Gateway public request spans include Porthook attributes for routing and result context:

- `porthook.subdomain`
- `porthook.custom_domain`
- `porthook.tunnel_id`
- `porthook.stream_id`
- `porthook.request_bytes`
- `porthook.response_bytes`
- `porthook.outcome`

Trace context is propagated with W3C tracecontext and baggage headers.

## Configuration

Common environment variables:

| Environment variable | Default | Description |
| --- | --- | --- |
| `PORTHOOK_OTEL_ENABLED` | `false` | Enables tracing when set to `true`. Explicit `false` disables tracing even if an exporter is configured. |
| `PORTHOOK_OTEL_SERVICE_NAME` | binary name | Service name override. `OTEL_SERVICE_NAME` is also honored. |
| `PORTHOOK_OTEL_EXPORTER` | `none` | Trace exporter. Supports `otlp`, `otlp-http`, `otlp-grpc`, `stdout`, `console`, and `none`. `OTEL_TRACES_EXPORTER` is also honored. |
| `PORTHOOK_OTEL_PROTOCOL` | `http/protobuf` | Protocol used when `PORTHOOK_OTEL_EXPORTER=otlp`. Supports `http/protobuf` and `grpc`. `OTEL_EXPORTER_OTLP_PROTOCOL` is also honored. |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | exporter default | OTLP collector endpoint, for example `http://otel-collector:4318` for HTTP or `http://otel-collector:4317` for gRPC. |
| `OTEL_EXPORTER_OTLP_HEADERS` | empty | Optional OTLP headers. Do not commit secret header values. |
| `OTEL_EXPORTER_OTLP_INSECURE` | exporter default | Set to `true` for local insecure OTLP gRPC endpoints. |
| `OTEL_SDK_DISABLED` | `false` | Standard hard-disable switch. Set to `true` to disable tracing. |

Use stdout locally:

```sh
PORTHOOK_OTEL_ENABLED=true \
PORTHOOK_OTEL_EXPORTER=stdout \
go run ./server/gateway/cmd/porthook-gateway
```

Send traces to an OTLP HTTP collector:

```sh
PORTHOOK_OTEL_ENABLED=true \
PORTHOOK_OTEL_EXPORTER=otlp \
PORTHOOK_OTEL_PROTOCOL=http/protobuf \
OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector:4318 \
porthook-gateway
```

Send traces to an OTLP gRPC collector:

```sh
PORTHOOK_OTEL_ENABLED=true \
PORTHOOK_OTEL_EXPORTER=otlp \
PORTHOOK_OTEL_PROTOCOL=grpc \
OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector:4317 \
OTEL_EXPORTER_OTLP_INSECURE=true \
porthook-gateway
```

## Docker Compose

The checked-in control-plane and production Compose files pass the OTel environment variables through to the gateway and control-plane containers. Configure them in the copied `.env.control-plane` or `.env.production` file.

Example:

```text
PORTHOOK_OTEL_ENABLED=true
PORTHOOK_OTEL_EXPORTER=otlp
PORTHOOK_OTEL_PROTOCOL=http/protobuf
OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector:4318
```

Keep collector credentials and private headers out of the repository.
