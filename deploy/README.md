# Deployment

License: Apache-2.0 unless noted otherwise.

This directory will contain self-hosting assets:

- Docker Compose.
- Example environment files.
- Reverse proxy examples.
- TLS and wildcard DNS documentation.
- Future Helm charts.

The first supported deployment path should be Docker Compose.

The current Docker Compose gateway smoke path is documented in [compose/README.md](./compose/README.md).

## Self-Hosted Token Flow

The gateway can run in two authentication modes:

- Static-token mode for local development: set `PORTHOOK_STATIC_TOKEN`.
- Control-plane mode for self-hosted deployments: set `PORTHOOK_CONTROL_PLANE_URL` on the gateway and run `porthook-control-plane` with `PORTHOOK_DATABASE_URL`.

Minimal production shape:

1. Run Postgres.
2. Run `porthook-control-plane` with `PORTHOOK_DATABASE_URL` and `PORTHOOK_CONTROL_ADMIN_TOKEN`.
3. Create an agent token through `POST /api/v1/tokens`.
4. Run `porthook-gateway` with `PORTHOOK_CONTROL_PLANE_URL` pointing at the control plane.
5. Run the local agent with `porthook login --server <gateway-agent-url> --token <created-token>`.

Operators still need to configure wildcard DNS and TLS in front of the public gateway listener for real internet traffic.
