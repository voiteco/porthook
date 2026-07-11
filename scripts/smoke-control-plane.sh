#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN_DIR="${ROOT_DIR}/bin"

VERSION="${VERSION:-smoke-control-plane}"
LOCAL_PORT="${PORTHOOK_SMOKE_LOCAL_PORT:-13001}"
PUBLIC_PORT="${PORTHOOK_SMOKE_PUBLIC_PORT:-18082}"
AGENT_PORT="${PORTHOOK_SMOKE_AGENT_PORT:-18083}"
CONTROL_PORT="${PORTHOOK_SMOKE_CONTROL_PORT:-18084}"
MANAGEMENT_PORT="${PORTHOOK_SMOKE_MANAGEMENT_PORT:-18086}"
SUBDOMAIN="${PORTHOOK_SMOKE_SUBDOMAIN:-control-demo}"
ADMIN_TOKEN="${PORTHOOK_SMOKE_ADMIN_TOKEN:-smoke-admin-token}"
VALIDATOR_TOKEN="${PORTHOOK_SMOKE_VALIDATOR_TOKEN:-smoke-validator-token}"
MANAGEMENT_TOKEN="${PORTHOOK_SMOKE_MANAGEMENT_TOKEN:-smoke-management-token}"
BASIC_USERNAME="${PORTHOOK_SMOKE_BASIC_USERNAME:-smoke-user}"
BASIC_PASSWORD="${PORTHOOK_SMOKE_BASIC_PASSWORD:-smoke-password}"
DATABASE_URL="${PORTHOOK_SMOKE_DATABASE_URL:-}"
REQUEST_LOG_DATABASE_URL="${PORTHOOK_SMOKE_REQUEST_LOG_DATABASE_URL:-${DATABASE_URL}}"
DURABLE_RESTART="${PORTHOOK_SMOKE_DURABLE_RESTART:-0}"
KEEP_LOGS="${PORTHOOK_SMOKE_KEEP_LOGS:-0}"
SKIP_BUILD="${PORTHOOK_SMOKE_SKIP_BUILD:-0}"

TMP_DIR="$(mktemp -d)"
FIXTURE_DIR="${TMP_DIR}/fixture"
LOG_DIR="${TMP_DIR}/logs"
CONFIG_FILE="${TMP_DIR}/agent-config.json"
UPLOAD_FILE="${TMP_DIR}/upload.bin"
UPLOAD_RESPONSE_FILE="${TMP_DIR}/upload-response.bin"
MARKER="porthook control-plane smoke ok"

mkdir -p "${FIXTURE_DIR}" "${LOG_DIR}"
printf '%s\n' "${MARKER}" > "${FIXTURE_DIR}/smoke.txt"
python3 - "${UPLOAD_FILE}" <<'PY'
import pathlib
import sys

pathlib.Path(sys.argv[1]).write_bytes((b"porthook-control-plane-stream-" * 3000) + b"ok")
PY

pids=()
control_pid=""
gateway_pid=""

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
		echo "Control-plane smoke test failed. Logs:" >&2
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

stop_process() {
	pid="$1"
	name="$2"
	if [[ -z "${pid}" ]]; then
		return 0
	fi
	if kill -0 "${pid}" 2>/dev/null; then
		kill "${pid}" 2>/dev/null || true
		wait "${pid}" 2>/dev/null || true
	fi
	echo "Stopped ${name}" >>"${LOG_DIR}/smoke-control-plane-events.log"
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
	PORTHOOK_REQUEST_LOG_DATABASE_URL="${REQUEST_LOG_DATABASE_URL}" \
		"${BIN_DIR}/porthook-gateway" >>"${LOG_DIR}/gateway.log" 2>&1 &
	gateway_pid="$!"
	pids+=("${gateway_pid}")
	wait_for_url "http://127.0.0.1:${MANAGEMENT_PORT}/healthz" "gateway"
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
require_command cmp
require_command python3

if [[ "${DURABLE_RESTART}" == "1" && -z "${DATABASE_URL}" ]]; then
	echo "PORTHOOK_SMOKE_DATABASE_URL is required when PORHOOK_SMOKE_DURABLE_RESTART=1" >&2
	exit 1
fi

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

    def do_POST(self):
        if self.path != "/echo":
            self.send_error(404)
            return
        length = int(self.headers.get("Content-Length", "0"))
        body = self.rfile.read(length)
        self.send_response(200)
        self.send_header("Content-Type", "application/octet-stream")
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

start_control_plane

dashboard_html="$(curl -fsS "http://127.0.0.1:${CONTROL_PORT}/dashboard/")"
if [[ "${dashboard_html}" != *"Token management"* ]]; then
	echo "Dashboard index did not include token management UI" >&2
	exit 1
fi
if [[ "${dashboard_html}" != *"Admin tokens"* ]]; then
	echo "Dashboard index did not include admin token management UI" >&2
	exit 1
fi

dashboard_js="$(curl -fsS "http://127.0.0.1:${CONTROL_PORT}/dashboard/app.js")"
if [[ "${dashboard_js}" != *"/api/v1/status"* ]]; then
	echo "Dashboard JavaScript did not include status API client" >&2
	exit 1
fi
if [[ "${dashboard_js}" != *"/api/v1/reserved-subdomains"* ]]; then
	echo "Dashboard JavaScript did not include reserved subdomain API client" >&2
	exit 1
fi
if [[ "${dashboard_js}" != *"/api/v1/admin-tokens"* ]]; then
	echo "Dashboard JavaScript did not include admin token API client" >&2
	exit 1
fi
if [[ "${dashboard_js}" != *"/api/v1/access-policies"* ]]; then
	echo "Dashboard JavaScript did not include access policy API client" >&2
	exit 1
fi
if [[ "${dashboard_js}" != *"/api/v1/tunnels"* ]]; then
	echo "Dashboard JavaScript did not include gateway tunnel API client" >&2
	exit 1
fi
if [[ "${dashboard_js}" != *"/api/v1/request-logs"* ]]; then
	echo "Dashboard JavaScript did not include gateway request log API client" >&2
	exit 1
fi

status_json="$(curl -fsS "http://127.0.0.1:${CONTROL_PORT}/api/v1/status")"
printf '%s' "${status_json}" | python3 -c '
import json
import sys

want_version = sys.argv[1]
payload = json.load(sys.stdin)
if payload.get("status") != "ready" or payload.get("ready") is not True:
    raise SystemExit(f"unexpected control-plane status: {payload}")
got_version = payload.get("version")
if got_version != want_version:
    raise SystemExit(f"unexpected control-plane version: {got_version!r}, want {want_version!r}")
' "${VERSION}"

unauthorized_status="$(curl -sS -o /dev/null -w "%{http_code}" \
	-X POST "http://127.0.0.1:${CONTROL_PORT}/api/v1/tokens/validate" \
	-H "Content-Type: application/json" \
	-d '{"token":"ph_missing","scope":"register_tunnel"}')"
if [[ "${unauthorized_status}" != "401" ]]; then
	echo "Unexpected unauthenticated validation status: ${unauthorized_status}" >&2
	exit 1
fi

created_token_json="$(printf '%s' "${ADMIN_TOKEN}" | \
	"${BIN_DIR}/porthook" tokens create \
	--control-plane "http://127.0.0.1:${CONTROL_PORT}" \
	--admin-token-stdin \
	--name "smoke agent" \
	--scope "register_tunnel" \
	--json)"
AGENT_TOKEN_ID="$(printf '%s' "${created_token_json}" | extract_json_field id)"
AGENT_TOKEN="$(printf '%s' "${created_token_json}" | extract_json_field token)"

printf '%s' "${ADMIN_TOKEN}" | \
	"${BIN_DIR}/porthook" tokens list \
	--control-plane "http://127.0.0.1:${CONTROL_PORT}" \
	--admin-token-stdin \
	--json \
	>"${LOG_DIR}/tokens-list.json" 2>"${LOG_DIR}/tokens-list.err"
if ! grep -q "${AGENT_TOKEN_ID}" "${LOG_DIR}/tokens-list.json"; then
	echo "Created token was not listed: ${AGENT_TOKEN_ID}" >&2
	exit 1
fi

created_admin_token_json="$(printf '%s' "${ADMIN_TOKEN}" | \
	"${BIN_DIR}/porthook" admin tokens create \
	--control-plane "http://127.0.0.1:${CONTROL_PORT}" \
	--admin-token-stdin \
	--name "smoke audit reader" \
	--scope "audit_history" \
	--json)"
SCOPED_ADMIN_TOKEN_ID="$(printf '%s' "${created_admin_token_json}" | extract_json_field id)"
SCOPED_ADMIN_TOKEN="$(printf '%s' "${created_admin_token_json}" | extract_json_field token)"

printf '%s' "${ADMIN_TOKEN}" | \
	"${BIN_DIR}/porthook" admin tokens list \
	--control-plane "http://127.0.0.1:${CONTROL_PORT}" \
	--admin-token-stdin \
	--json \
	>"${LOG_DIR}/admin-tokens-list.json" 2>"${LOG_DIR}/admin-tokens-list.err"
if ! grep -q "${SCOPED_ADMIN_TOKEN_ID}" "${LOG_DIR}/admin-tokens-list.json"; then
	echo "Created admin token was not listed: ${SCOPED_ADMIN_TOKEN_ID}" >&2
	exit 1
fi
if grep -q "${SCOPED_ADMIN_TOKEN}" "${LOG_DIR}/admin-tokens-list.json"; then
	echo "Admin token list exposed a plaintext admin token" >&2
	exit 1
fi

printf '%s' "${SCOPED_ADMIN_TOKEN}" | \
	"${BIN_DIR}/porthook" history events \
	--control-plane "http://127.0.0.1:${CONTROL_PORT}" \
	--admin-token-stdin \
	--event "control_plane.admin_token_created" \
	--limit 20 \
	--json \
	>"${LOG_DIR}/history-events-scoped-admin-token.json" 2>"${LOG_DIR}/history-events-scoped-admin-token.err"
python3 - "${LOG_DIR}/history-events-scoped-admin-token.json" <<'PY'
import json
import pathlib
import sys

payload = json.loads(pathlib.Path(sys.argv[1]).read_text())
if not any(event.get("event") == "control_plane.admin_token_created" for event in payload.get("events", [])):
    raise SystemExit(f"missing admin_token_created audit event: {payload}")
PY

if printf '%s' "${SCOPED_ADMIN_TOKEN}" | \
	"${BIN_DIR}/porthook" tokens list \
	--control-plane "http://127.0.0.1:${CONTROL_PORT}" \
	--admin-token-stdin \
	--json \
	>"${LOG_DIR}/tokens-list-scoped-admin-token.json" 2>"${LOG_DIR}/tokens-list-scoped-admin-token.err"; then
	echo "Audit-only admin token unexpectedly listed agent tokens" >&2
	exit 1
fi
if ! grep -q "missing the required admin scope" "${LOG_DIR}/tokens-list-scoped-admin-token.err"; then
	echo "Scoped admin token failure did not explain missing scope" >&2
	exit 1
fi

printf '%s' "${ADMIN_TOKEN}" | \
	"${BIN_DIR}/porthook" reserved create \
	--control-plane "http://127.0.0.1:${CONTROL_PORT}" \
	--admin-token-stdin \
	--name "${SUBDOMAIN}" \
	--token-id "${AGENT_TOKEN_ID}" \
	--json \
	>"${LOG_DIR}/reserved-create.json" 2>"${LOG_DIR}/reserved-create.err"
if ! grep -q "${SUBDOMAIN}" "${LOG_DIR}/reserved-create.json"; then
	echo "Reserved subdomain was not created: ${SUBDOMAIN}" >&2
	exit 1
fi
RESERVED_SUBDOMAIN_ID="$(extract_json_field id <"${LOG_DIR}/reserved-create.json")"

access_policy_json="$(printf '%s' "${ADMIN_TOKEN}" | \
	"${BIN_DIR}/porthook" access create \
	--control-plane "http://127.0.0.1:${CONTROL_PORT}" \
	--admin-token-stdin \
	--reserved-subdomain-id "${RESERVED_SUBDOMAIN_ID}" \
	--mode "basic_auth" \
	--basic-username "${BASIC_USERNAME}" \
	--basic-password "${BASIC_PASSWORD}" \
	--json \
	2>"${LOG_DIR}/access-create.err")"
printf '%s' "${access_policy_json}" >"${LOG_DIR}/access-create.json"
ACCESS_POLICY_ID="$(printf '%s' "${access_policy_json}" | extract_json_field id)"
if grep -q "${BASIC_PASSWORD}" "${LOG_DIR}/access-create.json"; then
	echo "Access policy create output exposed the configured password" >&2
	exit 1
fi

printf '%s' "${ADMIN_TOKEN}" | \
	"${BIN_DIR}/porthook" access list \
	--control-plane "http://127.0.0.1:${CONTROL_PORT}" \
	--admin-token-stdin \
	--json \
	>"${LOG_DIR}/access-list.json" 2>"${LOG_DIR}/access-list.err"
if ! grep -q "${ACCESS_POLICY_ID}" "${LOG_DIR}/access-list.json"; then
	echo "Created access policy was not listed: ${ACCESS_POLICY_ID}" >&2
	exit 1
fi

printf '%s' "${ADMIN_TOKEN}" | \
	"${BIN_DIR}/porthook" reserved list \
	--control-plane "http://127.0.0.1:${CONTROL_PORT}" \
	--admin-token-stdin \
	--json \
	>"${LOG_DIR}/reserved-list.json" 2>"${LOG_DIR}/reserved-list.err"
if ! grep -q "${AGENT_TOKEN_ID}" "${LOG_DIR}/reserved-list.json"; then
	echo "Reserved subdomain owner token was not listed: ${AGENT_TOKEN_ID}" >&2
	exit 1
fi

start_gateway

printf '%s' "${AGENT_TOKEN}" | \
PORTHOOK_CONFIG_PATH="${CONFIG_FILE}" \
PORTHOOK_SERVER_URL="" \
PORTHOOK_TOKEN="" \
	"${BIN_DIR}/porthook" login \
	--server "http://127.0.0.1:${AGENT_PORT}" \
	--token-stdin \
	>"${LOG_DIR}/login.log" 2>"${LOG_DIR}/login.err"

if [[ ! -f "${CONFIG_FILE}" ]]; then
	echo "Login did not create config file: ${CONFIG_FILE}" >&2
	exit 1
fi

PORTHOOK_CONFIG_PATH="${CONFIG_FILE}" \
PORTHOOK_SERVER_URL="" \
PORTHOOK_TOKEN="" \
	"${BIN_DIR}/porthook" http "${LOCAL_PORT}" \
	--subdomain "${SUBDOMAIN}" \
	>"${LOG_DIR}/agent.log" 2>"${LOG_DIR}/agent.err" &
pids+=("$!")
wait_for_log "${LOG_DIR}/agent.log" "Tunnel established" "agent registration"

tunnels_json="$(curl -fsS -H "Authorization: Bearer ${MANAGEMENT_TOKEN}" "http://127.0.0.1:${MANAGEMENT_PORT}/api/v1/tunnels")"
printf '%s' "${tunnels_json}" | python3 -c '
import json
import sys

want_subdomain = sys.argv[1]
payload = json.load(sys.stdin)
tunnels = payload.get("tunnels", [])
if not any(tunnel.get("subdomain") == want_subdomain for tunnel in tunnels):
    raise SystemExit(f"active tunnel {want_subdomain!r} was not listed: {payload}")
' "${SUBDOMAIN}"

unauthorized_status="$(curl -sS \
	-o "${LOG_DIR}/protected-unauthorized.body" \
	-D "${LOG_DIR}/protected-unauthorized.headers" \
	-w "%{http_code}" \
	-H "Host: ${SUBDOMAIN}.localhost" \
	"http://127.0.0.1:${PUBLIC_PORT}/smoke.txt")"
if [[ "${unauthorized_status}" != "401" ]]; then
	echo "Unexpected protected tunnel status without credentials: ${unauthorized_status}" >&2
	exit 1
fi
if ! grep -qi 'WWW-Authenticate: Basic realm="Porthook tunnel"' "${LOG_DIR}/protected-unauthorized.headers"; then
	echo "Protected tunnel did not return a Basic auth challenge" >&2
	exit 1
fi

response="$(curl -fsS \
	-u "${BASIC_USERNAME}:${BASIC_PASSWORD}" \
	-H "Host: ${SUBDOMAIN}.localhost" \
	"http://127.0.0.1:${PUBLIC_PORT}/smoke.txt")"
if [[ "${response}" != "${MARKER}" ]]; then
	echo "Unexpected smoke response: ${response}" >&2
	exit 1
fi

wait_for_log "${LOG_DIR}/agent.log" "GET /smoke.txt -> 200" "agent request log"

query_response="$(curl -fsS \
	-u "${BASIC_USERNAME}:${BASIC_PASSWORD}" \
	-H "Host: ${SUBDOMAIN}.localhost" \
	"http://127.0.0.1:${PUBLIC_PORT}/smoke.txt?smoke_query_marker=present")"
if [[ "${query_response}" != "${MARKER}" ]]; then
	echo "Unexpected query smoke response: ${query_response}" >&2
	exit 1
fi

request_log_found=0
for _ in $(seq 1 20); do
	request_logs_json="$(curl -fsS -H "Authorization: Bearer ${MANAGEMENT_TOKEN}" "http://127.0.0.1:${MANAGEMENT_PORT}/api/v1/request-logs?limit=20")"
	if printf '%s' "${request_logs_json}" | python3 -c '
import json
import sys

want_subdomain = sys.argv[1]
raw = sys.stdin.read()
if "smoke_query_marker=present" in raw:
    print("request logs exposed the raw query string", file=sys.stderr)
    raise SystemExit(2)
payload = json.loads(raw)
logs = payload.get("request_logs", [])
if any(
    entry.get("subdomain") == want_subdomain
    and entry.get("path") == "/smoke.txt"
    and entry.get("query_present") is True
    and entry.get("status") == 200
    and entry.get("outcome") == "completed"
    for entry in logs
):
    raise SystemExit(0)
raise SystemExit(1)
' "${SUBDOMAIN}"; then
		request_log_found=1
		break
	else
		status=$?
		if [[ "${status}" -ne 1 ]]; then
			exit "${status}"
		fi
	fi
	sleep 0.1
done
if [[ "${request_log_found}" -ne 1 ]]; then
	echo "missing completed query request log for ${SUBDOMAIN@Q}: ${request_logs_json}" >&2
	exit 1
fi

printf '%s' "${ADMIN_TOKEN}" | \
	"${BIN_DIR}/porthook" history events \
	--control-plane "http://127.0.0.1:${CONTROL_PORT}" \
	--admin-token-stdin \
	--event "control_plane.token_created" \
	--limit 20 \
	--json \
	>"${LOG_DIR}/history-events.json" 2>"${LOG_DIR}/history-events.err"
python3 - "${LOG_DIR}/history-events.json" <<'PY'
import json
import pathlib
import sys

payload = json.loads(pathlib.Path(sys.argv[1]).read_text())
if not any(event.get("event") == "control_plane.token_created" for event in payload.get("events", [])):
    raise SystemExit(f"missing token_created audit event: {payload}")
PY

printf '%s' "${ADMIN_TOKEN}" | \
	"${BIN_DIR}/porthook" history requests \
	--control-plane "http://127.0.0.1:${CONTROL_PORT}" \
	--admin-token-stdin \
	--subdomain "${SUBDOMAIN}" \
	--path "/smoke.txt" \
	--status 200 \
	--limit 20 \
	--json \
	>"${LOG_DIR}/history-requests.json" 2>"${LOG_DIR}/history-requests.err"
python3 - "${LOG_DIR}/history-requests.json" "${SUBDOMAIN}" <<'PY'
import json
import pathlib
import sys

payload = json.loads(pathlib.Path(sys.argv[1]).read_text())
want_subdomain = sys.argv[2]
if not any(
    log.get("subdomain") == want_subdomain
    and log.get("path") == "/smoke.txt"
    and log.get("status") == 200
    for log in payload.get("request_logs", [])
):
    raise SystemExit(f"missing history request log for {want_subdomain!r}: {payload}")
PY

curl -fsS \
	-H "Host: ${SUBDOMAIN}.localhost" \
	-u "${BASIC_USERNAME}:${BASIC_PASSWORD}" \
	-H "Content-Type: application/octet-stream" \
	--data-binary "@${UPLOAD_FILE}" \
	"http://127.0.0.1:${PUBLIC_PORT}/echo" \
	-o "${UPLOAD_RESPONSE_FILE}"

if ! cmp -s "${UPLOAD_FILE}" "${UPLOAD_RESPONSE_FILE}"; then
	echo "Unexpected upload echo response" >&2
	exit 1
fi

wait_for_log "${LOG_DIR}/agent.log" "POST /echo -> 200" "agent upload request log"

if [[ "${DURABLE_RESTART}" == "1" ]]; then
	stop_process "${gateway_pid}" "gateway"
	start_gateway

	printf '%s' "${ADMIN_TOKEN}" | \
		"${BIN_DIR}/porthook" history requests \
		--control-plane "http://127.0.0.1:${CONTROL_PORT}" \
		--admin-token-stdin \
		--subdomain "${SUBDOMAIN}" \
		--path "/smoke.txt" \
		--status 200 \
		--limit 20 \
		--json \
		>"${LOG_DIR}/history-requests-after-gateway-restart.json" 2>"${LOG_DIR}/history-requests-after-gateway-restart.err"
	python3 - "${LOG_DIR}/history-requests-after-gateway-restart.json" "${SUBDOMAIN}" <<'PY'
import json
import pathlib
import sys

payload = json.loads(pathlib.Path(sys.argv[1]).read_text())
want_subdomain = sys.argv[2]
if not any(log.get("subdomain") == want_subdomain and log.get("path") == "/smoke.txt" for log in payload.get("request_logs", [])):
    raise SystemExit(f"request logs did not persist after gateway restart: {payload}")
PY

	stop_process "${control_pid}" "control plane"
	start_control_plane

	printf '%s' "${ADMIN_TOKEN}" | \
		"${BIN_DIR}/porthook" history events \
		--control-plane "http://127.0.0.1:${CONTROL_PORT}" \
		--admin-token-stdin \
		--event "control_plane.token_created" \
		--limit 20 \
		--json \
		>"${LOG_DIR}/history-events-after-control-restart.json" 2>"${LOG_DIR}/history-events-after-control-restart.err"
	python3 - "${LOG_DIR}/history-events-after-control-restart.json" <<'PY'
import json
import pathlib
import sys

payload = json.loads(pathlib.Path(sys.argv[1]).read_text())
if not any(event.get("event") == "control_plane.token_created" for event in payload.get("events", [])):
    raise SystemExit(f"audit events did not persist after control-plane restart: {payload}")
PY

	printf '%s' "${ADMIN_TOKEN}" | \
		"${BIN_DIR}/porthook" admin tokens list \
		--control-plane "http://127.0.0.1:${CONTROL_PORT}" \
		--admin-token-stdin \
		--json \
		>"${LOG_DIR}/admin-tokens-list-after-control-restart.json" 2>"${LOG_DIR}/admin-tokens-list-after-control-restart.err"
	if ! grep -q "${SCOPED_ADMIN_TOKEN_ID}" "${LOG_DIR}/admin-tokens-list-after-control-restart.json"; then
		echo "Admin tokens did not persist after control-plane restart: ${SCOPED_ADMIN_TOKEN_ID}" >&2
		exit 1
	fi
fi

printf '%s' "${ADMIN_TOKEN}" | \
	"${BIN_DIR}/porthook" tokens revoke \
	--control-plane "http://127.0.0.1:${CONTROL_PORT}" \
	--admin-token-stdin \
	"${AGENT_TOKEN_ID}" \
	>"${LOG_DIR}/tokens-revoke.log" 2>"${LOG_DIR}/tokens-revoke.err"

printf '%s' "${ADMIN_TOKEN}" | \
	"${BIN_DIR}/porthook" admin tokens revoke \
	--control-plane "http://127.0.0.1:${CONTROL_PORT}" \
	--admin-token-stdin \
	"${SCOPED_ADMIN_TOKEN_ID}" \
	>"${LOG_DIR}/admin-tokens-revoke.log" 2>"${LOG_DIR}/admin-tokens-revoke.err"

if printf '%s' "${SCOPED_ADMIN_TOKEN}" | \
	"${BIN_DIR}/porthook" history events \
	--control-plane "http://127.0.0.1:${CONTROL_PORT}" \
	--admin-token-stdin \
	--limit 1 \
	--json \
	>"${LOG_DIR}/history-events-revoked-admin-token.json" 2>"${LOG_DIR}/history-events-revoked-admin-token.err"; then
	echo "Revoked admin token unexpectedly read audit events" >&2
	exit 1
fi
if ! grep -q "401 Unauthorized" "${LOG_DIR}/history-events-revoked-admin-token.err"; then
	echo "Revoked admin token failure did not report unauthorized" >&2
	exit 1
fi

echo "Control-plane smoke test passed: http://${SUBDOMAIN}.localhost:${PUBLIC_PORT}/smoke.txt"
