# Control Plane

License: AGPL-3.0-only

The control plane owns users, tokens, tunnel metadata, reserved names, and limits.

Current scope is token management for self-hosted gateway authentication.

Supported token scopes:

- `register_tunnel`

Token APIs reject unknown scopes and malformed JSON. Token admin operations are logged without plaintext token values.

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

The dashboard can list, create, and revoke agent tokens. Plaintext agent tokens are displayed only from the create response; the control plane stores token hashes and later list responses contain token summaries only.

## Runtime Configuration

| Environment variable | Default | Description |
| --- | --- | --- |
| `PORTHOOK_CONTROL_ADDR` | `:8082` | Control-plane HTTP listener address. |
| `PORTHOOK_CONTROL_ADMIN_TOKEN` | empty | Bearer token required for token creation, listing, and revocation. If empty, those requests return `401 Unauthorized`. |
| `PORTHOOK_CONTROL_VALIDATOR_TOKEN` | empty | Bearer token required for token validation requests from the gateway. If empty, validation returns `401 Unauthorized`. |
| `PORTHOOK_DATABASE_URL` | empty | Postgres connection URL. If empty, the process uses in-memory storage for development. |

## CLI

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

## Operational Endpoints

- `GET /healthz`
- `GET /readyz`
- `GET /metrics`
- `GET /dashboard/`

`/readyz` checks the token store. For Postgres-backed deployments, it pings the configured database. Metrics use Prometheus text format and include token admin operations, token validations, authorization failures, and readiness failures.

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

The local control-plane integration path can be checked with:

```sh
make smoke-control-plane
```
