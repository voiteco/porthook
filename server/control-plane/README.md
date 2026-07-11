# Control Plane

License: AGPL-3.0-only

The control plane owns users, agent tokens, scoped admin tokens, tunnel metadata, reserved names, custom domain mappings, access policies, and limits.

Current scope is admin credential management, agent token management, reserved subdomain ownership, custom domain mapping, and public access policy management for self-hosted gateway authentication.

Supported token scopes:

- `register_tunnel`

Token APIs reject unknown scopes and malformed JSON. Token admin, validator, reservation, access policy, and authorization operations are logged without plaintext token values.

Supported admin token scopes:

- `admin_tokens`
- `tokens`
- `reservations`
- `domains`
- `access_policies`
- `audit_history`
- `runtime_diagnostics`

`PORTHOOK_CONTROL_ADMIN_TOKEN` is a full-scope bootstrap and recovery token. For routine operations, create scoped admin tokens with `porthook admin tokens create` or the dashboard. Admin token plaintext is returned only once at creation time; list responses contain summaries without plaintext values.

Run locally:

```sh
go run ./server/control-plane/cmd/porthook-control-plane
```

Build the local Docker image:

```sh
make docker-build-control-plane
```

For a local Postgres-backed stack with the gateway, see [../../deploy/compose/README.md](../../deploy/compose/README.md).

## Dashboard

The control plane serves the self-hosted dashboard at:

```text
http://localhost:8082/dashboard/
```

Use the configured bootstrap token or a scoped admin token to log in. The dashboard stores that admin token in browser session storage for the current tab and sends it as a bearer token to the control-plane APIs. Use logout to clear the browser session value.

The dashboard can list, create, and revoke scoped admin tokens and agent tokens, reserve subdomains for active tokens, manage custom domains and access policies, inspect control-plane audit events, show active gateway tunnels, runtime, and metrics, run browser diagnostics, inspect recent gateway request logs, and download an operational JSON export. Plaintext admin and agent tokens are displayed only from create responses; the control plane stores token hashes and later list responses contain token summaries only. Access policy secrets are accepted only in create/update requests and are not returned by list responses. Successful token validation updates `last_used_at` metadata for token list views.

Gateway operator endpoints use the configured private management connection:

- `GET /api/v1/gateway/healthz`
- `GET /api/v1/gateway/readyz`
- `GET /api/v1/gateway/metrics`
- `GET /api/v1/gateway/tunnels`
- `GET /api/v1/gateway/tunnels/{id}`
- `GET /api/v1/gateway/runtime`
- `GET /api/v1/gateway/request-logs`

Health, readiness, metrics, tunnel inventory, and runtime require `runtime_diagnostics`. Request logs require `audit_history`. The bootstrap admin token has all scopes.

## Runtime Configuration

| Environment variable | Default | Description |
| --- | --- | --- |
| `PORTHOOK_CONTROL_ADDR` | `:8082` | Control-plane HTTP listener address. |
| `PORTHOOK_CONTROL_ADMIN_TOKEN` | empty | Full-scope bootstrap/recovery bearer token for control-plane administration. Production config checks require it, but routine operator access should use scoped admin tokens. |
| `PORTHOOK_CONTROL_VALIDATOR_TOKEN` | empty | Bearer token required for token validation requests from the gateway. If empty, validation returns `401 Unauthorized`. |
| `PORTHOOK_GATEWAY_MANAGEMENT_URL` | empty | Private gateway management base URL used by authenticated operator APIs. Required by production configuration validation. |
| `PORTHOOK_GATEWAY_MANAGEMENT_TOKEN` | empty | Service bearer token used only for control-plane-to-gateway management requests. Required when the management URL is configured. |
| `PORTHOOK_GATEWAY_MANAGEMENT_TIMEOUT` | `5s` | Timeout for gateway management requests. |
| `PORTHOOK_DATABASE_URL` | empty | Postgres connection URL. If empty, the process uses in-memory storage for development. |
| `PORTHOOK_AUDIT_EVENT_RETENTION` | `2160h` | Control-plane audit event retention window. Set `0s` to disable audit event pruning. |
| `PORTHOOK_AUDIT_EVENT_PRUNE_INTERVAL` | `1h` | How often the control plane prunes audit events older than the retention window. Set `0s` to disable the pruning worker. |
| `PORTHOOK_HEALTHCHECK_URL` | derived from `PORTHOOK_CONTROL_ADDR` | Optional explicit URL used by `porthook-control-plane healthcheck`. |
| `PORTHOOK_HEALTHCHECK_TIMEOUT` | `2s` | HTTP timeout used by `porthook-control-plane healthcheck`. |
| `PORTHOOK_OTEL_ENABLED` | `false` | Enable OpenTelemetry tracing. |
| `PORTHOOK_OTEL_EXPORTER` | `none` | Trace exporter: `otlp`, `otlp-http`, `otlp-grpc`, `stdout`, `console`, or `none`. |
| `PORTHOOK_OTEL_PROTOCOL` | `http/protobuf` | OTLP protocol when using `PORTHOOK_OTEL_EXPORTER=otlp`: `http/protobuf` or `grpc`. |

When Postgres is configured, `porthook-control-plane` applies embedded versioned migrations at startup and records applied versions in `schema_migrations`.

OpenTelemetry tracing is disabled by default. See [../../docs/OBSERVABILITY.md](../../docs/OBSERVABILITY.md) for OTLP and stdout trace exporter configuration.

## CLI

Run operational diagnostics:

```sh
printf '%s' 'admin-secret' | porthook doctor \
  --gateway http://localhost:8080 \
  --control-plane http://localhost:8082 \
  --admin-token-stdin
```

Inspect recent audit events:

```sh
printf '%s' 'admin-secret' | porthook history events \
  --control-plane http://localhost:8082 \
  --admin-token-stdin \
  --limit 50
```

Create a scoped admin token:

```sh
printf '%s' 'admin-secret' | porthook admin tokens create \
  --control-plane http://localhost:8082 \
  --admin-token-stdin \
  --name 'audit reader' \
  --scope audit_history
```

List admin tokens:

```sh
printf '%s' 'admin-secret' | porthook admin tokens list \
  --control-plane http://localhost:8082 \
  --admin-token-stdin
```

Revoke an admin token:

```sh
printf '%s' 'admin-secret' | porthook admin tokens revoke \
  --control-plane http://localhost:8082 \
  --admin-token-stdin \
  adm_...
```

Create an agent token:

```sh
printf '%s' 'admin-secret' | porthook tokens create \
  --control-plane http://localhost:8082 \
  --admin-token-stdin \
  --name 'local agent'
```

List tokens:

```sh
printf '%s' 'admin-secret' | porthook tokens list \
  --control-plane http://localhost:8082 \
  --admin-token-stdin
```

Revoke a token:

```sh
printf '%s' 'admin-secret' | porthook tokens revoke \
  --control-plane http://localhost:8082 \
  --admin-token-stdin \
  tok_...
```

The token plaintext is returned only once at creation time. Storage keeps only a hash.

Reserve a requested subdomain for a token:

```sh
printf '%s' 'admin-secret' | porthook reserved create \
  --control-plane http://localhost:8082 \
  --admin-token-stdin \
  --name demo \
  --token-id tok_...
```

Create an access policy for a reserved subdomain:

```sh
porthook access create \
  --control-plane http://localhost:8082 \
  --admin-token 'admin-secret' \
  --reserved-subdomain-id rs_... \
  --mode basic_auth \
  --basic-username demo \
  --basic-password 'gateway-password'
```

Create a custom domain mapping for a reserved subdomain:

```sh
printf '%s' 'admin-secret' | porthook domains create \
  --control-plane http://localhost:8082 \
  --admin-token-stdin \
  --hostname preview.example.test \
  --reserved-subdomain-id rs_...
```

List and delete custom domain mappings:

```sh
printf '%s' 'admin-secret' | porthook domains list \
  --control-plane http://localhost:8082 \
  --admin-token-stdin

printf '%s' 'admin-secret' | porthook domains delete \
  --control-plane http://localhost:8082 \
  --admin-token-stdin \
  preview.example.test
```

## Operational Endpoints

- `GET /healthz`
- `GET /readyz`
- `GET /metrics`
- `GET /dashboard/`
- `GET /api/v1/status`
- `GET /api/v1/events`
- `GET /api/v1/admin-tokens`
- `POST /api/v1/admin-tokens`
- `DELETE /api/v1/admin-tokens/{id}`
- `GET /api/v1/reserved-subdomains`
- `POST /api/v1/reserved-subdomains`
- `DELETE /api/v1/reserved-subdomains/{id}`
- `GET /api/v1/access-policies`
- `POST /api/v1/access-policies`
- `GET /api/v1/access-policies/{id}`
- `PUT /api/v1/access-policies/{id}`
- `DELETE /api/v1/access-policies/{id}`
- `POST /api/v1/access-policies/evaluate`
- `GET /api/v1/custom-domains`
- `POST /api/v1/custom-domains`
- `GET /api/v1/custom-domains/{id}`
- `DELETE /api/v1/custom-domains/{id}`
- `POST /api/v1/custom-domains/lookup`

`/readyz` checks the agent token, admin token, reservation, access policy, custom domain, and audit event stores. For Postgres-backed deployments, it pings the configured database. Metrics use Prometheus text format and include readiness state, process uptime, token inventory, reserved subdomain inventory, access policy inventory, custom domain inventory, token admin operations, token validations, reserved subdomain operations, access policy operations/evaluations, custom domain operations/lookups, authorization failures, and readiness failures.

`/api/v1/status` returns JSON with the control-plane readiness state and binary version for dashboard and automation checks.

`/api/v1/events` returns recent audit events for admin users with the `audit_history` scope. Postgres-backed deployments store events durably; deployments without `PORTHOOK_DATABASE_URL` use an in-memory ring buffer. Events are newest-first, omit plaintext tokens and access policy secrets, and support `limit`, `event`, `level`, `request_id`, `remote_ip`, `field`, `since`, `until`, and `cursor` query parameters. `since` and `until` use RFC3339 timestamps. Responses include `events`, echoed `filters`, and `next_cursor` when another page is available.

`porthook doctor` checks gateway and control-plane operational endpoints, including `/api/v1/events` when an admin token is provided, and prints response request IDs for log correlation.

The container image includes `porthook-control-plane healthcheck`, which calls `/readyz` through the local control-plane listener. Set `PORTHOOK_HEALTHCHECK_URL` only when the listener cannot be reached through the derived localhost URL.

Control-plane logs are structured text logs written to stdout. Audit-relevant logs include an `event` field such as `control_plane.auth_failed`, `control_plane.admin_token_created`, `control_plane.admin_token_revoked`, `control_plane.token_created`, `control_plane.token_validated`, `control_plane.token_revoked`, `control_plane.reservation_created`, `control_plane.reservation_authorized`, `control_plane.custom_domain_created`, `control_plane.custom_domain_lookup`, `control_plane.access_policy_created`, and `control_plane.access_policy_evaluated`. Logs include method, path, remote IP, optional `request_id` from `X-Request-ID` or `X-Correlation-ID`, token IDs where available, admin token IDs where available, subdomains, custom domain IDs, policy IDs, outcomes, and denial reasons. Authorization headers, plaintext token values, and access policy secrets are not logged.

## API

Create an admin token:

```sh
curl -sS -X POST http://localhost:8082/api/v1/admin-tokens \
  -H 'Authorization: Bearer admin-secret' \
  -H 'Content-Type: application/json' \
  -d '{"name":"audit reader","scopes":["audit_history"]}'
```

List admin tokens:

```sh
curl -sS http://localhost:8082/api/v1/admin-tokens \
  -H 'Authorization: Bearer admin-secret'
```

Admin token summaries include `id`, `name`, `scopes`, `created_at`, optional `last_used_at`, and optional `revoked_at`. Plaintext token values are not returned by list responses.

Revoke an admin token:

```sh
curl -i -X DELETE http://localhost:8082/api/v1/admin-tokens/adm_... \
  -H 'Authorization: Bearer admin-secret'
```

Create a token:

```sh
curl -sS -X POST http://localhost:8082/api/v1/tokens \
  -H 'Authorization: Bearer admin-secret' \
  -H 'Content-Type: application/json' \
  -d '{"name":"local agent","scopes":["register_tunnel"]}'
```

List tokens:

```sh
curl -sS http://localhost:8082/api/v1/tokens \
  -H 'Authorization: Bearer admin-secret'
```

Token summaries include `id`, `name`, `scopes`, `created_at`, optional `last_used_at`, and optional `revoked_at`. Plaintext token values are not returned by list responses.

Validate a token:

```sh
curl -sS -X POST http://localhost:8082/api/v1/tokens/validate \
  -H 'Authorization: Bearer validator-secret' \
  -H 'Content-Type: application/json' \
  -d '{"token":"ph_...","scope":"register_tunnel"}'
```

Revoke a token:

```sh
curl -i -X DELETE http://localhost:8082/api/v1/tokens/tok_... \
  -H 'Authorization: Bearer admin-secret'
```

Set `PORTHOOK_CONTROL_ADMIN_TOKEN` before bootstrapping scoped admin tokens. A scoped admin token may then authorize the APIs matching its scopes. Set `PORTHOOK_CONTROL_VALIDATOR_TOKEN` and configure the same value as `PORTHOOK_CONTROL_PLANE_TOKEN` on the gateway before using control-plane token validation.

Create a reserved subdomain:

```sh
curl -sS -X POST http://localhost:8082/api/v1/reserved-subdomains \
  -H 'Authorization: Bearer admin-secret' \
  -H 'Content-Type: application/json' \
  -d '{"name":"demo","token_id":"tok_..."}'
```

List reserved subdomains:

```sh
curl -sS http://localhost:8082/api/v1/reserved-subdomains \
  -H 'Authorization: Bearer admin-secret'
```

Create an access policy:

```sh
curl -sS -X POST http://localhost:8082/api/v1/access-policies \
  -H 'Authorization: Bearer admin-secret' \
  -H 'Content-Type: application/json' \
  -d '{"reserved_subdomain_id":"rs_...","mode":"basic_auth","basic_username":"demo","basic_password":"gateway-password"}'
```

List access policies:

```sh
curl -sS http://localhost:8082/api/v1/access-policies \
  -H 'Authorization: Bearer admin-secret'
```

Create a custom domain:

```sh
curl -sS -X POST http://localhost:8082/api/v1/custom-domains \
  -H 'Authorization: Bearer admin-secret' \
  -H 'Content-Type: application/json' \
  -d '{"hostname":"preview.example.test","reserved_subdomain_id":"rs_..."}'
```

List custom domains:

```sh
curl -sS http://localhost:8082/api/v1/custom-domains \
  -H 'Authorization: Bearer admin-secret'
```

Lookup a custom domain for the gateway:

```sh
curl -sS -X POST http://localhost:8082/api/v1/custom-domains/lookup \
  -H 'Authorization: Bearer validator-secret' \
  -H 'Content-Type: application/json' \
  -d '{"hostname":"preview.example.test"}'
```

Evaluate an access policy for the gateway:

```sh
curl -sS -X POST http://localhost:8082/api/v1/access-policies/evaluate \
  -H 'Authorization: Bearer validator-secret' \
  -H 'Content-Type: application/json' \
  -d '{"subdomain":"demo","remote_ip":"192.0.2.10","basic_username":"demo","basic_password":"gateway-password"}'
```

The gateway uses `POST /api/v1/reserved-subdomains/authorize` with the validator bearer token to authorize requested subdomains during tunnel registration. It uses `POST /api/v1/custom-domains/lookup` with the same validator bearer token to route public requests for custom hostnames.

The local control-plane integration path can be checked with:

```sh
make smoke-control-plane
```
