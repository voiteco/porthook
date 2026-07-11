# Release Process

Porthook releases are created from Git tags that start with `v`.

The release workflow is intentionally conservative for the pre-1.0 stage: it runs the same Go checks as CI, runs the local tunnel smoke test, builds release binaries, verifies embedded versions, generates checksums, validates Docker Compose configuration, checks production hardening assumptions, builds Docker images, and publishes a GitHub Release with notes extracted from `CHANGELOG.md`.

## Version Format

Use semantic version tags:

```sh
v0.15.0
```

The tag name is injected into the CLI, gateway, and control-plane binaries. After download, users can verify the embedded version:

```sh
porthook version
porthook-gateway version
porthook-control-plane version
```

## Local Release Check

Before pushing a tag, run:

```sh
make fmt-check
make test
make race
make vulncheck
make vet
make smoke-local VERSION=v0.15.0
make smoke-control-plane VERSION=v0.15.0
make release-build VERSION=v0.15.0
make release-checksums
make release-verify VERSION=v0.15.0
make compose-config
make production-hardening-check
make docker-build VERSION=v0.15.0
```

`make release-verify` checks that the expected binary assets are present, checks `SHA256SUMS`, verifies the embedded version in the local OS/architecture binaries, and runs production `configcheck` for the gateway and control plane.

## Create a Release

Create and push an annotated tag:

```sh
git tag -a v0.15.0 -m "v0.15.0"
git push origin v0.15.0
```

Before tagging, move the relevant `CHANGELOG.md` entries from `Unreleased` into a dated version section such as:

```md
## [0.12.0] - 2026-07-02
```

GitHub Actions will create a release and upload:

- `porthook_linux_amd64`
- `porthook_linux_arm64`
- `porthook_darwin_amd64`
- `porthook_darwin_arm64`
- `porthook-gateway_linux_amd64`
- `porthook-gateway_linux_arm64`
- `porthook-gateway_darwin_amd64`
- `porthook-gateway_darwin_arm64`
- `porthook-control-plane_linux_amd64`
- `porthook-control-plane_linux_arm64`
- `porthook-control-plane_darwin_amd64`
- `porthook-control-plane_darwin_arm64`
- `SHA256SUMS`

## Install a Binary

See [INSTALL.md](./INSTALL.md) for release downloads, checksum verification, install paths, version checks, and `configcheck` commands.

For a full local tunnel verification after building from source, see [MVP_SMOKE_TEST.md](./MVP_SMOKE_TEST.md).
