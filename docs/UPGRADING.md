# Upgrading

Porthook is pre-1.0. Review the release notes before upgrading between minor versions.

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
