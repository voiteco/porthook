# Changelog

All notable changes to Porthook are documented here.

## [Unreleased]

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
