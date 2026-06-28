# Porthook

Porthook is an open-source reverse tunnel service for exposing local development services to the internet.

This repository is the public self-hosted product. Private commercial, hosted-cloud, pricing, and operating plans should live in separate private repositories.

Project status: pre-alpha. The local MVP HTTP tunnel prototype is implemented and covered by `make smoke-local`.

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
- Basic request logs for local debugging.
- Agent reconnects for transient gateway disconnects.
- A simple dashboard after the tunnel core is stable.

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

The current prototype proves the local full tunnel path:

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

Out of scope for the first MVP:

- Raw TCP and UDP tunnels.
- Multi-region routing.
- Custom domains.
- Organization management.
- Hosted-cloud operations.
- Commercial features.

## Planned Components

### Agent

The local CLI that connects to the gateway, registers tunnels, forwards requests to local ports, and displays runtime logs.

### Gateway

The public edge service that terminates HTTP or HTTPS, routes hostnames to active tunnels, and multiplexes traffic over persistent agent connections.

### Control Plane

The API layer for users, tokens, tunnel sessions, reserved names, and limits.

### Dashboard

A web UI for active tunnels, request history, tokens, and domains.

## Development Roadmap

1. Product and protocol spec.
2. Minimal Go agent and gateway over WebSocket.
3. HTTP tunnel request forwarding.
4. Token authentication and tunnel registration.
5. Docker Compose self-hosting.
6. Request logging and CLI polish.
7. Dashboard and control-plane API.

## Specification

The public product specification lives in [docs/SPEC.md](./docs/SPEC.md).

The implementation-focused technical specification lives in [docs/TECHNICAL_SPEC.md](./docs/TECHNICAL_SPEC.md).

The first Go MVP smoke test lives in [docs/MVP_SMOKE_TEST.md](./docs/MVP_SMOKE_TEST.md).

The pre-alpha release process lives in [docs/RELEASE.md](./docs/RELEASE.md).

Release notes are maintained in [CHANGELOG.md](./CHANGELOG.md).
