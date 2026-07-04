# Control-Plane Access Boundary

The self-hosted control plane serves administrative APIs and the dashboard. It is protected by a full-scope bootstrap token in `PORTHOOK_CONTROL_ADMIN_TOKEN` and by scoped admin tokens created through the control plane. These tokens are not a complete production access boundary by themselves.

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
| Gateway operational APIs | Public unless externally protected with the gateway hostname. |
| Control-plane API | Private or externally protected. |
| Dashboard | Same boundary as the control-plane API. |

The dashboard stores the admin token in browser session storage for the current tab and sends it to the control-plane API as a bearer token. The token can be the bootstrap token or a scoped admin token. Always use HTTPS and avoid exposing the dashboard on a public hostname without an additional boundary.

Gateway operational APIs such as `GET /api/v1/tunnels`, `GET /api/v1/runtime`, `GET /metrics`, and `GET /api/v1/request-logs` are served on the gateway public listener for self-hosted dashboard visibility. Request log responses omit raw query strings and authorization values, and runtime responses omit tokens, local targets, and control-plane URLs, but these endpoints still include operational metadata such as paths, statuses, outcomes, counters, limits, and remote IPs. Protect or route these endpoints externally if that metadata should not be public in your deployment.

## Production Compose

The production Compose stack exposes only the reverse proxy on host ports `80` and `443`. The gateway, control plane, and Postgres are private to the Compose network.

By default, the production stack mounts:

```text
PORTHOOK_CADDYFILE_PATH=../reverse-proxy/caddy/Caddyfile
```

That default Caddyfile includes a route for `PORTHOOK_CONTROL_DOMAIN`. If `PORTHOOK_CONTROL_DOMAIN` resolves publicly, add an external boundary before relying on the stack for production.

The Caddy examples set `X-Content-Type-Options`, `X-Frame-Options`, and `Referrer-Policy` on the control-plane hostname. They do not set those headers on wildcard tunnel traffic because tunneled applications may need their own response policy.

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
- Treat `PORTHOOK_CONTROL_ADMIN_TOKEN` as a full-scope bootstrap and recovery token. Keep it offline or restricted after creating scoped admin tokens for routine operators.
- Give routine admin tokens only the scopes required for their work: `admin_tokens`, `tokens`, `reservations`, `domains`, `access_policies`, `audit_history`, or `runtime_diagnostics`.
- Do not reuse agent tokens as admin or validator tokens.
- Keep generated `.env.*` files out of Git.
- Revoke scoped admin tokens after operator turnover or suspected exposure.
- Rotate the bootstrap admin token if the environment value is exposed.
- Rotate the validator token if gateway or control-plane configuration is exposed.

Changing the validator token requires updating both the gateway and control plane together. Changing the bootstrap admin token affects recovery access and any CLI or dashboard sessions still using that value. Revoking a scoped admin token affects only sessions using that scoped token.

## Diagnostics and Exports

`porthook doctor`, `porthook history`, and `porthook export` are designed for operational handoff without plaintext token exposure. They omit plaintext agent tokens, policy secrets, local target URLs, raw query strings, and authorization headers.

Treat exported files and copied diagnostics as operational data anyway. They can include hostnames, paths, statuses, request IDs, tunnel IDs, token IDs, remote IPs, timestamps, counters, and audit fields.
