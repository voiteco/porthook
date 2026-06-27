# Gateway

License: AGPL-3.0-only

The gateway is the public edge service.

Responsibilities:

- Accept public HTTP and HTTPS traffic.
- Route hostnames to active tunnels.
- Maintain agent tunnel connections.
- Forward requests and responses.
- Enforce limits.
- Expose health and metrics endpoints.
