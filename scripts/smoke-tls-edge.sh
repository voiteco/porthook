#!/usr/bin/env bash
set -euo pipefail

# Proves that the checked-in Caddy deployment pattern completes real HTTPS
# and secure WebSocket round trips, with certificate coverage and routing
# verified separately for the wildcard tunnel, dedicated agent, protected
# control-plane, and a custom-domain hostname, and runs the complete
# custom-domain create/verify/activate/route/access-policy/delete lifecycle
# through that real edge. See docs/PRODUCTION_ROADMAP.md Block 6.
#
# All certificates and keys used here are generated fresh into a temporary
# directory and destroyed on exit; nothing is ever committed.

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN_DIR="${ROOT_DIR}/bin"

VERSION="${VERSION:-smoke-tls-edge}"
LOCAL_PORT="${PORTHOOK_SMOKE_LOCAL_PORT:-13300}"
PUBLIC_PORT="${PORTHOOK_SMOKE_PUBLIC_PORT:-18380}"
AGENT_PORT="${PORTHOOK_SMOKE_AGENT_PORT:-18381}"
CONTROL_PORT="${PORTHOOK_SMOKE_CONTROL_PORT:-18384}"
MANAGEMENT_PORT="${PORTHOOK_SMOKE_MANAGEMENT_PORT:-18386}"
DNS_PORT="${PORTHOOK_SMOKE_DNS_PORT:-15353}"
CADDY_PORT="${PORTHOOK_SMOKE_CADDY_PORT:-18443}"
ADMIN_TOKEN="${PORTHOOK_SMOKE_ADMIN_TOKEN:-smoke-admin-token}"
VALIDATOR_TOKEN="${PORTHOOK_SMOKE_VALIDATOR_TOKEN:-smoke-validator-token}"
MANAGEMENT_TOKEN="${PORTHOOK_SMOKE_MANAGEMENT_TOKEN:-smoke-management-token}"
DATABASE_URL="${PORTHOOK_SMOKE_DATABASE_URL:-}"
KEEP_LOGS="${PORTHOOK_SMOKE_KEEP_LOGS:-0}"
SKIP_BUILD="${PORTHOOK_SMOKE_SKIP_BUILD:-0}"

ROOT_DOMAIN="porthook-tls-edge.test"
WILDCARD_SUBDOMAIN="tls-demo"
WILDCARD_HOST="${WILDCARD_SUBDOMAIN}.${ROOT_DOMAIN}"
AGENT_HOST="agent.${ROOT_DOMAIN}"
CONTROL_HOST="control.${ROOT_DOMAIN}"
# Deliberately NOT a subdomain of ROOT_DOMAIN: the gateway treats any host
# ending in ROOT_DOMAIN as wildcard tunnel routing before it ever considers
# the custom-domain lookup, exactly like preview.customer.com must be a
# genuinely separate domain from tunnels.example.com in production.
CUSTOM_HOST="custom-edge.porthook-tls-edge-custom.test"

# All local processes in this smoke test share the loopback address, so
# trusting only the exact peer that fronts the gateway and control plane
# (Caddy) mirrors the production Compose network, which trusts the internal
# service subnet and never exposes those ports outside it.
TRUSTED_PROXIES="127.0.0.1/32"

TMP_DIR="$(mktemp -d)"
LOG_DIR="${TMP_DIR}/logs"
CERT_DIR="${TMP_DIR}/certs"
CONFIG_FILE="${TMP_DIR}/agent-config.json"
CADDYFILE="${TMP_DIR}/Caddyfile"
MARKER="porthook wsecho smoke ok"

mkdir -p "${LOG_DIR}" "${CERT_DIR}"

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
		echo "TLS edge smoke test failed. Logs:" >&2
		for log in "${LOG_DIR}"/*; do
			if [[ -f "${log}" ]]; then
				echo "==> ${log}" >&2
				sed -n '1,200p' "${log}" >&2
			fi
		done
	fi

	if [[ "${KEEP_LOGS}" == "1" ]]; then
		echo "Smoke logs and certs kept at ${TMP_DIR}" >&2
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

gen_cert() {
	name="$1"
	san="$2"
	openssl req -x509 -newkey rsa:2048 -nodes \
		-keyout "${CERT_DIR}/${name}-key.pem" \
		-out "${CERT_DIR}/${name}-cert.pem" \
		-days 1 \
		-subj "/CN=porthook-tls-edge-smoke-${name}" \
		-addext "subjectAltName=DNS:${san}" \
		>"${LOG_DIR}/openssl-${name}.log" 2>&1
}

# curl_edge issues a request through the real Caddy TLS listener, resolving
# host to loopback without touching DNS or /etc/hosts, and verifying the
# certificate presented for that hostname.
curl_edge() {
	host="$1"
	cert="$2"
	path="$3"
	shift 3
	curl -sS \
		--resolve "${host}:${CADDY_PORT}:127.0.0.1" \
		--cacert "${cert}" \
		"$@" \
		"https://${host}:${CADDY_PORT}${path}"
}

require_command curl
require_command openssl
require_command go
require_command caddy
require_command python3

if [[ "${SKIP_BUILD}" != "1" ]]; then
	(
		cd "${ROOT_DIR}"
		make build VERSION="${VERSION}"
	)
fi

gen_cert wildcard "*.${ROOT_DOMAIN}"
gen_cert agent "${AGENT_HOST}"
gen_cert control "${CONTROL_HOST}"
gen_cert custom "${CUSTOM_HOST}"
WILDCARD_CERT="${CERT_DIR}/wildcard-cert.pem"
AGENT_CERT="${CERT_DIR}/agent-cert.pem"
CONTROL_CERT="${CERT_DIR}/control-cert.pem"
CUSTOM_CERT="${CERT_DIR}/custom-cert.pem"

# Built directly (not `go run`) so the backgrounded PID this script tracks
# and kills on cleanup is the actual server process, not a wrapper that may
# leave it running as an orphan.
go build -o "${TMP_DIR}/wsecho" "${ROOT_DIR}/scripts/testdata/wsecho"
go build -o "${TMP_DIR}/dnsstub" "${ROOT_DIR}/scripts/testdata/dnsstub"

WSECHO_ADDR="127.0.0.1:${LOCAL_PORT}" \
	"${TMP_DIR}/wsecho" \
	>"${LOG_DIR}/wsecho.log" 2>&1 &
pids+=("$!")
wait_for_log "${LOG_DIR}/wsecho.log" "listening on" "local HTTP/WebSocket fixture"

PORTHOOK_CONTROL_ADDR="127.0.0.1:${CONTROL_PORT}" \
PORTHOOK_CONTROL_ADMIN_TOKEN="${ADMIN_TOKEN}" \
PORTHOOK_CONTROL_VALIDATOR_TOKEN="${VALIDATOR_TOKEN}" \
PORTHOOK_TRUSTED_PROXIES="${TRUSTED_PROXIES}" \
PORTHOOK_GATEWAY_MANAGEMENT_URL="http://127.0.0.1:${MANAGEMENT_PORT}" \
PORTHOOK_GATEWAY_MANAGEMENT_TOKEN="${MANAGEMENT_TOKEN}" \
PORTHOOK_DNS_RESOLVER_ADDR="127.0.0.1:${DNS_PORT}" \
PORTHOOK_DATABASE_URL="${DATABASE_URL}" \
	"${BIN_DIR}/porthook-control-plane" >>"${LOG_DIR}/control-plane.log" 2>&1 &
pids+=("$!")
wait_for_url "http://127.0.0.1:${CONTROL_PORT}/readyz" "control plane"

PORTHOOK_ADDR="127.0.0.1:${PUBLIC_PORT}" \
PORTHOOK_AGENT_ADDR="127.0.0.1:${AGENT_PORT}" \
PORTHOOK_MANAGEMENT_ADDR="127.0.0.1:${MANAGEMENT_PORT}" \
PORTHOOK_MANAGEMENT_TOKEN="${MANAGEMENT_TOKEN}" \
PORTHOOK_TRUSTED_PROXIES="${TRUSTED_PROXIES}" \
PORTHOOK_ROOT_DOMAIN="${ROOT_DOMAIN}" \
PORTHOOK_PUBLIC_URL="https://${ROOT_DOMAIN}:${CADDY_PORT}" \
PORTHOOK_STATIC_TOKEN="unused-static-token" \
PORTHOOK_CONTROL_PLANE_URL="http://127.0.0.1:${CONTROL_PORT}" \
PORTHOOK_CONTROL_PLANE_TOKEN="${VALIDATOR_TOKEN}" \
PORTHOOK_CUSTOM_DOMAIN_CACHE_TTL="50ms" \
PORTHOOK_CUSTOM_DOMAIN_MISS_TTL="50ms" \
	"${BIN_DIR}/porthook-gateway" >>"${LOG_DIR}/gateway.log" 2>&1 &
pids+=("$!")
wait_for_url "http://127.0.0.1:${MANAGEMENT_PORT}/healthz" "gateway"

cat >"${CADDYFILE}" <<EOF
https://*.${ROOT_DOMAIN}:${CADDY_PORT} {
	tls ${WILDCARD_CERT} ${WILDCARD_CERT%-cert.pem}-key.pem
	reverse_proxy 127.0.0.1:${PUBLIC_PORT}
}

https://${AGENT_HOST}:${CADDY_PORT} {
	tls ${AGENT_CERT} ${AGENT_CERT%-cert.pem}-key.pem
	reverse_proxy 127.0.0.1:${AGENT_PORT}
}

https://${CONTROL_HOST}:${CADDY_PORT} {
	tls ${CONTROL_CERT} ${CONTROL_CERT%-cert.pem}-key.pem
	reverse_proxy 127.0.0.1:${CONTROL_PORT}
}

https://${CUSTOM_HOST}:${CADDY_PORT} {
	tls ${CUSTOM_CERT} ${CUSTOM_CERT%-cert.pem}-key.pem
	reverse_proxy 127.0.0.1:${PUBLIC_PORT}
}
EOF

caddy run --config "${CADDYFILE}" --adapter caddyfile \
	>"${LOG_DIR}/caddy.log" 2>&1 &
pids+=("$!")

caddy_ready=0
for _ in $(seq 1 100); do
	if curl_edge "${CONTROL_HOST}" "${CONTROL_CERT}" "/readyz" -fsS -o /dev/null 2>/dev/null; then
		caddy_ready=1
		break
	fi
	sleep 0.1
done
if [[ "${caddy_ready}" -ne 1 ]]; then
	echo "Timed out waiting for caddy to front the control plane over TLS" >&2
	exit 1
fi

created_token_json="$(printf '%s' "${ADMIN_TOKEN}" | \
	"${BIN_DIR}/porthook" tokens create \
	--control-plane "http://127.0.0.1:${CONTROL_PORT}" \
	--admin-token-stdin \
	--name "smoke tls edge agent" \
	--scope "register_tunnel" \
	--json)"
AGENT_TOKEN_ID="$(printf '%s' "${created_token_json}" | extract_json_field id)"
AGENT_TOKEN="$(printf '%s' "${created_token_json}" | extract_json_field token)"

reserved_json="$(printf '%s' "${ADMIN_TOKEN}" | \
	"${BIN_DIR}/porthook" reserved create \
	--control-plane "http://127.0.0.1:${CONTROL_PORT}" \
	--admin-token-stdin \
	--name "${WILDCARD_SUBDOMAIN}" \
	--token-id "${AGENT_TOKEN_ID}" \
	--json)"
RESERVED_SUBDOMAIN_ID="$(printf '%s' "${reserved_json}" | extract_json_field id)"

# Log in and register the tunnel through the real Caddy TLS listener on the
# dedicated agent hostname. PORTHOOK_CONNECT_ADDR dials loopback directly
# (avoiding any dependency on DNS for this fake test domain) while the HTTP
# Host header and TLS SNI still use the real agent hostname, exactly as an
# operator's real agent would when PORTHOOK_ROOT_DOMAIN is a real domain.
# PORTHOOK_CA_FILE trusts the ephemeral certificate generated above. This is
# the real production porthook binary completing a real secure WebSocket
# round trip end to end, satisfying the Block 6 exit gate.
printf '%s' "${AGENT_TOKEN}" | \
PORTHOOK_CONFIG_PATH="${CONFIG_FILE}" \
PORTHOOK_SERVER_URL="" \
PORTHOOK_TOKEN="" \
	"${BIN_DIR}/porthook" login \
	--server "https://${AGENT_HOST}:${CADDY_PORT}" \
	--token-stdin \
	>"${LOG_DIR}/login.log" 2>"${LOG_DIR}/login.err"

PORTHOOK_CONFIG_PATH="${CONFIG_FILE}" \
PORTHOOK_SERVER_URL="" \
PORTHOOK_TOKEN="" \
PORTHOOK_CA_FILE="${AGENT_CERT}" \
PORTHOOK_CONNECT_ADDR="127.0.0.1:${CADDY_PORT}" \
	"${BIN_DIR}/porthook" http "${LOCAL_PORT}" \
	--subdomain "${WILDCARD_SUBDOMAIN}" \
	>"${LOG_DIR}/agent.log" 2>"${LOG_DIR}/agent.err" &
pids+=("$!")
wait_for_log "${LOG_DIR}/agent.log" "Tunnel established" "agent registration"

# --- Wildcard tunnel hostname: real HTTPS round trip ---

wildcard_headers="${LOG_DIR}/wildcard.headers"
wildcard_status="$(curl_edge "${WILDCARD_HOST}" "${WILDCARD_CERT}" "/smoke.txt" \
	-o "${LOG_DIR}/wildcard.body" -D "${wildcard_headers}" -w '%{http_code}')"
if [[ "${wildcard_status}" != "200" ]]; then
	echo "Wildcard tunnel HTTPS request = ${wildcard_status}, want 200" >&2
	exit 1
fi
if [[ "$(cat "${LOG_DIR}/wildcard.body")" != "${MARKER}" ]]; then
	echo "Unexpected wildcard body: $(cat "${LOG_DIR}/wildcard.body")" >&2
	exit 1
fi
if ! grep -qi "^x-echo-forwarded-host: ${WILDCARD_HOST}" "${wildcard_headers}"; then
	echo "Local target did not observe X-Forwarded-Host: ${WILDCARD_HOST}" >&2
	exit 1
fi
if ! grep -qi "^x-echo-forwarded-proto: https" "${wildcard_headers}"; then
	echo "Local target did not observe X-Forwarded-Proto: https" >&2
	exit 1
fi
if ! grep -qi "^x-echo-forwarded-for: 127.0.0.1" "${wildcard_headers}"; then
	echo "Local target did not observe X-Forwarded-For: 127.0.0.1 (the Caddy-observed peer)" >&2
	exit 1
fi

# --- Wildcard tunnel hostname: real secure WebSocket round trip ---

go run "${ROOT_DIR}/scripts/testdata/wsclient" \
	"wss://127.0.0.1:${CADDY_PORT}/socket" \
	"${WILDCARD_HOST}" \
	"${WILDCARD_CERT}" \
	>"${LOG_DIR}/wsclient.log" 2>&1
if ! grep -q "websocket smoke check passed" "${LOG_DIR}/wsclient.log"; then
	echo "WebSocket client did not report success:" >&2
	cat "${LOG_DIR}/wsclient.log" >&2
	exit 1
fi

# --- Original host, request ID, and client address in the gateway's own log ---

wildcard_request_log="$(curl -fsS -H "Authorization: Bearer ${MANAGEMENT_TOKEN}" \
	"http://127.0.0.1:${MANAGEMENT_PORT}/api/v1/request-logs?limit=20")"
python3 -c '
import json
import sys

want_host = sys.argv[1]
payload = json.load(sys.stdin)
for entry in payload.get("request_logs", []):
    host = entry.get("host", "").split(":")[0]
    if host == want_host and entry.get("path") == "/smoke.txt" and entry.get("status") == 200:
        remote_ip = entry.get("remote_ip")
        custom_domain = entry.get("custom_domain")
        assert remote_ip == "127.0.0.1", f"remote_ip = {remote_ip!r}, want 127.0.0.1"
        assert entry.get("request_id"), "request_id is empty"
        assert not custom_domain, f"custom_domain = {custom_domain!r}, want empty for wildcard traffic"
        sys.exit(0)
sys.exit("no matching wildcard request log entry found")
' "${WILDCARD_HOST}" <<<"${wildcard_request_log}"

# --- Custom domain: create, DNS-verify, activate, and route through TLS ---

domain_json="$(printf '%s' "${ADMIN_TOKEN}" | \
	"${BIN_DIR}/porthook" domains create \
	--control-plane "http://127.0.0.1:${CONTROL_PORT}" \
	--admin-token-stdin \
	--hostname "${CUSTOM_HOST}" \
	--reserved-subdomain-id "${RESERVED_SUBDOMAIN_ID}" \
	--json)"
DOMAIN_ID="$(printf '%s' "${domain_json}" | extract_json_field id)"
VERIFICATION_NAME="$(printf '%s' "${domain_json}" | extract_json_field verification_name)"
VERIFICATION_TOKEN="$(printf '%s' "${domain_json}" | extract_json_field verification_token)"

DNSSTUB_ADDR="127.0.0.1:${DNS_PORT}" \
DNSSTUB_NAME="${VERIFICATION_NAME}" \
DNSSTUB_VALUE="porthook-domain-verification=${VERIFICATION_TOKEN}" \
	"${TMP_DIR}/dnsstub" \
	>"${LOG_DIR}/dnsstub.log" 2>&1 &
pids+=("$!")
wait_for_log "${LOG_DIR}/dnsstub.log" "listening on" "DNS TXT verification stub"

domain_status=""
for _ in $(seq 1 50); do
	verify_json="$(printf '%s' "${ADMIN_TOKEN}" | \
		"${BIN_DIR}/porthook" domains verify \
		--control-plane "http://127.0.0.1:${CONTROL_PORT}" \
		--admin-token-stdin \
		--json \
		"${DOMAIN_ID}")"
	domain_status="$(printf '%s' "${verify_json}" | extract_json_field status)"
	if [[ "${domain_status}" == "active" ]]; then
		break
	fi
	sleep 0.1
done
if [[ "${domain_status}" != "active" ]]; then
	echo "Custom domain status = ${domain_status@Q}, want active" >&2
	exit 1
fi

custom_headers="${LOG_DIR}/custom.headers"
custom_status="$(curl_edge "${CUSTOM_HOST}" "${CUSTOM_CERT}" "/smoke.txt" \
	-o "${LOG_DIR}/custom.body" -D "${custom_headers}" -w '%{http_code}')"
if [[ "${custom_status}" != "200" ]]; then
	echo "Custom domain HTTPS request = ${custom_status}, want 200" >&2
	exit 1
fi
if [[ "$(cat "${LOG_DIR}/custom.body")" != "${MARKER}" ]]; then
	echo "Unexpected custom domain body: $(cat "${LOG_DIR}/custom.body")" >&2
	exit 1
fi
if ! grep -qi "^x-echo-forwarded-host: ${CUSTOM_HOST}" "${custom_headers}"; then
	echo "Local target did not observe X-Forwarded-Host: ${CUSTOM_HOST}" >&2
	exit 1
fi

custom_request_log="$(curl -fsS -H "Authorization: Bearer ${MANAGEMENT_TOKEN}" \
	"http://127.0.0.1:${MANAGEMENT_PORT}/api/v1/request-logs?limit=20")"
python3 -c '
import json
import sys

want_host = sys.argv[1]
payload = json.load(sys.stdin)
for entry in payload.get("request_logs", []):
    host = entry.get("host", "").split(":")[0]
    if host == want_host and entry.get("path") == "/smoke.txt" and entry.get("status") == 200:
        custom_domain = entry.get("custom_domain")
        assert custom_domain == want_host, f"custom_domain = {custom_domain!r}, want {want_host!r}"
        sys.exit(0)
sys.exit("no matching custom-domain request log entry found")
' "${CUSTOM_HOST}" <<<"${custom_request_log}"

# --- Access policy applies to both the wildcard and custom-domain hostnames ---

access_policy_json="$(printf '%s' "${ADMIN_TOKEN}" | \
	"${BIN_DIR}/porthook" access create \
	--control-plane "http://127.0.0.1:${CONTROL_PORT}" \
	--admin-token-stdin \
	--reserved-subdomain-id "${RESERVED_SUBDOMAIN_ID}" \
	--mode "ip_allowlist" \
	--ip-allowlist "203.0.113.0/24" \
	--json)"
ACCESS_POLICY_ID="$(printf '%s' "${access_policy_json}" | extract_json_field id)"

denied_wildcard_status="$(curl_edge "${WILDCARD_HOST}" "${WILDCARD_CERT}" "/smoke.txt" -o /dev/null -w '%{http_code}')"
if [[ "${denied_wildcard_status}" != "403" ]]; then
	echo "Wildcard request while access policy denies loopback = ${denied_wildcard_status}, want 403" >&2
	exit 1
fi
denied_custom_status="$(curl_edge "${CUSTOM_HOST}" "${CUSTOM_CERT}" "/smoke.txt" -o /dev/null -w '%{http_code}')"
if [[ "${denied_custom_status}" != "403" ]]; then
	echo "Custom domain request while access policy denies loopback = ${denied_custom_status}, want 403" >&2
	exit 1
fi

printf '%s' "${ADMIN_TOKEN}" | \
	"${BIN_DIR}/porthook" access update \
	--control-plane "http://127.0.0.1:${CONTROL_PORT}" \
	--admin-token-stdin \
	--mode "public" \
	"${ACCESS_POLICY_ID}" \
	>"${LOG_DIR}/access-update.json" 2>"${LOG_DIR}/access-update.err"

allowed_wildcard_status="$(curl_edge "${WILDCARD_HOST}" "${WILDCARD_CERT}" "/smoke.txt" -o /dev/null -w '%{http_code}')"
if [[ "${allowed_wildcard_status}" != "200" ]]; then
	echo "Wildcard request after loosening access policy = ${allowed_wildcard_status}, want 200" >&2
	exit 1
fi

# --- Deleting the custom-domain mapping stops routing that hostname, but not the tunnel itself ---

printf '%s' "${ADMIN_TOKEN}" | \
	"${BIN_DIR}/porthook" domains delete \
	--control-plane "http://127.0.0.1:${CONTROL_PORT}" \
	--admin-token-stdin \
	"${DOMAIN_ID}" \
	>"${LOG_DIR}/domain-delete.log" 2>"${LOG_DIR}/domain-delete.err"

# The gateway briefly caches active custom-domain lookups (tuned above to
# 50ms so this test doesn't wait out the 30s production default); retry
# until that cache entry expires and the deletion becomes visible.
deleted_custom_status=""
for _ in $(seq 1 50); do
	deleted_custom_status="$(curl_edge "${CUSTOM_HOST}" "${CUSTOM_CERT}" "/smoke.txt" -o /dev/null -w '%{http_code}')"
	if [[ "${deleted_custom_status}" == "404" ]]; then
		break
	fi
	sleep 0.05
done
if [[ "${deleted_custom_status}" != "404" ]]; then
	echo "Custom domain request after deleting the mapping = ${deleted_custom_status}, want 404" >&2
	exit 1
fi

still_routed_wildcard_status="$(curl_edge "${WILDCARD_HOST}" "${WILDCARD_CERT}" "/smoke.txt" -o /dev/null -w '%{http_code}')"
if [[ "${still_routed_wildcard_status}" != "200" ]]; then
	echo "Wildcard request after deleting the unrelated custom-domain mapping = ${still_routed_wildcard_status}, want 200" >&2
	exit 1
fi

echo "TLS edge smoke test passed: wildcard, agent, control-plane, and custom-domain hostnames all served real HTTPS and secure WebSocket traffic through https://*.${ROOT_DOMAIN}:${CADDY_PORT}"
