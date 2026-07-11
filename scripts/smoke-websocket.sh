#!/usr/bin/env bash
set -euo pipefail

# Proves that a real gateway and agent binary pair tunnel real public
# WebSocket traffic to a real local WebSocket service end to end. See
# docs/PRODUCTION_ROADMAP.md Block 5.

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN_DIR="${ROOT_DIR}/bin"

VERSION="${VERSION:-smoke-websocket}"
WSECHO_PORT="${PORTHOOK_SMOKE_WSECHO_PORT:-13200}"
PUBLIC_PORT="${PORTHOOK_SMOKE_PUBLIC_PORT:-18280}"
AGENT_PORT="${PORTHOOK_SMOKE_AGENT_PORT:-18281}"
MANAGEMENT_PORT="${PORTHOOK_SMOKE_MANAGEMENT_PORT:-18285}"
SUBDOMAIN="${PORTHOOK_SMOKE_SUBDOMAIN:-ws-demo}"
TOKEN="${PORTHOOK_SMOKE_TOKEN:-smoke-token}"
KEEP_LOGS="${PORTHOOK_SMOKE_KEEP_LOGS:-0}"
SKIP_BUILD="${PORTHOOK_SMOKE_SKIP_BUILD:-0}"

TMP_DIR="$(mktemp -d)"
LOG_DIR="${TMP_DIR}/logs"
mkdir -p "${LOG_DIR}"

pids=()

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

	if [[ "${status}" -ne 0 ]]; then
		echo "WebSocket smoke test failed. Logs:" >&2
		for log in "${LOG_DIR}"/*; do
			if [[ -f "${log}" ]]; then
				echo "==> ${log}" >&2
				sed -n '1,200p' "${log}" >&2
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

	for _ in $(seq 1 100); do
		if grep -q "${pattern}" "${log_file}" 2>/dev/null; then
			return 0
		fi
		sleep 0.1
	done

	echo "Timed out waiting for ${name} log pattern: ${pattern}" >&2
	return 1
}

require_command curl
require_command go

if [[ "${SKIP_BUILD}" != "1" ]]; then
	(
		cd "${ROOT_DIR}"
		make build VERSION="${VERSION}"
	)
fi

WSECHO_ADDR="127.0.0.1:${WSECHO_PORT}" \
	go run "${ROOT_DIR}/scripts/testdata/wsecho" \
	>"${LOG_DIR}/wsecho.log" 2>&1 &
pids+=("$!")
wait_for_log "${LOG_DIR}/wsecho.log" "listening on" "local websocket echo service"

PORTHOOK_ADDR="127.0.0.1:${PUBLIC_PORT}" \
PORTHOOK_AGENT_ADDR="127.0.0.1:${AGENT_PORT}" \
PORTHOOK_MANAGEMENT_ADDR="127.0.0.1:${MANAGEMENT_PORT}" \
PORTHOOK_ROOT_DOMAIN="localhost" \
PORTHOOK_PUBLIC_URL="http://localhost:${PUBLIC_PORT}" \
PORTHOOK_STATIC_TOKEN="${TOKEN}" \
	"${BIN_DIR}/porthook-gateway" >"${LOG_DIR}/gateway.log" 2>&1 &
pids+=("$!")
wait_for_url "http://127.0.0.1:${MANAGEMENT_PORT}/healthz" "gateway"

"${BIN_DIR}/porthook" http "${WSECHO_PORT}" \
	--server "http://127.0.0.1:${AGENT_PORT}" \
	--token "${TOKEN}" \
	--subdomain "${SUBDOMAIN}" \
	>"${LOG_DIR}/agent.log" 2>"${LOG_DIR}/agent.err" &
pids+=("$!")
wait_for_log "${LOG_DIR}/agent.log" "Tunnel established" "agent registration"

go run "${ROOT_DIR}/scripts/testdata/wsclient" \
	"ws://127.0.0.1:${PUBLIC_PORT}/socket" \
	"${SUBDOMAIN}.localhost" \
	>"${LOG_DIR}/wsclient.log" 2>&1
cat "${LOG_DIR}/wsclient.log"
if ! grep -q "websocket smoke check passed" "${LOG_DIR}/wsclient.log"; then
	echo "WebSocket client did not report success" >&2
	exit 1
fi

wait_for_log "${LOG_DIR}/agent.log" "WS /socket -> completed" "agent websocket request log"

echo "WebSocket smoke test passed: ws://${SUBDOMAIN}.localhost:${PUBLIC_PORT}/socket"
