# Repository Boundary

Status: draft

This repository is intended to be the public open-source monorepo for the self-hosted Porthook product.

The goal is to keep public development fast and transparent while keeping commercial strategy, hosted-service implementation, and sensitive operational details out of the public repository.

## Public Repository

Recommended name:

```text
porthook
```

This repository should contain:

- CLI agent.
- Public tunnel protocol.
- Gateway server.
- Self-hosted control plane.
- Self-hosted dashboard.
- Docker Compose deployment.
- Public API and CLI documentation.
- Public security model.
- Contributor-facing roadmap.
- Public issue templates and contribution docs.

This repository should not contain:

- Pricing strategy.
- Hosted-service architecture.
- Billing implementation.
- Production infrastructure topology.
- Private deployment secrets.
- Internal monitoring and incident runbooks.
- Abuse detection internals.
- Launch plans.
- Sales material.
- Investor material.
- Private competitive analysis.
- Unannounced commercial roadmap items.

## Private Repositories

Recommended private repositories:

```text
porthook-cloud
porthook-internal
```

### porthook-cloud

Private hosted-service implementation.

Suggested contents:

- Managed gateway orchestration.
- Cloud account and organization logic.
- Billing integration.
- Hosted-service-specific access control.
- Usage metering.
- Production deployment manifests.
- Internal observability.
- Cloud-only dashboard modules.

### porthook-internal

Private product, business, and operations workspace.

Suggested contents:

- Pricing notes.
- Packaging decisions.
- Launch checklist.
- Internal roadmap.
- Production runbooks.
- Security response process.
- Abuse response process.
- Vendor notes.
- Business planning docs.

## Public Spec Policy

Public specs are allowed when they help users and contributors implement or operate the self-hosted product.

Good public specs:

- Tunnel protocol behavior.
- CLI behavior.
- Gateway routing behavior.
- Self-hosted deployment behavior.
- Public API behavior.
- Security baseline.

Private specs:

- Hosted-cloud internals.
- Cost model.
- Commercial packaging.
- Abuse detection rules.
- Production incident procedures.
- Private integrations.

## Development Rule

When in doubt, ask this question:

```text
Would this document help someone run, audit, or contribute to the self-hosted product?
```

If yes, it probably belongs in the public repository.

If it mostly helps operate or monetize a hosted service, it should probably stay private.
