# Production Roadmap

## Objective

The objective of this roadmap is to take the public self-hosted Porthook product from its current pre-1.0 state to a production-stable `v1.0.0` release.

This document is the execution source of truth for that work. Tasks are ordered by dependency and risk. A task is complete only after its implementation, tests, documentation, commit, push, and required CI checks are complete.

Current phase: Blocks 1 through 7 are complete. Block 8 is ready to start.

## Supported v1 Scope

The first stable release intentionally supports a narrow, testable deployment shape.

Included in `v1.0.0`:

- A single self-hosted gateway, control plane, and Postgres database behind a reverse proxy.
- Linux `amd64` and `arm64` server images.
- Linux, macOS, and Windows CLI agent binaries.
- Public HTTP, HTTPS, WebSocket, and streaming/SSE traffic.
- Wildcard subdomain routing.
- Reserved subdomains, access policies, and custom domain mappings.
- Operator-managed TLS, with Caddy as the primary reference deployment.
- Scoped administrative access, durable operational history, metrics, traces, backups, upgrades, and rollback procedures.
- Compatibility guarantees for stable `1.x` protocol, API, configuration, and database changes.

Deferred until after `v1.0.0`:

- Multi-gateway high availability and distributed tunnel routing.
- Multi-region routing.
- Raw TCP and UDP tunnels.
- Zero-downtime tunnel handoff between gateways.
- Hosted-service, tenant, organization, billing, and other cloud-only features.

## Release Sequence

| Release | Purpose |
| --- | --- |
| `v0.14.0` | Reconcile and release the current baseline. |
| `v0.15.0` | Establish mandatory quality and security gates. |
| `v0.15.1` | Close gateway management and access-boundary risks. |
| `v0.16.0` | Complete the public traffic protocol and verify the real TLS edge path. |
| `v0.17.0` | Publish production-shaped distribution artifacts for broad beta use. |
| `v0.18.0` | Establish a measured reliability and capacity envelope. |
| `v0.19.0` | Freeze features and complete production rehearsals. |
| `v1.0.0-rc.N` | Validate release candidates without adding features. |
| `v1.0.0` | Publish the production-stable release. |

## Execution Rules

Work through the blocks in order. Do not start the next block until the current block's exit gate is complete.

For every coherent change:

1. Define or update the behavioral contract and tests.
2. Implement the smallest complete change that advances the current block.
3. Run focused tests, then the repository checks appropriate to the change.
4. Update configuration examples, operational documentation, upgrade notes, and the changelog when behavior changes.
5. Review the diff for secrets, private information, compatibility regressions, and unrelated edits.
6. Create a focused Conventional Commit and push it.
7. Wait for required GitHub Actions checks to pass before continuing.

Only mark a checkbox complete after its code and documentation are pushed and CI is green. Update this roadmap as part of the final documentation commit for each block.

## Block 1: Release the Current Baseline

Target: `v0.14.0`

When this roadmap was created, `v0.12.0` was the latest release while `main` contained the release-stability and scoped-admin-token work described as `0.13.x` and `0.14.x`. That state was released as one honest baseline instead of synthesizing a historical `v0.13.0` release.

Tasks:

- [x] Move the current `Unreleased` changelog entries into a dated `0.14.0` section.
- [x] Replace the draft `0.12 -> 0.13 -> 0.14` upgrade sequence with a direct `0.12 -> 0.14` procedure.
- [x] Update `SECURITY.md` to identify `0.14.x` and `main` as the supported lines.
- [x] Add a concise known-limitations section covering the public management endpoints, proxy client IP behavior, supported traffic, single-node topology, packaging, and TLS expectations.
- [x] Ensure README status and installation claims match the release artifacts, especially Windows support.
- [x] Run the complete existing test, smoke, Compose, image, and release-verification suite.
- [x] Prepare, tag, push, and verify `v0.14.0`, including checksums and embedded versions.
- [x] Verify the published release from a clean directory using only release documentation and assets.

Exit gate:

- [x] `v0.14.0` is published, installable, documented as pre-production, and has a green release workflow.
- [x] No public documentation refers to an unreleased `v0.13.0` as an available upgrade step.

## Block 2: Make Quality and Security Checks Mandatory

Target: `v0.15.0`

Tasks:

- [x] Fix the concurrent test log-buffer access reported by `go test -race ./...`.
- [x] Add a `make race` target and make a complete race run a required CI and release check.
- [x] Add fuzz targets for protocol envelopes, binary body frames, forwarded headers, hostname parsing, and cursor/filter parsing.
- [x] Run fuzz seed corpora in normal CI and provide a bounded extended fuzz workflow.
- [x] Add `govulncheck` for Go dependencies and reachable vulnerable code.
- [x] Add CodeQL or an equivalent maintained static-analysis workflow.
- [x] Add automated dependency update configuration for Go modules and GitHub Actions.
- [x] Add repository secret scanning and fail CI on committed test credentials that are not explicit fixtures.
- [x] Add `docs/THREAT_MODEL.md` covering trust boundaries, assets, attacker capabilities, abuse cases, and accepted v1 risks.
- [x] Configure the new checks as required release gates.

Exit gate:

- [x] Unit, smoke, race, fuzz-seed, vulnerability, static-analysis, and secret checks pass on `main`.
- [x] The threat model explicitly informs Blocks 3 through 6.

## Block 3: Isolate the Gateway Management Surface

Target: `v0.15.1`

The public listener must be transparent to tunneled applications. Operational routes must not expose metadata or reserve application paths on wildcard tunnel hosts.

Tasks:

- [x] Add a dedicated gateway management listener, configured separately from public and agent listeners.
- [x] Move health, readiness, metrics, tunnel inventory, runtime diagnostics, and request-log APIs off the public listener.
- [x] Keep unauthenticated health and readiness endpoints private to the management network.
- [x] Protect management data endpoints with a dedicated control-plane-to-gateway service credential.
- [x] Add authenticated control-plane operator endpoints for gateway tunnels, runtime, metrics, and request logs.
- [x] Enforce `runtime_diagnostics` for runtime, metrics, and tunnel inventory, and `audit_history` for request-log history.
- [x] Move dashboard diagnostics and gateway history reads to the authenticated control-plane endpoints.
- [x] Move CLI `doctor`, `tunnels`, `history requests`, and `export` workflows to the authenticated operator path.
- [x] Update Compose, Caddy, healthchecks, configuration validation, and production-hardening checks for the private management listener.
- [x] Remove public operational CORS behavior that is no longer required.
- [x] Add regression tests proving `/healthz`, `/metrics`, and `/api/v1/*` can be forwarded unchanged to a tunneled application.

Exit gate:

- [x] Wildcard tunnel hosts expose no Porthook operational metadata.
- [x] Every application path is transparent on the public listener.
- [x] Dashboard and CLI operational workflows require the correct scoped admin token.

## Block 4: Harden Client Identity and Access Policies

Target: `v0.15.1`

Tasks:

- [x] Add a shared client-IP resolver for gateway and control-plane HTTP handlers.
- [x] Define explicit trusted-proxy configuration and reject forwarded client headers from untrusted peers.
- [x] Parse proxy chains consistently and select the first untrusted client address.
- [x] Give the production Compose network an explicit trust configuration suitable for Caddy forwarding.
- [x] Use the resolved client address in access-policy evaluation, forwarded headers, request logs, audit events, traces, and rate-limit keys where applicable.
- [x] Add direct, trusted-proxy, multi-proxy, IPv4, IPv6, and spoofed-header tests.
- [x] Add an end-to-end Caddy test proving tunnel IP allowlists use the original client address.
- [x] Introduce versioned Argon2id or bcrypt hashes for user-selected basic-auth passwords.
- [x] Preserve constant-time verification and lazily upgrade legacy password hashes after successful authentication.
- [x] Keep fast SHA-256 lookup hashes only for generated high-entropy bearer, agent, admin, and service tokens.
- [x] Add bounded authentication-attempt protection without preventing normal tunnel traffic.
- [x] Verify that plaintext credentials and reusable authorization values never appear in logs, metrics, traces, exports, or API summaries.

Exit gate:

- [x] IP allowlists and audit client addresses are correct through the supported Caddy deployment.
- [x] User-selected passwords are no longer stored as unsalted fast hashes.
- [x] Forwarded-header spoofing tests pass for every supported deployment mode.

## Block 5: Complete the Public Traffic Protocol

Target: `v0.16.0`

Tasks:

- [x] Specify the next protocol revision and additive capability-negotiation rules.
- [x] Preserve HTTP interoperability with protocol `v0.2` agents for a documented compatibility window.
- [x] Define WebSocket open, accept, text, binary, ping/pong, close, cancellation, and error messages.
- [x] Implement public WebSocket upgrade handling in the gateway and local WebSocket dialing in the agent.
- [x] Add per-stream flow control, bounded buffering, frame-size limits, cancellation, and backpressure.
- [x] Preserve relevant request headers and close status without forwarding unsafe hop-by-hop state.
- [x] Replace the short absolute stream deadline with configurable request, idle, and maximum-lifetime policies suitable for SSE and long polling.
- [x] Verify response flushing, repeated headers, cookies, binary bodies, chunked requests, client cancellation, slow clients, and local-service failures.
- [x] Add mixed-version tests using independently built agent and gateway binaries.
- [x] Update the protocol and technical specifications before releasing the new protocol revision.

Exit gate:

- [x] HTTP, streaming/SSE, and WebSocket paths pass local and control-plane-backed end-to-end tests.
- [x] Compatible versions negotiate capabilities, and incompatible versions fail with an actionable error.
- [x] No stream can grow memory or goroutine usage without a configured bound.

## Block 6: Verify the Real TLS and Custom-Domain Edge

Target: `v0.16.0`

Tasks:

- [x] Add an end-to-end edge smoke environment using Caddy and certificates generated at test time.
- [x] Verify wildcard tunnel HTTPS, the dedicated agent WebSocket hostname, and the protected control-plane hostname.
- [x] Verify certificate coverage and routing for separate wildcard, agent, control-plane, and custom-domain names.
- [x] Add a supported Caddy configuration path for routing explicitly configured custom domains to the gateway.
- [x] Run the complete custom-domain create, DNS verification, activation, TLS, routing, access-policy, and deletion lifecycle.
- [x] Verify `X-Forwarded-*`, original host, request ID, and client-address behavior across the edge.
- [x] Add configuration checks for inconsistent public URL, root domain, proxy, and certificate settings where they can be validated safely.
- [x] Document certificate issuance, renewal, reload, expiry monitoring, and rollback without committing certificate material.
- [x] Run the TLS edge smoke in CI and the release workflow.

Exit gate:

- [x] The checked-in production deployment completes real HTTPS and secure WebSocket round trips.
- [x] Custom domains work through the documented proxy and operator-managed TLS flow.
- [x] Test certificates and private keys are generated ephemerally and never committed.

## Block 7: Publish Production-Shaped Distribution Artifacts

Target: `v0.17.0`

Tasks:

- [x] Publish versioned Linux `amd64` and `arm64` gateway and control-plane images to GHCR.
- [x] Add immutable version, commit, architecture, license, and source OCI labels.
- [x] Publish architecture manifests and immutable digest references.
- [x] Provide a release Compose bundle that pulls published images and does not require a source checkout or local build.
- [x] Keep source-build Compose files available for contributors without making them the production default.
- [x] Publish Linux, macOS, and Windows CLI agent artifacts for supported architectures.
- [x] Package binaries consistently, including license notices, version output, checksums, and installation metadata.
- [x] Generate SPDX or CycloneDX SBOMs for binaries and images.
- [x] Add keyless signing and build provenance/attestations for release artifacts and images.
- [x] Provide a checksum-verifying installer or maintained package-manager path without bypassing artifact verification.
- [x] Test clean installation and startup from release artifacts on Linux `amd64`, Linux `arm64`, macOS, and Windows.
- [x] Document upgrades by immutable image tag or digest and prevent accidental deployment of development tags.

Exit gate:

- [x] A new operator can deploy the supported stack using only published release artifacts and public documentation.
- [x] Every artifact has a checksum, SBOM, provenance, and a verifiable signature or attestation.
- [x] `v0.17.0` is suitable for broad external beta use, but remains explicitly pre-1.0.

## Block 8: Establish Reliability and Capacity Limits

Target: `v0.18.0`

Tasks:

- [x] Add a reproducible load harness and document reference hardware and network conditions.
- [x] Verify an initial minimum profile of 100 active tunnels, 500 concurrent streams across tunnels, and 100 aggregate requests per second.
- [x] Run sustained load for at least 30 minutes with no unexpected errors or unbounded resource growth.
- [ ] Run a 72-hour soak covering HTTP, streaming, WebSocket, reconnects, and operational-history retention. (Deliberately deferred: GitHub Actions' 6-hour job cap rules out an unattended 72-hour run in CI. Accepted for now as a 33-minute representative soak plus a weekly 5-hour scheduled soak (`.github/workflows/soak.yml`); the literal 72-hour run remains an operator-executed procedure ahead of the `v1.0.0` release rehearsal in Block 9 — see docs/RELIABILITY.md.)
- [x] Track and assert memory, goroutine, file-descriptor, WebSocket, database-connection, latency, and error-rate behavior.
- [x] Exercise agent disconnects, gateway graceful shutdown, reverse-proxy restart, control-plane outage, Postgres restart, slow Postgres, and network interruption.
- [x] Define and verify recovery bounds for each supported failure mode.
- [x] Configure and document database pool limits, migration serialization, retention performance, and disk-capacity planning.
- [x] Verify backup, restore, upgrade, rollback, and retention pruning with realistic stored data.
- [x] Add Prometheus alert examples for readiness, error rate, tunnel churn, saturation, database failures, and certificate expiry.
- [x] Publish measured capacity limits and avoid claims beyond the tested single-node envelope.
- [x] Add bounded CI smoke coverage and a separate release/soak workflow for expensive scenarios.

Exit gate:

- [ ] The reference load and 72-hour soak complete without leaks, deadlocks, data corruption, or unexplained errors. (The 30-minute reference load and a 33-minute representative soak have, with no leaks, deadlocks, corruption, or unexplained errors; the literal 72-hour soak is deliberately deferred — accepted for now per the task note above — see docs/RELIABILITY.md.)
- [x] Failure recovery and supported capacity are measurable, documented, and repeatable.

## Block 9: Freeze, Rehearse, and Release v1

Targets: `v0.19.0`, `v1.0.0-rc.N`, and `v1.0.0`

Tasks:

- [ ] Freeze new features at `v0.19.0`; accept only release blockers, compatibility fixes, tests, and documentation afterward.
- [x] Publish the stable protocol/API/configuration/database compatibility matrix.
- [x] Publish the `1.x` deprecation, migration, supported-version, and security-fix policies.
- [x] Complete clean production-shaped installations on Linux `amd64` and `arm64` hosts.
- [x] Upgrade durable deployments from `v0.14.0` and `v0.17.0` through the documented paths.
- [x] Restore a production-shaped backup on a fresh host and verify tokens, reservations, policies, domains, audit events, and request history.
- [x] Rehearse code rollback and database-compatible rollback for every migration introduced after `v0.14.0`.
- [ ] Complete at least two independent self-hosted deployment evaluations using only public documentation. (Deferred: revisit before promoting the qualified RC to `v1.0.0`, not before `v1.0.0-rc.1`. Needs genuinely independent evaluators, not the assistant that built the codebase.)
- [x] Perform a final security review against the threat model and resolve all release-blocking findings.
- [ ] Publish `v1.0.0-rc.1` from the complete release pipeline.
- [ ] Allow only blocker fixes in subsequent release candidates and repeat affected qualification suites.
- [ ] Run the selected release candidate for at least 14 days without an unresolved P0 or P1 defect.
- [ ] Prepare the final changelog, upgrade guide, release notes, checksums, image digests, SBOMs, attestations, and rollback references.
- [ ] Publish `v1.0.0` from source equivalent to the qualified release candidate.
- [ ] Run post-release install, upgrade, tunnel, dashboard, backup, and rollback smoke checks against published artifacts.

Exit gate:

- [ ] All roadmap tasks and global release gates are complete.
- [ ] `v1.0.0` is published, reproducible, installable, supportable, and stable within the documented single-node scope.

## Global v1 Release Gates

All of these conditions are mandatory for `v1.0.0`:

- [ ] The public listener is transparent and exposes no management data.
- [ ] Scoped authorization, trusted-proxy handling, IP allowlists, and password storage meet the threat-model requirements.
- [ ] HTTP, HTTPS, WebSocket, streaming/SSE, wildcard domains, and custom domains pass end-to-end tests.
- [ ] Unit, integration, smoke, race, fuzz-seed, vulnerability, static-analysis, secret, Compose, image, and release checks are green.
- [ ] Published binaries and images are versioned, checksummed, signed or attested, and accompanied by SBOMs.
- [ ] Installation does not require a source checkout.
- [ ] Backup, restore, upgrade, and rollback have succeeded on fresh production-shaped hosts.
- [ ] The reference load, failure matrix, and 72-hour soak have passed.
- [ ] The selected release candidate has completed the qualification period without unresolved P0 or P1 defects.
- [ ] README, specifications, operations, security, installation, release, upgrade, and known-limitations documentation agree with shipped behavior.
- [ ] No secrets, private operations, hosted-service internals, or non-public product material are present in the repository or artifacts.

## Scope Changes

Changes to the supported v1 scope or release gates must update this document explicitly. Do not silently defer an incomplete gate, mark partial work complete, or expand the roadmap with hosted-service concerns. Any deferred item must remain compatible with the stable single-node guarantees made by `v1.0.0`.
