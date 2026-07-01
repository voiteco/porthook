# Control Plane

License: AGPL-3.0-only

The control plane owns users, tokens, tunnel metadata, reserved names, custom domain mappings, access policies, and limits.

Current scope is token management, reserved subdomain ownership, custom domain mapping, and public access policy management for self-hosted gateway authentication.

Supported token scopes:

- `register_tunnel`

Token APIs reject unknown scopes and malformed JSON. Token admin, validator, reservation, access policy, and authorization operations are logged without plaintext token values.

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

Use the configured `PORTHOOK_CONTROL_ADMIN_TOKEN` to log in. The dashboard stores that admin token in browser session storage for the current tab and sends it as a bearer token to the control-plane token APIs. Use logout to clear the browser session value.

The dashboard can list, create, and revoke agent tokens, reserve subdomains for active tokens, manage custom domains and access policies, inspect control-plane audit events, show active gateway tunnels, run browser diagnostics, and inspect recent gateway request logs. Plaintext agent tokens are displayed only from the create response; the control plane stores token hashes and later list responses contain token summaries only. Access policy secrets are accepted only in create/update requests and are not returned by list responses. Successful token validation updates `last_used_at` metadata for token list views.

## Runtime Configuration

| Environment variable | Default | Description |
| --- | --- | --- |
| `PORTHOOK_CONTROL_ADDR` | `:8082` | Control-plane HTTP listener address. |
| `PORTHOOK_CONTROL_ADMIN_TOKEN` | empty | Bearer token required for token creation, listing, and revocation. If empty, those requests return `401 Unauthorized`. |
| `PORTHOOK_CONTROL_VALIDATOR_TOKEN` | empty | Bearer token required for token validation requests from the gateway. If empty, validation returns `401 Unauthorized`. |
| `PORTHOOK_DATABASE_URL` | empty | Postgres connection URL. If empty, the process uses in-memory storage for development. |
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

`/readyz` checks the token, reservation, access policy, and custom domain stores. For Postgres-backed deployments, it pings the configured database. Metrics use Prometheus text format and include readiness state, process uptime, token inventory, reserved subdomain inventory, access policy inventory, custom domain inventory, token admin operations, token validations, reserved subdomain operations, access policy operations/evaluations, custom domain operations/lookups, authorization failures, and readiness failures.

`/api/v1/status` returns JSON with the control-plane readiness state and binary version for dashboard and automation checks.

`/api/v1/events` returns recent in-memory audit events for admin users. Events are newest-first, support `?limit=N`, and omit plaintext tokens and access policy secrets.

`porthook doctor` checks gateway and control-plane operational endpoints, including `/api/v1/events` when an admin token is provided, and prints response request IDs for log correlation.

The container image includes `porthook-control-plane healthcheck`, which calls `/readyz` through the local control-plane listener. Set `PORTHOOK_HEALTHCHECK_URL` only when the listener cannot be reached through the derived localhost URL.

Control-plane logs are structured text logs written to stdout. Audit-relevant logs include an `event` field such as `control_plane.auth_failed`, `control_plane.token_created`, `control_plane.token_validated`, `control_plane.token_revoked`, `control_plane.reservation_created`, `control_plane.reservation_authorized`, `control_plane.custom_domain_created`, `control_plane.custom_domain_lookup`, `control_plane.access_policy_created`, and `control_plane.access_policy_evaluated`. Logs include method, path, remote IP, optional `request_id` from `X-Request-ID` or `X-Correlation-ID`, token IDs where available, subdomains, custom domain IDs, policy IDs, outcomes, and denial reasons. Authorization headers, plaintext token values, and access policy secrets are not logged.

## API

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

Set `PORTHOOK_CONTROL_ADMIN_TOKEN` before creating, listing, or revoking tokens. Set `PORTHOOK_CONTROL_VALIDATOR_TOKEN` and configure the same value as `PORTHOOK_CONTROL_PLANE_TOKEN` on the gateway before using control-plane token validation.

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
