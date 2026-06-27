# Contributing to Porthook

Porthook is design-stage, pre-alpha software. The most useful contributions right now are focused design review, threat modeling, documentation fixes, protocol feedback, and implementation work that follows the public specs.

Before making a large change, open an issue or draft PR so the design can be discussed early.

## Repository Boundary

This repository is for the public self-hosted product:

- local agent
- shared protocol packages
- self-hosted gateway, control plane, and dashboard
- deployment examples
- public product and technical documentation

Private hosted-cloud operations, commercial plans, pricing, internal runbooks, and private customer material should not be added here.

See [docs/REPOSITORY_BOUNDARY.md](./docs/REPOSITORY_BOUNDARY.md) for the full boundary.

## Licensing

Porthook uses a split-license model. By contributing, you agree that your contribution is licensed under the license for the component you change:

| Path | License |
| --- | --- |
| `server/` | AGPL-3.0-only |
| `agent/` | Apache-2.0 |
| `protocol/` | Apache-2.0 |
| `deploy/` | Apache-2.0 unless noted otherwise |
| `docs/` | Apache-2.0 unless noted otherwise |

If a change spans multiple components, each file follows the license for its path. New source files should include an `SPDX-License-Identifier` header matching the component license.

See [LICENSE.md](./LICENSE.md) and the full texts under [LICENSES/](./LICENSES/).

## Issues

Use issues for reproducible bugs, concrete feature requests, documentation gaps, and design questions.

Before opening an issue:

- Search for an existing issue first.
- Keep one problem or proposal per issue.
- Include the affected component when possible.
- Do not report security vulnerabilities in public issues. Follow [SECURITY.md](./SECURITY.md).

## Pull Requests

Keep PRs small and focused. A good PR should:

- Explain the problem and the chosen approach.
- Link to the relevant issue or spec section when available.
- Update documentation when behavior, protocol, deployment, or public interfaces change.
- Add or update tests once executable code exists.
- Call out security, compatibility, migration, and licensing implications.
- Avoid committing secrets, credentials, local environment files, or unrelated generated artifacts.

Changes to tunnel protocol behavior, authentication, routing, deployment defaults, or public APIs should update the relevant spec before or alongside implementation.

## Development Expectations

The implementation is expected to stay aligned with:

- [docs/SPEC.md](./docs/SPEC.md)
- [docs/TECHNICAL_SPEC.md](./docs/TECHNICAL_SPEC.md)
- [docs/REPOSITORY_BOUNDARY.md](./docs/REPOSITORY_BOUNDARY.md)

When code is added, prefer simple, testable components and clear boundaries between `agent/`, `protocol/`, and `server/`.
