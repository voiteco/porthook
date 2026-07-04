# Docker Compose Deployment

This directory contains the first self-hosted Docker Compose paths.

- `docker-compose.yml` runs only `porthook-gateway` with static-token auth for the local MVP smoke path.
- `docker-compose.control-plane.yml` runs Postgres, `porthook-control-plane`, and `porthook-gateway` with control-plane token validation.
- `docker-compose.production.yml` runs Postgres, `porthook-control-plane`, `porthook-gateway`, and Caddy for TLS termination.

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

Generate separate values for the Postgres password, bootstrap control-plane admin token, and gateway validator token:

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
| `PORTHOOK_CONTROL_ADMIN_TOKEN` | Full-scope bootstrap/recovery admin bearer token. Use it to create scoped admin tokens for routine operations. |
| `PORTHOOK_CONTROL_VALIDATOR_TOKEN` | Shared bearer token used by the gateway when validating agent tokens through the control plane. |
| `PORTHOOK_ROOT_DOMAIN` | Root domain used for tunnel subdomains. Use `localhost` for local smoke testing. |
| `PORTHOOK_PUBLIC_URL` | Public base URL printed by the gateway when a tunnel is registered. |
| `PORTHOOK_CUSTOM_DOMAIN_CACHE_TTL` | Gateway cache TTL for active custom-domain mappings. |
| `PORTHOOK_CUSTOM_DOMAIN_MISS_TTL` | Gateway cache TTL for custom-domain misses and pending verification states. |
| `PORTHOOK_PUBLIC_PORT` | Host port mapped to the gateway public HTTP listener. |
| `PORTHOOK_AGENT_PORT` | Host port mapped to the gateway agent WebSocket listener. |
| `PORTHOOK_CONTROL_PORT` | Host port mapped to the control-plane API listener. |

Optional OpenTelemetry values in the example env file can enable gateway and control-plane trace export. Leave `PORTHOOK_OTEL_ENABLED=false` unless an OTLP collector or stdout trace debugging is intentionally configured. See [../../docs/OBSERVABILITY.md](../../docs/OBSERVABILITY.md).

The control-plane and production stacks configure `PORTHOOK_REQUEST_LOG_DATABASE_URL` for the gateway, so public request summaries are stored in Postgres and served from the durable request-log endpoint.

Start Postgres, the control plane, and the gateway:

```sh
make compose-up
```

Use `make compose-up-detached` to start the same stack in the background, `make compose-ps` to inspect container health, `make compose-logs` to follow service logs, and `make compose-down` to stop it. These targets use `deploy/compose/.env.control-plane` and `deploy/compose/docker-compose.control-plane.yml` by default.

The stack listens on:

- public HTTP: `http://localhost:8080`
- agent WebSocket: `http://localhost:8081/agent/connect`
- control-plane API: `http://localhost:8082`
- dashboard: `http://localhost:8082/dashboard/`

Postgres is available only inside the Compose network as `postgres:5432`.
The gateway and control-plane containers define readiness healthchecks, and the gateway waits for a healthy control plane before starting.

## Production Stack

The production Compose stack keeps the gateway and control-plane ports private to the Compose network and exposes only the Caddy reverse proxy on ports `80` and `443`.

Read [../../docs/DOMAINS_TLS.md](../../docs/DOMAINS_TLS.md) before using this stack for internet-facing traffic.
Read [../../docs/ACCESS_BOUNDARY.md](../../docs/ACCESS_BOUNDARY.md) before exposing `PORTHOOK_CONTROL_DOMAIN`.
Use [../../docs/OPERATIONS.md](../../docs/OPERATIONS.md) for backup, restore, upgrade, rollback, and token rotation runbooks.
Use [../../docs/OBSERVABILITY.md](../../docs/OBSERVABILITY.md) for Prometheus metrics and OpenTelemetry tracing configuration.

Copy the production example environment and replace every `change-me` value:

```sh
cp deploy/compose/.env.production.example deploy/compose/.env.production
```

Configure these public names:

| Environment variable | Purpose |
| --- | --- |
| `PORTHOOK_ROOT_DOMAIN` | Wildcard tunnel domain, for example `tunnels.example.com`. |
| `PORTHOOK_PUBLIC_URL` | Public URL printed for registered tunnels, for example `https://tunnels.example.com`. |
| `PORTHOOK_CUSTOM_DOMAIN_CACHE_TTL` | Gateway cache TTL for active custom-domain mappings. |
| `PORTHOOK_CUSTOM_DOMAIN_MISS_TTL` | Gateway cache TTL for custom-domain misses and pending verification states. |
| `PORTHOOK_AGENT_DOMAIN` | Dedicated hostname agents use for WebSocket connections. |
| `PORTHOOK_CONTROL_DOMAIN` | Dedicated hostname for the control-plane API and dashboard. |
| `PORTHOOK_CADDYFILE_PATH` | Caddyfile mounted into the reverse proxy. |
| `PORTHOOK_CONTROL_ALLOWED_IPS` | CIDR allowlist used by `Caddyfile.control-allowlist`. |
| `PORTHOOK_TLS_CERT_PATH` | Absolute host path to the wildcard certificate mounted into Caddy. |
| `PORTHOOK_TLS_KEY_PATH` | Absolute host path to the wildcard private key mounted into Caddy. |

Before starting the stack:

- Point wildcard DNS for `*.${PORTHOOK_ROOT_DOMAIN}` at the host running the reverse proxy.
- Point `PORTHOOK_AGENT_DOMAIN` at the same host.
- Keep `PORTHOOK_CONTROL_DOMAIN` private or protect it with a separate access boundary.
- Mount a certificate that covers `*.${PORTHOOK_ROOT_DOMAIN}`, `PORTHOOK_AGENT_DOMAIN`, and `PORTHOOK_CONTROL_DOMAIN`.
- For custom domains, point each custom hostname at the same public edge and make sure the reverse proxy has a certificate for that hostname.

To use the checked-in Caddy IP allowlist example for the control-plane hostname:

```text
PORTHOOK_CADDYFILE_PATH=../reverse-proxy/caddy/Caddyfile.control-allowlist
PORTHOOK_CONTROL_ALLOWED_IPS="203.0.113.0/24 2001:db8:1234::/48"
```

Start the production stack:

```sh
docker compose \
  --env-file deploy/compose/.env.production \
  -f deploy/compose/docker-compose.production.yml \
  up --build -d
```

The stack listens externally on:

- public HTTPS tunnels: `https://<subdomain>.<PORTHOOK_ROOT_DOMAIN>`
- agent WebSocket endpoint: `https://<PORTHOOK_AGENT_DOMAIN>/agent/connect`
- control-plane API and dashboard: `https://<PORTHOOK_CONTROL_DOMAIN>`

The gateway, control plane, and Postgres are only reachable inside the Compose network.
The gateway and control-plane services expose container healthchecks, and the reverse proxy waits for both services to become healthy.

## Dashboard

Open the dashboard after the control-plane stack starts:

```text
http://localhost:8082/dashboard/
```

Use the configured bootstrap token or a scoped admin token to log in. The dashboard stores the admin token in browser session storage for the current tab and sends it to the control-plane API as a bearer token.

The dashboard can create, list, and revoke scoped admin tokens and agent tokens, reserve requested subdomains for tokens, manage custom domains and access policies, and show active gateway tunnels and request logs from the gateway public API. Plaintext admin and agent tokens are displayed only from create responses. Token tables include creation time, last successful validation time, and revocation status.

Create a scoped admin token for routine use:

```sh
admin_token_json="$(printf '%s' '<bootstrap-admin-token>' | porthook admin tokens create \
  --control-plane http://localhost:8082 \
  --admin-token-stdin \
  --name 'local operator' \
  --scope tokens \
  --scope reservations \
  --scope audit_history \
  --json)"
admin_token="$(printf '%s' "${admin_token_json}" | python3 -c 'import json,sys; print(json.load(sys.stdin)["token"])')"
```

Create an agent token through the control plane:

```sh
created_token_json="$(printf '%s' "${admin_token}" | porthook tokens create \
  --control-plane http://localhost:8082 \
  --admin-token-stdin \
  --name 'local agent' \
  --json)"
agent_token_id="$(printf '%s' "${created_token_json}" | python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])')"
agent_token="$(printf '%s' "${created_token_json}" | python3 -c 'import json,sys; print(json.load(sys.stdin)["token"])')"
```

Reserve a requested subdomain for the token:

```sh
reservation_json="$(printf '%s' "${admin_token}" | porthook reserved create \
  --control-plane http://localhost:8082 \
  --admin-token-stdin \
  --name demo \
  --token-id "${agent_token_id}" \
  --json)"
reservation_id="$(printf '%s' "${reservation_json}" | python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])')"
```

Optionally map a custom hostname to the reserved subdomain:

```sh
printf '%s' '<admin-token-with-domains-scope>' | porthook domains create \
  --control-plane http://localhost:8082 \
  --admin-token-stdin \
  --hostname demo.example.test \
  --reserved-subdomain-id "${reservation_id}"
```

Use the plaintext token with the agent:

```sh
printf '%s' "${agent_token}" | porthook login --server http://localhost:8081 --token-stdin
porthook http 3000 --subdomain demo
```

The full binary integration path can also be checked without Docker Compose:

```sh
make smoke-control-plane
```

## Upgrades

Back up the `postgres-data` volume before upgrading a Postgres-backed control-plane stack. The control plane applies pending embedded migrations at startup and records them in `schema_migrations`.

For the 0.9.x line, the migrations are additive and create `custom_domains` for per-tunnel custom domain mappings. See [../../docs/UPGRADING.md](../../docs/UPGRADING.md).

## Operational Endpoints

The Compose stack exposes:

| Service | URL |
| --- | --- |
| Gateway health | `http://localhost:8080/healthz` |
| Gateway readiness | `http://localhost:8080/readyz` |
| Gateway metrics | `http://localhost:8080/metrics` |
| Gateway active tunnels | `http://localhost:8080/api/v1/tunnels` |
| Control-plane health | `http://localhost:8082/healthz` |
| Control-plane readiness | `http://localhost:8082/readyz` |
| Control-plane metrics | `http://localhost:8082/metrics` |
| Control-plane status | `http://localhost:8082/api/v1/status` |
| Dashboard | `http://localhost:8082/dashboard/` |

Gateway metrics include active tunnels, public request counts, custom domain lookup results, token validation attempts, auth failures, and successful tunnel registrations. The gateway active-tunnels JSON endpoint is read-only and omits local target URLs. Control-plane metrics include token admin operations, token validation results, reserved subdomain operations, custom domain operations, auth failures, and readiness failures.

The control-plane `/readyz` endpoint checks the token, reservation, access policy, and custom domain stores. In this Compose stack, that means it pings Postgres.

The gateway and control-plane images include built-in `healthcheck` subcommands, so the Compose healthchecks work in the scratch runtime images without adding curl or a shell. Use `docker compose ps` to inspect container health and `porthook doctor` for an external operator-facing check.

The gateway and control-plane binaries also include `configcheck` subcommands for validating effective environment configuration:

```sh
porthook-gateway configcheck
porthook-gateway configcheck --production
porthook-control-plane configcheck
porthook-control-plane configcheck --production
```

From a source checkout, `make configcheck` validates local development defaults and `make configcheck-production` validates a production-shaped example configuration with generated placeholder-safe values.

## Internet-Facing Notes

For real internet traffic, update `PORTHOOK_ROOT_DOMAIN` and `PORTHOOK_PUBLIC_URL`, point wildcard DNS at the public gateway, and terminate TLS in front of the public listener. For custom domains, point the exact custom hostnames at the public gateway edge and serve certificates for those names. Keep the control-plane API and dashboard private or protected by an additional access boundary.

Reverse proxy examples for Caddy and Traefik live in [../reverse-proxy/](../reverse-proxy/).

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
make compose-down
```

Stop the production stack:

```sh
docker compose \
  --env-file deploy/compose/.env.production \
  -f deploy/compose/docker-compose.production.yml \
  down
```
