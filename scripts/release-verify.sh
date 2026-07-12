#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="${DIST_DIR:-${ROOT_DIR}/dist}"
VERSION="${VERSION:-dev}"

expected_binaries=(
	"porthook_linux_amd64"
	"porthook_linux_arm64"
	"porthook_darwin_amd64"
	"porthook_darwin_arm64"
	"porthook_windows_amd64.exe"
	"porthook_windows_arm64.exe"
	"porthook-gateway_linux_amd64"
	"porthook-gateway_linux_arm64"
	"porthook-gateway_darwin_amd64"
	"porthook-gateway_darwin_arm64"
	"porthook-control-plane_linux_amd64"
	"porthook-control-plane_linux_arm64"
	"porthook-control-plane_darwin_amd64"
	"porthook-control-plane_darwin_arm64"
)

# Non-executable assets shipped alongside the binaries: checked for presence
# and included in the checksum manifest, but not for the executable bit.
expected_files=(
	"LICENSE-agent-Apache-2.0.txt"
	"LICENSE-server-AGPL-3.0.txt"
)

fail() {
	echo "release-verify: $*" >&2
	exit 1
}

if [[ ! -d "${DIST_DIR}" ]]; then
	fail "missing dist directory: ${DIST_DIR}"
fi

tmp_expected="$(mktemp)"
tmp_actual="$(mktemp)"
tmp_checksum="$(mktemp)"
trap 'rm -f "${tmp_expected}" "${tmp_actual}" "${tmp_checksum}"' EXIT

for binary in "${expected_binaries[@]}"; do
	path="${DIST_DIR}/${binary}"
	if [[ ! -f "${path}" ]]; then
		fail "missing binary ${binary}"
	fi
	if [[ ! -x "${path}" ]]; then
		fail "binary is not executable: ${binary}"
	fi
	printf '%s\n' "${binary}" >>"${tmp_expected}"
done

for file in "${expected_files[@]}"; do
	path="${DIST_DIR}/${file}"
	if [[ ! -f "${path}" ]]; then
		fail "missing file ${file}"
	fi
	if [[ ! -s "${path}" ]]; then
		fail "file is empty: ${file}"
	fi
	printf '%s\n' "${file}" >>"${tmp_expected}"
done

find "${DIST_DIR}" -maxdepth 1 -type f -print | while IFS= read -r path; do
	basename "${path}"
done | sort >"${tmp_actual}"

{
	sort "${tmp_expected}"
	printf '%s\n' "SHA256SUMS"
} | sort >"${tmp_expected}.assets"

if ! cmp -s "${tmp_expected}.assets" "${tmp_actual}"; then
	echo "Expected release assets:" >&2
	sed 's/^/  /' "${tmp_expected}.assets" >&2
	echo "Actual release assets:" >&2
	sed 's/^/  /' "${tmp_actual}" >&2
	fail "dist contains missing or unexpected files"
fi

if [[ ! -s "${DIST_DIR}/SHA256SUMS" ]]; then
	fail "missing SHA256SUMS"
fi

awk '{print $NF}' "${DIST_DIR}/SHA256SUMS" | sort >"${tmp_checksum}"
sort "${tmp_expected}" >"${tmp_expected}.checksums"
if ! cmp -s "${tmp_expected}.checksums" "${tmp_checksum}"; then
	fail "SHA256SUMS does not list exactly the expected binaries"
fi

if command -v sha256sum >/dev/null 2>&1; then
	(cd "${DIST_DIR}" && sha256sum -c SHA256SUMS >/dev/null)
elif command -v shasum >/dev/null 2>&1; then
	(cd "${DIST_DIR}" && shasum -a 256 -c SHA256SUMS >/dev/null)
else
	fail "sha256sum or shasum is required"
fi

case "$(uname -s)" in
	Linux) host_os="linux" ;;
	Darwin) host_os="darwin" ;;
	*) fail "unsupported release verification host OS: $(uname -s)" ;;
esac

case "$(uname -m)" in
	x86_64 | amd64) host_arch="amd64" ;;
	arm64 | aarch64) host_arch="arm64" ;;
	*) fail "unsupported release verification host architecture: $(uname -m)" ;;
esac

check_version() {
	binary="$1"
	got="$("${DIST_DIR}/${binary}_${host_os}_${host_arch}" version)"
	if [[ "${got}" != "${VERSION}" ]]; then
		fail "${binary} version is ${got}, want ${VERSION}"
	fi
}

check_version "porthook"
check_version "porthook-gateway"
check_version "porthook-control-plane"

PORTHOOK_ROOT_DOMAIN="tunnels.example.com" \
PORTHOOK_PUBLIC_URL="https://tunnels.example.com" \
PORTHOOK_CONTROL_PLANE_URL="http://control-plane:8082" \
PORTHOOK_CONTROL_PLANE_TOKEN="validator-secret" \
PORTHOOK_MANAGEMENT_TOKEN="management-secret" \
PORTHOOK_TRUSTED_PROXIES="172.30.0.0/24" \
PORTHOOK_REQUEST_LOG_DATABASE_URL="postgres://porthook:secret@postgres:5432/porthook?sslmode=disable" \
	"${DIST_DIR}/porthook-gateway_${host_os}_${host_arch}" configcheck --production >/dev/null

PORTHOOK_CONTROL_ADMIN_TOKEN="admin-secret" \
PORTHOOK_CONTROL_VALIDATOR_TOKEN="validator-secret" \
PORTHOOK_GATEWAY_MANAGEMENT_URL="http://gateway:8082" \
PORTHOOK_GATEWAY_MANAGEMENT_TOKEN="management-secret" \
PORTHOOK_TRUSTED_PROXIES="172.30.0.0/24" \
PORTHOOK_DATABASE_URL="postgres://porthook:secret@postgres:5432/porthook?sslmode=disable" \
	"${DIST_DIR}/porthook-control-plane_${host_os}_${host_arch}" configcheck --production >/dev/null

echo "release verification passed for ${VERSION}"
