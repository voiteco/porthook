# Deployment

License: Apache-2.0 unless noted otherwise.

This directory will contain self-hosting assets:

- Docker Compose.
- Example environment files.
- Reverse proxy examples.
- TLS and wildcard DNS documentation.
- Future Helm charts.

The first supported deployment path should be Docker Compose.

The Docker Compose gateway-only and control-plane stacks are documented in [compose/README.md](./compose/README.md).

## Self-Hosted Token Flow

The gateway can run in two authentication modes:

- Static-token mode for local development: set `PORTHOOK_STATIC_TOKEN`.
- Control-plane mode for self-hosted deployments: set `PORTHOOK_CONTROL_PLANE_URL` and `PORTHOOK_CONTROL_PLANE_TOKEN` on the gateway, and run `porthook-control-plane` with `PORTHOOK_DATABASE_URL` and `PORTHOOK_CONTROL_VALIDATOR_TOKEN`.

Minimal production shape:

1. Run Postgres.
2. Run `porthook-control-plane` with `PORTHOOK_DATABASE_URL`, `PORTHOOK_CONTROL_ADMIN_TOKEN`, and `PORTHOOK_CONTROL_VALIDATOR_TOKEN`.
3. Create an agent token with `printf '%s' '<admin-token>' | porthook tokens create --control-plane <control-plane-url> --admin-token-stdin --name '<agent-name>'`.
4. Run `porthook-gateway` with `PORTHOOK_CONTROL_PLANE_URL` pointing at the control plane and `PORTHOOK_CONTROL_PLANE_TOKEN` matching the control-plane validator token.
5. Save the local agent login with `printf '%s' '<created-token>' | porthook login --server <gateway-agent-url> --token-stdin`.

The end-to-end control-plane path can be checked locally with:

```sh
make smoke-control-plane
```

The compose control-plane stack lives in [compose/docker-compose.control-plane.yml](./compose/docker-compose.control-plane.yml). Copy [compose/.env.control-plane.example](./compose/.env.control-plane.example), replace the placeholder values, and start it with Docker Compose.

Operators still need to configure wildcard DNS and TLS in front of the public gateway listener for real internet traffic.
