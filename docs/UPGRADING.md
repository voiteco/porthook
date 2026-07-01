# Upgrading

Porthook is pre-1.0. Review the release notes before upgrading between minor versions.

## Upgrade from 0.9.x to 0.10.x

Version 0.10.x adds custom domain TXT verification, request ID correlation, control-plane audit events, CLI diagnostics, and container healthchecks for self-hosted deployments.

Before upgrading a Postgres-backed control plane:

1. Back up the Postgres volume or database.
2. Stop the old `porthook-control-plane` and `porthook-gateway` processes or Compose stack.
3. Deploy the new control-plane, gateway, dashboard assets, CLI binaries, and images together.
4. Start the control plane and check `GET /readyz`.
5. Start the gateway and check `GET /readyz`, `GET /api/v1/tunnels`, and `GET /api/v1/request-logs`.
6. Run `porthook doctor --gateway <gateway-url> --control-plane <control-plane-url>` and include `--admin-token-stdin` when checking audit event access.
7. For newly added custom domains, create the required `_porthook.<hostname>` TXT record and run `porthook domains verify` or use the dashboard verification action before routing traffic.
8. For Compose deployments, confirm `docker compose ps` shows healthy gateway and control-plane containers.

The 0.10.x migration is additive:

- add `custom_domains.verification_token`
- add `custom_domains.verified_at`
- expand the `custom_domains.status` check to include `pending_verification` and `verification_failed`
- create `custom_domains_status_idx`
- keep existing 0.9.x custom domains active with legacy verification tokens

Rollback to 0.9.x should not require removing the additive columns or status index, but a 0.9.x gateway and dashboard will not enforce or display custom-domain verification states, request IDs in dashboard request logs, audit events, CLI diagnostics, or container healthchecks.

## Upgrade from 0.8.x to 0.9.x

Version 0.9.x adds custom domain mappings for control-plane-backed self-hosted deployments. Custom domains map a fully qualified hostname to a reserved subdomain, and gateway access policies continue to apply through that reservation.

Before upgrading a Postgres-backed control plane:

1. Back up the Postgres volume or database.
2. Stop the old `porthook-control-plane` and `porthook-gateway` processes or Compose stack.
3. Deploy the new control-plane, gateway, dashboard assets, and CLI binaries or images together.
4. Start the control plane and check `GET /readyz`.
5. Start the gateway and check `GET /readyz` and `GET /metrics`.
6. Use `porthook domains create` or the dashboard to map custom hostnames to reserved subdomains.
7. Point each custom hostname at the gateway edge, provision TLS for that hostname, and confirm the reverse proxy preserves the original `Host` header.
8. Run a custom-domain request and confirm it reaches the same active tunnel as the reserved subdomain.

The 0.9.x migration is additive:

- create `custom_domains` if it does not exist
- create custom-domain hostname and reserved-subdomain indexes if they do not exist

Rollback to 0.8.x should not require removing the `custom_domains` table, but a 0.8.x gateway will not route custom domains and a 0.8.x dashboard will not display them. Wildcard reserved subdomain routing remains available after rollback.

## Upgrade from 0.7.x to 0.8.x

Version 0.8.x adds reserved-subdomain access policies, gateway enforcement for those policies, CLI/dashboard access policy management, and gateway request log visibility.

Before upgrading a Postgres-backed control plane:

1. Back up the Postgres volume or database.
2. Stop the old `porthook-control-plane` and `porthook-gateway` processes or Compose stack.
3. Deploy the new control-plane, gateway, dashboard assets, and CLI binaries or images together.
4. Start the control plane and check `GET /readyz`.
5. Start the gateway and check `GET /readyz` and `GET /api/v1/request-logs`.
6. Use `porthook access create` or the dashboard to add access policies for reserved subdomains that should not remain public.
7. Run a protected-tunnel check and confirm unauthenticated public requests are denied before traffic reaches the local service.

The 0.8.x migration is additive:

- create `access_policies` if it does not exist
- create the reserved-subdomain access policy index if it does not exist

Rollback to 0.7.x should not require removing the `access_policies` table, but a 0.7.x gateway will not enforce access policies and a 0.7.x dashboard will not display them. Treat rollback as making protected reserved subdomains public unless an external reverse proxy or upstream service also enforces access.

## Upgrade from 0.5.x to 0.6.x

Version 0.6.x adds reserved subdomain ownership for requested tunnel names. In control-plane-backed gateway deployments, agents that pass `--subdomain NAME` must use a token that owns a matching reservation. Agents that omit `--subdomain` continue to receive random subdomains without a reservation.

Before upgrading a Postgres-backed control plane:

1. Back up the Postgres volume or database.
2. Stop the old `porthook-control-plane` and `porthook-gateway` processes or Compose stack.
3. Deploy the new control-plane, gateway, and CLI binaries or images together.
4. Start the control plane and check `GET /readyz`.
5. For each requested subdomain workflow, create a reservation with `porthook reserved create --name NAME --token-id TOKEN_ID`.
6. Start the gateway and agents.
7. Open `/dashboard/` and confirm tokens, reservations, and active tunnels are visible.

The 0.6.x migration is additive:

- create `reserved_subdomains` if it does not exist
- create the reserved-subdomain token owner index if it does not exist

Rollback to 0.5.x should not require removing the `reserved_subdomains` table, but a 0.5.x gateway will not enforce or display reservations.

## Upgrade from 0.4.x to 0.5.x

Version 0.5.x adds the embedded self-hosted dashboard and versioned Postgres migrations for control-plane token storage.

Before upgrading a Postgres-backed control plane:

1. Back up the Postgres volume or database.
2. Stop the old `porthook-control-plane` process or Compose stack.
3. Deploy the new `porthook-control-plane` binary or image.
4. Start the control plane and check `GET /readyz`.
5. Open `/dashboard/` on the control-plane listener and log in with `PORTHOOK_CONTROL_ADMIN_TOKEN`.
6. Run `porthook tokens list` and confirm existing tokens are present.

The control plane applies pending embedded migrations at startup and records them in `schema_migrations`. The 0.5.x migrations are additive for token storage:

- create `api_tokens` if it does not exist
- add `api_tokens.last_used_at` if it does not exist
- create the token hash index if it does not exist

No plaintext token values are introduced by the migration. Existing tokens remain hashed.

Rollback to 0.4.x should not require removing the additive `last_used_at` column or `schema_migrations` table, but 0.4.x will not update or display `last_used_at`.
