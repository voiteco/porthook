# MVP Smoke Test

This smoke test verifies the first local HTTP round-trip:

```text
public HTTP client -> porthook-gateway -> WebSocket tunnel -> porthook agent -> local HTTP service
```

## Prerequisites

- Go installed.
- Ports `3000`, `8080`, and `8081` available.

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

The MVP uses whole-body JSON messages over WebSocket. It is intentionally simple and should be replaced with chunked or binary body streaming after the single request path is stable.
