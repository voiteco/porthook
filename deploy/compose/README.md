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

Generate separate values for the Postgres password, control-plane admin token, and gateway validator token:

```sh
openssl rand -base64 32
```

If OpenSSL is not available, use:

```sh
python3 -c 'import secrets; print(secrets.token_urlsafe(32))'
```

If ports `8080`, `8081`, or `8082` are already in use, change `PORTHOOK_PUBLIC_PORT`, `PORTHOOK_AGENT_PORT`, and `PORTHOOK_CONTROL_PORT` in that env file.

Required local environment values:

| Environment variable | Purpose |
| --- | --- |
| `PORTHOOK_POSTGRES_PASSWORD` | Password for the Compose Postgres user and database URL. |
| `PORTHOOK_CONTROL_ADMIN_TOKEN` | Admin bearer token for token creation, listing, and revocation. |
| `PORTHOOK_CONTROL_VALIDATOR_TOKEN` | Shared bearer token used by the gateway when validating agent tokens through the control plane. |
| `PORTHOOK_ROOT_DOMAIN` | Root domain used for tunnel subdomains. Use `localhost` for local smoke testing. |
| `PORTHOOK_PUBLIC_URL` | Public base URL printed by the gateway when a tunnel is registered. |
| `PORTHOOK_PUBLIC_PORT` | Host port mapped to the gateway public HTTP listener. |
| `PORTHOOK_AGENT_PORT` | Host port mapped to the gateway agent WebSocket listener. |
| `PORTHOOK_CONTROL_PORT` | Host port mapped to the control-plane API listener. |

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
- dashboard: `http://localhost:8082/dashboard/`

Postgres is available only inside the Compose network as `postgres:5432`.

## Dashboard

Open the dashboard after the control-plane stack starts:

```text
http://localhost:8082/dashboard/
```

Use the configured `PORTHOOK_CONTROL_ADMIN_TOKEN` to log in. The dashboard stores the admin token in browser session storage for the current tab and sends it to the control-plane API as a bearer token.

The dashboard can create, list, and revoke agent tokens. The plaintext agent token is displayed only from the create response. Token tables include creation time, last successful validation time, and revocation status.

Create an agent token through the control plane:

```sh
printf '%s' '<admin-token>' | porthook tokens create \
  --control-plane http://localhost:8082 \
  --admin-token-stdin \
  --name 'local agent'
```

The output includes the plaintext token once. Use it with the agent:

```sh
printf '%s' 'ph_...' | porthook login --server http://localhost:8081 --token-stdin
porthook http 3000 --subdomain demo
```

The full binary integration path can also be checked without Docker Compose:

```sh
make smoke-control-plane
```

## Upgrades

Back up the `postgres-data` volume before upgrading a Postgres-backed control-plane stack. The control plane applies pending embedded migrations at startup and records them in `schema_migrations`.

For the 0.5.x line, the migrations are additive for token storage and add `last_used_at` metadata for token list views. See [../../docs/UPGRADING.md](../../docs/UPGRADING.md).

## Operational Endpoints

The Compose stack exposes:

| Service | URL |
| --- | --- |
| Gateway health | `http://localhost:8080/healthz` |
| Gateway readiness | `http://localhost:8080/readyz` |
| Gateway metrics | `http://localhost:8080/metrics` |
| Control-plane health | `http://localhost:8082/healthz` |
| Control-plane readiness | `http://localhost:8082/readyz` |
| Control-plane metrics | `http://localhost:8082/metrics` |
| Dashboard | `http://localhost:8082/dashboard/` |

Gateway metrics include active tunnels, public request counts, token validation attempts, auth failures, and successful tunnel registrations. Control-plane metrics include token admin operations, token validation results, auth failures, and readiness failures.

The control-plane `/readyz` endpoint checks the token store. In this Compose stack, that means it pings Postgres.

## Internet-Facing Notes

For real internet traffic, update `PORTHOOK_ROOT_DOMAIN` and `PORTHOOK_PUBLIC_URL`, point wildcard DNS at the public gateway, and terminate TLS in front of the public listener. Keep the control-plane API and dashboard private or protected by an additional access boundary.

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
