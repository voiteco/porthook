# Operations Runbooks

These runbooks cover the public self-hosted Porthook stack: gateway, control plane, dashboard, reverse proxy, and Postgres.

Commands that use `${PORTHOOK_ROOT_DOMAIN}`, `${PORTHOOK_AGENT_DOMAIN}`, or `${PORTHOOK_CONTROL_DOMAIN}` assume those values are exported in your shell. Otherwise, replace them with the values from `deploy/compose/.env.production`.

Use them together with:

- [Domains and TLS](./DOMAINS_TLS.md)
- [Control-Plane Access Boundary](./ACCESS_BOUNDARY.md)
- [Docker Compose Deployment](../deploy/compose/README.md)
- [Observability](./OBSERVABILITY.md)
- [Upgrading](./UPGRADING.md)

## Production Compose Deploy

1. Copy the example environment file:

   ```sh
   cp deploy/compose/.env.production.example deploy/compose/.env.production
   ```

2. Replace every `change-me` value with generated values.

3. Configure DNS and TLS:

   - `*.${PORTHOOK_ROOT_DOMAIN}` routes to the reverse proxy.
   - `PORTHOOK_AGENT_DOMAIN` routes to the reverse proxy.
   - `PORTHOOK_CONTROL_DOMAIN` is private or externally protected.
   - `PORTHOOK_TLS_CERT_PATH` and `PORTHOOK_TLS_KEY_PATH` point to readable certificate files.

4. Validate the Compose config:

   ```sh
   docker compose \
     --env-file deploy/compose/.env.production \
     -f deploy/compose/docker-compose.production.yml \
     config
   ```

5. Start or update the stack:

   ```sh
   docker compose \
     --env-file deploy/compose/.env.production \
     -f deploy/compose/docker-compose.production.yml \
     up --build -d
   ```

6. Check readiness:

   ```sh
   curl -i https://check.${PORTHOOK_ROOT_DOMAIN}/healthz
   curl -i https://${PORTHOOK_CONTROL_DOMAIN}/readyz
   docker compose \
     --env-file deploy/compose/.env.production \
     -f deploy/compose/docker-compose.production.yml \
     ps
   ```

7. Run an end-to-end tunnel check with a newly created agent token and, if using a requested subdomain, a matching reservation.

## Health Triage

Check container status:

```sh
docker compose \
  --env-file deploy/compose/.env.production \
  -f deploy/compose/docker-compose.production.yml \
  ps
```

The gateway and control-plane containers should report `healthy`. If either stays `starting` or `unhealthy`, inspect its logs before debugging the reverse proxy.

Check service logs:

```sh
docker compose \
  --env-file deploy/compose/.env.production \
  -f deploy/compose/docker-compose.production.yml \
  logs --tail=200 porthook-gateway porthook-control-plane reverse-proxy
```

Run the CLI diagnostics bundle:

```sh
printf '%s' '<admin-token>' | porthook doctor \
  --gateway https://check.${PORTHOOK_ROOT_DOMAIN} \
  --control-plane https://${PORTHOOK_CONTROL_DOMAIN} \
  --admin-token-stdin
```

Useful log events:

| Event | Meaning |
| --- | --- |
| `gateway.started` | Gateway listeners started. |
| `gateway.agent_auth_failed` | Agent authentication failed. Check token, control-plane validation, and validator token configuration. |
| `gateway.tunnel_registered` | Agent tunnel registered successfully. |
| `gateway.public_request` | Public request reached the gateway. Check `outcome`, `status`, `subdomain`, and `tunnel_id`. |
| `gateway.agent_keepalive_failed` | Gateway could not keep the agent WebSocket alive. Check network stability and proxy WebSocket handling. |
| `control_plane.auth_failed` | Admin or validator bearer authorization failed. Check access boundary and token configuration. |
| `control_plane.token_validated` | Gateway token validation request completed. Check `valid` and `token_id`. |
| `control_plane.reservation_authorized` | Requested-subdomain authorization completed. Check `allowed`, `reason`, and `subdomain`. |
| `control_plane.access_policy_created` | Admin created an access policy for a reserved subdomain. |
| `control_plane.access_policy_evaluated` | Gateway access policy evaluation completed. Check `allowed`, `mode`, `reason`, and `subdomain`. |

Gateway and control-plane HTTP responses include `X-Request-ID`. If an upstream proxy already sends `X-Request-ID` or `X-Correlation-ID`, Porthook preserves it; otherwise Porthook generates one. Use that value to correlate client-visible errors with `request_id` in logs.

Common symptoms:

| Symptom | First checks |
| --- | --- |
| Agent cannot log in | `PORTHOOK_AGENT_DOMAIN` DNS, reverse proxy route to `porthook-gateway:8081`, `gateway.agent_auth_failed`, control-plane validator token. |
| Tunnel URL returns 404 | Wildcard DNS, `PORTHOOK_ROOT_DOMAIN`, active `gateway.tunnel_registered` event for that subdomain. |
| Custom domain returns 404 | `gateway.public_request` outcome, TXT verification status, `PORTHOOK_CUSTOM_DOMAIN_MISS_TTL`, preserved `Host` header, and active tunnel for the mapped reserved subdomain. |
| Tunnel URL returns 429 | Per-tunnel rate limit settings on the gateway. |
| Tunnel URL returns 401 | Basic or bearer access policy requires valid public credentials. |
| Tunnel URL returns 403 | IP allowlist or another access policy denied the public request. |
| Tunnel URL returns 503 | Concurrent stream limit, disconnected agent, or overloaded local service. |
| Tunnel URL returns 504 | `PORTHOOK_STREAM_TIMEOUT`, slow local service, or broken WebSocket path. |
| Dashboard login fails | Control-plane admin token, access boundary, browser session storage, `/api/v1/status`. |
| Requested subdomain rejected | Reservation ownership for the token used by the agent. |
| Protected tunnel unexpectedly public | Missing access policy for the reserved subdomain or gateway not configured with `PORTHOOK_CONTROL_PLANE_URL`. |

Useful metrics:

| Metric | Use |
| --- | --- |
| `porthook_gateway_active_tunnels` | Current gateway tunnel sessions. |
| `porthook_gateway_public_request_errors_total` | Public requests that returned `5xx`. |
| `porthook_gateway_public_request_rate_limited_total` | Per-tunnel rate limit pressure. |
| `porthook_gateway_public_request_access_denied_total` | Public requests rejected by tunnel access policy. |
| `porthook_gateway_public_request_access_policy_errors_total` | Gateway could not evaluate a tunnel access policy. |
| `porthook_gateway_public_request_timeouts_total` | Tunnel round-trip timeout pressure. |
| `porthook_gateway_tunnel_registration_failures_total` | Agent registration failures after authentication. |
| `porthook_control_plane_ready` | Control-plane readiness as `1` or `0`. |
| `porthook_control_plane_tokens` | Current token records. |
| `porthook_control_plane_tokens_revoked` | Current revoked token records. |
| `porthook_control_plane_reserved_subdomains` | Current reserved subdomain records. |
| `porthook_control_plane_access_policies` | Current access policy records. |
| `porthook_control_plane_access_policy_evaluation_denied_total` | Access policy evaluations that denied public traffic. |

## Postgres Backup

Create backups before upgrades, risky configuration changes, and token migrations:

```sh
backup_file="porthook_$(date -u +%Y%m%dT%H%M%SZ).sql"
docker compose \
  --env-file deploy/compose/.env.production \
  -f deploy/compose/docker-compose.production.yml \
  exec -T postgres pg_dump -U porthook -d porthook > "${backup_file}"
```

The backup contains token hashes, reserved subdomain ownership metadata, and access policy secret hashes. Treat it as sensitive operational data.

Verify that the backup is not empty:

```sh
test -s "${backup_file}"
```

Store backups outside the Compose host or on durable storage with access controls.

## Postgres Restore

Prefer restoring into a fresh stack or a maintenance window.

1. Stop writers:

   ```sh
   docker compose \
     --env-file deploy/compose/.env.production \
     -f deploy/compose/docker-compose.production.yml \
     stop porthook-gateway porthook-control-plane
   ```

2. Restore the dump into the target database:

   ```sh
   docker compose \
     --env-file deploy/compose/.env.production \
     -f deploy/compose/docker-compose.production.yml \
     exec -T postgres psql -U porthook -d porthook < porthook_backup.sql
   ```

3. Start services:

   ```sh
   docker compose \
     --env-file deploy/compose/.env.production \
     -f deploy/compose/docker-compose.production.yml \
     up -d porthook-control-plane porthook-gateway
   ```

4. Check `/readyz`, `/api/v1/status`, token listing, and a tunnel round trip.

## Token Rotation

### Admin Token

1. Generate a new `PORTHOOK_CONTROL_ADMIN_TOKEN`.
2. Update `deploy/compose/.env.production`.
3. Restart the control plane:

   ```sh
   docker compose \
     --env-file deploy/compose/.env.production \
     -f deploy/compose/docker-compose.production.yml \
     up -d porthook-control-plane
   ```

4. Verify CLI token administration and dashboard login with the new value.

### Validator Token

The gateway and control plane must use the same validator token. The current self-hosted implementation supports one validator token at a time.

1. Generate a new `PORTHOOK_CONTROL_VALIDATOR_TOKEN`.
2. Update `deploy/compose/.env.production`.
3. Recreate both services together:

   ```sh
   docker compose \
     --env-file deploy/compose/.env.production \
     -f deploy/compose/docker-compose.production.yml \
     up -d porthook-control-plane porthook-gateway
   ```

4. Watch for `control_plane.auth_failed` and `gateway.agent_auth_failed` events.
5. Start a fresh agent connection and confirm token validation succeeds.

Expect existing agents to reconnect if the gateway process restarts.

### Agent Tokens

1. Create a new token:

   ```sh
   printf '%s' '<admin-token>' | porthook tokens create \
     --control-plane https://${PORTHOOK_CONTROL_DOMAIN} \
     --admin-token-stdin \
     --name '<agent-name>'
   ```

2. Move required subdomain reservations to the new token ID. If a name is already reserved for the old token, perform a short handoff window:

   ```sh
   printf '%s' '<admin-token>' | porthook reserved delete \
     --control-plane https://${PORTHOOK_CONTROL_DOMAIN} \
     --admin-token-stdin \
     demo

   printf '%s' '<admin-token>' | porthook reserved create \
     --control-plane https://${PORTHOOK_CONTROL_DOMAIN} \
     --admin-token-stdin \
     --name demo \
     --token-id '<new-token-id>'
   ```

3. Update the agent login:

   ```sh
   printf '%s' '<new-agent-token>' | porthook login \
     --server https://${PORTHOOK_AGENT_DOMAIN} \
     --token-stdin
   ```

4. Restart the affected agent tunnel.
5. Revoke the old token after traffic has moved:

   ```sh
   printf '%s' '<admin-token>' | porthook tokens revoke \
     --control-plane https://${PORTHOOK_CONTROL_DOMAIN} \
     --admin-token-stdin \
     '<old-token-id>'
   ```

## Upgrade

1. Read [UPGRADING.md](./UPGRADING.md) and the changelog for the target version.
2. Back up Postgres.
3. Pull the target source or release assets.
4. Validate locally:

   ```sh
   make test
   make vet
   make compose-config
   make production-hardening-check
   ```

5. Recreate the production stack:

   ```sh
   docker compose \
     --env-file deploy/compose/.env.production \
     -f deploy/compose/docker-compose.production.yml \
     up --build -d
   ```

6. Check:

   - gateway `/healthz` and `/readyz`
   - control-plane `/healthz`, `/readyz`, and `/api/v1/status`
   - dashboard load
   - token creation/listing
   - requested-subdomain authorization
   - one HTTP tunnel round trip

## Rollback

1. Decide whether the rollback is code-only or data-related.
2. For code-only rollback, deploy the previous release or source revision with the existing database.
3. For data-related rollback, stop gateway and control-plane writers, restore a known-good Postgres backup, then start services.
4. Watch logs for auth failures, readiness failures, and migration errors.

Schema migrations in the current pre-1.0 line are intended to be additive, but always keep a backup before upgrading.

## Incident Response

### Suspected Admin Token Exposure

1. Rotate `PORTHOOK_CONTROL_ADMIN_TOKEN`.
2. Review recent control-plane audit events through `/api/v1/events`, plus `control_plane.auth_failed`, `control_plane.token_created`, `control_plane.token_revoked`, `control_plane.reservation_created`, and `control_plane.reservation_deleted` logs.
3. Revoke unknown or unused agent tokens.
4. Review reserved subdomains for unexpected ownership.
5. Confirm the control-plane access boundary still blocks untrusted networks.

### Suspected Validator Token Exposure

1. Rotate `PORTHOOK_CONTROL_VALIDATOR_TOKEN`.
2. Recreate gateway and control-plane services together.
3. Review `control_plane.token_validated` and `control_plane.auth_failed` events.
4. Restart trusted agents to establish fresh sessions.

### Suspected Agent Token Exposure

1. Create a replacement agent token.
2. Move required subdomain reservations to the replacement token.
3. Update the affected agent.
4. Revoke the exposed token.
5. Check for unexpected `gateway.tunnel_registered` events for the exposed token ID before revocation.
