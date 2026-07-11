#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN_DIR="${ROOT_DIR}/bin"

VERSION="${VERSION:-smoke}"
LOCAL_PORT="${PORTHOOK_SMOKE_LOCAL_PORT:-13000}"
PUBLIC_PORT="${PORTHOOK_SMOKE_PUBLIC_PORT:-18080}"
AGENT_PORT="${PORTHOOK_SMOKE_AGENT_PORT:-18081}"
MANAGEMENT_PORT="${PORTHOOK_SMOKE_MANAGEMENT_PORT:-18085}"
SUBDOMAIN="${PORTHOOK_SMOKE_SUBDOMAIN:-demo}"
TOKEN="${PORTHOOK_SMOKE_TOKEN:-smoke-token}"
KEEP_LOGS="${PORTHOOK_SMOKE_KEEP_LOGS:-0}"
SKIP_BUILD="${PORTHOOK_SMOKE_SKIP_BUILD:-0}"

TMP_DIR="$(mktemp -d)"
FIXTURE_DIR="${TMP_DIR}/fixture"
LOG_DIR="${TMP_DIR}/logs"
UPLOAD_FILE="${TMP_DIR}/upload.bin"
UPLOAD_RESPONSE_FILE="${TMP_DIR}/upload-response.bin"
MARKER="porthook smoke ok"

mkdir -p "${FIXTURE_DIR}" "${LOG_DIR}"
printf '%s\n' "${MARKER}" > "${FIXTURE_DIR}/smoke.txt"
python3 - "${UPLOAD_FILE}" <<'PY'
import pathlib
import sys

pathlib.Path(sys.argv[1]).write_bytes((b"porthook-stream-" * 5000) + b"ok")
PY

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
		echo "Smoke test failed. Logs:" >&2
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
require_command cmp
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

port = int(sys.argv[1])
fixture_dir = pathlib.Path(sys.argv[2])

class Handler(http.server.BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def do_GET(self):
        if self.path != "/smoke.txt":
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

PORTHOOK_ADDR="127.0.0.1:${PUBLIC_PORT}" \
PORTHOOK_AGENT_ADDR="127.0.0.1:${AGENT_PORT}" \
PORTHOOK_MANAGEMENT_ADDR="127.0.0.1:${MANAGEMENT_PORT}" \
PORTHOOK_ROOT_DOMAIN="localhost" \
PORTHOOK_PUBLIC_URL="http://localhost:${PUBLIC_PORT}" \
PORTHOOK_STATIC_TOKEN="${TOKEN}" \
	"${BIN_DIR}/porthook-gateway" >"${LOG_DIR}/gateway.log" 2>&1 &
pids+=("$!")
wait_for_url "http://127.0.0.1:${MANAGEMENT_PORT}/healthz" "gateway"

"${BIN_DIR}/porthook" http "${LOCAL_PORT}" \
	--server "http://127.0.0.1:${AGENT_PORT}" \
	--token "${TOKEN}" \
	--subdomain "${SUBDOMAIN}" \
	>"${LOG_DIR}/agent.log" 2>"${LOG_DIR}/agent.err" &
pids+=("$!")
wait_for_log "${LOG_DIR}/agent.log" "Tunnel established" "agent registration"

response="$(curl -fsS -H "Host: ${SUBDOMAIN}.localhost" "http://127.0.0.1:${PUBLIC_PORT}/smoke.txt")"
if [[ "${response}" != "${MARKER}" ]]; then
	echo "Unexpected smoke response: ${response}" >&2
	exit 1
fi

wait_for_log "${LOG_DIR}/agent.log" "GET /smoke.txt -> 200" "agent request log"

curl -fsS \
	-H "Host: ${SUBDOMAIN}.localhost" \
	-H "Content-Type: application/octet-stream" \
	--data-binary "@${UPLOAD_FILE}" \
	"http://127.0.0.1:${PUBLIC_PORT}/echo" \
	-o "${UPLOAD_RESPONSE_FILE}"

if ! cmp -s "${UPLOAD_FILE}" "${UPLOAD_RESPONSE_FILE}"; then
	echo "Unexpected upload echo response" >&2
	exit 1
fi

wait_for_log "${LOG_DIR}/agent.log" "POST /echo -> 200" "agent upload request log"

echo "Smoke test passed: http://${SUBDOMAIN}.localhost:${PUBLIC_PORT}/smoke.txt"
