# Docker Compose Deployment

This directory contains the first self-hosted Docker Compose path for the gateway.

The Compose stack runs only `porthook-gateway`. The local HTTP service and `porthook` agent still run on the host during the MVP smoke test.

## Start the Gateway

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
