# Licensing

Porthook uses a split-license model. The licensing boundary is part of the product architecture and should remain easy to understand from the repository layout.

## Component Licenses

| Path | License | Notes |
| --- | --- | --- |
| `server/` | AGPL-3.0 | Gateway, control plane, dashboard, and server-side product logic. |
| `agent/` | Apache-2.0 | Local CLI agent distributed to end users. |
| `protocol/` | Apache-2.0 | Shared protocol definitions and SDK packages. |
| `deploy/` | Apache-2.0 unless noted otherwise | Self-hosting examples and operational templates. |
| `docs/` | Apache-2.0 unless noted otherwise | Product and technical documentation. |

## Intent

The server-side code is AGPL-3.0 so improvements to network-accessible server deployments remain available to the community.

The agent and protocol packages are Apache-2.0 so developers, companies, and integrations can adopt them with minimal friction.

## Required Before First Public Release

Before publishing the first public release, add canonical full license texts under `LICENSES/`:

- `LICENSES/AGPL-3.0.txt`
- `LICENSES/Apache-2.0.txt`

New source files should include an SPDX header matching their component license.
