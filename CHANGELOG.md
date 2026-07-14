# Changelog

All notable changes to Porthook are documented here.

## [Unreleased]

## [1.0.0-rc.1] - 2026-07-14

First release candidate for `v1.0.0`, cut from the feature-frozen `v0.19.0` line per [docs/PRODUCTION_ROADMAP.md](./docs/PRODUCTION_ROADMAP.md) Block 9. Starts the 14-day qualification window; only release blockers, compatibility fixes, tests, and documentation are accepted before `v1.0.0` ships.

### Added
- Added release-candidate support to the release pipeline (`.github/workflows/release.yml`): a hyphen in the tag (e.g. `-rc.1`) is detected as a pre-release, publishes the GitHub Release with `--prerelease`, and publishes container images under their exact version tag only, leaving `:latest` pointing at the last stable release. Documented in [docs/RELEASE.md](./docs/RELEASE.md)'s new Release Candidates section.

## [0.19.0] - 2026-07-12

Feature freeze for the `1.0.0` line: from this release onward, only release blockers, compatibility fixes, tests, and documentation are accepted until `v1.0.0` ships (see the roadmap's Block 9).

### Added
- Added `docs/COMPATIBILITY.md`, publishing what `1.x` guarantees to keep working across the wire protocol, HTTP API, configuration, database schema, and CLI export format, and what's explicitly excluded from that guarantee.
- Added `docs/RELEASE_REHEARSAL.md`, recording real production-shaped verification ahead of `v1.0.0`: clean installs from published images on both Linux `amd64` and `arm64` with a live tunnel round trip on each, upgrade rehearsals from both `v0.14.0` (built from source, exercising the `v0.15.1` management-listener isolation change) and `v0.17.0` through to current with all seeded data verified to survive, a backup/restore rehearsal on a genuinely fresh host using the documented `compose-backup` procedure, confirmation that no migration has changed since `v0.14.0` (so there is nothing yet to roll back), and a final security review against the threat model.
- Added the post-`v1.0.0` supported-version, deprecation, migration, and security-fix release policies to `SECURITY.md`, replacing the stale pre-`1.0` table.
- Added two concrete findings from Block 8's failure-injection work to `docs/THREAT_MODEL.md`'s abuse-cases table: the fail-closed behavior when access-policy evaluation can't reach the control plane, and the `auth_unavailable` reconnect fix.

## [0.18.0] - 2026-07-12

### Added
- Added reliability metrics — goroutine count, heap memory, database connection pool stats, open file descriptors (Linux), and a public-request/request-duration latency histogram — exposed on both services' `/metrics` endpoints.
- Added configurable database connection pool limits (`PORTHOOK_DB_MAX_OPEN_CONNS`, `PORTHOOK_DB_MAX_IDLE_CONNS`, `PORTHOOK_DB_CONN_MAX_LIFETIME`, `PORTHOOK_DB_CONN_MAX_IDLE_TIME`) to the gateway and control plane, and `PORTHOOK_SHUTDOWN_TIMEOUT` to the control plane, matching the gateway's existing setting.
- Added a reproducible load-generation harness (`scripts/testdata/loadgen`, `make smoke-capacity`) and verified the minimum capacity profile — 100 active tunnels, 500 concurrent WebSocket streams, 100 aggregate requests/second — sustained for 30 minutes against a real Postgres-backed gateway with zero errors and no unbounded goroutine or heap growth.
- Added a failure-injection test suite (`make smoke-failure-injection`) exercising agent disconnect, gateway graceful shutdown, reverse-proxy restart, control-plane outage, Postgres restart, slow Postgres, and network interruption against real processes, with measured recovery bounds documented in `docs/RELIABILITY.md`.
- Added an automated backup, restore, upgrade, rollback, and retention-pruning verification suite (`make smoke-backup-restore`) against a real Postgres database with a realistic volume of stored data, including a real upgrade/rollback check against the previous published release's binaries.
- Added `deploy/prometheus/alerts.yml`, example Prometheus alerting rules for readiness, error rate, latency, database pool saturation, goroutine/heap growth, tunnel churn, and TLS certificate expiry.
- Added `docs/RELIABILITY.md`, publishing measured single-node capacity, database pool sizing guidance, and documented failure-mode recovery bounds.
- Added `.github/workflows/soak.yml`, a scheduled (and manually dispatchable) workflow running a multi-hour capacity soak with agent reconnect churn and operational-history retention pruning, plus a larger-scale backup/restore check.

### Fixed
- Fixed the agent treating a transient `auth_unavailable` error — the control plane being temporarily unreachable — the same as a permanent, non-retryable `invalid_token` error; the agent now retries `auth_unavailable` with the normal reconnect backoff instead of exiting.

## [0.17.0] - 2026-07-12

### Added
- Added versioned `linux/amd64` and `linux/arm64` `porthook-gateway` and `porthook-control-plane` images published to GHCR, with `org.opencontainers.image.{title,description,version,revision,created,source,licenses}` labels and a multi-architecture manifest per release.
- Added keyless-signed (Sigstore-backed) build provenance attestations and SPDX SBOMs for every published image and release binary, verifiable with `gh attestation verify` and no extra secrets.
- Added `deploy/compose/docker-compose.production.yml` support for pulling published images by tag or digest (`PORTHOOK_GATEWAY_IMAGE`/`PORTHOOK_CONTROL_PLANE_IMAGE`, both required) instead of building from source; the prior build-from-source behavior moved to a new `deploy/compose/docker-compose.source-build.yml` for contributors.
- Added Windows `amd64`/`arm64` release builds of the `porthook` CLI agent.
- Added `LICENSE-agent-Apache-2.0.txt` and `LICENSE-server-AGPL-3.0.txt` to every release, and embedded the matching license text at `/LICENSE` in both container images.
- Added `scripts/install.sh` (Linux/macOS) and `scripts/install.ps1` (Windows), checksum-verifying installers that refuse to install anything that fails verification and opportunistically verify build provenance attestations when the GitHub CLI is available.
- Documented pinning releases by immutable image digest and verifying attestations in `docs/UPGRADING.md`.

## [0.16.0] - 2026-07-12

### Added
- Added protocol revision 0.3 with additive capability negotiation: peers no longer need an exact `protocol_version` match, only the required capability set (unchanged since 0.2), so agents and gateways built at different revisions keep interoperating for HTTP.
- Added public WebSocket tunneling: the gateway relays a public WebSocket upgrade to the agent's local target over the existing multiplexed tunnel connection, gated by the new optional `websocket_tunnel` capability so agents that lack it keep working for HTTP and get a clear `501` for WebSocket attempts on their tunnels.
- Added a configurable request/idle/max-lifetime stream deadline policy (`PORTHOOK_STREAM_REQUEST_TIMEOUT`, `PORTHOOK_STREAM_IDLE_TIMEOUT`, `PORTHOOK_STREAM_MAX_LIFETIME`), replacing the previous fixed 30-second stream deadline that killed long-lived SSE and long-polling responses regardless of activity.
- Added `PORTHOOK_WS_MESSAGE_MAX_BYTES` to bound a single tunneled WebSocket message on the gateway and agent.
- Added `make smoke-websocket`, a real binary-level smoke test that tunnels to a local WebSocket echo service through the actual gateway and agent binaries.
- Added `PORTHOOK_CA_FILE` so the agent can trust a gateway certificate issued by a private or internal CA.
- Added `PORTHOOK_CONNECT_ADDR`, mirroring curl's `--resolve`, so a new edge deployment can be tested against its real hostname and certificate before DNS points at it.
- Added `PORTHOOK_DNS_RESOLVER_ADDR` so the control plane can verify custom-domain TXT records against a specific DNS server instead of the system resolver, for split-horizon DNS, a private authoritative zone, or local testing.
- Added `deploy/reverse-proxy/caddy/Caddyfile.custom-domain-example`, the supported pattern for routing an explicitly configured custom domain to the gateway, since the wildcard site block only matches subdomains of `PORTHOOK_ROOT_DOMAIN`.
- Added `make smoke-tls-edge`, an end-to-end smoke test proving the checked-in Caddy deployment completes real HTTPS and secure WebSocket round trips, with certificate coverage and routing verified separately for the wildcard tunnel, agent, control-plane, and a custom-domain hostname, and the complete custom-domain create/DNS-verify/activate/route/access-policy/delete lifecycle exercised through that real edge.
- Added a `configcheck` warning when the gateway's `PORTHOOK_PUBLIC_URL` host doesn't match `PORTHOOK_ROOT_DOMAIN`, and validation for a malformed `PORTHOOK_DNS_RESOLVER_ADDR`.
- Documented certificate issuance, renewal, reload, expiry monitoring, and rollback for the checked-in Caddy deployment.

### Changed
- The public listener's HTTP write timeout is disabled in favor of the new stream-level deadline policy, so response duration for SSE, long polling, and WebSocket tunnels is bounded by activity and an absolute cap rather than a fixed schedule.

### Fixed
- Fixed a goroutine-lifecycle bug where the agent's `Run` could return before in-flight per-request goroutines finished writing to the configured output, observable as a data race under `-race`.
- Fixed the gateway's agent-connection read loop, which only dispatched a fixed whitelist of HTTP response message types to a stream's pending channel; every new WebSocket protocol message silently vanished until this was corrected.
- Fixed a race in both the gateway's and agent's WebSocket relay where closing a shared connection to honor one direction's clean close could report the whole relay as failed depending on goroutine scheduling.

## [0.15.1] - 2026-07-11

### Added
- Added a dedicated private gateway management listener and a separate control-plane-to-gateway management credential.
- Added authenticated control-plane operator endpoints for gateway health, readiness, metrics, tunnels, runtime diagnostics, and request logs.
- Added a shared, trusted-proxy-aware client-IP resolver used consistently by the gateway and control plane for access-policy evaluation, forwarded headers, request logs, audit events, and traces.
- Added `PORTHOOK_TRUSTED_PROXIES` configuration and an explicit trust boundary for the production Compose network so only the reverse proxy's peer address can supply forwarded client-address headers.
- Added versioned Argon2id password hashing for user-selected basic-auth access-policy passwords, with lazy upgrade of legacy hashes on successful authentication; generated bearer, agent, admin, and service tokens keep the fast SHA-256 lookup hash.
- Added bounded authentication-attempt throttling for basic-auth and bearer-token access-policy checks, keyed by subdomain and resolved client address, without limiting public traffic or already-authenticated requests.
- Added an end-to-end Caddy edge smoke test (`make smoke-caddy-edge`) proving `ip_allowlist` access policies and gateway request logs use the client address the reverse proxy actually observed, not a client-supplied `X-Forwarded-For` header.

### Changed
- Dashboard and CLI operational workflows now use scoped control-plane APIs instead of direct gateway URLs; CLI exports use schema version 3.
- Production Compose keeps gateway management inside the service network and verifies that Caddy does not route it.
- Gateway health, metrics, and diagnostic APIs moved off the public listener, which now forwards every application path transparently.

### Fixed
- Stabilized the gateway stream-error test by waiting for tunnel registration before issuing its public request.
- Fixed `porthook access update` usage text and argument parsing, which silently required policy option flags before the trailing policy ID rather than after it as documented.

## [0.15.0] - 2026-07-11

### Added
- Added mandatory full-suite race detection, seeded protocol fuzz tests, a bounded scheduled fuzz workflow, and durable smoke diagnostics for asynchronous request-log persistence.
- Added pinned `govulncheck` dependency analysis, CodeQL static analysis, Gitleaks full-history secret scanning, and Dependabot updates for Go modules and GitHub Actions.
- Added a public v1 threat model covering trust boundaries, assets, attacker capabilities, abuse cases, security requirements, and accepted self-hosted risks.

### Changed
- Updated the required Go toolchain and server build images to Go 1.26.5 and upgraded `github.com/jackc/pgx/v5` to 5.9.2 to remediate reachable vulnerability findings.
- Release workflows now gate publication on vulnerability, secret, and CodeQL checks as well as existing test, race, smoke, packaging, and deployment validation.

### Fixed
- Synchronized concurrent test output and log capture so the full race detector suite is stable.
- Made the durable control-plane smoke test wait for asynchronous request-log persistence without weakening query-redaction checks.

## [0.14.0] - 2026-07-10

### Added
- Added scoped admin token storage, Postgres migration, validation, revocation, and admin-token APIs for self-hosted control-plane administration.
- Added `porthook admin tokens create`, `porthook admin tokens list`, and `porthook admin tokens revoke`.
- Added dashboard admin-token management with scoped token creation, revocation, and one-time plaintext display.
- Added control-plane smoke coverage for scoped admin token creation, list redaction, scope-denied access, durable persistence, and revocation.
- Added `configcheck` subcommands to `porthook-gateway` and `porthook-control-plane`, including production-mode validation for durable self-hosted deployments.
- Added `make release-verify` and CI/release workflow checks for release asset completeness, checksums, embedded versions, and production configuration validation.
- Added `make smoke-durable` for a Postgres-backed control-plane smoke test that verifies audit events and gateway request logs survive service restarts.
- Added local control-plane Compose Make targets for startup, shutdown, logs, status, and Postgres backups.
- Added release binary installation and checksum verification documentation.

### Changed
- Control-plane admin APIs now authorize requests with endpoint-specific admin scopes, while `PORTHOOK_CONTROL_ADMIN_TOKEN` remains a full-scope bootstrap and recovery token.
- CLI and dashboard authorization errors now distinguish rejected admin tokens from valid tokens missing a required scope.
- Production control-plane config checks now warn operators to keep the bootstrap admin token restricted and use scoped admin tokens for routine access.
- Improved self-hosted operations, backup, restore, upgrade, release, and Compose documentation for production-oriented deployments.
- Improved dashboard operational log usability with hash-backed audit/request-log filters, copyable request and tunnel IDs, and a request-log tunnel ID column.
- Hardened Caddy control-plane examples with response security headers and extended production hardening checks to enforce them.

## [0.12.0] - 2026-07-02

### Added
- Added Postgres-backed control-plane audit event storage with an in-memory fallback for development deployments.
- Added cursor pagination and expanded filters to `GET /api/v1/events`, including event, level, request ID, remote IP, field text, and time-window queries.
- Added optional Postgres-backed gateway request log ingestion for self-hosted deployments.
- Added cursor pagination, echoed filters, and durable-store reads to `GET /api/v1/request-logs`.
- Added retention pruning for durable control-plane audit events and gateway request logs.
- Added dashboard pagination for audit events and gateway request logs using API cursors.
- Added `porthook history events` and `porthook history requests` for paginated CLI access to audit events and gateway request logs.
- Expanded `porthook doctor` to check gateway runtime, metrics, and request-log APIs.
- Added operational export schema version 2 with embedded diagnostics and audit/request-log pagination metadata.

## [0.11.0] - 2026-07-01

### Added
- Added a dashboard tunnel detail view backed by `GET /api/v1/tunnels/{id}` with public tunnel metadata, stream counts, uptime, recent request summaries, and custom-domain associations without local target URLs.
- Added gateway request-log filters for subdomain, method, host, path, status, outcome, request ID, tunnel ID, and time windows, with matching dashboard filter controls.
- Added a dashboard audit events view with filtering for control-plane event, level, request ID, remote IP, and event fields.
- Added browser-run dashboard diagnostics for control-plane readiness and configured gateway tunnel, runtime, metrics, and request-log APIs.
- Added `GET /api/v1/runtime` on the gateway and a dashboard runtime summary for safe uptime, limits, timeouts, stream, request-log, and counter metadata.
- Added a dashboard Prometheus metrics drilldown for gateway metric names, types, values, and help text.
- Added `porthook tunnels list` and `porthook tunnels show` for inspecting active gateway tunnels from the CLI without an admin token.
- Added best-effort operational JSON export from `porthook export` and the dashboard, covering safe control-plane summaries, audit events, active tunnels, gateway runtime, metrics, request logs, and partial endpoint errors without plaintext tokens, policy secrets, or local target URLs.

### Changed
- Expanded self-hosted dashboard, operations, upgrade, release, and CLI documentation for the deeper operational visibility workflows.

### Fixed
- Stabilized gateway active-session tests by waiting for registered sessions before issuing public tunnel requests.

## [0.10.0] - 2026-07-01

### Added
- Added DNS TXT verification lifecycle for custom domains, including verification tokens, `POST /api/v1/custom-domains/{id}/verify`, `porthook domains verify`, dashboard verification actions, and self-hosting documentation.
- Added request ID propagation across the gateway and control plane, including generated `X-Request-ID` values, public error response request IDs, access/request log fields, and OpenTelemetry span attributes.
- Added a control-plane audit events endpoint at `GET /api/v1/events` for recent in-memory admin and validator events without plaintext tokens or access policy secrets.
- Added `porthook doctor` for operator diagnostics across gateway health, gateway readiness, active tunnel API reachability, control-plane status, and audit event access.
- Added built-in `healthcheck` subcommands for `porthook-gateway` and `porthook-control-plane`, Docker image healthcheck metadata, and Compose healthchecks for self-hosted stacks.

### Changed
- Hardened custom-domain gateway routing with separate hit and miss cache TTLs, root-domain bypasses, control-plane returned-subdomain validation, and clearer request outcomes for pending, failed, missing, and lookup-error states.
- Updated dashboard request logs with request ID display and filtering.
- Updated production Compose dependencies so the gateway and reverse proxy wait for healthy upstream services.
- Extended production hardening checks to enforce container healthchecks and `service_healthy` dependencies.

### Fixed
- Stabilized gateway large-body request tests by waiting for tunnel session registration before public requests.

## [0.9.0] - 2026-07-01

### Added
- Added custom domain storage, Postgres migration, admin APIs, and gateway lookup API for control-plane-backed self-hosted deployments.
- Added gateway custom-domain routing with lookup caching, metrics, request log fields, and control-plane client integration.
- Added `porthook domains create`, `porthook domains list`, and `porthook domains delete` for self-hosted custom domain administration.
- Added dashboard custom domain management.
- Added custom domain DNS, TLS, deployment, upgrade, and self-hosting documentation.
- Added optional OpenTelemetry tracing for gateway and control-plane HTTP paths with stdout and OTLP exporters.
- Added dashboard operational overview charts for active tunnels, recent requests, error rate, latency, outcomes, and status classes.

### Changed
- Updated the Go toolchain requirement and server Docker build images to Go 1.25 for current OpenTelemetry dependencies.
- Updated README, specifications, Compose examples, and operations docs for custom domains, tracing, and dashboard operational views.

## [0.8.0] - 2026-07-01

### Added
- Added reserved-subdomain access policy storage, Postgres migration, admin APIs, and gateway evaluation API.
- Added gateway enforcement for `public`, `basic_auth`, `bearer_token`, and `ip_allowlist` access policies before public requests reach agents.
- Added `porthook access create`, `porthook access list`, `porthook access update`, and `porthook access delete` for self-hosted access policy administration.
- Added dashboard access policy management and gateway request log visibility.
- Added `GET /api/v1/request-logs` on the gateway with an in-memory recent request log ring buffer.

### Changed
- Extended gateway, control-plane, dashboard, operations, upgrade, and specification documentation for access policies and request logs.
- Extended the control-plane smoke test to cover protected tunnel requests and gateway request logs.

## [0.7.0] - 2026-06-30

### Added
- Added Caddy and Traefik reverse-proxy examples for self-hosted TLS deployments.
- Added a production Docker Compose stack and example environment for reverse-proxy-backed self-hosted deployments.
- Added domain, wildcard DNS, and TLS guidance for internet-facing self-hosted deployments.
- Added control-plane access-boundary guidance and a Caddy IP allowlist example.
- Added audit-friendly gateway and control-plane log events with stable fields and token redaction tests.
- Added operational runbooks for production Compose deploys, health triage, backups, restores, token rotation, upgrades, rollbacks, and incidents.
- Added gateway request outcome metrics and control-plane readiness/inventory metrics.
- Added a CI-enforced production hardening check for the production Compose stack and control-plane allowlist override.

## [0.6.0] - 2026-06-30

### Added
- Added control-plane reserved subdomain storage, Postgres migration, admin APIs, and validator authorization API.
- Added `porthook reserved create`, `porthook reserved list`, and `porthook reserved delete` for reserved subdomain administration.
- Added gateway authorization for requested subdomains through the control plane.
- Added dashboard reserved subdomain administration and active gateway tunnel visibility.

### Changed
- Extended the control-plane smoke test and self-hosted documentation for reserved subdomain workflows.

## [0.5.0] - 2026-06-30

### Added
- Added a self-hosted dashboard MVP at `/dashboard/` for admin token login, token listing, token creation, token revocation, and control-plane readiness status.
- Added `last_used_at` token metadata updates on successful control-plane token validation and exposed it in token list API, CLI output, and dashboard tables.
- Added versioned Postgres migrations and startup schema verification for control-plane token storage with `schema_migrations` tracking.
- Added upgrade notes and self-hosted deployment guidance for the dashboard and control-plane migrations.
- Added `GET /api/v1/status` for control-plane readiness and version status, and extended the control-plane smoke test to verify dashboard assets.

## [0.4.0] - 2026-06-30

### Added
- Added a control-plane integration smoke test for token creation, CLI login, gateway validation, and HTTP round-trip.
- Added a Docker Compose control-plane stack with Postgres, control-plane, and gateway services.
- Added `porthook login --token-stdin` and hidden interactive token prompts for safer login input.
- Added `porthook tokens create`, `porthook tokens list`, and `porthook tokens revoke` for self-hosted control-plane token management.
- Added `GET /api/v1/tokens` for admin token listing without plaintext token exposure.
- Added control-plane token admin operation logs without plaintext token values.
- Added Prometheus text `/metrics` endpoints for gateway and control-plane operational counters.

### Changed
- Updated the release workflow, release documentation, and security policy for the supported `0.4.x` release line.
- Improved self-hosted Docker Compose quickstart, deployment checklist, secret guidance, and operational endpoint documentation.
- Hardened control-plane token APIs with strict JSON decoding, request body limits, method `Allow` headers, and explicit unknown-scope errors.
- Control-plane `/readyz` now checks token store readiness, including Postgres ping when configured.
- Improved `porthook` and `porthook tokens` help output, environment default coverage, and control-plane token command error guidance.

## [0.3.0] - 2026-06-29

### Added
- Added basic per-tunnel gateway request rate limiting with configurable request-per-second and burst limits.
- Added a self-hosted control-plane token API with create, validate, and revoke endpoints.
- Added optional gateway token validation through the control plane.
- Added a shared gateway-to-control-plane validator token for token validation requests.
- Added `porthook login` and `porthook logout` for saved agent server/token configuration.
- Added `porthook-control-plane` to local and release builds.

### Fixed
- Switched gateway static token and control-plane bearer checks to constant-time secret comparisons.
- Disabled control-plane token creation and revocation when no admin token is configured.
- Updated the agent protocol mismatch test expectation to match the current explicit version error.

## [0.2.0] - 2026-06-28

### Added
- Added protocol compatibility negotiation on startup via `auth.request` and `auth.ok`.
- Added protocol version (`protocol_version`) and capability list (`capabilities`) to auth messages.
- Added required capability validation in gateway and agent handshake (`stream_start_end`, `binary_body_frames`, `stream_cancel`).
- Added clear `unsupported_protocol` auth errors with actionable messages.

### Changed
- Gateway and agent now include richer protocol compatibility checks before tunnel registration.
- Updated technical and protocol specs with explicit compatibility semantics.

### Fixed
- Improved diagnostics for incompatible protocol negotiation and removed ambiguous startup failures.
