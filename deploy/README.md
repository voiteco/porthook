# Deployment

License: Apache-2.0 unless noted otherwise.

This directory will contain self-hosting assets:

- Docker Compose.
- Example environment files.
- Reverse proxy examples.
- TLS and wildcard DNS documentation.
- Future Helm charts.

The first supported deployment path should be Docker Compose.

The Docker Compose gateway-only, control-plane, and production stacks are documented in [compose/README.md](./compose/README.md).

Reverse proxy examples for internet-facing gateway traffic live in [reverse-proxy/README.md](./reverse-proxy/README.md). Domain, wildcard DNS, and TLS guidance lives in [../docs/DOMAINS_TLS.md](../docs/DOMAINS_TLS.md). Control-plane and dashboard access-boundary guidance lives in [../docs/ACCESS_BOUNDARY.md](../docs/ACCESS_BOUNDARY.md).

## Self-Hosted Token Flow

The gateway can run in two authentication modes:

- Static-token mode for local development: set `PORTHOOK_STATIC_TOKEN`.
- Control-plane mode for self-hosted deployments: set `PORTHOOK_CONTROL_PLANE_URL` and `PORTHOOK_CONTROL_PLANE_TOKEN` on the gateway, and run `porthook-control-plane` with `PORTHOOK_DATABASE_URL` and `PORTHOOK_CONTROL_VALIDATOR_TOKEN`.

Minimal production shape:

1. Run Postgres.
2. Run `porthook-control-plane` with `PORTHOOK_DATABASE_URL`, `PORTHOOK_CONTROL_ADMIN_TOKEN`, and `PORTHOOK_CONTROL_VALIDATOR_TOKEN`.
3. Create an agent token with `printf '%s' '<admin-token>' | porthook tokens create --control-plane <control-plane-url> --admin-token-stdin --name '<agent-name>'`.
4. Reserve any requested subdomain with `printf '%s' '<admin-token>' | porthook reserved create --control-plane <control-plane-url> --admin-token-stdin --name <subdomain> --token-id <token-id>`.
5. Run `porthook-gateway` with `PORTHOOK_CONTROL_PLANE_URL` pointing at the control plane and `PORTHOOK_CONTROL_PLANE_TOKEN` matching the control-plane validator token.
6. Save the local agent login with `printf '%s' '<created-token>' | porthook login --server <gateway-agent-url> --token-stdin`.

The control-plane process also serves the self-hosted dashboard at `/dashboard/` on the control-plane listener. It uses the same admin token as the token admin API.

The end-to-end control-plane path can be checked locally with:

```sh
make smoke-control-plane
```

The compose control-plane stack lives in [compose/docker-compose.control-plane.yml](./compose/docker-compose.control-plane.yml). Copy [compose/.env.control-plane.example](./compose/.env.control-plane.example), replace the placeholder values, and start it with Docker Compose.

For reverse-proxy-backed deployments, use [compose/docker-compose.production.yml](./compose/docker-compose.production.yml) with [compose/.env.production.example](./compose/.env.production.example). That stack exposes only the reverse proxy on the host and keeps the gateway, control plane, and Postgres on the Compose network.

Operators still need to configure wildcard DNS and TLS in front of the public gateway listener for real internet traffic. Start with [../docs/DOMAINS_TLS.md](../docs/DOMAINS_TLS.md) and the Caddy or Traefik examples in [reverse-proxy/](./reverse-proxy/).

## Production Checklist

Before using the Compose control-plane stack beyond local testing:

- Replace every `change-me` value with a generated secret. Use separate values for `PORTHOOK_POSTGRES_PASSWORD`, `PORTHOOK_CONTROL_ADMIN_TOKEN`, and `PORTHOOK_CONTROL_VALIDATOR_TOKEN`.
- Keep `.env.control-plane` out of Git. The repository ignores `.env.*` files except checked-in examples.
- Configure `PORTHOOK_ROOT_DOMAIN` for the wildcard domain routed to the public gateway.
- Configure `PORTHOOK_PUBLIC_URL` to the externally reachable public gateway URL.
- Put TLS termination in front of the public gateway listener.
- Confirm certificates cover the wildcard tunnel domain and the dedicated agent/control hostnames.
- Restrict access to the control-plane API with network policy, firewall rules, reverse-proxy rules, or a private listener. The admin token protects API actions but should not be the only operational boundary.
- Treat `/dashboard/` as part of the control-plane API surface and protect it with the same access boundary.
- Persist and back up the Postgres volume before relying on issued tokens.
- Back up Postgres before upgrades. The control plane applies pending embedded migrations at startup.
- Scrape `/metrics` and alert on readiness failures, auth failures, and unexpected token validation errors.
- Rotate admin and validator tokens periodically, then update gateway and control-plane configuration together.
- Revoke unused agent tokens with `porthook tokens revoke`.
- Review reserved subdomains with `porthook reserved list` and delete unused names with `porthook reserved delete`.
- Run `make smoke-control-plane` after configuration changes that affect token validation or tunnel routing.

Compose is still the first supported deployment path for this pre-1.0 repository. Review the reverse proxy, TLS, wildcard DNS, and access-boundary notes before using it for internet-facing traffic.

Upgrade notes live in [../docs/UPGRADING.md](../docs/UPGRADING.md).
