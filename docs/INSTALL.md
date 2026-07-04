# Install From Releases

Use release binaries for self-hosted deployments and local agents when you do not want to build from source.

## Download

Pick the release version and the target operating system/architecture:

```sh
VERSION=v0.12.0
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

| OS | Architecture |
| --- | --- |
| `linux` | `amd64`, `arm64` |
| `darwin` | `amd64`, `arm64` |

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

The verification should print `OK` for each downloaded binary.

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
make release-build VERSION=v0.12.0
make release-checksums
make release-verify VERSION=v0.12.0
```
