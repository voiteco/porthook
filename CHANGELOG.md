# Changelog

All notable changes to Porthook are documented here.

## [Unreleased]

### Added
- Added a control-plane integration smoke test for token creation, CLI login, gateway validation, and HTTP round-trip.
- Added a Docker Compose control-plane stack with Postgres, control-plane, and gateway services.
- Added `porthook login --token-stdin` and hidden interactive token prompts for safer login input.
- Added `porthook tokens create`, `porthook tokens list`, and `porthook tokens revoke` for self-hosted control-plane token management.
- Added `GET /api/v1/tokens` for admin token listing without plaintext token exposure.
- Added control-plane token admin operation logs without plaintext token values.

### Changed
- Hardened control-plane token APIs with strict JSON decoding, request body limits, method `Allow` headers, and explicit unknown-scope errors.

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
