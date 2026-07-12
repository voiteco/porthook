#!/usr/bin/env bash
set -euo pipefail

# Reproducible load harness for the minimum capacity profile in
# docs/PRODUCTION_ROADMAP.md Block 8: N active tunnels, M concurrent
# WebSocket streams, and a target aggregate request rate, sustained for a
# configurable duration, against a real gateway with a real durable
# Postgres request-log store.
#
# Bounded defaults keep this cheap enough for normal CI. For the reference
# load run (100 tunnels / 500 streams / 100 req/s / 30+ minutes), see
# docs/RELIABILITY.md and override the PORTHOOK_SMOKE_* variables below,
# for example:
#
#   PORTHOOK_SMOKE_TUNNEL_COUNT=100 \
#   PORTHOOK_SMOKE_WS_STREAMS=500 \
#   PORTHOOK_SMOKE_TARGET_RPS=100 \
#   PORTHOOK_SMOKE_DURATION=30m \
#     ./scripts/smoke-capacity.sh
#
# The same script doubles as the soak test: set PORTHOOK_SMOKE_DURATION to
# hours, PORTHOOK_SMOKE_RECONNECT_INTERVAL to periodically kill and restart
# a random agent (exercising reconnects under load, not just steady-state
# connections), and PORTHOOK_SMOKE_REQUEST_LOG_RETENTION/
# PORTHOOK_SMOKE_REQUEST_LOG_PRUNE_INTERVAL/PORTHOOK_SMOKE_RETENTION_OLD_ROWS
# to verify operational-history retention pruning actually runs and removes
# old rows during a long run. See .github/workflows/soak.yml for the
# scheduled reference invocation.

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN_DIR="${ROOT_DIR}/bin"

VERSION="${VERSION:-smoke-capacity}"
WSECHO_PORT="${PORTHOOK_SMOKE_WSECHO_PORT:-13400}"
PUBLIC_PORT="${PORTHOOK_SMOKE_PUBLIC_PORT:-18480}"
AGENT_PORT="${PORTHOOK_SMOKE_AGENT_PORT:-18481}"
MANAGEMENT_PORT="${PORTHOOK_SMOKE_MANAGEMENT_PORT:-18485}"
ROOT_DOMAIN="${PORTHOOK_SMOKE_ROOT_DOMAIN:-localhost}"
SUBDOMAIN_PREFIX="${PORTHOOK_SMOKE_SUBDOMAIN_PREFIX:-loadtest-}"
TOKEN="${PORTHOOK_SMOKE_TOKEN:-capacity-smoke-token}"
MANAGEMENT_TOKEN="${PORTHOOK_SMOKE_MANAGEMENT_TOKEN:-capacity-smoke-management-token}"

TUNNEL_COUNT="${PORTHOOK_SMOKE_TUNNEL_COUNT:-8}"
WS_STREAMS="${PORTHOOK_SMOKE_WS_STREAMS:-16}"
TARGET_RPS="${PORTHOOK_SMOKE_TARGET_RPS:-20}"
DURATION="${PORTHOOK_SMOKE_DURATION:-20s}"
REPORT_INTERVAL="${PORTHOOK_SMOKE_REPORT_INTERVAL:-10s}"
MAX_ERROR_RATE="${PORTHOOK_SMOKE_MAX_ERROR_RATE:-0.01}"

# The default pool (20 max open conns) saturates well below the 100 req/s
# reference target because every public request synchronously writes one
# durable request-log row (see recordRequestLog in server.go). Reference
# load runs should size this to the target throughput; see docs/RELIABILITY.md.
DB_MAX_OPEN_CONNS="${PORTHOOK_SMOKE_DB_MAX_OPEN_CONNS:-}"
DB_MAX_IDLE_CONNS="${PORTHOOK_SMOKE_DB_MAX_IDLE_CONNS:-}"

# Soak-only extensions, opt-in and off by default so the bounded CI smoke
# run stays unchanged. See docs/RELIABILITY.md for the reference soak
# invocation (docs/PRODUCTION_ROADMAP.md Block 8 asks this to also cover
# reconnects and operational-history retention over a long run, not just
# throughput).
#
# If set, one random agent is killed and restarted every RECONNECT_INTERVAL
# throughout the run, so the soak exercises real reconnect churn instead of
# only steady-state connections.
RECONNECT_INTERVAL="${PORTHOOK_SMOKE_RECONNECT_INTERVAL:-}"
# If set, applied to the gateway so request-log pruning actually runs (once
# immediately at startup, then on this interval) during the soak instead of
# the 30-day production default, and old rows seeded below are provably gone
# by the end of a long run instead of accumulating forever.
REQUEST_LOG_RETENTION="${PORTHOOK_SMOKE_REQUEST_LOG_RETENTION:-}"
REQUEST_LOG_PRUNE_INTERVAL="${PORTHOOK_SMOKE_REQUEST_LOG_PRUNE_INTERVAL:-}"
RETENTION_OLD_ROWS="${PORTHOOK_SMOKE_RETENTION_OLD_ROWS:-0}"

POSTGRES_IMAGE="${PORTHOOK_SMOKE_POSTGRES_IMAGE:-postgres:16-alpine}"
POSTGRES_PASSWORD="${PORTHOOK_SMOKE_POSTGRES_PASSWORD:-smoke-postgres-password}"
POSTGRES_CONTAINER="porthook-smoke-capacity-postgres-$$"

KEEP_LOGS="${PORTHOOK_SMOKE_KEEP_LOGS:-0}"
SKIP_BUILD="${PORTHOOK_SMOKE_SKIP_BUILD:-0}"

TMP_DIR="$(mktemp -d)"
LOG_DIR="${TMP_DIR}/logs"
REPORT_FILE="${TMP_DIR}/loadgen-report.json"
TIMESERIES_FILE="${LOG_DIR}/metrics-timeseries.tsv"
CHURN_PID_FILE="${TMP_DIR}/churn-pids.txt"
mkdir -p "${LOG_DIR}"

pids=()

cleanup() {
	status=$?

	if [[ -f "${CHURN_PID_FILE}" ]]; then
		while read -r churn_pid; do
			[[ -n "${churn_pid}" ]] && kill "${churn_pid}" 2>/dev/null || true
		done <"${CHURN_PID_FILE}"
	fi

	for pid in "${pids[@]:-}"; do
		if kill -0 "${pid}" 2>/dev/null; then
			kill "${pid}" 2>/dev/null || true
		fi
	done
	for pid in "${pids[@]:-}"; do
		wait "${pid}" 2>/dev/null || true
	done

	if docker ps -a --format '{{.Names}}' 2>/dev/null | grep -qx "${POSTGRES_CONTAINER}"; then
		if [[ "${status}" -ne 0 ]]; then
			echo "Capacity smoke Postgres logs:" >&2
			docker logs "${POSTGRES_CONTAINER}" >&2 || true
		fi
		docker stop "${POSTGRES_CONTAINER}" >/dev/null 2>&1 || true
	fi

	if [[ "${status}" -ne 0 ]]; then
		echo "Capacity smoke test failed. Logs:" >&2
		for log in "${LOG_DIR}"/*.log; do
			if [[ -f "${log}" ]]; then
				echo "==> ${log}" >&2
				tail -n 100 "${log}" >&2
			fi
		done
	fi

	if [[ "${KEEP_LOGS}" == "1" ]]; then
		echo "Smoke logs kept at ${LOG_DIR}" >&2
	else
		rm -rf "${TMP_DIR}"
	fi

	exit "${status}"
}

trap cleanup EXIT

require_command() {
	if ! command -v "$1" >/dev/null 2>&1; then
		echo "Required command not found: $1" >&2
		exit 1
	fi
}

wait_for_url() {
	url="$1"
	name="$2"

	for _ in $(seq 1 100); do
		if curl -fsS "${url}" >/dev/null 2>&1; then
			return 0
		fi
		sleep 0.1
	done

	echo "Timed out waiting for ${name}: ${url}" >&2
	return 1
}

wait_for_log() {
	log_file="$1"
	pattern="$2"
	name="$3"

	for _ in $(seq 1 200); do
		if grep -q "${pattern}" "${log_file}" 2>/dev/null; then
			return 0
		fi
		sleep 0.1
	done

	echo "Timed out waiting for ${name} log pattern: ${pattern}" >&2
	return 1
}

fetch_metrics() {
	label="$1"
	curl -fsS -H "Authorization: Bearer ${MANAGEMENT_TOKEN}" \
		"http://127.0.0.1:${MANAGEMENT_PORT}/metrics" \
		>"${LOG_DIR}/gateway-metrics-${label}.prom"
}

sample_metric() {
	printf '%s\n' "$1" | awk -v name="$2" '$1 == name {print $2; found=1} END {if (!found) print "NaN"}'
}

# Samples goroutine/heap/DB-pool gauges throughout the run, not just before
# and after, so a leak or a saturating pool shows up as a trend rather than
# only as a before/after delta. Reused as-is by the longer soak test.
start_metrics_sampler() {
	(
		printf 'timestamp\tgoroutines\theap_alloc_bytes\topen_fds\tdb_open\tdb_in_use\tdb_idle\tdb_wait_count\n' >"${TIMESERIES_FILE}"
		while true; do
			sleep "${REPORT_INTERVAL}"
			prom="$(curl -fsS -H "Authorization: Bearer ${MANAGEMENT_TOKEN}" "http://127.0.0.1:${MANAGEMENT_PORT}/metrics" 2>/dev/null || true)"
			if [[ -z "${prom}" ]]; then
				continue
			fi
			ts="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
			printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
				"${ts}" \
				"$(sample_metric "${prom}" porthook_gateway_goroutines)" \
				"$(sample_metric "${prom}" porthook_gateway_heap_alloc_bytes)" \
				"$(sample_metric "${prom}" porthook_gateway_open_fds)" \
				"$(sample_metric "${prom}" porthook_gateway_db_open_connections)" \
				"$(sample_metric "${prom}" porthook_gateway_db_in_use_connections)" \
				"$(sample_metric "${prom}" porthook_gateway_db_idle_connections)" \
				"$(sample_metric "${prom}" porthook_gateway_db_wait_count_total)" \
				>>"${TIMESERIES_FILE}"
		done
	) &
	pids+=("$!")
}

psql_exec() {
	docker exec -i "${POSTGRES_CONTAINER}" psql -v ON_ERROR_STOP=1 -U porthook -d porthook "$@"
}

table_count() {
	psql_exec -tAc "SELECT COUNT(*) FROM $1" | tr -d '[:space:]'
}

# Kills and restarts one random agent every RECONNECT_INTERVAL for the
# duration of the run, so a soak invocation exercises real reconnect churn
# (agent crash + gateway detecting it + agent backoff-reconnect +
# re-registration) continuously rather than only as a one-off scenario.
#
# Runs as a background subshell, which gets its own copy of agent_pids: any
# array mutation inside it is invisible to the parent script's cleanup
# trap, so each newly-spawned restart PID is additionally appended to
# CHURN_PID_FILE, which cleanup() reads back to kill everything this loop
# ever spawned, not just the loop process itself.
start_reconnect_churn() {
	: >"${CHURN_PID_FILE}"
	(
		declare -a churn_agent_pids=("${agent_pids[@]}")
		while true; do
			sleep "${RECONNECT_INTERVAL}"
			victim=$((RANDOM % TUNNEL_COUNT))
			victim_pid="${churn_agent_pids[${victim}]:-}"
			[[ -z "${victim_pid}" ]] && continue
			kill -9 "${victim_pid}" 2>/dev/null || true
			wait "${victim_pid}" 2>/dev/null || true
			subdomain="${SUBDOMAIN_PREFIX}${victim}"
			"${BIN_DIR}/porthook" http "${WSECHO_PORT}" \
				--server "http://127.0.0.1:${AGENT_PORT}" \
				--token "${TOKEN}" \
				--subdomain "${subdomain}" \
				>>"${LOG_DIR}/agent-${victim}.log" 2>>"${LOG_DIR}/agent-${victim}.err" &
			new_pid="$!"
			churn_agent_pids[${victim}]="${new_pid}"
			echo "${new_pid}" >>"${CHURN_PID_FILE}"
		done
	) &
	pids+=("$!")
}

require_command curl
require_command go
require_command docker

if [[ "${SKIP_BUILD}" != "1" ]]; then
	(
		cd "${ROOT_DIR}"
		make build VERSION="${VERSION}"
	)
fi

echo "Starting Postgres for the capacity smoke request-log store" >&2
docker run -d --rm \
	--name "${POSTGRES_CONTAINER}" \
	-e POSTGRES_DB=porthook \
	-e POSTGRES_USER=porthook \
	-e POSTGRES_PASSWORD="${POSTGRES_PASSWORD}" \
	-p 127.0.0.1::5432 \
	"${POSTGRES_IMAGE}" >/dev/null

ready_checks=0
for _ in $(seq 1 120); do
	if docker exec "${POSTGRES_CONTAINER}" pg_isready -U porthook -d porthook >/dev/null 2>&1; then
		ready_checks=$((ready_checks + 1))
		if [[ "${ready_checks}" -ge 3 ]]; then
			break
		fi
	else
		ready_checks=0
	fi
	sleep 0.5
done

if ! docker exec "${POSTGRES_CONTAINER}" pg_isready -U porthook -d porthook >/dev/null 2>&1; then
	echo "Timed out waiting for smoke Postgres" >&2
	exit 1
fi

POSTGRES_PORT="$(docker port "${POSTGRES_CONTAINER}" 5432/tcp | sed 's/.*://')"
if [[ -z "${POSTGRES_PORT}" ]]; then
	echo "Could not determine smoke Postgres host port" >&2
	exit 1
fi

DATABASE_URL="postgres://porthook:${POSTGRES_PASSWORD}@127.0.0.1:${POSTGRES_PORT}/porthook?sslmode=disable"

WSECHO_ADDR="127.0.0.1:${WSECHO_PORT}" \
	go run "${ROOT_DIR}/scripts/testdata/wsecho" \
	>"${LOG_DIR}/wsecho.log" 2>&1 &
pids+=("$!")
wait_for_log "${LOG_DIR}/wsecho.log" "listening on" "local echo service"

PORTHOOK_ADDR="127.0.0.1:${PUBLIC_PORT}" \
PORTHOOK_AGENT_ADDR="127.0.0.1:${AGENT_PORT}" \
PORTHOOK_MANAGEMENT_ADDR="127.0.0.1:${MANAGEMENT_PORT}" \
PORTHOOK_MANAGEMENT_TOKEN="${MANAGEMENT_TOKEN}" \
PORTHOOK_ROOT_DOMAIN="${ROOT_DOMAIN}" \
PORTHOOK_PUBLIC_URL="http://localhost:${PUBLIC_PORT}" \
PORTHOOK_STATIC_TOKEN="${TOKEN}" \
PORTHOOK_REQUEST_LOG_DATABASE_URL="${DATABASE_URL}" \
PORTHOOK_DB_MAX_OPEN_CONNS="${DB_MAX_OPEN_CONNS}" \
PORTHOOK_DB_MAX_IDLE_CONNS="${DB_MAX_IDLE_CONNS}" \
PORTHOOK_REQUEST_LOG_RETENTION="${REQUEST_LOG_RETENTION}" \
PORTHOOK_REQUEST_LOG_PRUNE_INTERVAL="${REQUEST_LOG_PRUNE_INTERVAL}" \
	"${BIN_DIR}/porthook-gateway" >"${LOG_DIR}/gateway.log" 2>&1 &
pids+=("$!")
wait_for_url "http://127.0.0.1:${MANAGEMENT_PORT}/healthz" "gateway"

echo "Registering ${TUNNEL_COUNT} agent tunnels against a shared local backend" >&2
declare -a agent_pids=()
for ((i = 0; i < TUNNEL_COUNT; i++)); do
	subdomain="${SUBDOMAIN_PREFIX}${i}"
	"${BIN_DIR}/porthook" http "${WSECHO_PORT}" \
		--server "http://127.0.0.1:${AGENT_PORT}" \
		--token "${TOKEN}" \
		--subdomain "${subdomain}" \
		>"${LOG_DIR}/agent-${i}.log" 2>"${LOG_DIR}/agent-${i}.err" &
	agent_pids[${i}]="$!"
	pids+=("${agent_pids[${i}]}")
done

for ((i = 0; i < TUNNEL_COUNT; i++)); do
	wait_for_log "${LOG_DIR}/agent-${i}.log" "Tunnel established" "agent ${i} registration"
done

active_tunnels="$(curl -fsS -H "Authorization: Bearer ${MANAGEMENT_TOKEN}" \
	"http://127.0.0.1:${MANAGEMENT_PORT}/api/v1/tunnels" |
	python3 -c 'import json,sys; print(len(json.load(sys.stdin).get("tunnels", [])))')"
if [[ "${active_tunnels}" -lt "${TUNNEL_COUNT}" ]]; then
	echo "Expected at least ${TUNNEL_COUNT} active tunnels, gateway reports ${active_tunnels}" >&2
	exit 1
fi
echo "Confirmed ${active_tunnels} active tunnels" >&2

if [[ "${RETENTION_OLD_ROWS}" -gt 0 ]]; then
	# Seeded after the gateway's own startup prune already ran, so these
	# rows are only removed if the ticker-based pruner fires during the
	# run (REQUEST_LOG_PRUNE_INTERVAL must be shorter than DURATION).
	echo "Seeding ${RETENTION_OLD_ROWS} request-log rows older than retention for the soak's retention-pruning check" >&2
	for ((i = 0; i < RETENTION_OLD_ROWS; i++)); do
		psql_exec -c "INSERT INTO gateway_request_logs (time, method, host, path, query_present, remote_ip, status, outcome, request_bytes, response_bytes, duration_ms) VALUES (NOW() - INTERVAL '60 days', 'GET', 'soak-retention-check.localhost', '/old-${i}', false, '127.0.0.1', 200, 'completed', 0, 0, 0)" >/dev/null
	done
fi

fetch_metrics before
start_metrics_sampler
if [[ -n "${RECONNECT_INTERVAL}" ]]; then
	echo "Starting reconnect churn: one random agent restarted every ${RECONNECT_INTERVAL}" >&2
	start_reconnect_churn
fi

echo "Running load generator: tunnels=${TUNNEL_COUNT} ws_streams=${WS_STREAMS} target_rps=${TARGET_RPS} duration=${DURATION}" >&2
LOADGEN_PUBLIC_ADDR="127.0.0.1:${PUBLIC_PORT}" \
LOADGEN_ROOT_DOMAIN="${ROOT_DOMAIN}" \
LOADGEN_SUBDOMAIN_PREFIX="${SUBDOMAIN_PREFIX}" \
LOADGEN_TUNNEL_COUNT="${TUNNEL_COUNT}" \
LOADGEN_WS_STREAMS="${WS_STREAMS}" \
LOADGEN_TARGET_RPS="${TARGET_RPS}" \
LOADGEN_DURATION="${DURATION}" \
LOADGEN_REPORT_INTERVAL="${REPORT_INTERVAL}" \
LOADGEN_MAX_ERROR_RATE="${MAX_ERROR_RATE}" \
LOADGEN_REPORT_PATH="${REPORT_FILE}" \
	go run "${ROOT_DIR}/scripts/testdata/loadgen" \
	2>&1 | tee "${LOG_DIR}/loadgen.log"
loadgen_status="${PIPESTATUS[0]}"

fetch_metrics after

if [[ "${loadgen_status}" -ne 0 ]]; then
	echo "Load generator exceeded the error-rate threshold (${MAX_ERROR_RATE})" >&2
	exit 1
fi

echo "Load generator report:" >&2
cat "${REPORT_FILE}" >&2

echo "Gateway runtime metrics before/after load:" >&2
grep -E "^porthook_gateway_(goroutines|heap_alloc_bytes|db_open_connections|db_in_use_connections) " \
	"${LOG_DIR}/gateway-metrics-before.prom" | sed 's/^/  before: /' >&2
grep -E "^porthook_gateway_(goroutines|heap_alloc_bytes|db_open_connections|db_in_use_connections) " \
	"${LOG_DIR}/gateway-metrics-after.prom" | sed 's/^/  after:  /' >&2

if [[ -f "${TIMESERIES_FILE}" ]]; then
	echo "Gateway runtime metrics timeseries (${TIMESERIES_FILE}):" >&2
	column -t "${TIMESERIES_FILE}" >&2 || cat "${TIMESERIES_FILE}" >&2

	# Automated bound, not just a report to eyeball: fails if a sampled
	# gauge grew far beyond normal run-to-run/GC noise between the first
	# and last sample, which is what unbounded growth (a leak) looks like
	# over the course of one run. Multipliers are deliberately generous
	# (reconnect churn and GC sawtooth both cause real, harmless swings)
	# so this only catches genuinely runaway growth, not noise.
	assert_bounded_growth() {
		column_index="$1"
		name="$2"
		multiplier="$3"
		first="$(awk -F'\t' -v c="${column_index}" 'NR==2{print $c}' "${TIMESERIES_FILE}")"
		last="$(awk -F'\t' -v c="${column_index}" 'END{print $c}' "${TIMESERIES_FILE}")"
		if [[ -z "${first}" || -z "${last}" || "${first}" == "NaN" || "${last}" == "NaN" || "${first}" == "0" ]]; then
			return 0
		fi
		if awk -v f="${first}" -v l="${last}" -v m="${multiplier}" 'BEGIN{exit !(l > f * m)}'; then
			echo "Resource growth check FAILED: ${name} grew from ${first} to ${last} (more than ${multiplier}x) over the run" >&2
			exit 1
		fi
		echo "Resource growth check passed: ${name} ${first} -> ${last} (bound ${multiplier}x)" >&2
	}
	assert_bounded_growth 2 goroutines 3
	assert_bounded_growth 3 heap_alloc_bytes 5
	assert_bounded_growth 4 open_fds 3
fi

if [[ "${RETENTION_OLD_ROWS}" -gt 0 ]]; then
	remaining="$(psql_exec -tAc "SELECT COUNT(*) FROM gateway_request_logs WHERE host = 'soak-retention-check.localhost'" | tr -d '[:space:]')"
	if [[ "${remaining}" == "0" ]]; then
		echo "Retention pruning check passed: all ${RETENTION_OLD_ROWS} seeded old rows were pruned during the run" >&2
	else
		echo "Retention pruning check FAILED: ${remaining} of ${RETENTION_OLD_ROWS} seeded old rows still present (REQUEST_LOG_PRUNE_INTERVAL may be longer than DURATION)" >&2
		exit 1
	fi
fi

if [[ -n "${RECONNECT_INTERVAL}" ]]; then
	# The churn loop kills and spawns a brand-new process rather than
	# letting one process auto-reconnect, so a churned agent's log shows a
	# second "Tunnel established" (first-connection wording), not "Tunnel
	# restored" (same-process reconnect wording). Total established count
	# exceeding the tunnel count is the signal that churn actually happened.
	established_events="$(grep -c "Tunnel established" "${LOG_DIR}"/agent-*.log 2>/dev/null | awk -F: '{sum+=$2} END{print sum+0}' || true)"
	echo "Reconnect churn check: ${established_events} total tunnel establishments across ${TUNNEL_COUNT} tunnels" >&2
	if [[ "${established_events}" -le "${TUNNEL_COUNT}" ]]; then
		echo "Reconnect churn check FAILED: expected more than ${TUNNEL_COUNT} establishments (evidence of at least one restart) during the run" >&2
		exit 1
	fi
fi

echo "Capacity smoke test passed: ${TUNNEL_COUNT} tunnels, ${WS_STREAMS} WS streams, ${TARGET_RPS} target req/s for ${DURATION}"
