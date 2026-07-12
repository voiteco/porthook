#!/usr/bin/env bash
set -euo pipefail

# Downloads a porthook release binary for the running Linux or macOS host,
# verifies it against that release's SHA256SUMS manifest, and installs it.
# Never installs anything that fails checksum verification.

REPO="voiteco/porthook"
BINARY="porthook"
VERSION="${PORTHOOK_INSTALL_VERSION:-latest}"
INSTALL_DIR="${PORTHOOK_INSTALL_DIR:-/usr/local/bin}"

usage() {
	cat <<'EOF'
Usage: install.sh [--version VERSION] [--dir DIR] [--binary NAME]

Downloads a porthook release binary, verifies it against that release's
SHA256SUMS manifest, and installs it. Refuses to install anything that
fails checksum verification.

  --version VERSION   Release to install, e.g. v0.17.0 (default: latest)
  --dir DIR            Install directory (default: /usr/local/bin, or
                        $PORTHOOK_INSTALL_DIR)
  --binary NAME         porthook (default), porthook-gateway, or
                        porthook-control-plane

Also configurable via PORTHOOK_INSTALL_VERSION and PORTHOOK_INSTALL_DIR.
EOF
}

while [ "$#" -gt 0 ]; do
	case "$1" in
	--version)
		VERSION="$2"
		shift 2
		;;
	--dir)
		INSTALL_DIR="$2"
		shift 2
		;;
	--binary)
		BINARY="$2"
		shift 2
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		echo "install.sh: unknown argument: $1" >&2
		usage >&2
		exit 2
		;;
	esac
done

case "${BINARY}" in
porthook | porthook-gateway | porthook-control-plane) ;;
*)
	echo "install.sh: unsupported --binary ${BINARY}" >&2
	exit 2
	;;
esac

require_command() {
	if ! command -v "$1" >/dev/null 2>&1; then
		echo "install.sh: required command not found: $1" >&2
		exit 1
	fi
}

require_command curl

case "$(uname -s)" in
Linux) os="linux" ;;
Darwin) os="darwin" ;;
*)
	echo "install.sh: unsupported OS: $(uname -s)" >&2
	exit 1
	;;
esac

case "$(uname -m)" in
x86_64 | amd64) arch="amd64" ;;
arm64 | aarch64) arch="arm64" ;;
*)
	echo "install.sh: unsupported architecture: $(uname -m)" >&2
	exit 1
	;;
esac

if [ "${VERSION}" = "latest" ]; then
	# grep must not use -m1/-q here: an early-exiting reader closes the pipe
	# while curl is still writing, curl reports that as a failure, and
	# pipefail then trips set -e even though the value was captured fine.
	VERSION="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
		| grep '"tag_name"' \
		| sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')"
	if [ -z "${VERSION}" ]; then
		echo "install.sh: could not resolve the latest release version" >&2
		exit 1
	fi
fi

asset="${BINARY}_${os}_${arch}"
base_url="https://github.com/${REPO}/releases/download/${VERSION}"

tmp_dir="$(mktemp -d)"
trap 'rm -rf "${tmp_dir}"' EXIT

echo "install.sh: downloading ${asset} ${VERSION}"
curl -fsSL -o "${tmp_dir}/${asset}" "${base_url}/${asset}"
curl -fsSL -o "${tmp_dir}/SHA256SUMS" "${base_url}/SHA256SUMS"

echo "install.sh: verifying checksum"
(
	cd "${tmp_dir}"
	grep -E " ${asset}\$" SHA256SUMS >SHA256SUMS.selected || true
	if [ ! -s SHA256SUMS.selected ]; then
		echo "install.sh: no checksum entry for ${asset} in SHA256SUMS" >&2
		exit 1
	fi
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum -c SHA256SUMS.selected
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 -c SHA256SUMS.selected
	else
		echo "install.sh: sha256sum or shasum is required to verify the download" >&2
		exit 1
	fi
)

chmod +x "${tmp_dir}/${asset}"

if command -v gh >/dev/null 2>&1 && gh auth status >/dev/null 2>&1; then
	echo "install.sh: verifying build provenance attestation"
	if gh attestation verify "${tmp_dir}/${asset}" --repo "${REPO}" >/dev/null 2>&1; then
		echo "install.sh: build provenance attestation verified"
	else
		echo "install.sh: warning: could not verify a build provenance attestation for ${asset}" >&2
	fi
fi

mkdir -p "${INSTALL_DIR}" 2>/dev/null || true
destination="${INSTALL_DIR}/${BINARY}"

if [ -w "${INSTALL_DIR}" ]; then
	install -m 0755 "${tmp_dir}/${asset}" "${destination}"
elif command -v sudo >/dev/null 2>&1; then
	echo "install.sh: ${INSTALL_DIR} is not writable, retrying with sudo"
	sudo install -m 0755 "${tmp_dir}/${asset}" "${destination}"
else
	echo "install.sh: ${INSTALL_DIR} is not writable and sudo is unavailable; rerun with --dir pointing at a writable directory" >&2
	exit 1
fi

echo "install.sh: installed ${BINARY} ${VERSION} to ${destination}"
"${destination}" version
