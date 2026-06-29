# Control Plane

License: AGPL-3.0-only

The control plane owns users, tokens, tunnel metadata, reserved names, and limits.

Current scope is token management for self-hosted gateway authentication.

Run locally:

```sh
go run ./server/control-plane/cmd/porthook-control-plane
```

## Runtime Configuration

| Environment variable | Default | Description |
| --- | --- | --- |
| `PORTHOOK_CONTROL_ADDR` | `:8082` | Control-plane HTTP listener address. |
| `PORTHOOK_CONTROL_ADMIN_TOKEN` | empty | Bearer token required for token creation and revocation. If empty, token creation and revocation return `401 Unauthorized`. |
| `PORTHOOK_DATABASE_URL` | empty | Postgres connection URL. If empty, the process uses in-memory storage for development. |

## API

Create a token:

```sh
curl -sS -X POST http://localhost:8082/api/v1/tokens \
  -H 'Authorization: Bearer admin-secret' \
  -H 'Content-Type: application/json' \
  -d '{"name":"local agent","scopes":["register_tunnel"]}'
```

Validate a token:

```sh
curl -sS -X POST http://localhost:8082/api/v1/tokens/validate \
  -H 'Content-Type: application/json' \
  -d '{"token":"ph_...","scope":"register_tunnel"}'
```

Revoke a token:

```sh
curl -i -X DELETE http://localhost:8082/api/v1/tokens/tok_... \
  -H 'Authorization: Bearer admin-secret'
```

The token plaintext is returned only once at creation time. Storage keeps only a hash.

Set `PORTHOOK_CONTROL_ADMIN_TOKEN` before creating or revoking tokens.
