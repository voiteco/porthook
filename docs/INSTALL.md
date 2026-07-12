# Install From Releases

Use release binaries for self-hosted deployments and local agents when you do not want to build from source. The gateway and control plane are also published as container images; see [Docker Compose Deployment](../deploy/compose/README.md).

## Quick Install (CLI Agent)

`scripts/install.sh` (Linux/macOS) and `scripts/install.ps1` (Windows) download the CLI agent, verify it against the release's `SHA256SUMS` manifest, and refuse to install anything that fails verification:

```sh
curl -fsSL https://raw.githubusercontent.com/voiteco/porthook/main/scripts/install.sh | bash
```

```powershell
irm https://raw.githubusercontent.com/voiteco/porthook/main/scripts/install.ps1 | iex
```

Both accept a specific version (`install.sh --version v0.17.0`, or `PORTHOOK_INSTALL_VERSION`) and install directory (`--dir`, or `PORTHOOK_INSTALL_DIR`); run with `--help` for the full list. When the [GitHub CLI](https://cli.github.com/) is installed and authenticated, they also verify the release's build provenance attestation and warn (without blocking) if that check fails. The rest of this document explains the manual download/verify/install steps the scripts automate, and how to install the gateway and control-plane binaries the same way.

## Download

Pick the release version and the target operating system/architecture:

```sh
VERSION=v0.17.0
OS=linux
ARCH=amd64
```

Download the CLI agent, gateway, control plane, and checksum manifest from the GitHub Release:

```sh
curl -LO "https://github.com/voiteco/porthook/releases/download/${VERSION}/porthook_${OS}_${ARCH}"
curl -LO "https://github.com/voiteco/porthook/releases/download/${VERSION}/porthook-gateway_${OS}_${ARCH}"
curl -LO "https://github.com/voiteco/porthook/releases/download/${VERSION}/porthook-control-plane_${OS}_${ARCH}"
curl -LO "https://github.com/voiteco/porthook/releases/download/${VERSION}/SHA256SUMS"
```

Supported release targets are:

| OS | Architecture | Binaries |
| --- | --- | --- |
| `linux` | `amd64`, `arm64` | `porthook`, `porthook-gateway`, `porthook-control-plane` |
| `darwin` | `amd64`, `arm64` | `porthook`, `porthook-gateway`, `porthook-control-plane` |
| `windows` | `amd64`, `arm64` | `porthook` only (`.exe`); the gateway and control plane are Linux-container-first, see [Docker Compose Deployment](../deploy/compose/README.md) |

Every release also includes `LICENSE-agent-Apache-2.0.txt` (covers `porthook`) and `LICENSE-server-AGPL-3.0.txt` (covers `porthook-gateway` and `porthook-control-plane`).

## Verify Checksums

Verify the downloaded files before installing them:

```sh
grep -E " (porthook|porthook-gateway|porthook-control-plane)_${OS}_${ARCH}$" SHA256SUMS > SHA256SUMS.selected

if command -v sha256sum >/dev/null 2>&1; then
  sha256sum -c SHA256SUMS.selected
else
  shasum -a 256 -c SHA256SUMS.selected
fi
```

The verification should print `OK` for each downloaded binary. See [Verifying and Pinning Releases](./UPGRADING.md#verifying-and-pinning-releases) to also verify each binary's build provenance attestation and SBOM.

## Install Binaries

Make the binaries executable:

```sh
chmod +x "porthook_${OS}_${ARCH}" \
  "porthook-gateway_${OS}_${ARCH}" \
  "porthook-control-plane_${OS}_${ARCH}"
```

Install them on the host path used by your deployment:

```sh
install -m 0755 "porthook_${OS}_${ARCH}" /usr/local/bin/porthook
install -m 0755 "porthook-gateway_${OS}_${ARCH}" /usr/local/bin/porthook-gateway
install -m 0755 "porthook-control-plane_${OS}_${ARCH}" /usr/local/bin/porthook-control-plane
```

On macOS without `install`, copy the files and then run `chmod +x` on the destination.

## Verify Versions and Configuration

Check the embedded version:

```sh
porthook version
porthook-gateway version
porthook-control-plane version
```

Check gateway and control-plane configuration before starting long-running services:

```sh
porthook-gateway configcheck
porthook-control-plane configcheck
```

For production-shaped deployments, use:

```sh
porthook-gateway configcheck --production
porthook-control-plane configcheck --production
```

`configcheck --production` expects durable/control-plane-backed settings and generated secrets. It should be run with the same environment variables used to start each service.

## Build From Source

From a source checkout:

```sh
make build VERSION=dev
make release-build VERSION=v0.17.0
make release-checksums
make release-verify VERSION=v0.17.0
```
