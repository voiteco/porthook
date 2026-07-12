#!/usr/bin/env bash
set -euo pipefail

# Verifies backup, restore, upgrade, rollback, and retention pruning against
# a real Postgres database with a realistic volume of stored data, per
# docs/PRODUCTION_ROADMAP.md Block 8. Three phases, in order:
#
#   1. Upgrade/rollback: start the previous published release's binaries,
#      seed minimal data, upgrade in place to the binaries built from this
#      checkout, verify data and startup, then roll back to the previous
#      release's binaries and verify they still work (schema migrations in
#      the pre-1.0 line are additive-only; see docs/OPERATIONS.md).
#   2. Backup/restore: seed a realistic volume of tokens, reservations,
#      access policies, and request-log rows via real traffic, take a
#      pg_dump backup, wipe the database to simulate total loss, restore
#      from the backup, and verify every row count and a spot-checked row's
#      content survived exactly.
#   3. Retention pruning: seed rows older than the configured retention
#      window alongside recent rows, restart (pruning runs once at
#      startup), and verify only the old rows were removed.

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN_DIR="${ROOT_DIR}/bin"

VERSION="${VERSION:-smoke-backup-restore}"
PREVIOUS_RELEASE="${PORTHOOK_SMOKE_PREVIOUS_RELEASE:-v0.17.0}"
WSECHO_PORT="${PORTHOOK_SMOKE_WSECHO_PORT:-13600}"
PUBLIC_PORT="${PORTHOOK_SMOKE_PUBLIC_PORT:-18680}"
AGENT_PORT="${PORTHOOK_SMOKE_AGENT_PORT:-18681}"
CONTROL_PORT="${PORTHOOK_SMOKE_CONTROL_PORT:-18683}"
MANAGEMENT_PORT="${PORTHOOK_SMOKE_MANAGEMENT_PORT:-18685}"
ADMIN_TOKEN="${PORTHOOK_SMOKE_ADMIN_TOKEN:-smoke-admin-token}"
VALIDATOR_TOKEN="${PORTHOOK_SMOKE_VALIDATOR_TOKEN:-smoke-validator-token}"
MANAGEMENT_TOKEN="${PORTHOOK_SMOKE_MANAGEMENT_TOKEN:-smoke-management-token}"

TOKEN_COUNT="${PORTHOOK_SMOKE_TOKEN_COUNT:-5}"
REQUEST_LOG_COUNT="${PORTHOOK_SMOKE_REQUEST_LOG_TARGET:-500}"
RETENTION_OLD_ROWS="${PORTHOOK_SMOKE_RETENTION_OLD_ROWS:-20}"
RETENTION_RECENT_ROWS="${PORTHOOK_SMOKE_RETENTION_RECENT_ROWS:-5}"

POSTGRES_IMAGE="${PORTHOOK_SMOKE_POSTGRES_IMAGE:-postgres:16-alpine}"
POSTGRES_PASSWORD="${PORTHOOK_SMOKE_POSTGRES_PASSWORD:-smoke-postgres-password}"
POSTGRES_CONTAINER="porthook-smoke-backup-postgres-$$"
POSTGRES_PORT="${PORTHOOK_SMOKE_POSTGRES_PORT:-15533}"

KEEP_LOGS="${PORTHOOK_SMOKE_KEEP_LOGS:-0}"
SKIP_BUILD="${PORTHOOK_SMOKE_SKIP_BUILD:-0}"

TMP_DIR="$(mktemp -d)"
LOG_DIR="${TMP_DIR}/logs"
PREV_DIR="${TMP_DIR}/previous-release"
BACKUP_FILE="${TMP_DIR}/backup.sql"
mkdir -p "${LOG_DIR}" "${PREV_DIR}"

DATABASE_URL="postgres://porthook:${POSTGRES_PASSWORD}@127.0.0.1:${POSTGRES_PORT}/porthook?sslmode=disable"

pids=()
control_pid=""
gateway_pid=""
seed_agent_pids=()
seed_agent_configs=()
upgrade_agent_pid=""
checks_failed=0
declare -a check_results=()

cleanup() {
	status=$?

	for pid in "${pids[@]:-}"; do
		if kill -0 "${pid}" 2>/dev/null; then
			kill "${pid}" 2>/dev/null || true
		fi
	done
	for pid in "${pids[@]:-}"; do
		wait "${pid}" 2>/dev/null || true
	done

	if docker ps -a --format '{{.Names}}' 2>/dev/null | grep -qx "${POSTGRES_CONTAINER}"; then
		docker stop "${POSTGRES_CONTAINER}" >/dev/null 2>&1 || true
	fi

	echo "" >&2
	echo "Backup/restore/retention check results:" >&2
	for line in "${check_results[@]:-}"; do
		echo "  ${line}" >&2
	done

	if [[ "${status}" -ne 0 || "${checks_failed}" -gt 0 ]]; then
		echo "Backup/restore smoke test failed. Logs:" >&2
		for log in "${LOG_DIR}"/*.log; do
			[[ -f "${log}" ]] && { echo "==> ${log}" >&2; tail -n 80 "${log}" >&2; }
		done
	fi

	if [[ "${KEEP_LOGS}" == "1" ]]; then
		echo "Smoke logs kept at ${LOG_DIR}" >&2
	else
		rm -rf "${TMP_DIR}"
	fi

	if [[ "${checks_failed}" -gt 0 ]]; then
		exit 1
	fi
	exit "${status}"
}
trap cleanup EXIT

require_command() {
	command -v "$1" >/dev/null 2>&1 || { echo "Required command not found: $1" >&2; exit 1; }
}

record() {
	check_results+=("$1")
}

check_eq() {
	description="$1"
	got="$2"
	want="$3"
	if [[ "${got}" == "${want}" ]]; then
		record "PASS ${description}: ${got}"
	else
		record "FAIL ${description}: got ${got}, want ${want}"
		checks_failed=$((checks_failed + 1))
	fi
}

wait_for_url() {
	for _ in $(seq 1 100); do
		curl -fsS "$1" >/dev/null 2>&1 && return 0
		sleep 0.1
	done
	echo "Timed out waiting for $2: $1" >&2
	return 1
}

wait_for_log() {
	for _ in $(seq 1 200); do
		grep -q "$2" "$1" 2>/dev/null && return 0
		sleep 0.1
	done
	echo "Timed out waiting for $3 log pattern: $2" >&2
	return 1
}

psql_exec() {
	docker exec -i "${POSTGRES_CONTAINER}" psql -v ON_ERROR_STOP=1 -U porthook -d porthook "$@"
}

table_count() {
	psql_exec -tAc "SELECT COUNT(*) FROM $1" | tr -d '[:space:]'
}

extract_json_field() {
	python3 -c '
import json, sys
value = json.load(sys.stdin).get(sys.argv[1], "")
if not isinstance(value, str) or value == "":
    raise SystemExit(f"missing string field: {sys.argv[1]}")
print(value)
' "$1"
}

start_postgres() {
	docker run -d --rm \
		--name "${POSTGRES_CONTAINER}" \
		-e POSTGRES_DB=porthook \
		-e POSTGRES_USER=porthook \
		-e POSTGRES_PASSWORD="${POSTGRES_PASSWORD}" \
		-p "127.0.0.1:${POSTGRES_PORT}:5432" \
		"${POSTGRES_IMAGE}" >/dev/null
	ready_checks=0
	for _ in $(seq 1 120); do
		if docker exec "${POSTGRES_CONTAINER}" pg_isready -U porthook -d porthook >/dev/null 2>&1; then
			ready_checks=$((ready_checks + 1))
			[[ "${ready_checks}" -ge 3 ]] && break
		else
			ready_checks=0
		fi
		sleep 0.5
	done
	docker exec "${POSTGRES_CONTAINER}" pg_isready -U porthook -d porthook >/dev/null 2>&1 || {
		echo "Timed out waiting for smoke Postgres" >&2
		exit 1
	}
}

start_control_plane() {
	bin_dir="$1"
	PORTHOOK_CONTROL_ADDR="127.0.0.1:${CONTROL_PORT}" \
	PORTHOOK_CONTROL_ADMIN_TOKEN="${ADMIN_TOKEN}" \
	PORTHOOK_CONTROL_VALIDATOR_TOKEN="${VALIDATOR_TOKEN}" \
	PORTHOOK_GATEWAY_MANAGEMENT_URL="http://127.0.0.1:${MANAGEMENT_PORT}" \
	PORTHOOK_GATEWAY_MANAGEMENT_TOKEN="${MANAGEMENT_TOKEN}" \
	PORTHOOK_DATABASE_URL="${DATABASE_URL}" \
	PORTHOOK_AUDIT_EVENT_RETENTION="${AUDIT_EVENT_RETENTION:-}" \
		"${bin_dir}/porthook-control-plane" >>"${LOG_DIR}/control-plane.log" 2>&1 &
	control_pid="$!"
	pids+=("${control_pid}")
	wait_for_url "http://127.0.0.1:${CONTROL_PORT}/readyz" "control plane"
}

start_gateway() {
	bin_dir="$1"
	PORTHOOK_ADDR="127.0.0.1:${PUBLIC_PORT}" \
	PORTHOOK_AGENT_ADDR="127.0.0.1:${AGENT_PORT}" \
	PORTHOOK_MANAGEMENT_ADDR="127.0.0.1:${MANAGEMENT_PORT}" \
	PORTHOOK_MANAGEMENT_TOKEN="${MANAGEMENT_TOKEN}" \
	PORTHOOK_ROOT_DOMAIN="localhost" \
	PORTHOOK_PUBLIC_URL="http://localhost:${PUBLIC_PORT}" \
	PORTHOOK_STATIC_TOKEN="unused-static-token" \
	PORTHOOK_CONTROL_PLANE_URL="http://127.0.0.1:${CONTROL_PORT}" \
	PORTHOOK_CONTROL_PLANE_TOKEN="${VALIDATOR_TOKEN}" \
	PORTHOOK_REQUEST_LOG_DATABASE_URL="${DATABASE_URL}" \
	PORTHOOK_REQUEST_LOG_RETENTION="${REQUEST_LOG_RETENTION:-}" \
		"${bin_dir}/porthook-gateway" >>"${LOG_DIR}/gateway.log" 2>&1 &
	gateway_pid="$!"
	pids+=("${gateway_pid}")
	wait_for_url "http://127.0.0.1:${MANAGEMENT_PORT}/healthz" "gateway"
}

stop_process() {
	pid="$1"
	[[ -z "${pid}" ]] && return 0
	kill -0 "${pid}" 2>/dev/null && { kill "${pid}" 2>/dev/null || true; wait "${pid}" 2>/dev/null || true; }
}

require_command curl
require_command docker
require_command python3

if [[ "${SKIP_BUILD}" != "1" ]]; then
	(cd "${ROOT_DIR}" && make build VERSION="${VERSION}")
fi

# Cached across runs: downloading and attestation-verifying three binaries
# is the slowest part of this script and never changes for a given release.
PREVIOUS_RELEASE_CACHE_DIR="${PORTHOOK_SMOKE_PREVIOUS_RELEASE_CACHE_DIR:-${TMPDIR:-/tmp}/porthook-smoke-backup-restore-cache/${PREVIOUS_RELEASE}}"
if [[ -x "${PREVIOUS_RELEASE_CACHE_DIR}/porthook" && -x "${PREVIOUS_RELEASE_CACHE_DIR}/porthook-gateway" && -x "${PREVIOUS_RELEASE_CACHE_DIR}/porthook-control-plane" ]]; then
	echo "Using cached previous release ${PREVIOUS_RELEASE} binaries from ${PREVIOUS_RELEASE_CACHE_DIR}" >&2
	cp "${PREVIOUS_RELEASE_CACHE_DIR}/porthook" "${PREVIOUS_RELEASE_CACHE_DIR}/porthook-gateway" "${PREVIOUS_RELEASE_CACHE_DIR}/porthook-control-plane" "${PREV_DIR}/"
else
	echo "Installing previous release ${PREVIOUS_RELEASE} for the upgrade/rollback check" >&2
	for binary in porthook porthook-gateway porthook-control-plane; do
		"${ROOT_DIR}/scripts/install.sh" --binary "${binary}" --version "${PREVIOUS_RELEASE}" --dir "${PREV_DIR}" \
			>>"${LOG_DIR}/install-previous.log" 2>&1
	done
	mkdir -p "${PREVIOUS_RELEASE_CACHE_DIR}"
	cp "${PREV_DIR}/porthook" "${PREV_DIR}/porthook-gateway" "${PREV_DIR}/porthook-control-plane" "${PREVIOUS_RELEASE_CACHE_DIR}/"
fi

start_postgres

WSECHO_ADDR="127.0.0.1:${WSECHO_PORT}" \
	go run "${ROOT_DIR}/scripts/testdata/wsecho" \
	>"${LOG_DIR}/wsecho.log" 2>&1 &
pids+=("$!")
wait_for_log "${LOG_DIR}/wsecho.log" "listening on" "local echo service"

##############################################################################
# Phase 1: upgrade / rollback
##############################################################################
echo "" >&2
echo "=== Phase 1: upgrade / rollback (${PREVIOUS_RELEASE} -> current -> ${PREVIOUS_RELEASE}) ===" >&2

start_control_plane "${PREV_DIR}"
start_gateway "${PREV_DIR}"

upgrade_token_json="$(printf '%s' "${ADMIN_TOKEN}" | \
	"${PREV_DIR}/porthook" tokens create \
	--control-plane "http://127.0.0.1:${CONTROL_PORT}" \
	--admin-token-stdin --name "upgrade-check" --scope "register_tunnel" --json)"
UPGRADE_TOKEN="$(printf '%s' "${upgrade_token_json}" | extract_json_field token)"
UPGRADE_TOKEN_ID="$(printf '%s' "${upgrade_token_json}" | extract_json_field id)"
printf '%s' "${ADMIN_TOKEN}" | \
	"${PREV_DIR}/porthook" reserved create \
	--control-plane "http://127.0.0.1:${CONTROL_PORT}" \
	--admin-token-stdin --name "upgrade-check" --token-id "${UPGRADE_TOKEN_ID}" --json >/dev/null

printf '%s' "${UPGRADE_TOKEN}" | \
PORTHOOK_CONFIG_PATH="${TMP_DIR}/upgrade-agent-config.json" \
PORTHOOK_SERVER_URL="" PORTHOOK_TOKEN="" \
	"${PREV_DIR}/porthook" login --server "http://127.0.0.1:${AGENT_PORT}" --token-stdin \
	>"${LOG_DIR}/upgrade-login.log" 2>"${LOG_DIR}/upgrade-login.err"
PORTHOOK_CONFIG_PATH="${TMP_DIR}/upgrade-agent-config.json" \
PORTHOOK_SERVER_URL="" PORTHOOK_TOKEN="" \
	"${PREV_DIR}/porthook" http "${WSECHO_PORT}" --subdomain "upgrade-check" \
	>"${LOG_DIR}/upgrade-agent.log" 2>"${LOG_DIR}/upgrade-agent.err" &
upgrade_agent_pid="$!"
pids+=("${upgrade_agent_pid}")
wait_for_log "${LOG_DIR}/upgrade-agent.log" "Tunnel established" "upgrade-check agent registration"

tokens_before_upgrade="$(table_count api_tokens)"
reservations_before_upgrade="$(table_count reserved_subdomains)"

stop_process "${gateway_pid}"
stop_process "${control_pid}"

start_control_plane "${BIN_DIR}"
start_gateway "${BIN_DIR}"

tokens_after_upgrade="$(table_count api_tokens)"
reservations_after_upgrade="$(table_count reserved_subdomains)"
check_eq "upgrade preserves token count" "${tokens_after_upgrade}" "${tokens_before_upgrade}"
check_eq "upgrade preserves reservation count" "${reservations_after_upgrade}" "${reservations_before_upgrade}"

status="000"
for _ in $(seq 1 100); do
	status="$(curl -sS -o /dev/null -w "%{http_code}" --max-time 3 -H "Host: upgrade-check.localhost" "http://127.0.0.1:${PUBLIC_PORT}/smoke.txt" 2>/dev/null || echo 000)"
	[[ "${status}" == "200" ]] && break
	sleep 0.2
done
if [[ "${status}" == "200" ]]; then
	record "PASS upgrade: tunnel registered under the previous release still serves after upgrading the gateway/control-plane binaries"
else
	record "FAIL upgrade: tunnel stopped serving after upgrade, status=${status}"
	checks_failed=$((checks_failed + 1))
fi

stop_process "${gateway_pid}"
stop_process "${control_pid}"

start_control_plane "${PREV_DIR}"
start_gateway "${PREV_DIR}"

status="000"
for _ in $(seq 1 100); do
	status="$(curl -sS -o /dev/null -w "%{http_code}" --max-time 3 -H "Host: upgrade-check.localhost" "http://127.0.0.1:${PUBLIC_PORT}/smoke.txt" 2>/dev/null || echo 000)"
	[[ "${status}" == "200" ]] && break
	sleep 0.2
done
if [[ "${status}" == "200" ]]; then
	record "PASS rollback: previous release binaries still start and serve against the upgraded schema"
else
	record "FAIL rollback: previous release binaries failed to serve after rollback, status=${status}"
	checks_failed=$((checks_failed + 1))
fi

stop_process "${gateway_pid}"
stop_process "${control_pid}"
stop_process "${upgrade_agent_pid}"

##############################################################################
# Phase 2: backup / restore with realistic data volume
##############################################################################
echo "" >&2
echo "=== Phase 2: backup / restore (realistic data volume) ===" >&2

psql_exec -c "DROP SCHEMA public CASCADE; CREATE SCHEMA public;" >/dev/null

start_control_plane "${BIN_DIR}"
start_gateway "${BIN_DIR}"

echo "Seeding ${TOKEN_COUNT} tokens, reservations, access policies, and ~${REQUEST_LOG_COUNT} request-log rows" >&2
declare -a seed_subdomains=()
for ((i = 0; i < TOKEN_COUNT; i++)); do
	token_json="$(printf '%s' "${ADMIN_TOKEN}" | \
		"${BIN_DIR}/porthook" tokens create \
		--control-plane "http://127.0.0.1:${CONTROL_PORT}" \
		--admin-token-stdin --name "seed-token-${i}" --scope "register_tunnel" --json)"
	token_id="$(printf '%s' "${token_json}" | extract_json_field id)"
	token_secret="$(printf '%s' "${token_json}" | extract_json_field token)"
	subdomain="seed-${i}"
	printf '%s' "${ADMIN_TOKEN}" | \
		"${BIN_DIR}/porthook" reserved create \
		--control-plane "http://127.0.0.1:${CONTROL_PORT}" \
		--admin-token-stdin --name "${subdomain}" --token-id "${token_id}" --json >"${LOG_DIR}/reserved-${i}.json"
	reserved_id="$(extract_json_field id <"${LOG_DIR}/reserved-${i}.json")"
	if ((i % 2 == 0)); then
		printf '%s' "${ADMIN_TOKEN}" | \
			"${BIN_DIR}/porthook" access create \
			--control-plane "http://127.0.0.1:${CONTROL_PORT}" \
			--admin-token-stdin --reserved-subdomain-id "${reserved_id}" \
			--mode "basic_auth" --basic-username "seed-user-${i}" --basic-password "seed-password-${i}" \
			--json >/dev/null
	fi
	if ((i < 3)); then
		printf '%s' "${token_secret}" | \
		PORTHOOK_CONFIG_PATH="${TMP_DIR}/seed-agent-${i}-config.json" \
		PORTHOOK_SERVER_URL="" PORTHOOK_TOKEN="" \
			"${BIN_DIR}/porthook" login --server "http://127.0.0.1:${AGENT_PORT}" --token-stdin \
			>"${LOG_DIR}/seed-login-${i}.log" 2>"${LOG_DIR}/seed-login-${i}.err"
		PORTHOOK_CONFIG_PATH="${TMP_DIR}/seed-agent-${i}-config.json" \
		PORTHOOK_SERVER_URL="" PORTHOOK_TOKEN="" \
			"${BIN_DIR}/porthook" http "${WSECHO_PORT}" --subdomain "${subdomain}" \
			>"${LOG_DIR}/seed-agent-${i}.log" 2>"${LOG_DIR}/seed-agent-${i}.err" &
		agent_pid="$!"
		pids+=("${agent_pid}")
		seed_agent_pids+=("${agent_pid}")
		seed_agent_configs+=("${TMP_DIR}/seed-agent-${i}-config.json")
		wait_for_log "${LOG_DIR}/seed-agent-${i}.log" "Tunnel established" "seed agent ${i} registration"
		seed_subdomains+=("${subdomain}")
	fi
done

LOADGEN_PUBLIC_ADDR="127.0.0.1:${PUBLIC_PORT}" \
LOADGEN_ROOT_DOMAIN="localhost" \
LOADGEN_SUBDOMAIN_PREFIX="seed-" \
LOADGEN_TUNNEL_COUNT="${#seed_subdomains[@]}" \
LOADGEN_WS_STREAMS=0 \
LOADGEN_TARGET_RPS=100 \
LOADGEN_DURATION="$((REQUEST_LOG_COUNT / 100 + 1))s" \
LOADGEN_REPORT_INTERVAL=30s \
LOADGEN_MAX_ERROR_RATE=1 \
	go run "${ROOT_DIR}/scripts/testdata/loadgen" >"${LOG_DIR}/seed-loadgen.log" 2>&1 || true

# Stop the seed agents before snapshotting: left running, they keep
# reconnecting and re-authenticating against whatever gateway comes back up
# next, adding new control-plane audit events unrelated to backup/restore
# correctness and making an exact before/after count comparison flaky.
for seed_pid in "${seed_agent_pids[@]:-}"; do
	stop_process "${seed_pid}"
done

tokens_before="$(table_count api_tokens)"
reservations_before="$(table_count reserved_subdomains)"
policies_before="$(table_count access_policies)"
audit_before="$(table_count audit_events)"
request_logs_before="$(table_count gateway_request_logs)"
sample_request_log_row="$(psql_exec -tAc "SELECT path || '|' || status FROM gateway_request_logs ORDER BY id LIMIT 1")"

echo "Pre-backup counts: tokens=${tokens_before} reservations=${reservations_before} policies=${policies_before} audit_events=${audit_before} request_logs=${request_logs_before}" >&2

docker exec "${POSTGRES_CONTAINER}" pg_dump -U porthook -d porthook >"${BACKUP_FILE}"
if [[ -s "${BACKUP_FILE}" ]]; then
	record "PASS backup: pg_dump produced a non-empty backup file ($(wc -c <"${BACKUP_FILE}" | tr -d '[:space:]') bytes)"
else
	record "FAIL backup: pg_dump produced an empty backup file"
	checks_failed=$((checks_failed + 1))
fi

stop_process "${gateway_pid}"
stop_process "${control_pid}"

echo "Simulating total data loss (drop and recreate schema)" >&2
psql_exec -c "DROP SCHEMA public CASCADE; CREATE SCHEMA public;" >/dev/null

echo "Restoring from backup" >&2
psql_exec <"${BACKUP_FILE}" >"${LOG_DIR}/restore.log" 2>&1

start_control_plane "${BIN_DIR}"
start_gateway "${BIN_DIR}"

tokens_after="$(table_count api_tokens)"
reservations_after="$(table_count reserved_subdomains)"
policies_after="$(table_count access_policies)"
audit_after="$(table_count audit_events)"
request_logs_after="$(table_count gateway_request_logs)"
sample_request_log_row_after="$(psql_exec -tAc "SELECT path || '|' || status FROM gateway_request_logs ORDER BY id LIMIT 1")"

check_eq "restore preserves token count" "${tokens_after}" "${tokens_before}"
check_eq "restore preserves reservation count" "${reservations_after}" "${reservations_before}"
check_eq "restore preserves access policy count" "${policies_after}" "${policies_before}"
check_eq "restore preserves audit event count" "${audit_after}" "${audit_before}"
check_eq "restore preserves request log count" "${request_logs_after}" "${request_logs_before}"
check_eq "restore preserves sampled request log row content" "${sample_request_log_row_after}" "${sample_request_log_row}"

# Restart the seed-1 agent (stopped above, before the snapshot): seed-1 is
# plain public mode, unlike seed-0/2/4 which have a basic_auth access
# policy and would 401 an unauthenticated check.
PORTHOOK_CONFIG_PATH="${seed_agent_configs[1]}" \
PORTHOOK_SERVER_URL="" PORTHOOK_TOKEN="" \
	"${BIN_DIR}/porthook" http "${WSECHO_PORT}" --subdomain "seed-1" \
	>"${LOG_DIR}/seed-agent-1-restored.log" 2>"${LOG_DIR}/seed-agent-1-restored.err" &
pids+=("$!")
wait_for_log "${LOG_DIR}/seed-agent-1-restored.log" "Tunnel established" "seed-1 agent re-registration after restore"

status="000"
for _ in $(seq 1 100); do
	status="$(curl -sS -o /dev/null -w "%{http_code}" --max-time 3 -H "Host: seed-1.localhost" "http://127.0.0.1:${PUBLIC_PORT}/smoke.txt" 2>/dev/null || echo 000)"
	[[ "${status}" == "200" ]] && break
	sleep 0.2
done
if [[ "${status}" == "200" ]]; then
	record "PASS restore: a seeded tunnel serves requests again after restore"
else
	record "FAIL restore: seeded tunnel does not serve requests after restore, status=${status}"
	checks_failed=$((checks_failed + 1))
fi

##############################################################################
# Phase 3: retention pruning
##############################################################################
echo "" >&2
echo "=== Phase 3: retention pruning ===" >&2

echo "Seeding ${RETENTION_OLD_ROWS} request-log rows older than retention and ${RETENTION_RECENT_ROWS} recent rows" >&2
for ((i = 0; i < RETENTION_OLD_ROWS; i++)); do
	psql_exec -c "INSERT INTO gateway_request_logs (time, method, host, path, query_present, remote_ip, status, outcome, request_bytes, response_bytes, duration_ms) VALUES (NOW() - INTERVAL '60 days', 'GET', 'retention-check.localhost', '/old-${i}', false, '127.0.0.1', 200, 'completed', 0, 0, 0)" >/dev/null
done
for ((i = 0; i < RETENTION_RECENT_ROWS; i++)); do
	psql_exec -c "INSERT INTO gateway_request_logs (time, method, host, path, query_present, remote_ip, status, outcome, request_bytes, response_bytes, duration_ms) VALUES (NOW(), 'GET', 'retention-check.localhost', '/recent-${i}', false, '127.0.0.1', 200, 'completed', 0, 0, 0)" >/dev/null
done

for ((i = 0; i < RETENTION_OLD_ROWS; i++)); do
	psql_exec -c "INSERT INTO audit_events (time, level, message, event, method, path, remote_ip) VALUES (NOW() - INTERVAL '120 days', 'INFO', 'seed old audit event ${i}', 'smoke.retention_seed', '', '', '')" >/dev/null
done
for ((i = 0; i < RETENTION_RECENT_ROWS; i++)); do
	psql_exec -c "INSERT INTO audit_events (time, level, message, event, method, path, remote_ip) VALUES (NOW(), 'INFO', 'seed recent audit event ${i}', 'smoke.retention_seed', '', '', '')" >/dev/null
done

request_logs_before_prune="$(table_count gateway_request_logs)"
audit_before_prune="$(table_count audit_events)"
echo "Before pruning: request_logs=${request_logs_before_prune} audit_events=${audit_before_prune}" >&2

stop_process "${gateway_pid}"
stop_process "${control_pid}"

REQUEST_LOG_RETENTION="24h" start_control_plane "${BIN_DIR}"
AUDIT_EVENT_RETENTION="24h" REQUEST_LOG_RETENTION="24h" start_gateway "${BIN_DIR}"
# The pruner runs once immediately at startup; give it a moment to finish.
sleep 2

request_logs_after_prune="$(table_count gateway_request_logs)"
audit_after_prune="$(table_count audit_events)"
old_request_logs_remaining="$(psql_exec -tAc "SELECT COUNT(*) FROM gateway_request_logs WHERE host = 'retention-check.localhost' AND path LIKE '/old-%'" | tr -d '[:space:]')"
recent_request_logs_remaining="$(psql_exec -tAc "SELECT COUNT(*) FROM gateway_request_logs WHERE host = 'retention-check.localhost' AND path LIKE '/recent-%'" | tr -d '[:space:]')"
old_audit_remaining="$(psql_exec -tAc "SELECT COUNT(*) FROM audit_events WHERE event = 'smoke.retention_seed' AND message LIKE 'seed old%'" | tr -d '[:space:]')"
recent_audit_remaining="$(psql_exec -tAc "SELECT COUNT(*) FROM audit_events WHERE event = 'smoke.retention_seed' AND message LIKE 'seed recent%'" | tr -d '[:space:]')"

echo "After pruning: request_logs=${request_logs_after_prune} audit_events=${audit_after_prune}" >&2

check_eq "retention pruning removes all old (60d) request-log rows" "${old_request_logs_remaining}" "0"
check_eq "retention pruning keeps all recent request-log rows" "${recent_request_logs_remaining}" "${RETENTION_RECENT_ROWS}"
check_eq "retention pruning removes all old (120d) audit-event rows" "${old_audit_remaining}" "0"
check_eq "retention pruning keeps all recent audit-event rows" "${recent_audit_remaining}" "${RETENTION_RECENT_ROWS}"

echo "" >&2
if [[ "${checks_failed}" -eq 0 ]]; then
	echo "Backup/restore/retention smoke test passed" >&2
fi
