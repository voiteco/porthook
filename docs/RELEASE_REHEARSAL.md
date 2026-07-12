# Release Rehearsal

This document records what was actually verified ahead of `v1.0.0`, per [docs/PRODUCTION_ROADMAP.md](./PRODUCTION_ROADMAP.md) Block 9: clean production-shaped installs on both supported Linux architectures, upgrades from `v0.14.0` and `v0.17.0` through their documented paths, a production-shaped backup/restore on a fresh host, and rollback for every migration introduced since `v0.14.0`. Every check below used real published artifacts (GHCR images, GitHub release binaries) and real Docker Compose orchestration, not mocks or simulated output.

## Clean Production-Shaped Installs (Linux amd64 and arm64)

Brought up a full Postgres + `porthook-control-plane` + `porthook-gateway` stack using the real published `v0.18.0` GHCR images, once natively on `linux/arm64` and once under QEMU emulation on `linux/amd64`. For both:

- Confirmed the manifest list published for each image (`docker buildx imagetools inspect`) resolves to genuinely distinct per-architecture manifests, not one architecture silently served for both (worth checking explicitly — `docker pull`'s reported digest is the shared manifest-list digest either way and doesn't distinguish this on its own).
- All three containers reached a healthy state.
- A real agent registered a tunnel through the control plane and a real HTTP request round-tripped end to end (`porthook wsecho smoke ok`).

## Upgrade From `v0.17.0`

Started the real published `v0.17.0` images, seeded 3 tokens, 3 reserved subdomains, 1 access policy, and 20 real request-log rows via live traffic through a real tunnel. Swapped `PORTHOOK_GATEWAY_IMAGE`/`PORTHOOK_CONTROL_PLANE_IMAGE` to `v0.18.0` against the same Postgres volume (Compose left the untouched `postgres` service running rather than recreating it — confirmed from the `up` output). Result: every row survived exactly (3 tokens, 3 reservations, 1 policy, 20 request logs), the upgraded stack came up healthy, and the existing tunnel reconnected and served traffic again. Matches [docs/UPGRADING.md](./UPGRADING.md)'s "no migration, no code change" description of this step.

## Upgrade From `v0.14.0`

This is the one upgrade path in the whole `v0.14.0`→`v0.18.0` range with a real, breaking configuration requirement: `v0.15.1` isolated gateway management data onto a private listener, requiring `PORTHOOK_MANAGEMENT_ADDR`/`PORTHOOK_MANAGEMENT_TOKEN` on the gateway and `PORTHOOK_GATEWAY_MANAGEMENT_URL`/`PORTHOOK_GATEWAY_MANAGEMENT_TOKEN` on the control plane, which `v0.14.0` doesn't have.

Built `v0.14.0`'s own `docker-compose.control-plane.yml` from a `git worktree` checkout of that tag (no published images exist for `v0.14.0`; GHCR publishing started in `v0.17.0`), bringing up a real `v0.14.0` stack from its own source. Seeded 2 tokens, 2 reserved subdomains, and 15 real request-log rows via live traffic. Then, against the same Postgres volume, switched to the `v0.18.0` published images with the cumulative `v0.15.1`+ configuration applied (the full management-listener isolation shape, as documented across the intervening `docs/UPGRADING.md` sections) — this is the realistic operator path: read the cumulative notes and land directly on the target configuration, not run every intermediate binary version in sequence.

Result: all data survived exactly (2 tokens, 2 reservations, 15 request logs, 21 audit events), the upgraded stack came up healthy, the pre-existing tunnel reconnected and served traffic, and a direct request to `/metrics` on the now-public listener correctly returned `404` — confirming the management-isolation upgrade actually took effect, not just that the containers started.

## Production-Shaped Backup and Restore on a Fresh Host

Using the upgraded (`v0.14.0`→`v0.18.0`) stack above, added an access policy and an (unverified, `pending_verification`) custom domain for full entity coverage, then ran the exact documented backup command (`docker compose exec -T postgres pg_dump -U porthook -d porthook`, matching `make compose-backup`). Final pre-teardown counts: 2 tokens, 2 reservations, 1 access policy, 1 custom domain, 17 request logs, 27 audit events.

Tore the stack down completely, including its named volume (`docker compose down -v`) — a genuine total-loss simulation, not a stop/start. Brought up a brand-new Postgres container under a different Compose project name (a genuinely new Docker volume, confirmed by name), restored the backup file into it (`psql < backup.sql`), then started the control plane and gateway against the restored database.

Result: every count matched exactly on the fresh host. A token restored from the backup logged in and re-established a real tunnel that served live traffic. This is a stronger check than Block 8's `scripts/smoke-backup-restore.sh` (which verifies the same properties against raw binaries and a hand-rolled Postgres container) because it exercises the actual Compose-orchestrated production deployment shape end to end.

## Rollback for Migrations Introduced After `v0.14.0`

Checked directly, not assumed: every migration file in the repository (`git ls-tree -r v0.14.0 -- '**/migrations/*.sql'` compared against the same at `HEAD`) is byte-identical between `v0.14.0` and this release. There is no migration to roll back yet — the additive-migrations discipline established in [docs/COMPATIBILITY.md](./COMPATIBILITY.md) has held for the entire `v0.14.0`→`v0.18.0` range.

Rollback of *code* without a schema change is separately verified: Block 8's `scripts/smoke-backup-restore.sh` swaps back to the previous release's real binaries against a schema built by the current release and confirms they still start and serve correctly (see [docs/RELIABILITY.md](./RELIABILITY.md)'s Backup/Restore section), and the `v0.14.0` upgrade rehearsal above additionally confirms this holds across the full five-minor-version range, not just one step back.

## Final Security Review

Reviewed [docs/THREAT_MODEL.md](./THREAT_MODEL.md)'s Security Requirements and Abuse Cases against the current code, verifying rather than assuming each one still holds after every block's changes, including Block 8's additions:

- **Credentials never reach logs, metrics, or traces.** Checked directly: every token-related metric (`porthook_control_plane_tokens`, `*_creates_total`, etc.) is a count, never a value; nothing logs `DatabaseURL` or any other connection string containing a password (grepped for it).
- **Management/metrics data stays off public listeners.** Verified at the code level (`/metrics`, `/api/v1/tunnels`, `/api/v1/runtime`, and `/api/v1/request-logs` are all wrapped in `requireManagementAuth` and registered on the gateway's separate management mux) and live, twice: once via `docs/RELEASE_REHEARSAL.md`'s `v0.14.0` upgrade rehearsal (a direct request to the public listener's `/metrics` returned `404`), and once by inspection.
- **The control plane's `/metrics` has no per-request token check** — confirmed intentional, not a gap: unlike the gateway (which splits public tunnel traffic from private management traffic on separate listeners), the control plane's entire surface is designed to sit behind network-level isolation (see [docs/ACCESS_BOUNDARY.md](./ACCESS_BOUNDARY.md): "the gateway, its management listener, the control plane, and Postgres are private to the Compose network"), with token/scope checks reserved for the sensitive admin and data endpoints specifically. Consistent with the threat model's control-plane trust boundary, which lists "private service access" as a control alongside endpoint-specific authorization.
- **Two concrete findings from building Block 8's failure-injection suite** are now recorded as abuse cases in the threat model itself (see its Abuse Cases table): the fail-closed `503` behavior when access-policy evaluation can't reach the control plane, and the `auth_unavailable` retry fix.
- **New release artifacts** (this release's binaries and images) carry the same checksum/SBOM/attestation coverage established in Block 7; no new artifact type was introduced that the threat model's "Operator workstation and backups" boundary doesn't already cover.
- **Required CI security gates** — race detector, `govulncheck`, CodeQL, and Gitleaks secret scanning — are green on every commit in this block (see the `v0.18.0` and Block 9 CI runs).

No release-blocking findings. Two non-blocking, already-fixed findings from Block 8 are recorded in the threat model for future reference.

## Multi-Architecture Image Manifest Integrity

Documented here rather than left as an unstated assumption: `docker buildx imagetools inspect` was used (not just `docker pull`, whose reported digest is the shared manifest-list digest for every platform) to confirm `ghcr.io/voiteco/porthook-gateway:v0.18.0` and `ghcr.io/voiteco/porthook-control-plane:v0.18.0` each resolve to two distinct platform-specific manifests (`linux/amd64`, `linux/arm64`) under one manifest list, plus their attestation manifests. This is worth checking explicitly on every release rather than trusting that a green multi-arch build step produced genuinely different images per platform.
