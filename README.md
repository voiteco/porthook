# Porthook

Porthook is an open-source reverse tunnel service for exposing local development services to the internet.

This repository is the public self-hosted product. Private commercial, hosted-cloud, pricing, and operating plans should live in separate private repositories.

Project status: pre-1.0. The local HTTP tunnel path, self-hosted control-plane token management, gateway token validation, reserved subdomains, access policies, custom domain mappings, operational endpoints, and Docker Compose smoke paths are implemented. Public APIs and operational defaults can still change before 1.0.

## What It Does

Porthook lets a developer run a local agent:

```sh
porthook http 3000
```

The agent creates an outbound tunnel to a public gateway. The gateway receives public HTTP traffic and forwards it through the tunnel to the developer's local service:

```text
Public client
    |
    v
https://random.porthook.example
    |
    v
Porthook gateway
    |
    v
Persistent tunnel
    |
    v
Local agent -> localhost:3000
```

## Product Direction

The first product shape is intentionally narrow:

- HTTP and HTTPS tunnels first.
- Self-hosted gateway with Docker Compose.
- CLI agent for macOS, Linux, and Windows.
- Wildcard subdomain routing.
- Token-based tunnel authentication.
- Optional control-plane token validation for self-hosted deployments.
- Reserved subdomains, access policies, and custom domain mappings for control-plane-backed deployments.
- Health, readiness, and Prometheus text metrics endpoints.
- Optional OpenTelemetry traces for gateway and control-plane HTTP paths.
- Basic request logs for local debugging.
- Agent reconnects for transient gateway disconnects.
- A self-hosted dashboard for control-plane administration.

This public repository should stay focused on the self-hosted open-source product.

## Repository Layout

```text
.
|-- agent/             # CLI agent. Apache-2.0.
|-- protocol/          # Shared protocol and SDK packages. Apache-2.0.
|-- server/            # Gateway, control plane, and dashboard. AGPL-3.0-only.
|   |-- control-plane/
|   |-- dashboard/
|   `-- gateway/
|-- deploy/            # Self-hosting assets.
|-- docs/              # Product and technical documentation.
|-- LICENSE.md         # Repository licensing map.
`-- README.md
```

## Licensing

Porthook uses a split-license model:

- `server/` is licensed under AGPL-3.0-only.
- `agent/` and `protocol/` are licensed under Apache-2.0.
- Documentation and deployment examples follow the license declared in their file headers or directory README.

The goal is to keep the agent and protocol easy to adopt while protecting the network-accessible server core from closed hosted forks.

See [LICENSE.md](./LICENSE.md) for the current licensing map.

See [docs/REPOSITORY_BOUNDARY.md](./docs/REPOSITORY_BOUNDARY.md) for what belongs in this public repository and what should stay private.

## MVP Scope

The current prototype proves the local and self-hosted control-plane tunnel paths:

1. A user starts a local HTTP service.
2. The user runs `porthook http 3000`.
3. The agent authenticates to the gateway with a token.
4. The gateway assigns or accepts a subdomain.
5. Public HTTP requests are forwarded to the local service.
6. The agent prints request logs and the public URL.

Run the local smoke test from the repository root:

```sh
make smoke-local
```

Run the control-plane-backed smoke test:

```sh
make smoke-control-plane
```

Out of scope for the first MVP:

- Raw TCP and UDP tunnels.
- Multi-region routing.
- Organization management.
- Hosted-cloud operations.
- Commercial features.

## Planned Components

### Agent

The local CLI that connects to the gateway, registers tunnels, forwards requests to local ports, and displays runtime logs.

### Gateway

The public edge service that terminates HTTP or HTTPS, routes hostnames to active tunnels, and multiplexes traffic over persistent agent connections.

### Control Plane

The API layer for users, tokens, tunnel sessions, reserved names, custom domain mappings, and limits. The current implementation includes `porthook-control-plane` for self-hosted token creation, validation, revocation, reserved subdomain ownership, access policies, and custom domains.

### Dashboard

A self-hosted web UI for control-plane administration. The current dashboard is served by `porthook-control-plane` at `/dashboard/` and supports admin token login, token administration, reserved subdomain administration, custom domain management, access policy management, operational charts, active gateway tunnel visibility, diagnostics, audit events, runtime and metrics views, gateway request logs, and operational JSON export.

## Development Roadmap

Completed foundations:

1. Product and protocol specs.
2. Minimal Go agent and gateway over WebSocket.
3. HTTP tunnel request forwarding.
4. Token authentication and tunnel registration.
5. Docker Compose self-hosting.
6. Request logging, reconnects, keepalives, timeouts, and CLI polish.
7. Self-hosted control-plane token API and CLI token helpers.
8. Self-hosted dashboard for token administration.
9. Reserved subdomains for requested tunnel names.
10. Per-tunnel public access policies for reserved subdomains.
11. Gateway request log visibility.
12. Per-tunnel custom domain mappings for self-hosted deployments.
13. OpenTelemetry trace export for gateway and control-plane HTTP paths.
14. Dashboard operational overview charts.
15. Operational JSON export from the CLI and dashboard.

Next major public work:

1. Deeper operational views.

## Self-Hosted Quick Start

The quickest control-plane-backed local stack uses Docker Compose:

```sh
cp deploy/compose/.env.control-plane.example deploy/compose/.env.control-plane
```

Replace every `change-me` value in `deploy/compose/.env.control-plane`. Generate separate local secrets for the Postgres password, control-plane admin token, and gateway validator token:

```sh
openssl rand -base64 32
```

Start Postgres, the control plane, and the gateway:

```sh
docker compose \
  --env-file deploy/compose/.env.control-plane \
  -f deploy/compose/docker-compose.control-plane.yml \
  up --build
```

Open the self-hosted dashboard:

```text
http://localhost:8082/dashboard/
```

Log in with the configured control-plane admin token. The dashboard can create, list, and revoke agent tokens, reserve subdomains for tokens, manage custom domains and access policies, show active gateway tunnels, runtime, and metrics, run diagnostics, inspect audit events, inspect recent gateway request logs, and download an operational JSON export.

Check the local gateway and control-plane operational endpoints:

```sh
printf '%s' '<admin-token>' | porthook doctor \
  --gateway http://localhost:8080 \
  --control-plane http://localhost:8082 \
  --admin-token-stdin
```

Capture a best-effort operational JSON export:

```sh
printf '%s' '<admin-token>' | porthook export \
  --gateway http://localhost:8080 \
  --control-plane http://localhost:8082 \
  --admin-token-stdin \
  --output porthook-operational-export.json
```

Inspect active tunnels from the CLI:

```sh
porthook tunnels list --gateway http://localhost:8080
```

Create an agent token:

```sh
created_token_json="$(printf '%s' '<admin-token>' | porthook tokens create \
  --control-plane http://localhost:8082 \
  --admin-token-stdin \
  --name 'local agent' \
  --json)"
agent_token_id="$(printf '%s' "${created_token_json}" | python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])')"
agent_token="$(printf '%s' "${created_token_json}" | python3 -c 'import json,sys; print(json.load(sys.stdin)["token"])')"
```

Reserve the requested subdomain for that token:

```sh
reservation_json="$(printf '%s' '<admin-token>' | porthook reserved create \
  --control-plane http://localhost:8082 \
  --admin-token-stdin \
  --name demo \
  --token-id "${agent_token_id}" \
  --json)"
reservation_id="$(printf '%s' "${reservation_json}" | python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])')"
```

Optionally map a custom hostname to that reserved subdomain:

```sh
domain_json="$(printf '%s' '<admin-token>' | porthook domains create \
  --control-plane http://localhost:8082 \
  --admin-token-stdin \
  --hostname demo.example.test \
  --reserved-subdomain-id "${reservation_id}" \
  --json)"
domain_id="$(printf '%s' "${domain_json}" | python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])')"
verification_name="$(printf '%s' "${domain_json}" | python3 -c 'import json,sys; print(json.load(sys.stdin)["verification_name"])')"
verification_token="$(printf '%s' "${domain_json}" | python3 -c 'import json,sys; print(json.load(sys.stdin)["verification_token"])')"
```

Create a TXT record named `${verification_name}` with value `porthook-domain-verification=${verification_token}`, then activate the mapping:

```sh
printf '%s' '<admin-token>' | porthook domains verify \
  --control-plane http://localhost:8082 \
  --admin-token-stdin \
  "${domain_id}"
```

Optionally protect that reserved subdomain before exposing it publicly:

```sh
printf '%s' '<admin-token>' | porthook access create \
  --control-plane http://localhost:8082 \
  --admin-token-stdin \
  --reserved-subdomain-id "${reservation_id}" \
  --mode basic_auth \
  --basic-username demo \
  --basic-password '<gateway-password>'
```

Start a local service and tunnel it:

```sh
python3 -m http.server 3000 --bind 127.0.0.1
printf '%s' "${agent_token}" | porthook login --server http://localhost:8081 --token-stdin
porthook http 3000 --subdomain demo
```

Then request the public gateway. Without an access policy, the plain request should reach the local service:

```sh
curl -i -H 'Host: demo.localhost' http://localhost:8080/
curl -i -H 'Host: demo.example.test' http://localhost:8080/
```

With the optional basic-auth access policy above, use:

```sh
curl -i -u demo:'<gateway-password>' -H 'Host: demo.localhost' http://localhost:8080/
curl -i -u demo:'<gateway-password>' -H 'Host: demo.example.test' http://localhost:8080/
```

The gateway public listener and control plane both expose `GET /healthz`, `GET /readyz`, and `GET /metrics` for local operations checks. `porthook doctor` runs the common gateway and control-plane checks and includes response request IDs for log correlation. See [deploy/compose/README.md](./deploy/compose/README.md) for the full Compose guide.

For internet-facing self-hosted deployments, see [docs/DOMAINS_TLS.md](./docs/DOMAINS_TLS.md) for wildcard DNS, custom root domain, and TLS guidance. See [docs/ACCESS_BOUNDARY.md](./docs/ACCESS_BOUNDARY.md) before exposing the control-plane API or dashboard.

Operational runbooks for deployment, backups, token rotation, upgrades, rollback, and incident response live in [docs/OPERATIONS.md](./docs/OPERATIONS.md).

Metrics and OpenTelemetry tracing configuration are documented in [docs/OBSERVABILITY.md](./docs/OBSERVABILITY.md).

## Specification

The public product specification lives in [docs/SPEC.md](./docs/SPEC.md).

The implementation-focused technical specification lives in [docs/TECHNICAL_SPEC.md](./docs/TECHNICAL_SPEC.md).

The first Go MVP smoke test lives in [docs/MVP_SMOKE_TEST.md](./docs/MVP_SMOKE_TEST.md).

The release process lives in [docs/RELEASE.md](./docs/RELEASE.md).

Upgrade notes live in [docs/UPGRADING.md](./docs/UPGRADING.md).

Release notes are maintained in [CHANGELOG.md](./CHANGELOG.md).
