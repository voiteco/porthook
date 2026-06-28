# Changelog

All notable changes to Porthook are documented here.

## [Unreleased]

- No unreleased changes.

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
