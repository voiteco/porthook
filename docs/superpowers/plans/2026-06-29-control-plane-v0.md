# Control Plane V0 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a minimal self-hosted control plane for API token creation, validation, and revocation, then wire the gateway and CLI to use it while preserving static-token local development.

**Architecture:** The control plane exposes HTTP endpoints backed by a token service and storage interface. The storage package includes an in-memory implementation for tests and local composition of behavior, plus a Postgres-backed implementation for production wiring. The gateway can validate agent tokens against the control plane when `PORTHOOK_CONTROL_PLANE_URL` is configured, otherwise it keeps the current static-token fallback.

**Tech Stack:** Go standard library HTTP, `database/sql`, PostgreSQL driver, existing `nhooyr.io/websocket`, existing Makefile and smoke tests.

---

### Task 1: Token Domain And Store

**Files:**
- Create: `server/control-plane/internal/tokens/token.go`
- Create: `server/control-plane/internal/tokens/memory_store.go`
- Create: `server/control-plane/internal/tokens/service.go`
- Test: `server/control-plane/internal/tokens/service_test.go`

- [ ] Write failing token-service tests for create, validate, revoke, and scoped validation.
- [ ] Implement token generation, hashing, in-memory persistence, validation, and revocation.
- [ ] Run `go test ./server/control-plane/internal/tokens`.
- [ ] Commit: `Add control-plane token service`.

### Task 2: Control Plane HTTP API

**Files:**
- Create: `server/control-plane/internal/controlplane/config.go`
- Create: `server/control-plane/internal/controlplane/server.go`
- Create: `server/control-plane/cmd/porthook-control-plane/main.go`
- Test: `server/control-plane/internal/controlplane/server_test.go`
- Test: `server/control-plane/cmd/porthook-control-plane/main_test.go`

- [ ] Write failing HTTP API tests for `POST /api/v1/tokens`, `POST /api/v1/tokens/validate`, `DELETE /api/v1/tokens/{id}`, `/healthz`, and `/readyz`.
- [ ] Implement the control-plane server and command.
- [ ] Run `go test ./server/control-plane/...`.
- [ ] Commit: `Add control-plane token API`.

### Task 3: Postgres Store Wiring

**Files:**
- Create: `server/control-plane/internal/tokens/postgres_store.go`
- Create: `server/control-plane/internal/tokens/schema.go`
- Modify: `server/control-plane/internal/controlplane/config.go`
- Modify: `server/control-plane/cmd/porthook-control-plane/main.go`

- [ ] Add Postgres store implementation using `database/sql`.
- [ ] Add startup schema migration for the token table.
- [ ] Keep tests on store contract with in-memory storage; avoid requiring Postgres in unit tests.
- [ ] Run `go test ./server/control-plane/...`.
- [ ] Commit: `Add Postgres token store`.

### Task 4: Gateway Token Validation

**Files:**
- Create: `server/gateway/internal/gateway/token_validator.go`
- Modify: `server/gateway/internal/gateway/config.go`
- Modify: `server/gateway/internal/gateway/server.go`
- Test: `server/gateway/internal/gateway/server_test.go`

- [ ] Write failing gateway tests proving control-plane validation accepts valid tokens, rejects invalid tokens, and static-token fallback still works.
- [ ] Implement configurable control-plane validation.
- [ ] Run `go test ./server/gateway/internal/gateway`.
- [ ] Commit: `Validate gateway tokens via control plane`.

### Task 5: CLI Login And Logout

**Files:**
- Create: `agent/internal/agent/config_file.go`
- Modify: `agent/internal/agent/config.go`
- Modify: `agent/cmd/porthook/main.go`
- Test: `agent/internal/agent/config_file_test.go`
- Test: `agent/cmd/porthook/main_test.go`

- [ ] Write failing CLI tests for `login`, `logout`, config-file loading, and flag/env precedence.
- [ ] Implement config file read/write/delete using JSON and `PORTHOOK_CONFIG_PATH` test override.
- [ ] Run `go test ./agent/...`.
- [ ] Commit: `Add CLI login and logout`.

### Task 6: Self-Hosted Documentation

**Files:**
- Modify: `README.md`
- Modify: `docs/SPEC.md`
- Modify: `docs/TECHNICAL_SPEC.md`
- Modify: `server/control-plane/README.md`
- Modify: `server/gateway/README.md`
- Modify: `deploy/README.md`
- Modify: `deploy/compose/README.md`
- Modify: `CHANGELOG.md`

- [ ] Document control-plane API, token setup, gateway validation fallback, CLI login/logout, and production DNS/TLS notes.
- [ ] Run `make fmt-check`, `go test ./...`, `go vet ./...`, and `make smoke-local`.
- [ ] Commit: `Document control-plane self-hosting`.

### Task 7: Final Integration

**Files:**
- Existing files only.

- [ ] Run `git diff --check`.
- [ ] Run `go test ./...`.
- [ ] Run `go vet ./...`.
- [ ] Run `make smoke-local`.
- [ ] Merge branch to `main`.
- [ ] Push `main`.
