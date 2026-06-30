# Control-Plane Access Boundary

The self-hosted control plane serves administrative APIs and the dashboard. It is protected by `PORTHOOK_CONTROL_ADMIN_TOKEN`, but that token is not a complete production access boundary by itself.

For internet-facing deployments, place the control-plane hostname behind at least one external boundary:

- private network or VPN
- firewall or load balancer allowlist
- reverse-proxy IP allowlist
- reverse-proxy authentication
- SSO or identity-aware proxy
- mutual TLS

The public gateway listener is designed for internet traffic. The control-plane API and dashboard are not.

## Surfaces

| Surface | Production exposure |
| --- | --- |
| Wildcard tunnel traffic | Public, routed to the gateway public listener. |
| Agent WebSocket hostname | Public or restricted to agent networks, routed to the gateway agent listener. |
| Control-plane API | Private or externally protected. |
| Dashboard | Same boundary as the control-plane API. |

The dashboard stores the admin token in browser session storage for the current tab and sends it to the control-plane API as a bearer token. Always use HTTPS and avoid exposing the dashboard on a public hostname without an additional boundary.

## Production Compose

The production Compose stack exposes only the reverse proxy on host ports `80` and `443`. The gateway, control plane, and Postgres are private to the Compose network.

By default, the production stack mounts:

```text
PORTHOOK_CADDYFILE_PATH=../reverse-proxy/caddy/Caddyfile
```

That default Caddyfile includes a route for `PORTHOOK_CONTROL_DOMAIN`. If `PORTHOOK_CONTROL_DOMAIN` resolves publicly, add an external boundary before relying on the stack for production.

For a Caddy IP allowlist example, set:

```text
PORTHOOK_CADDYFILE_PATH=../reverse-proxy/caddy/Caddyfile.control-allowlist
PORTHOOK_CONTROL_ALLOWED_IPS="203.0.113.0/24 2001:db8:1234::/48"
```

Then restart the reverse proxy:

```sh
docker compose \
  --env-file deploy/compose/.env.production \
  -f deploy/compose/docker-compose.production.yml \
  up -d reverse-proxy
```

Use CIDR ranges that represent operator networks, VPN egress ranges, or identity-aware proxy egress ranges. Do not use broad public ranges as placeholders in real deployments.

## Boundary Checks

After starting the stack, check the control-plane hostname from an allowed network:

```sh
curl -i https://control.example.com/healthz
```

Check from an untrusted network or a machine outside the VPN:

```sh
curl -i https://control.example.com/healthz
```

Expected result with the allowlist example:

- allowed network: `200 OK`
- untrusted network: `403 Forbidden`

Also confirm the dashboard is covered by the same boundary:

```sh
curl -i https://control.example.com/dashboard/
```

## Token Handling

- Generate separate values for `PORTHOOK_CONTROL_ADMIN_TOKEN` and `PORTHOOK_CONTROL_VALIDATOR_TOKEN`.
- Do not reuse agent tokens as admin or validator tokens.
- Keep generated `.env.*` files out of Git.
- Rotate the admin token after operator turnover or suspected exposure.
- Rotate the validator token if gateway or control-plane configuration is exposed.

Changing the validator token requires updating both the gateway and control plane together. Changing the admin token affects CLI token administration and dashboard login.
