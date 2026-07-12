# Reliability and Capacity

This document states what has actually been measured for a single-node Porthook deployment (one gateway, one control plane, one Postgres database, behind a reverse proxy). It intentionally does not extrapolate beyond what was tested. See [docs/PRODUCTION_ROADMAP.md](./PRODUCTION_ROADMAP.md) Block 8 for the acceptance criteria this document satisfies.

Multi-gateway high availability, distributed routing, and multi-region deployment are out of scope for `v1.0.0` (see the roadmap's Supported v1 Scope); this document only covers the single-node shape.

## Load Harness

- `scripts/testdata/loadgen` is a standalone Go traffic generator that drives sustained public HTTP and WebSocket load against a gateway's public listener. It only talks to public endpoints — it does not depend on any internal package — so it can run against any real, already-registered set of tunnels. See its `envString`/`envInt`/`envDuration` config functions for the full list of `LOADGEN_*` environment variables.
- `scripts/smoke-capacity.sh` orchestrates a full local capacity run: it starts a real Postgres container, a real `porthook-gateway`, N real `porthook` agent CLI processes (one per tunnel, all forwarding to a shared local echo/backend service), confirms the tunnels are active via the management API, then runs `loadgen` against them while sampling the gateway's `/metrics` endpoint on an interval. It exits non-zero if the load generator's error rate exceeds `PORTHOOK_SMOKE_MAX_ERROR_RATE` (default 1%).
- Bounded defaults (a handful of tunnels/streams for a few seconds) keep it cheap enough to run in every CI pass. The full reference profile below was produced by overriding the `PORTHOOK_SMOKE_*` environment variables documented at the top of the script, for example:

  ```sh
  PORTHOOK_SMOKE_TUNNEL_COUNT=100 \
  PORTHOOK_SMOKE_WS_STREAMS=500 \
  PORTHOOK_SMOKE_TARGET_RPS=100 \
  PORTHOOK_SMOKE_DURATION=30m \
  PORTHOOK_SMOKE_DB_MAX_OPEN_CONNS=20 \
  PORTHOOK_SMOKE_KEEP_LOGS=1 \
    ./scripts/smoke-capacity.sh
  ```

## Reference Hardware and Network Conditions

The numbers in this document were measured on a single machine, with the gateway, control plane, Postgres, agents, and load generator all running as local processes/containers talking over loopback (`127.0.0.1`). There is no real network between components: no cross-host latency, no packet loss, no NAT, no TLS termination overhead (the reference deployment terminates TLS at Caddy in front of the gateway; the load harness talks to the gateway's plain-HTTP public listener directly, matching how `scripts/smoke-tls-edge.sh` separately verifies the TLS edge path). Treat every number below as an upper bound on what a real deployment across a real network will see, not a guarantee.

| | |
| --- | --- |
| CPU | Apple M5, 10 cores |
| Memory | 24 GB |
| OS | macOS (Darwin 25.5.0), arm64 |
| Go | go1.26.5 |
| Docker | 29.4.0 (Postgres 16-alpine container) |
| Network | loopback only |

## Measured Capacity Profile

Reference run (`./scripts/smoke-capacity.sh` with `PORTHOOK_SMOKE_TUNNEL_COUNT=100 PORTHOOK_SMOKE_WS_STREAMS=500 PORTHOOK_SMOKE_TARGET_RPS=100 PORTHOOK_SMOKE_DURATION=30m PORTHOOK_SMOKE_DB_MAX_OPEN_CONNS=20`), against a real gateway with a real Postgres-backed durable request-log store:

| Metric | Result |
| --- | --- |
| Active tunnels | 100 |
| Concurrent WebSocket streams | 500 (held at exactly 500 for the full run) |
| Target / achieved aggregate request rate | 100 req/s target, 99.98 req/s achieved |
| Duration | 30 minutes |
| Total HTTP requests | 179,962 |
| HTTP errors | 0 (0.0000 error rate) |
| WebSocket round trips | 179,500 |
| WebSocket errors | 0 |
| Latency p50 / p95 / p99 | 1.18ms / 5.39ms / 10.75ms |
| Latency max | 1021.5ms (a single outlier; not sustained — see below) |
| Gateway goroutines | Stable, 2,313–2,376 throughout (started at 311 idle, jumped to ~2,320 once 100 tunnels + 500 streams were active, then stayed flat — no unbounded growth) |
| Gateway heap | Oscillated 18–37MB throughout in a normal GC sawtooth, no monotonic growth |
| Gateway DB pool | Settled at 5 open connections (default `PORTHOOK_DB_MAX_OPEN_CONNS=20`), with one brief spike to 20/20 (open/in-use) |
| Gateway DB pool waits | `porthook_gateway_db_wait_count_total` grew from 0 to 877 over the 30 minutes (~0.49% of requests), consistent with the pool-sizing finding below — did not cause any public request failures |

Zero errors, zero WebSocket errors, and flat goroutine/heap trends over the full 30-minute run satisfy the roadmap's exit gate for the initial minimum profile: "100 active tunnels, 500 concurrent streams across tunnels, and 100 aggregate requests per second... sustained load for at least 30 minutes with no unexpected errors or unbounded resource growth." The one 1021.5ms latency outlier was a single sample (out of 359,462 latency samples across both HTTP and WebSocket round trips) and did not recur or trend upward — most likely a single GC pause or scheduling hiccup, not a systemic issue, but noted here rather than hidden.

The full 72-hour soak required by the roadmap (covering HTTP, streaming, WebSocket, reconnects, and operational-history retention) is a separate, longer-running exercise — see the Soak Test section below.

## Failure Modes and Recovery Bounds

`scripts/smoke-failure-injection.sh` exercises the failure modes required by the roadmap end to end against real processes (a real gateway, control plane, Postgres container, and `porthook` agent binaries — not mocks), and measures actual recovery time against a documented bound for each. Run it with `./scripts/smoke-failure-injection.sh`; override `PORTHOOK_SMOKE_*_BOUND_SECONDS` variables at the top of the script to tighten or loosen the assertions.

All 7 required failure modes pass with the following measured recovery times (single-node, loopback-network conditions as above; treat as an upper bound, not a guarantee, on a real network):

| Failure mode | Behavior during the failure | Measured recovery |
| --- | --- | --- |
| Agent disconnect (crash) | Gateway detects the dropped connection and removes the tunnel from `/api/v1/tunnels` | ~0s to detect; agent re-establishes the tunnel in ~0.1s after restart |
| Control-plane outage | New tunnel registrations are correctly rejected (agents retry with backoff rather than giving up — see the `auth_unavailable` fix below); **already-registered tunnels fail closed with 503**, not silent 200s, because access-policy evaluation is a per-request control-plane dependency (see below) | New tunnel registers ~2s after control plane recovers; existing tunnel resumes serving immediately (~0s) once control plane is reachable again |
| Reverse-proxy (Caddy) restart | Gateway keeps serving requests directly on its own public port throughout, since Caddy is a separate, independent process in front of it | Caddy itself resumes serving in ~0.2s after restart |
| Postgres restart | Same fail-closed 503 behavior as a control-plane outage (access-policy evaluation depends on the control plane's own database), not a crash or hang; gateway and control-plane processes both survive the restart without exiting | Control plane reports ready again ~0s after Postgres becomes reachable |
| Slow Postgres (CPU-throttled to 5% via `docker update --cpus`) | Public requests keep succeeding — request-log durability degrades independently of public traffic, per the pool-sizing section below | N/A (no outage to recover from) |
| Gateway graceful shutdown | Process exits within its configured `PORTHOOK_SHUTDOWN_TIMEOUT` | Agent reconnects ~1.3s after the gateway process comes back up |
| Network interruption (agent process frozen with `SIGSTOP`, simulating a stalled connection rather than a clean disconnect) | Requests fail gracefully (timeout, not a gateway crash) while the agent is unresponsive | Requests recover immediately (~0s) once the agent is resumed (`SIGCONT`); a real network partition that the gateway's WebSocket keepalive (ping every `PORTHOOK_WS_PING_INTERVAL`, default 15s, pong timeout `PORTHOOK_WS_PONG_TIMEOUT`, default 5s) has to detect and close would additionally need the agent's normal reconnect-with-backoff path, as in the crash scenario above |

Two non-obvious, important facts fell out of building and running this suite against the real system (not assumed, discovered by running it):

1. **Access-policy evaluation is a per-request control-plane dependency; token validation is not.** A registered agent's token is validated once, at registration (`authenticateAgent` in `server.go`), so an existing tunnel does not need the control plane to keep accepting new connections from its own agent. But every *public* request additionally calls `EvaluateAccessPolicy` against the control plane (`evaluatePublicAccess` in `server.go`), because access policies (basic auth, IP allowlists, revocation) must take effect immediately without requiring agents to reconnect. This means a control-plane or Postgres outage causes existing tunnels to start returning `503 access_policy_unavailable` for every request, not just block new registrations — a deliberate fail-closed security tradeoff, not a bug, but one worth knowing before you assume "the control plane is just for registration."
2. **`auth_unavailable` is now correctly retried instead of treated as fatal.** Building this test suite surfaced a real bug: when the gateway could not reach its token validator (control-plane outage), it sent the agent an `auth_unavailable` error using the same message type as an actually-invalid token (`invalid_token`), and the agent treated *every* auth error as permanent and exited instead of retrying. Fixed in `agent/internal/agent/runner.go` — `auth_unavailable` now uses the normal reconnect-with-backoff path, while `invalid_token` and `unsupported_protocol` remain correctly non-retryable (retrying with the same bad token or an incompatible protocol version can never succeed).

## Backup, Restore, Upgrade, Rollback, and Retention Pruning

`scripts/smoke-backup-restore.sh` verifies all five against a real Postgres database with a realistic volume of stored data (tokens, reserved subdomains, access policies, and hundreds of request-log/audit-event rows generated by real traffic, not fixtures), in three phases:

1. **Upgrade / rollback.** Starts the previous published release's binaries (downloaded and attestation-verified via `scripts/install.sh --binary ...`, cached locally after the first run), seeds a token/reservation/tunnel, swaps in the binaries built from the current checkout ("upgrade"), verifies the data survived and the tunnel still serves, then swaps back to the previous release's binaries ("rollback") and verifies those still start and serve correctly against the now-current schema. This exercises the additive-migrations compatibility claim in [docs/OPERATIONS.md](./OPERATIONS.md) with real binaries, not just an assumption — schema checks (e.g. `verifySchema` in `request_log_postgres_store.go`) count a *minimum* required set of columns rather than an exact match, which is exactly what makes an older binary tolerate a newer, additively-migrated schema.
2. **Backup / restore.** Seeds tokens, reservations, access policies, and request-log rows via real CLI operations and real HTTP traffic, takes a `pg_dump` backup, wipes the database (`DROP SCHEMA public CASCADE`) to simulate total data loss, restores from the backup, and verifies every row count (tokens, reservations, access policies, audit events, request logs) matches exactly, plus a spot-checked row's content survives byte-for-byte, plus a tunnel serves live traffic again afterward.
3. **Retention pruning.** Seeds request-log rows 60 days old and audit-event rows 120 days old alongside recent rows, restarts with a short `PORTHOOK_REQUEST_LOG_RETENTION`/`PORTHOOK_AUDIT_EVENT_RETENTION` (pruning runs once immediately at startup, not just on the ticker), and verifies the old rows are gone while the recent ones remain.

Two more things worth knowing, found by actually running this rather than assumed:

- **Background agents make exact audit-event-count comparisons flaky if left running across a restart.** Every public request triggers a control-plane `access_policy_evaluated` audit event (see above), and an agent that's still running when the gateway restarts auto-reconnects and re-authenticates, generating a few more. The script stops seed agents before the backup snapshot and only restarts one, deliberately, after restore — a real operational backup procedure should likewise expect some in-flight audit/request-log activity during the exact backup window, not treat a live system as byte-for-byte deterministic mid-restart.
- **Repeated unauthenticated requests against a `basic_auth`-protected tunnel trip the bounded auth-attempt limiter (429), not a plain 401 loop.** Useful to know before writing a polling health check against a protected tunnel from a script — poll a public-mode tunnel, or send real credentials.

## Database Connection Pool Sizing

Every public request that reaches a tunnel writes one durable row to the request-log store synchronously, in the same goroutine that just finished serving the response (`recordRequestLog` in `server/gateway/internal/gateway/server.go`, called from the deferred block in `handlePublicRequest`). This does **not** add latency to the client-facing response — the write happens after the response is already sent — but it does mean request-log durability, not public request serving, is what the database pool bounds under load.

Configure the pool with `PORTHOOK_DB_MAX_OPEN_CONNS`, `PORTHOOK_DB_MAX_IDLE_CONNS`, `PORTHOOK_DB_CONN_MAX_LIFETIME`, and `PORTHOOK_DB_CONN_MAX_IDLE_TIME` (gateway and control plane both support all four; see [docs/TECHNICAL_SPEC.md](./TECHNICAL_SPEC.md)). The default is `PORTHOOK_DB_MAX_OPEN_CONNS=20`.

Measured behavior against a real local Postgres container, with the default pool of 20 connections:

- At a sustained 50 requests/second (90-second run), `porthook_gateway_db_wait_count_total` increased by 31 — roughly 0.7% of requests experienced a pool wait before their log write could proceed.
- At a sustained 100 requests/second (30-minute run, 100 tunnels, 500 concurrent WebSocket streams), `porthook_gateway_db_wait_count_total` grew from 0 to 877 over the full run — roughly 0.49% of the 179,962 total requests.

In both cases this did not fail a single public request (0 errors observed in either run); it only delays (and, if it exceeds `PORTHOOK_REQUEST_LOG_WRITE_TIMEOUT`, default 1s, drops) the request-log entry.

Recommendation: if you enable durable request logging (`PORTHOOK_REQUEST_LOG_DATABASE_URL`), size `PORTHOOK_DB_MAX_OPEN_CONNS` to comfortably exceed your target sustained requests/second, not just your expected number of concurrent HTTP handlers. Watch `porthook_gateway_db_wait_count_total` (see `deploy/prometheus/alerts.yml`'s `PorthookGatewayDBPoolWaiting` alert) — a non-zero, growing rate under steady load is the signal to raise the pool size.

## Migration Serialization

Every Postgres-backed store (gateway request logs; control-plane tokens, admin tokens, reserved subdomains, access policies, custom domains, and audit events) applies its own migrations inside a single transaction at startup, tracked in its own `schema_migrations` table, with `CREATE TABLE IF NOT EXISTS` idempotency but **no explicit cross-process lock** (no `pg_advisory_lock` or equivalent). This is safe for the supported `v1.0.0` deployment shape — one gateway process and one control-plane process, each started once — because there is never more than one process racing to apply the same store's migrations. It is not safe to start multiple replicas of the same service concurrently against a fresh, unmigrated database (not a supported v1 deployment topology; see the roadmap's Supported v1 Scope). If you script a zero-downtime-style rolling restart in the future, apply migrations from a single instance first (for example `configcheck` against the target database, or simply accept the brief unavailability of a single-instance restart as documented in [docs/OPERATIONS.md](./OPERATIONS.md)) rather than starting two instances at once.

## Disk Capacity Planning

Measured directly (100,000 realistic rows inserted into a `gateway_request_logs` table with its `(time DESC, id DESC)` index, using `pg_total_relation_size`, table + index together): **~362 bytes per request-log row.** Audit-event rows are a similar order of magnitude (fewer columns, no dedicated secondary index beyond `time`/`event`/`request_id`).

Approximate steady-state disk usage at the default 30-day `PORTHOOK_REQUEST_LOG_RETENTION`, extrapolated from that measurement (not independently re-measured at each rate — treat as an order-of-magnitude planning figure, not a guarantee):

| Sustained request rate | Rows/day | Approx. disk/day | Approx. steady-state at 30-day retention |
| --- | --- | --- | --- |
| 1 req/s | 86,400 | ~31 MB | ~0.9 GB |
| 10 req/s | 864,000 | ~313 MB | ~9.4 GB |
| 100 req/s (the tested reference profile) | 8,640,000 | ~3.1 GB | ~94 GB |

Recommendation: provision Postgres storage for at least the steady-state figure above for your expected sustained traffic and configured `PORTHOOK_REQUEST_LOG_RETENTION`/`PORTHOOK_AUDIT_EVENT_RETENTION`, with headroom for traffic spikes and the time between pruning ticks (`PORTHOOK_REQUEST_LOG_PRUNE_INTERVAL`/`PORTHOOK_AUDIT_EVENT_PRUNE_INTERVAL`, default 1 hour for both) — rows accumulate for up to one interval before they're eligible for pruning. If durable request logging isn't needed for your deployment, omitting `PORTHOOK_REQUEST_LOG_DATABASE_URL` avoids this storage growth entirely (public traffic serving does not depend on it — see the pool-sizing section above).

## Soak Test

The same `scripts/smoke-capacity.sh` harness doubles as a configurable-duration soak test: set `PORTHOOK_SMOKE_DURATION` to hours, `PORTHOOK_SMOKE_RECONNECT_INTERVAL` to periodically kill and restart a random agent (exercising reconnects under sustained load, not just at steady state), and `PORTHOOK_SMOKE_REQUEST_LOG_RETENTION`/`PORTHOOK_SMOKE_REQUEST_LOG_PRUNE_INTERVAL`/`PORTHOOK_SMOKE_RETENTION_OLD_ROWS` to seed rows older than retention and verify the pruner actually removes them during the run, not just in isolation.

Representative soak run (20 tunnels, 50 WebSocket streams, 50 req/s target, `PORTHOOK_SMOKE_RECONNECT_INTERVAL=45s`, `PORTHOOK_SMOKE_REQUEST_LOG_RETENTION=10m`, `PORTHOOK_SMOKE_REQUEST_LOG_PRUNE_INTERVAL=5m`, `PORTHOOK_SMOKE_DB_MAX_OPEN_CONNS=40`), against a real gateway with a real Postgres-backed durable request-log store:

| Metric | Result |
| --- | --- |
| Duration | 33 minutes (stopped by the interactive environment running it, not a product failure; see note below) |
| Total HTTP requests | 98,996 |
| HTTP errors | 1 (0.001%) — a single `502 agent_disconnected`, occurring in the same second as a scheduled reconnect-churn kill; not a leak or hang |
| Achieved aggregate request rate | 50.0 req/s sustained throughout |
| Latency p50 / p95 / p99 | 1.1ms / 2.0ms / 3.3ms |
| Reconnect churn | 44 forced agent restarts over 33 minutes (one every 45s as configured); 64 total "tunnel established" log lines against 20 tunnels confirms each restart re-registered successfully |
| Retention pruning | Fired automatically 4 times during the run (every 5 minutes as configured), each removing ~30,000 request-log rows older than the 10-minute retention window — confirmed via `gateway.request_logs_pruned` log events, not just the isolated backup/restore test |
| Gateway goroutines | 264–493 across the run (wider variance than the churn-free 30-minute reference run, expected given 44 forced reconnects; no monotonic growth) |
| Gateway heap | 3.8–9.7MB, normal GC sawtooth |
| Gateway DB pool waits | `porthook_gateway_db_wait_count_total` grew from 0 to only 10 (a generously sized 40-connection pool at this request rate) |
| Crashes / panics / fatal errors | None |

A handful of `404 no_active_session` responses (3 total, not counted as errors by the load generator's own definition, which only counts 5xx/connection failures) also occurred, each within the same second as a reconnect-churn restart — the expected brief gap between an old agent disconnecting and its replacement re-registering, not unexplained behavior.

This run was stopped by the tool environment orchestrating it at the 33-minute mark rather than completing its full configured 45 minutes; the data collected up to that point (near-zero errors, bounded goroutines/heap, multiple successful retention-pruning cycles, dozens of confirmed reconnects) is genuine and was not truncated or edited. It stands as the representative soak for this release; the roadmap's full 72-hour soak is still outstanding, per the note below.

The roadmap's full 72-hour soak exceeds GitHub-hosted Actions runners' hard 6-hour job limit, so it cannot run unattended in this repository's CI. `.github/workflows/soak.yml` runs a bounded but meaningfully long soak (default 5 hours) on a weekly schedule and on demand; the full 72-hour run is an operator-executed procedure using the same script with `PORTHOOK_SMOKE_DURATION=72h`, run outside CI (for example on a long-lived operator machine or a self-hosted runner without the 6-hour cap), ahead of the `v1.0.0` release rehearsal in a later roadmap block.

For `v0.18.0`, the 33-minute representative soak plus the weekly 5-hour scheduled soak were accepted as sufficient in place of the literal 72-hour run — a deliberate, explicit tradeoff, not an oversight. Revisit before `v1.0.0`.

## Metrics and Alerting

`docs/OBSERVABILITY.md` documents every metric exposed by `/metrics` on both services. `deploy/prometheus/alerts.yml` has example alerting rules for gateway/control-plane availability, error rate, latency, database pool saturation, goroutine/heap growth, tunnel churn, and TLS certificate expiry (via `blackbox_exporter`, since Porthook's reference deployment terminates TLS at Caddy, not in the gateway itself). Validate the rule file with `promtool check rules deploy/prometheus/alerts.yml` before deploying it.
