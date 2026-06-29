# Release Process

Porthook releases are created from Git tags that start with `v`.

The release workflow is intentionally conservative for the pre-alpha stage: it runs the same Go checks as CI, runs the local tunnel smoke test, builds release binaries, verifies embedded versions, generates checksums, validates Docker Compose configuration, builds the gateway Docker image, and publishes a GitHub prerelease.

## Version Format

Use semantic version tags:

```sh
v0.3.0
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
make vet
make smoke-local VERSION=v0.3.0
make smoke-control-plane VERSION=v0.3.0
make release-build VERSION=v0.3.0
make release-checksums
```

On Linux `amd64`, verify the release binaries directly:

```sh
./dist/porthook_linux_amd64 version
./dist/porthook-gateway_linux_amd64 version
./dist/porthook-control-plane_linux_amd64 version
```

On macOS, run the binary that matches the local architecture:

```sh
./dist/porthook_darwin_arm64 version
./dist/porthook-gateway_darwin_arm64 version
./dist/porthook-control-plane_darwin_arm64 version
```

## Create a Release

Create and push an annotated tag:

```sh
git tag -a v0.3.0 -m "v0.3.0"
git push origin v0.3.0
```

GitHub Actions will create a prerelease and upload:

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

Download the binary for the target operating system and architecture from the GitHub Release, make it executable, and check the version:

```sh
chmod +x porthook_darwin_arm64
./porthook_darwin_arm64 version
```

The gateway binary supports the same version check:

```sh
chmod +x porthook-gateway_linux_amd64
./porthook-gateway_linux_amd64 version
```

The control-plane binary does too:

```sh
chmod +x porthook-control-plane_linux_amd64
./porthook-control-plane_linux_amd64 version
```

For a full local tunnel verification after building from source, see [MVP_SMOKE_TEST.md](./MVP_SMOKE_TEST.md).
