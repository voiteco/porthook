# Dashboard

License: AGPL-3.0-only

The dashboard provides a self-hosted web interface for control-plane administration.

The current dashboard scope is self-hosted administration:

- Token management.
- Reserved subdomain management.
- Custom domain management.
- Access policy management.
- Control-plane audit event visibility.
- Active gateway tunnel visibility.
- Active gateway tunnel detail view.
- Browser-run diagnostics for control-plane and gateway API reachability.
- Gateway runtime summary.
- Gateway metrics drilldown.
- Operational overview charts.
- Operational JSON export download.
- Gateway request logs.
- Control-plane readiness status.

The dashboard is embedded in the control-plane binary and served at:

```text
/dashboard/
```

Authentication uses the self-hosted `PORTHOOK_CONTROL_ADMIN_TOKEN`. The browser stores the admin token in session storage for the current tab and sends it to the control-plane API as a bearer token. Logout clears the session storage value.

The dashboard displays plaintext agent tokens only from the token create response. Token list responses contain summaries without plaintext token values.

Token tables include `last_used_at` metadata, updated when the gateway successfully validates an agent token through the control plane.

Access policy tables show policy modes and non-secret settings. Plaintext policy secrets are accepted only in create/update form submissions and are never returned by list responses.

Audit events are loaded from `GET /api/v1/events` with the configured admin token. The dashboard renders recent event metadata and flattened audit fields, while the control plane omits plaintext tokens and access policy secrets from event payloads.

Diagnostics run from the browser and check control-plane status/readiness, audit event API access, the configured gateway tunnel API, and the configured gateway request log API.

The gateway runtime view reads `GET /api/v1/runtime` from the configured gateway URL and renders safe uptime, stream, request-log, counter, limit, and timeout metadata. It does not expose local target URLs, tokens, or control-plane URLs.

The metrics drilldown reads Prometheus text from `GET /metrics` on the configured gateway URL and renders metric names, types, values, and help text.

The operational export downloads a best-effort JSON snapshot from the browser. It includes safe control-plane summaries, audit events, active gateway tunnels and tunnel details, gateway runtime, parsed and raw metrics, request logs, current filters, and diagnostics already run in the tab. It does not include plaintext agent tokens, policy secrets, or local target URLs.

Custom domain management maps a fully qualified hostname to a reserved subdomain. DNS and TLS for that hostname are handled outside the dashboard.

The operational overview, active tunnels, tunnel detail, gateway runtime, and request logs views read `GET /api/v1/tunnels`, `GET /api/v1/tunnels/{id}`, `GET /api/v1/runtime`, and `GET /api/v1/request-logs` from the configured gateway URL. The default is `http://<dashboard-host>:8080` for the local Compose stack. Request log filters are sent to the gateway with `subdomain`, `method`, `host`, `path`, `status`, `outcome`, `request_id`, `tunnel_id`, `since`, `until`, and `limit`. Request log entries include path, `query_present`, and `request_id`, but not raw query strings. Tunnel detail omits local target URLs.

Future dashboard scope:

- Self-hosted instance settings.
