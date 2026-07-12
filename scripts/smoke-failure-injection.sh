#!/usr/bin/env bash
set -uo pipefail

# Exercises the failure modes required by docs/PRODUCTION_ROADMAP.md Block 8:
# agent disconnect, gateway graceful shutdown, reverse-proxy restart,
# control-plane outage, Postgres restart, slow Postgres, and network
# interruption. Each scenario measures how long recovery actually took and
# asserts it against a documented bound; bounds and measurements are
# reported at the end and belong in docs/RELIABILITY.md.
#
# Unlike the other smoke-*.sh scripts, this one intentionally continues
# past individual scenario failures so a single bad scenario doesn't hide
# the results of the rest; the script's own exit code reflects whether any
# scenario failed.

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN_DIR="${ROOT_DIR}/bin"

VERSION="${VERSION:-smoke-failure-injection}"
WSECHO_PORT="${PORTHOOK_SMOKE_WSECHO_PORT:-13500}"
PUBLIC_PORT="${PORTHOOK_SMOKE_PUBLIC_PORT:-18580}"
AGENT_PORT="${PORTHOOK_SMOKE_AGENT_PORT:-18581}"
CONTROL_PORT="${PORTHOOK_SMOKE_CONTROL_PORT:-18583}"
MANAGEMENT_PORT="${PORTHOOK_SMOKE_MANAGEMENT_PORT:-18585}"
CADDY_PORT="${PORTHOOK_SMOKE_CADDY_PORT:-18586}"
SUBDOMAIN="${PORTHOOK_SMOKE_SUBDOMAIN:-failtest}"
SUBDOMAIN2="${PORTHOOK_SMOKE_SUBDOMAIN2:-failtest2}"
ADMIN_TOKEN="${PORTHOOK_SMOKE_ADMIN_TOKEN:-smoke-admin-token}"
VALIDATOR_TOKEN="${PORTHOOK_SMOKE_VALIDATOR_TOKEN:-smoke-validator-token}"
MANAGEMENT_TOKEN="${PORTHOOK_SMOKE_MANAGEMENT_TOKEN:-smoke-management-token}"

POSTGRES_IMAGE="${PORTHOOK_SMOKE_POSTGRES_IMAGE:-postgres:16-alpine}"
POSTGRES_PASSWORD="${PORTHOOK_SMOKE_POSTGRES_PASSWORD:-smoke-postgres-password}"
POSTGRES_CONTAINER="porthook-smoke-failure-postgres-$$"
# Fixed, not ephemeral: an ephemeral host port (`-p 127.0.0.1::5432`) can be
# reassigned by `docker restart` on some Docker setups, which would silently
# invalidate DATABASE_URL for every already-running process across the
# postgres_restart scenario. A real deployment addresses Postgres by a
# stable Compose service name, so a fixed local port here matches that,
# not the ephemeral-port convenience the other smoke-*.sh scripts use.
POSTGRES_PORT="${PORTHOOK_SMOKE_POSTGRES_PORT:-15532}"

# Recovery bounds. These are deliberately generous relative to the
# configured protocol timers (see the comment on each scenario) so the
# assertions are meaningful without being flaky under CI scheduling noise.
AGENT_RECONNECT_BOUND_SECONDS="${PORTHOOK_SMOKE_AGENT_RECONNECT_BOUND_SECONDS:-15}"
GATEWAY_SHUTDOWN_BOUND_SECONDS="${PORTHOOK_SMOKE_GATEWAY_SHUTDOWN_BOUND_SECONDS:-15}"
GATEWAY_RESTART_RECOVERY_BOUND_SECONDS="${PORTHOOK_SMOKE_GATEWAY_RESTART_RECOVERY_BOUND_SECONDS:-20}"
CONTROL_PLANE_RECOVERY_BOUND_SECONDS="${PORTHOOK_SMOKE_CONTROL_PLANE_RECOVERY_BOUND_SECONDS:-20}"
CADDY_RECOVERY_BOUND_SECONDS="${PORTHOOK_SMOKE_CADDY_RECOVERY_BOUND_SECONDS:-10}"
POSTGRES_RESTART_RECOVERY_BOUND_SECONDS="${PORTHOOK_SMOKE_POSTGRES_RESTART_RECOVERY_BOUND_SECONDS:-40}"
NETWORK_INTERRUPTION_BOUND_SECONDS="${PORTHOOK_SMOKE_NETWORK_INTERRUPTION_BOUND_SECONDS:-40}"

KEEP_LOGS="${PORTHOOK_SMOKE_KEEP_LOGS:-0}"
SKIP_BUILD="${PORTHOOK_SMOKE_SKIP_BUILD:-0}"

TMP_DIR="$(mktemp -d)"
LOG_DIR="${TMP_DIR}/logs"
CONFIG_FILE="${TMP_DIR}/agent-config.json"
CONFIG_FILE2="${TMP_DIR}/agent2-config.json"
CADDYFILE="${TMP_DIR}/Caddyfile"
mkdir -p "${LOG_DIR}"

pids=()
control_pid=""
gateway_pid=""
agent_pid=""
agent2_pid=""
caddy_pid=""
scenario_failures=0
declare -a scenario_results=()

cleanup() {
	status=$?

	for pid in "${pids[@]:-}"; do
		if kill -0 "${pid}" 2>/dev/null; then
			kill -CONT "${pid}" 2>/dev/null || true
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
	echo "Failure-injection scenario results:" >&2
	for line in "${scenario_results[@]:-}"; do
		echo "  ${line}" >&2
	done

	if [[ "${KEEP_LOGS}" == "1" ]]; then
		echo "Smoke logs kept at ${LOG_DIR}" >&2
	else
		rm -rf "${TMP_DIR}"
	fi

	if [[ "${scenario_failures}" -gt 0 ]]; then
		echo "Failure-injection smoke test FAILED: ${scenario_failures} scenario(s) failed" >&2
		exit 1
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
	timeout_seconds="${4:-20}"
	iterations=$((timeout_seconds * 10))
	for _ in $(seq 1 "${iterations}"); do
		if grep -q "${pattern}" "${log_file}" 2>/dev/null; then
			return 0
		fi
		sleep 0.1
	done
	echo "Timed out waiting for ${name} log pattern: ${pattern}" >&2
	return 1
}

now_epoch() {
	date +%s.%N
}

record_result() {
	scenario_results+=("$1")
}

# request_ok checks whether a request to the given subdomain via the given
# port currently succeeds with a 200. It never exits on failure so callers
# can poll it.
request_ok() {
	port="$1"
	subdomain="$2"
	status="$(curl -sS -o /dev/null -w "%{http_code}" --max-time 3 \
		-H "Host: ${subdomain}.localhost" \
		"http://127.0.0.1:${port}/smoke.txt" 2>/dev/null || echo "000")"
	[[ "${status}" == "200" ]]
}

# request_status returns the raw HTTP status code (or 000 if the request
# could not be made at all), so callers can distinguish a deliberate
# fail-closed response (503) from an actual crash/hang (000).
request_status() {
	port="$1"
	subdomain="$2"
	curl -sS -o /dev/null -w "%{http_code}" --max-time 3 \
		-H "Host: ${subdomain}.localhost" \
		"http://127.0.0.1:${port}/smoke.txt" 2>/dev/null || echo "000"
}

wait_until_true() {
	description="$1"
	timeout_seconds="$2"
	shift 2
	started="$(now_epoch)"
	while true; do
		if "$@"; then
			echo "$(now_epoch) ${started}" | awk '{printf "%.1f", $1-$2}'
			return 0
		fi
		elapsed="$(echo "$(now_epoch) ${started}" | awk '{print ($1-$2)}')"
		if awk -v e="${elapsed}" -v t="${timeout_seconds}" 'BEGIN{exit !(e>t)}'; then
			echo "TIMEOUT" >&2
			echo "-1"
			return 1
		fi
		sleep 0.2
	done
}

wait_until_false() {
	description="$1"
	timeout_seconds="$2"
	shift 2
	started="$(now_epoch)"
	while true; do
		if ! "$@"; then
			echo "$(now_epoch) ${started}" | awk '{printf "%.1f", $1-$2}'
			return 0
		fi
		elapsed="$(echo "$(now_epoch) ${started}" | awk '{print ($1-$2)}')"
		if awk -v e="${elapsed}" -v t="${timeout_seconds}" 'BEGIN{exit !(e>t)}'; then
			echo "TIMEOUT" >&2
			echo "-1"
			return 1
		fi
		sleep 0.2
	done
}

extract_json_field() {
	field="$1"
	python3 -c '
import json
import sys

field = sys.argv[1]
payload = json.load(sys.stdin)
value = payload.get(field, "")
if not isinstance(value, str) or value == "":
    raise SystemExit(f"missing string field: {field}")
print(value)
' "${field}"
}

tunnel_active() {
	subdomain="$1"
	curl -fsS -H "Authorization: Bearer ${MANAGEMENT_TOKEN}" \
		"http://127.0.0.1:${MANAGEMENT_PORT}/api/v1/tunnels" 2>/dev/null |
		python3 -c "
import json, sys
want = sys.argv[1]
try:
    payload = json.load(sys.stdin)
except Exception:
    sys.exit(1)
tunnels = payload.get('tunnels', [])
sys.exit(0 if any(t.get('subdomain') == want for t in tunnels) else 1)
" "${subdomain}"
}

start_control_plane() {
	PORTHOOK_CONTROL_ADDR="127.0.0.1:${CONTROL_PORT}" \
	PORTHOOK_CONTROL_ADMIN_TOKEN="${ADMIN_TOKEN}" \
	PORTHOOK_CONTROL_VALIDATOR_TOKEN="${VALIDATOR_TOKEN}" \
	PORTHOOK_GATEWAY_MANAGEMENT_URL="http://127.0.0.1:${MANAGEMENT_PORT}" \
	PORTHOOK_GATEWAY_MANAGEMENT_TOKEN="${MANAGEMENT_TOKEN}" \
	PORTHOOK_DATABASE_URL="${DATABASE_URL}" \
		"${BIN_DIR}/porthook-control-plane" >>"${LOG_DIR}/control-plane.log" 2>&1 &
	control_pid="$!"
	pids+=("${control_pid}")
	wait_for_url "http://127.0.0.1:${CONTROL_PORT}/readyz" "control plane"
}

start_gateway() {
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
		"${BIN_DIR}/porthook-gateway" >>"${LOG_DIR}/gateway.log" 2>&1 &
	gateway_pid="$!"
	pids+=("${gateway_pid}")
	wait_for_url "http://127.0.0.1:${MANAGEMENT_PORT}/healthz" "gateway"
}

start_agent() {
	local_port="$1"
	config_file="$2"
	subdomain="$3"
	log_prefix="$4"
	PORTHOOK_CONFIG_PATH="${config_file}" \
	PORTHOOK_SERVER_URL="" \
	PORTHOOK_TOKEN="" \
		"${BIN_DIR}/porthook" http "${local_port}" \
		--subdomain "${subdomain}" \
		>"${LOG_DIR}/${log_prefix}.log" 2>"${LOG_DIR}/${log_prefix}.err" &
	echo "$!"
}

require_command curl
require_command go
require_command docker
require_command python3

if [[ "${SKIP_BUILD}" != "1" ]]; then
	(
		cd "${ROOT_DIR}"
		make build VERSION="${VERSION}"
	)
fi

echo "Starting Postgres for failure-injection scenarios" >&2
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
if ! docker exec "${POSTGRES_CONTAINER}" pg_isready -U porthook -d porthook >/dev/null 2>&1; then
	echo "Timed out waiting for smoke Postgres" >&2
	exit 1
fi
DATABASE_URL="postgres://porthook:${POSTGRES_PASSWORD}@127.0.0.1:${POSTGRES_PORT}/porthook?sslmode=disable"

WSECHO_ADDR="127.0.0.1:${WSECHO_PORT}" \
	go run "${ROOT_DIR}/scripts/testdata/wsecho" \
	>"${LOG_DIR}/wsecho.log" 2>&1 &
pids+=("$!")
wait_for_log "${LOG_DIR}/wsecho.log" "listening on" "local echo service"

start_control_plane
start_gateway

created_token_json="$(printf '%s' "${ADMIN_TOKEN}" | \
	"${BIN_DIR}/porthook" tokens create \
	--control-plane "http://127.0.0.1:${CONTROL_PORT}" \
	--admin-token-stdin \
	--name "failure-injection agent" \
	--scope "register_tunnel" \
	--json)"
AGENT_TOKEN="$(printf '%s' "${created_token_json}" | extract_json_field token)"
AGENT_TOKEN_ID="$(printf '%s' "${created_token_json}" | extract_json_field id)"

created_token2_json="$(printf '%s' "${ADMIN_TOKEN}" | \
	"${BIN_DIR}/porthook" tokens create \
	--control-plane "http://127.0.0.1:${CONTROL_PORT}" \
	--admin-token-stdin \
	--name "failure-injection agent 2" \
	--scope "register_tunnel" \
	--json)"
AGENT_TOKEN2="$(printf '%s' "${created_token2_json}" | extract_json_field token)"
AGENT_TOKEN2_ID="$(printf '%s' "${created_token2_json}" | extract_json_field id)"

# Requesting a specific --subdomain requires it to be reserved for the
# token first; without this, tunnel registration fails with "subdomain is
# not reserved for this token" (discovered by actually running this script).
printf '%s' "${ADMIN_TOKEN}" | \
	"${BIN_DIR}/porthook" reserved create \
	--control-plane "http://127.0.0.1:${CONTROL_PORT}" \
	--admin-token-stdin \
	--name "${SUBDOMAIN}" \
	--token-id "${AGENT_TOKEN_ID}" \
	--json >"${LOG_DIR}/reserved-create.json" 2>"${LOG_DIR}/reserved-create.err"

printf '%s' "${ADMIN_TOKEN}" | \
	"${BIN_DIR}/porthook" reserved create \
	--control-plane "http://127.0.0.1:${CONTROL_PORT}" \
	--admin-token-stdin \
	--name "${SUBDOMAIN2}" \
	--token-id "${AGENT_TOKEN2_ID}" \
	--json >"${LOG_DIR}/reserved-create2.json" 2>"${LOG_DIR}/reserved-create2.err"

printf '%s' "${AGENT_TOKEN}" | \
PORTHOOK_CONFIG_PATH="${CONFIG_FILE}" \
PORTHOOK_SERVER_URL="" \
PORTHOOK_TOKEN="" \
	"${BIN_DIR}/porthook" login \
	--server "http://127.0.0.1:${AGENT_PORT}" \
	--token-stdin \
	>"${LOG_DIR}/login.log" 2>"${LOG_DIR}/login.err"

printf '%s' "${AGENT_TOKEN2}" | \
PORTHOOK_CONFIG_PATH="${CONFIG_FILE2}" \
PORTHOOK_SERVER_URL="" \
PORTHOOK_TOKEN="" \
	"${BIN_DIR}/porthook" login \
	--server "http://127.0.0.1:${AGENT_PORT}" \
	--token-stdin \
	>"${LOG_DIR}/login2.log" 2>"${LOG_DIR}/login2.err"

agent_pid="$(start_agent "${WSECHO_PORT}" "${CONFIG_FILE}" "${SUBDOMAIN}" "agent")"
pids+=("${agent_pid}")
wait_for_log "${LOG_DIR}/agent.log" "Tunnel established" "agent registration"

cat >"${CADDYFILE}" <<EOF
:${CADDY_PORT} {
	reverse_proxy 127.0.0.1:${PUBLIC_PORT}
}
EOF
if command -v caddy >/dev/null 2>&1; then
	caddy run --config "${CADDYFILE}" --adapter caddyfile \
		>"${LOG_DIR}/caddy.log" 2>&1 &
	caddy_pid="$!"
	pids+=("${caddy_pid}")
	# request_ok, not wait_for_url: the gateway 404s a Host-less request, so
	# a plain unauthenticated GET would time out here even once Caddy is up.
	elapsed="$(wait_until_true "caddy serving" 10 request_ok "${CADDY_PORT}" "${SUBDOMAIN}")"
	if [[ "${elapsed}" == "-1" ]]; then
		echo "Timed out waiting for caddy: http://127.0.0.1:${CADDY_PORT}/smoke.txt" >&2
	fi
fi

if ! request_ok "${PUBLIC_PORT}" "${SUBDOMAIN}"; then
	echo "Baseline request failed before any failure injection" >&2
	exit 1
fi
echo "Baseline established: ${SUBDOMAIN}.localhost through gateway and (if available) Caddy" >&2

# --- Scenario 1: agent disconnect (crash) ---
echo "" >&2
echo "=== Scenario 1: agent disconnect ===" >&2
kill -9 "${agent_pid}" 2>/dev/null || true
wait "${agent_pid}" 2>/dev/null || true
elapsed="$(wait_until_false "tunnel removed after crash" "${AGENT_RECONNECT_BOUND_SECONDS}" tunnel_active "${SUBDOMAIN}")"
if [[ "${elapsed}" == "-1" ]]; then
	record_result "FAIL agent_disconnect: gateway never removed the crashed tunnel within ${AGENT_RECONNECT_BOUND_SECONDS}s"
	scenario_failures=$((scenario_failures + 1))
else
	record_result "PASS agent_disconnect: gateway removed the crashed tunnel in ${elapsed}s (bound ${AGENT_RECONNECT_BOUND_SECONDS}s)"
fi

restart_started="$(now_epoch)"
agent_pid="$(start_agent "${WSECHO_PORT}" "${CONFIG_FILE}" "${SUBDOMAIN}" "agent-restart1")"
pids+=("${agent_pid}")
if wait_for_log "${LOG_DIR}/agent-restart1.log" "Tunnel established" "agent re-registration" "${AGENT_RECONNECT_BOUND_SECONDS}"; then
	elapsed="$(echo "$(now_epoch) ${restart_started}" | awk '{printf "%.1f", $1-$2}')"
	record_result "PASS agent_disconnect: agent re-established the tunnel in ${elapsed}s after restart"
else
	record_result "FAIL agent_disconnect: agent did not re-establish the tunnel within ${AGENT_RECONNECT_BOUND_SECONDS}s"
	scenario_failures=$((scenario_failures + 1))
fi
if request_ok "${PUBLIC_PORT}" "${SUBDOMAIN}"; then
	record_result "PASS agent_disconnect: requests succeed again after agent restart"
else
	record_result "FAIL agent_disconnect: requests still failing after agent restart"
	scenario_failures=$((scenario_failures + 1))
fi

# --- Scenario 2: control-plane outage ---
# Unlike token validation (checked once at registration), access-policy
# evaluation happens on every public request and calls the control plane
# (see evaluatePublicAccess/EvaluateAccessPolicy in server.go), so it fails
# closed with 503 rather than failing open when the control plane is
# unreachable. That is the intended, secure-by-design behavior: an outage
# must not silently turn a protected tunnel public. So existing tunnels are
# expected to start returning 503 during the outage, not keep serving 200s.
echo "" >&2
echo "=== Scenario 2: control-plane outage ===" >&2
kill "${control_pid}" 2>/dev/null || true
wait "${control_pid}" 2>/dev/null || true
sleep 1
outage_status="$(request_status "${PUBLIC_PORT}" "${SUBDOMAIN}")"
if [[ "${outage_status}" == "503" ]]; then
	record_result "PASS control_plane_outage: already-registered tunnel fails closed with 503 (not a crash/hang) while control plane is down"
else
	record_result "FAIL control_plane_outage: existing tunnel returned status ${outage_status} while control plane was down, want 503"
	scenario_failures=$((scenario_failures + 1))
fi

agent2_pid="$(start_agent "${WSECHO_PORT}" "${CONFIG_FILE2}" "${SUBDOMAIN2}" "agent2-during-outage")"
pids+=("${agent2_pid}")
sleep 2
if grep -q "Tunnel established" "${LOG_DIR}/agent2-during-outage.log" 2>/dev/null; then
	record_result "FAIL control_plane_outage: a new agent registered while the control plane was down (should have been rejected)"
	scenario_failures=$((scenario_failures + 1))
else
	record_result "PASS control_plane_outage: new agent registration correctly blocked while control plane is down"
fi

restart_started="$(now_epoch)"
start_control_plane
if wait_for_log "${LOG_DIR}/agent2-during-outage.log" "Tunnel established" "agent2 registration after recovery" "${CONTROL_PLANE_RECOVERY_BOUND_SECONDS}"; then
	elapsed="$(echo "$(now_epoch) ${restart_started}" | awk '{printf "%.1f", $1-$2}')"
	record_result "PASS control_plane_outage: new agent registered ${elapsed}s after control plane recovered (bound ${CONTROL_PLANE_RECOVERY_BOUND_SECONDS}s)"
else
	record_result "FAIL control_plane_outage: new agent never registered within ${CONTROL_PLANE_RECOVERY_BOUND_SECONDS}s of control plane recovery"
	scenario_failures=$((scenario_failures + 1))
fi
if request_ok "${PUBLIC_PORT}" "${SUBDOMAIN2}"; then
	record_result "PASS control_plane_outage: new tunnel serves requests after control plane recovery"
else
	record_result "FAIL control_plane_outage: new tunnel does not serve requests after control plane recovery"
	scenario_failures=$((scenario_failures + 1))
fi
elapsed="$(wait_until_true "existing tunnel serving again" "${CONTROL_PLANE_RECOVERY_BOUND_SECONDS}" request_ok "${PUBLIC_PORT}" "${SUBDOMAIN}")"
if [[ "${elapsed}" == "-1" ]]; then
	record_result "FAIL control_plane_outage: existing tunnel did not resume serving within ${CONTROL_PLANE_RECOVERY_BOUND_SECONDS}s of control plane recovery"
	scenario_failures=$((scenario_failures + 1))
else
	record_result "PASS control_plane_outage: existing tunnel resumed serving ${elapsed}s after control plane recovery (bound ${CONTROL_PLANE_RECOVERY_BOUND_SECONDS}s)"
fi
kill "${agent2_pid}" 2>/dev/null || true
wait "${agent2_pid}" 2>/dev/null || true

# --- Scenario 3: reverse-proxy (Caddy) restart ---
echo "" >&2
echo "=== Scenario 3: reverse-proxy restart ===" >&2
if [[ -n "${caddy_pid}" ]]; then
	kill "${caddy_pid}" 2>/dev/null || true
	wait "${caddy_pid}" 2>/dev/null || true
	if request_ok "${PUBLIC_PORT}" "${SUBDOMAIN}"; then
		record_result "PASS reverse_proxy_restart: gateway keeps serving requests directly while Caddy is down"
	else
		record_result "FAIL reverse_proxy_restart: gateway stopped serving requests while only Caddy was down"
		scenario_failures=$((scenario_failures + 1))
	fi
	restart_started="$(now_epoch)"
	caddy run --config "${CADDYFILE}" --adapter caddyfile \
		>>"${LOG_DIR}/caddy.log" 2>&1 &
	caddy_pid="$!"
	pids+=("${caddy_pid}")
	elapsed="$(wait_until_true "caddy serving again" "${CADDY_RECOVERY_BOUND_SECONDS}" request_ok "${CADDY_PORT}" "${SUBDOMAIN}")"
	if [[ "${elapsed}" == "-1" ]]; then
		record_result "FAIL reverse_proxy_restart: Caddy did not resume serving within ${CADDY_RECOVERY_BOUND_SECONDS}s"
		scenario_failures=$((scenario_failures + 1))
	else
		record_result "PASS reverse_proxy_restart: Caddy resumed serving in ${elapsed}s (bound ${CADDY_RECOVERY_BOUND_SECONDS}s)"
	fi
else
	record_result "SKIP reverse_proxy_restart: caddy binary not found on PATH"
fi

# --- Scenario 4: Postgres restart ---
# Like the control-plane outage (scenario 2), this tunnel's access policy is
# evaluated by the control plane on every request, which in turn depends on
# this same Postgres instance for its own token/policy lookups. So a brief
# Postgres restart is expected to produce brief 503 access_policy_unavailable
# responses (fail closed), not a crash/hang (000) and not silent 200s.
echo "" >&2
echo "=== Scenario 4: Postgres restart ===" >&2
docker restart "${POSTGRES_CONTAINER}" >/dev/null 2>&1 &
restart_bg_pid="$!"
public_degraded_gracefully=1
for _ in $(seq 1 20); do
	status="$(request_status "${PUBLIC_PORT}" "${SUBDOMAIN}")"
	if [[ "${status}" != "200" && "${status}" != "503" ]]; then
		public_degraded_gracefully=0
	fi
	sleep 0.5
done
wait "${restart_bg_pid}" 2>/dev/null || true
if [[ "${public_degraded_gracefully}" -eq 1 ]]; then
	record_result "PASS postgres_restart: public requests either succeeded or failed closed with 503 throughout the Postgres restart (no crash/hang)"
else
	record_result "FAIL postgres_restart: at least one public request returned neither 200 nor 503 during the Postgres restart"
	scenario_failures=$((scenario_failures + 1))
fi
if kill -0 "${gateway_pid}" 2>/dev/null && kill -0 "${control_pid}" 2>/dev/null; then
	record_result "PASS postgres_restart: gateway and control-plane processes survived the Postgres restart"
else
	record_result "FAIL postgres_restart: gateway or control-plane process died during the Postgres restart"
	scenario_failures=$((scenario_failures + 1))
fi
db_check_started="$(now_epoch)"
elapsed="$(wait_until_true "control plane ready again" "${POSTGRES_RESTART_RECOVERY_BOUND_SECONDS}" bash -c "curl -fsS http://127.0.0.1:${CONTROL_PORT}/readyz >/dev/null 2>&1")"
if [[ "${elapsed}" == "-1" ]]; then
	record_result "FAIL postgres_restart: control plane did not report ready within ${POSTGRES_RESTART_RECOVERY_BOUND_SECONDS}s of Postgres restart"
	scenario_failures=$((scenario_failures + 1))
else
	record_result "PASS postgres_restart: control plane reported ready again ${elapsed}s after Postgres restart (bound ${POSTGRES_RESTART_RECOVERY_BOUND_SECONDS}s)"
fi

# --- Scenario 5: slow Postgres ---
echo "" >&2
echo "=== Scenario 5: slow Postgres ===" >&2
metrics_before="$(curl -fsS -H "Authorization: Bearer ${MANAGEMENT_TOKEN}" "http://127.0.0.1:${MANAGEMENT_PORT}/metrics" 2>/dev/null | awk '$1=="porthook_gateway_db_wait_count_total"{print $2}')"
docker update --cpus 0.05 "${POSTGRES_CONTAINER}" >/dev/null 2>&1
slow_ok=1
for _ in $(seq 1 20); do
	if ! request_ok "${PUBLIC_PORT}" "${SUBDOMAIN}"; then
		slow_ok=0
	fi
	sleep 0.5
done
metrics_after="$(curl -fsS -H "Authorization: Bearer ${MANAGEMENT_TOKEN}" "http://127.0.0.1:${MANAGEMENT_PORT}/metrics" 2>/dev/null | awk '$1=="porthook_gateway_db_wait_count_total"{print $2}')"
docker update --cpus 0 "${POSTGRES_CONTAINER}" >/dev/null 2>&1
if [[ "${slow_ok}" -eq 1 ]]; then
	record_result "PASS slow_postgres: public requests kept succeeding while Postgres was CPU-throttled (durable logging degrades independently of public traffic)"
else
	record_result "FAIL slow_postgres: public requests failed while Postgres was slow, but request logging should not block public traffic"
	scenario_failures=$((scenario_failures + 1))
fi
record_result "INFO slow_postgres: porthook_gateway_db_wait_count_total went from ${metrics_before:-0} to ${metrics_after:-0} while Postgres was throttled"

# --- Scenario 6: gateway graceful shutdown ---
echo "" >&2
echo "=== Scenario 6: gateway graceful shutdown ===" >&2
shutdown_started="$(now_epoch)"
kill -TERM "${gateway_pid}" 2>/dev/null || true
shutdown_ok=0
for _ in $(seq 1 $((GATEWAY_SHUTDOWN_BOUND_SECONDS * 10))); do
	if ! kill -0 "${gateway_pid}" 2>/dev/null; then
		shutdown_ok=1
		break
	fi
	sleep 0.1
done
elapsed="$(echo "$(now_epoch) ${shutdown_started}" | awk '{printf "%.1f", $1-$2}')"
if [[ "${shutdown_ok}" -eq 1 ]]; then
	record_result "PASS gateway_graceful_shutdown: process exited in ${elapsed}s (bound ${GATEWAY_SHUTDOWN_BOUND_SECONDS}s)"
else
	record_result "FAIL gateway_graceful_shutdown: process did not exit within ${GATEWAY_SHUTDOWN_BOUND_SECONDS}s"
	scenario_failures=$((scenario_failures + 1))
	kill -9 "${gateway_pid}" 2>/dev/null || true
fi

restart_started="$(now_epoch)"
start_gateway
elapsed="$(wait_until_true "agent reconnected after gateway restart" "${GATEWAY_RESTART_RECOVERY_BOUND_SECONDS}" tunnel_active "${SUBDOMAIN}")"
if [[ "${elapsed}" == "-1" ]]; then
	record_result "FAIL gateway_graceful_shutdown: agent did not reconnect within ${GATEWAY_RESTART_RECOVERY_BOUND_SECONDS}s of gateway restart"
	scenario_failures=$((scenario_failures + 1))
else
	record_result "PASS gateway_graceful_shutdown: agent reconnected ${elapsed}s after gateway restart (bound ${GATEWAY_RESTART_RECOVERY_BOUND_SECONDS}s)"
fi
if request_ok "${PUBLIC_PORT}" "${SUBDOMAIN}"; then
	record_result "PASS gateway_graceful_shutdown: requests succeed again after gateway restart"
else
	record_result "FAIL gateway_graceful_shutdown: requests still failing after gateway restart"
	scenario_failures=$((scenario_failures + 1))
fi

# --- Scenario 7: network interruption (agent frozen, not disconnected) ---
echo "" >&2
echo "=== Scenario 7: network interruption ===" >&2
kill -STOP "${agent_pid}" 2>/dev/null || true
sleep 1
interruption_started="$(now_epoch)"
if request_ok "${PUBLIC_PORT}" "${SUBDOMAIN}"; then
	record_result "FAIL network_interruption: request unexpectedly succeeded while the agent process was frozen"
	scenario_failures=$((scenario_failures + 1))
else
	record_result "PASS network_interruption: requests fail gracefully (no gateway crash) while the agent is unresponsive"
fi
kill -CONT "${agent_pid}" 2>/dev/null || true
elapsed="$(wait_until_true "requests succeed again after network interruption" "${NETWORK_INTERRUPTION_BOUND_SECONDS}" request_ok "${PUBLIC_PORT}" "${SUBDOMAIN}")"
if [[ "${elapsed}" == "-1" ]]; then
	record_result "FAIL network_interruption: requests did not recover within ${NETWORK_INTERRUPTION_BOUND_SECONDS}s of resuming the agent"
	scenario_failures=$((scenario_failures + 1))
else
	total_elapsed="$(echo "$(now_epoch) ${interruption_started}" | awk '{printf "%.1f", $1-$2}')"
	record_result "PASS network_interruption: requests recovered ${elapsed}s after resuming the agent (${total_elapsed}s total from freeze; bound ${NETWORK_INTERRUPTION_BOUND_SECONDS}s)"
fi

echo "" >&2
if [[ "${scenario_failures}" -eq 0 ]]; then
	echo "Failure-injection smoke test passed: all scenarios recovered within bounds" >&2
fi
