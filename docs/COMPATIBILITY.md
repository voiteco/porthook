# Compatibility

This document states what `1.x` guarantees to keep working, what it explicitly does not, and how each surface has actually evolved so far (every change since `v0.14.0` has been additive; nothing has broken compatibility). See [docs/UPGRADING.md](./UPGRADING.md) for concrete per-version upgrade steps and [SECURITY.md](../SECURITY.md) for the supported-version and security-fix policy that pairs with this matrix.

## Guarantee

Within the `1.x` line:

- A `1.y` agent interoperates with any `1.z` gateway/control-plane for every capability both sides declare, and degrades explicitly (a clear, typed error) rather than silently misbehaving for a capability only one side has.
- A `1.y` deployment's configuration keeps working unmodified on `1.z` for `z >= y`. New environment variables ship with a default that preserves the previous behavior.
- A `1.y` database keeps working unmodified on `1.z` for `z >= y`, and every `1.x` binary keeps running against a database migrated to a later `1.x` schema (see Database Schema below).
- Removing a capability, environment variable, API field, or database column requires a major version bump and a documented migration path; it will not happen within `1.x`.

## Protocol (agent ↔ gateway)

The wire protocol is versioned informationally (`protocol/messages.ProtocolVersion`, currently `0.3`) but negotiated through additive capability flags, not version-number matching. Each side declares the capabilities it supports during `auth.request`/`auth.ok`; a peer missing an optional capability keeps working for everything else and only loses the specific gated feature (for example, a peer without `websocket_tunnel` still tunnels HTTP normally and gets an explicit `501` only for WebSocket upgrade attempts on its own tunnels).

`protocol/messages.MinSupportedProtocolVersion` (currently `0.2`) is the floor: peers below it are rejected outright with a clear `unsupported_protocol` error, because they predate capability negotiation and can't be identified safely otherwise. Within `0.2` and above, compatibility is capability-by-capability, not version-by-version.

| Capability | Introduced | Required or optional |
| --- | --- | --- |
| `stream_start_end`, `binary_body_frames`, `stream_cancel` | protocol `0.2` | Required — the `0.2` HTTP-interoperability floor. A peer lacking any of these is rejected. |
| `websocket_tunnel` | protocol `0.3` | Optional — additive. A peer lacking it tunnels HTTP fully; only public WebSocket upgrades against its own tunnels are rejected. |

Adding a new required capability is a breaking change and must raise `MinSupportedProtocolVersion`; this has not happened since `0.2` and won't happen within `1.x` without a major version bump. New optional capabilities may be added freely.

## HTTP API (control plane and gateway management)

Every control-plane and gateway-management endpoint is under `/api/v1/...`. Within `v1`:

- Existing fields in a JSON response keep their name, type, and meaning; new fields may be added, and callers must ignore fields they don't recognize (already true of every CLI/dashboard client in this repository).
- Existing request fields keep accepting the same values; new optional request fields may be added.
- Existing endpoints keep their method, path, and required scope. New endpoints may be added.
- A required scope may be added to a *new* endpoint but not tightened on an *existing* one within `1.x` (tightening an existing endpoint's authorization is a breaking change).

A `v2` API, if ever introduced, would be additive at the path level (`/api/v2/...` alongside `/api/v1/...`), not a replacement, consistent with how this repository has never removed a `v1` endpoint.

## Configuration

Every `PORTHOOK_*` environment variable documented in [docs/TECHNICAL_SPEC.md](./TECHNICAL_SPEC.md) keeps its name and meaning within `1.x`. New variables always ship with a default that preserves the pre-existing behavior (for example, `PORTHOOK_DB_MAX_OPEN_CONNS` defaulted to `20` when introduced in `v0.18.0`, rather than requiring every existing deployment to set it explicitly). `configcheck` (see [docs/INSTALL.md](./INSTALL.md)) validates a configuration is complete and internally consistent for a given binary version; running it after every upgrade is the supported way to confirm nothing needs attention.

## Database Schema

Each Postgres-backed store (gateway request logs; control-plane tokens, admin tokens, reserved subdomains, access policies, custom domains, and audit events) tracks its own `schema_migrations` table and applies pending migrations once, automatically, at startup. Migrations within `1.x` are additive only — new tables, new nullable or defaulted columns, new indexes — never a destructive rename or drop of something an older `1.x` binary depends on.

This is what makes rollback safe: an older `1.x` binary's own schema check counts a *minimum* required set of columns being present, not an exact match (see `verifySchema` in `server/gateway/internal/gateway/request_log_postgres_store.go` for the pattern used by every store), so it tolerates extra columns a newer binary added. Rolling back code without rolling back the database is therefore supported within `1.x`; see [docs/RELIABILITY.md](./RELIABILITY.md)'s Migration Serialization section for the one caveat (never start two instances of the same service concurrently against a fresh, unmigrated database).

As of `v0.18.0`, every migration file in the repository is unchanged since `v0.14.0` — there has not yet been a schema change to roll back, which is itself verified (not assumed) in [docs/PRODUCTION_ROADMAP.md](./PRODUCTION_ROADMAP.md) Block 9.

## CLI Operational Export

`porthook export` (see `agent/cmd/porthook/export.go`) embeds `schema_version` in its JSON output, currently `3`. Within `1.x`, existing fields at a given `schema_version` keep their meaning; a new `schema_version` is only introduced for an additive change and old exports remain parseable by their own documented shape. Automated tooling that consumes export output should check `schema_version` and treat an unrecognized (future) value as "unknown fields may be present," not as an error.

## Explicitly Not Covered

The following are free to change within `1.x` and should not be relied on for automation or scripting:

- Dashboard HTML/CSS/JS internals (`server/dashboard/`) — the dashboard is a UI, not an API; automate against `/api/v1/...` instead.
- Log line wording and structured-log field sets beyond what [docs/OBSERVABILITY.md](./OBSERVABILITY.md) documents as a named metric or trace attribute.
- CLI human-readable (non-`--json`) output formatting.
- Internal Go package APIs under any `internal/` directory — these are not importable outside their module by Go's own visibility rules and carry no compatibility guarantee at all.
- Error message text (as opposed to error *codes*, like `invalid_token` or `auth_unavailable`, which are part of the protocol/API contract).

## Out of Scope for `v1.0.0`

Multi-gateway high availability, distributed tunnel routing, multi-region routing, raw TCP/UDP tunnels, and zero-downtime tunnel handoff between gateways are not part of the supported `v1` deployment shape (see the roadmap's Supported v1 Scope) and are not covered by this compatibility matrix.
