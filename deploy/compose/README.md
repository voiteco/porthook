# Docker Compose Deployment

This directory contains the first self-hosted Docker Compose paths.

- `docker-compose.yml` runs only `porthook-gateway` with static-token auth for the local MVP smoke path.
- `docker-compose.control-plane.yml` runs Postgres, `porthook-control-plane`, and `porthook-gateway` with control-plane token validation.

The local HTTP service and `porthook` agent still run on the host.

## Gateway-Only Stack

From the repository root:

```sh
docker compose -f deploy/compose/docker-compose.yml up --build
```

The gateway listens on:

- public HTTP: `http://localhost:8080`
- agent WebSocket: `http://localhost:8081/agent/connect`

Default development configuration:

| Environment variable | Value |
| --- | --- |
| `PORTHOOK_ADDR` | `:8080` |
| `PORTHOOK_AGENT_ADDR` | `:8081` |
| `PORTHOOK_ROOT_DOMAIN` | `localhost` |
| `PORTHOOK_PUBLIC_URL` | `http://localhost:8080` |
| `PORTHOOK_STATIC_TOKEN` | `dev-token` |
| `PORTHOOK_RATE_LIMIT_RPS` | `60` |
| `PORTHOOK_RATE_LIMIT_BURST` | `120` |

## Control-Plane Stack

Copy the example environment file and replace every `change-me` value before using it beyond local testing:

```sh
cp deploy/compose/.env.control-plane.example deploy/compose/.env.control-plane
```

If ports `8080`, `8081`, or `8082` are already in use, change `PORTHOOK_PUBLIC_PORT`, `PORTHOOK_AGENT_PORT`, and `PORTHOOK_CONTROL_PORT` in that env file.

Start Postgres, the control plane, and the gateway:

```sh
docker compose \
  --env-file deploy/compose/.env.control-plane \
  -f deploy/compose/docker-compose.control-plane.yml \
  up --build
```

The stack listens on:

- public HTTP: `http://localhost:8080`
- agent WebSocket: `http://localhost:8081/agent/connect`
- control-plane API: `http://localhost:8082`

Postgres is available only inside the Compose network as `postgres:5432`.

Create an agent token through the control plane:

```sh
curl -sS -X POST http://localhost:8082/api/v1/tokens \
  -H 'Authorization: Bearer change-me-admin-token' \
  -H 'Content-Type: application/json' \
  -d '{"name":"local agent","scopes":["register_tunnel"]}'
```

The response includes the plaintext token once. Use it with the agent:

```sh
porthook login --server http://localhost:8081 --token ph_...
porthook http 3000 --subdomain demo
```

The full binary integration path can also be checked without Docker Compose:

```sh
make smoke-control-plane
```

## Smoke Test

Start a local HTTP service on the host:

```sh
python3 -m http.server 3000 --bind 127.0.0.1
```

Start the agent from the repository root:

```sh
go run ./agent/cmd/porthook http 3000 \
  --server http://localhost:8081 \
  --token dev-token \
  --subdomain demo
```

Send a public request through the gateway:

```sh
curl -i -H 'Host: demo.localhost' http://localhost:8080/README.md
```

Expected result:

- The response body comes from the local service.
- The agent prints a one-line request log.

## Stop the Gateway

```sh
docker compose -f deploy/compose/docker-compose.yml down
```

Stop the control-plane stack:

```sh
docker compose \
  --env-file deploy/compose/.env.control-plane \
  -f deploy/compose/docker-compose.control-plane.yml \
  down
```
