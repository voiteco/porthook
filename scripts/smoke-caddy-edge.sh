#!/usr/bin/env bash
set -euo pipefail

# Proves that an ip_allowlist access policy evaluates the original client
# address observed by a real Caddy reverse proxy, not a value a direct
# client injects via X-Forwarded-For. See docs/PRODUCTION_ROADMAP.md Block 4.

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN_DIR="${ROOT_DIR}/bin"

VERSION="${VERSION:-smoke-caddy-edge}"
LOCAL_PORT="${PORTHOOK_SMOKE_LOCAL_PORT:-13101}"
PUBLIC_PORT="${PORTHOOK_SMOKE_PUBLIC_PORT:-18182}"
AGENT_PORT="${PORTHOOK_SMOKE_AGENT_PORT:-18183}"
CONTROL_PORT="${PORTHOOK_SMOKE_CONTROL_PORT:-18184}"
MANAGEMENT_PORT="${PORTHOOK_SMOKE_MANAGEMENT_PORT:-18186}"
CADDY_PORT="${PORTHOOK_SMOKE_CADDY_PORT:-18187}"
SUBDOMAIN="${PORTHOOK_SMOKE_SUBDOMAIN:-edge-demo}"
ADMIN_TOKEN="${PORTHOOK_SMOKE_ADMIN_TOKEN:-smoke-admin-token}"
VALIDATOR_TOKEN="${PORTHOOK_SMOKE_VALIDATOR_TOKEN:-smoke-validator-token}"
MANAGEMENT_TOKEN="${PORTHOOK_SMOKE_MANAGEMENT_TOKEN:-smoke-management-token}"
DATABASE_URL="${PORTHOOK_SMOKE_DATABASE_URL:-}"
KEEP_LOGS="${PORTHOOK_SMOKE_KEEP_LOGS:-0}"
SKIP_BUILD="${PORTHOOK_SMOKE_SKIP_BUILD:-0}"

# All local processes in this smoke test share the loopback address, so
# trusting only the exact peer that fronts the gateway (Caddy) mirrors the
# production Compose network, which trusts the internal service subnet and
# never exposes the gateway's public port outside it.
TRUSTED_PROXIES="127.0.0.1/32"

TMP_DIR="$(mktemp -d)"
FIXTURE_DIR="${TMP_DIR}/fixture"
LOG_DIR="${TMP_DIR}/logs"
CONFIG_FILE="${TMP_DIR}/agent-config.json"
CADDYFILE="${TMP_DIR}/Caddyfile"
MARKER="porthook caddy edge smoke ok"

mkdir -p "${FIXTURE_DIR}" "${LOG_DIR}"
printf '%s\n' "${MARKER}" > "${FIXTURE_DIR}/smoke.txt"

pids=()
control_pid=""
gateway_pid=""
caddy_pid=""

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
		echo "Caddy edge smoke test failed. Logs:" >&2
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

require_command curl
require_command caddy
require_command python3

if [[ "${SKIP_BUILD}" != "1" ]]; then
	(
		cd "${ROOT_DIR}"
		make build VERSION="${VERSION}"
	)
fi

python3 - "${LOCAL_PORT}" "${FIXTURE_DIR}" \
	>"${LOG_DIR}/local-http.log" 2>&1 <<'PY' &
import http.server
import pathlib
import sys
import urllib.parse

port = int(sys.argv[1])
fixture_dir = pathlib.Path(sys.argv[2])

class Handler(http.server.BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def do_GET(self):
        path = urllib.parse.urlsplit(self.path).path
        if path != "/smoke.txt":
            self.send_error(404)
            return
        body = (fixture_dir / "smoke.txt").read_bytes()
        self.send_response(200)
        self.send_header("Content-Type", "text/plain")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, format, *args):
        return

with http.server.ThreadingHTTPServer(("127.0.0.1", port), Handler) as server:
    server.serve_forever()
PY
pids+=("$!")
wait_for_url "http://127.0.0.1:${LOCAL_PORT}/smoke.txt" "local HTTP service"

PORTHOOK_CONTROL_ADDR="127.0.0.1:${CONTROL_PORT}" \
PORTHOOK_CONTROL_ADMIN_TOKEN="${ADMIN_TOKEN}" \
PORTHOOK_CONTROL_VALIDATOR_TOKEN="${VALIDATOR_TOKEN}" \
PORTHOOK_TRUSTED_PROXIES="${TRUSTED_PROXIES}" \
PORTHOOK_GATEWAY_MANAGEMENT_URL="http://127.0.0.1:${MANAGEMENT_PORT}" \
PORTHOOK_GATEWAY_MANAGEMENT_TOKEN="${MANAGEMENT_TOKEN}" \
PORTHOOK_DATABASE_URL="${DATABASE_URL}" \
	"${BIN_DIR}/porthook-control-plane" >>"${LOG_DIR}/control-plane.log" 2>&1 &
control_pid="$!"
pids+=("${control_pid}")
wait_for_url "http://127.0.0.1:${CONTROL_PORT}/readyz" "control plane"

PORTHOOK_ADDR="127.0.0.1:${PUBLIC_PORT}" \
PORTHOOK_AGENT_ADDR="127.0.0.1:${AGENT_PORT}" \
PORTHOOK_MANAGEMENT_ADDR="127.0.0.1:${MANAGEMENT_PORT}" \
PORTHOOK_MANAGEMENT_TOKEN="${MANAGEMENT_TOKEN}" \
PORTHOOK_TRUSTED_PROXIES="${TRUSTED_PROXIES}" \
PORTHOOK_ROOT_DOMAIN="localhost" \
PORTHOOK_PUBLIC_URL="http://localhost:${PUBLIC_PORT}" \
PORTHOOK_STATIC_TOKEN="unused-static-token" \
PORTHOOK_CONTROL_PLANE_URL="http://127.0.0.1:${CONTROL_PORT}" \
PORTHOOK_CONTROL_PLANE_TOKEN="${VALIDATOR_TOKEN}" \
	"${BIN_DIR}/porthook-gateway" >>"${LOG_DIR}/gateway.log" 2>&1 &
gateway_pid="$!"
pids+=("${gateway_pid}")
wait_for_url "http://127.0.0.1:${MANAGEMENT_PORT}/healthz" "gateway"

created_token_json="$(printf '%s' "${ADMIN_TOKEN}" | \
	"${BIN_DIR}/porthook" tokens create \
	--control-plane "http://127.0.0.1:${CONTROL_PORT}" \
	--admin-token-stdin \
	--name "smoke edge agent" \
	--scope "register_tunnel" \
	--json)"
AGENT_TOKEN_ID="$(printf '%s' "${created_token_json}" | extract_json_field id)"
AGENT_TOKEN="$(printf '%s' "${created_token_json}" | extract_json_field token)"

reserved_json="$(printf '%s' "${ADMIN_TOKEN}" | \
	"${BIN_DIR}/porthook" reserved create \
	--control-plane "http://127.0.0.1:${CONTROL_PORT}" \
	--admin-token-stdin \
	--name "${SUBDOMAIN}" \
	--token-id "${AGENT_TOKEN_ID}" \
	--json)"
RESERVED_SUBDOMAIN_ID="$(printf '%s' "${reserved_json}" | extract_json_field id)"

# Allow only the loopback address Caddy will report as the true client.
access_policy_json="$(printf '%s' "${ADMIN_TOKEN}" | \
	"${BIN_DIR}/porthook" access create \
	--control-plane "http://127.0.0.1:${CONTROL_PORT}" \
	--admin-token-stdin \
	--reserved-subdomain-id "${RESERVED_SUBDOMAIN_ID}" \
	--mode "ip_allowlist" \
	--ip-allowlist "127.0.0.1/32" \
	--json)"
ACCESS_POLICY_ID="$(printf '%s' "${access_policy_json}" | extract_json_field id)"

printf '%s' "${AGENT_TOKEN}" | \
PORTHOOK_CONFIG_PATH="${CONFIG_FILE}" \
PORTHOOK_SERVER_URL="" \
PORTHOOK_TOKEN="" \
	"${BIN_DIR}/porthook" login \
	--server "http://127.0.0.1:${AGENT_PORT}" \
	--token-stdin \
	>"${LOG_DIR}/login.log" 2>"${LOG_DIR}/login.err"

PORTHOOK_CONFIG_PATH="${CONFIG_FILE}" \
PORTHOOK_SERVER_URL="" \
PORTHOOK_TOKEN="" \
	"${BIN_DIR}/porthook" http "${LOCAL_PORT}" \
	--subdomain "${SUBDOMAIN}" \
	>"${LOG_DIR}/agent.log" 2>"${LOG_DIR}/agent.err" &
pids+=("$!")
wait_for_log "${LOG_DIR}/agent.log" "Tunnel established" "agent registration"

cat >"${CADDYFILE}" <<EOF
:${CADDY_PORT} {
	reverse_proxy 127.0.0.1:${PUBLIC_PORT}
}
EOF

caddy run --config "${CADDYFILE}" --adapter caddyfile \
	>"${LOG_DIR}/caddy.log" 2>&1 &
caddy_pid="$!"
pids+=("${caddy_pid}")

caddy_ready=0
for _ in $(seq 1 100); do
	if curl -sS -o /dev/null -H "Host: ${SUBDOMAIN}.localhost" "http://127.0.0.1:${CADDY_PORT}/smoke.txt" 2>/dev/null; then
		caddy_ready=1
		break
	fi
	sleep 0.1
done
if [[ "${caddy_ready}" -ne 1 ]]; then
	echo "Timed out waiting for caddy: http://127.0.0.1:${CADDY_PORT}/smoke.txt" >&2
	exit 1
fi

# A direct client cannot use X-Forwarded-For to impersonate an allowed
# address: Caddy overwrites the header with the peer address it actually
# observed before forwarding to the gateway, so the spoofed value never
# reaches the access-policy evaluation.
allowed_status="$(curl -sS \
	-o "${LOG_DIR}/allowed.body" \
	-w "%{http_code}" \
	-H "Host: ${SUBDOMAIN}.localhost" \
	-H "X-Forwarded-For: 203.0.113.77" \
	"http://127.0.0.1:${CADDY_PORT}/smoke.txt")"
if [[ "${allowed_status}" != "200" ]]; then
	echo "Request through Caddy from the allowlisted loopback peer = ${allowed_status}, want 200" >&2
	exit 1
fi
if [[ "$(cat "${LOG_DIR}/allowed.body")" != "${MARKER}" ]]; then
	echo "Unexpected body from allowed request: $(cat "${LOG_DIR}/allowed.body")" >&2
	exit 1
fi

# The gateway's own request log must record the address Caddy observed,
# not the header value the client tried to inject.
request_log_ip=""
for _ in $(seq 1 20); do
	request_logs_json="$(curl -fsS -H "Authorization: Bearer ${MANAGEMENT_TOKEN}" "http://127.0.0.1:${MANAGEMENT_PORT}/api/v1/request-logs?limit=20")"
	request_log_ip="$(printf '%s' "${request_logs_json}" | python3 -c '
import json
import sys

want_subdomain = sys.argv[1]
payload = json.load(sys.stdin)
for entry in payload.get("request_logs", []):
    if entry.get("subdomain") == want_subdomain and entry.get("path") == "/smoke.txt" and entry.get("status") == 200:
        print(entry.get("remote_ip", ""))
        raise SystemExit(0)
raise SystemExit(1)
' "${SUBDOMAIN}")" && break
	sleep 0.1
done
if [[ "${request_log_ip}" != "127.0.0.1" ]]; then
	echo "Gateway request log remote_ip = ${request_log_ip@Q}, want the Caddy-observed loopback address, not the spoofed header" >&2
	exit 1
fi

# Now point the allowlist at the address the client claims to be, and prove
# the forged header still does not grant access: Caddy still reports the
# true loopback peer, which is no longer allowed.
printf '%s' "${ADMIN_TOKEN}" | \
	"${BIN_DIR}/porthook" access update \
	--control-plane "http://127.0.0.1:${CONTROL_PORT}" \
	--admin-token-stdin \
	--mode "ip_allowlist" \
	--ip-allowlist "203.0.113.50/32" \
	"${ACCESS_POLICY_ID}" \
	>"${LOG_DIR}/access-update.json" 2>"${LOG_DIR}/access-update.err"

denied_status="$(curl -sS \
	-o "${LOG_DIR}/denied.body" \
	-w "%{http_code}" \
	-H "Host: ${SUBDOMAIN}.localhost" \
	-H "X-Forwarded-For: 203.0.113.50" \
	"http://127.0.0.1:${CADDY_PORT}/smoke.txt")"
if [[ "${denied_status}" != "403" ]]; then
	echo "Request through Caddy with a forged allowed identity = ${denied_status}, want 403" >&2
	exit 1
fi

echo "Caddy edge smoke test passed: ip_allowlist policies use the original client address through http://127.0.0.1:${CADDY_PORT}"
