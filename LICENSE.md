# Licensing

Porthook uses a split-license model. The licensing boundary is part of the product architecture and should remain easy to understand from the repository layout.

## Component Licenses

| Path | License | Notes |
| --- | --- | --- |
| `server/` | AGPL-3.0-only | Gateway, control plane, dashboard, and server-side product logic. |
| `agent/` | Apache-2.0 | Local CLI agent distributed to end users. |
| `protocol/` | Apache-2.0 | Shared protocol definitions and SDK packages. |
| `deploy/` | Apache-2.0 unless noted otherwise | Self-hosting examples and operational templates. |
| `docs/` | Apache-2.0 unless noted otherwise | Product and technical documentation. |

## Intent

The server-side code is AGPL-3.0-only so improvements to network-accessible server deployments remain available to the community.

The agent and protocol packages are Apache-2.0 so developers, companies, and integrations can adopt them with minimal friction.

## Source File Headers

New source files should include an SPDX header matching their component license.

## Full License Texts

Canonical full license texts are included in this repository:

- [GNU Affero General Public License v3.0](./LICENSES/AGPL-3.0.txt)
- [Apache License 2.0](./LICENSES/Apache-2.0.txt)
