# MVP Smoke Test

This smoke test verifies the first local HTTP round-trip:

```text
public HTTP client -> porthook-gateway -> WebSocket tunnel -> porthook agent -> local HTTP service
```

## Prerequisites

- Go installed.
- Ports `13000`, `18080`, and `18081` available for the automated test.
- Ports `3000`, `8080`, and `8081` available for the manual steps.

## Automated Smoke Test

From the repository root:

```sh
make smoke-local
```

The automated smoke test builds local binaries, starts a temporary local HTTP fixture, starts `porthook-gateway`, starts the `porthook` agent, sends a public request through the tunnel, verifies the response body, and cleans up all started processes.

Default automated ports can be overridden with:

- `PORTHOOK_SMOKE_LOCAL_PORT`
- `PORTHOOK_SMOKE_PUBLIC_PORT`
- `PORTHOOK_SMOKE_AGENT_PORT`

The manual steps below are useful when debugging the individual processes.

## 1. Start a Local HTTP Service

From the repository root:

```sh
python3 -m http.server 3000 --bind 127.0.0.1
```

This serves repository files from `http://localhost:3000`.

## 2. Start the Gateway

In another terminal:

```sh
go run ./server/gateway/cmd/porthook-gateway
```

Alternatively, start the gateway with Docker Compose:

```sh
docker compose -f deploy/compose/docker-compose.yml up --build
```

Default gateway configuration:

- public HTTP listener: `:8080`
- agent WebSocket listener: `:8081`
- root domain: `localhost`
- static agent token: `dev-token`

## 3. Start the Agent

In another terminal:

```sh
go run ./agent/cmd/porthook http 3000 --server http://localhost:8081 --token dev-token --subdomain demo
```

Expected output:

```text
Tunnel established
Forwarding: http://demo.localhost:8080 -> http://localhost:3000
```

## 4. Send a Public Request

In another terminal:

```sh
curl -i -H 'Host: demo.localhost' http://localhost:8080/README.md
```

Expected result:

- HTTP status from the local service.
- Response body from `README.md`.
- Agent prints a one-line request log.

## Notes

The MVP uses JSON messages over WebSocket with chunked `start/body/end` frames for HTTP bodies. The current transport is intentionally simple and can move to binary chunks later without changing the public CLI flow.

The Docker Compose path is documented in [../deploy/compose/README.md](../deploy/compose/README.md).
