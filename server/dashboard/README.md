# Dashboard

License: AGPL-3.0-only

The dashboard provides a self-hosted web interface for control-plane administration.

The current dashboard scope is self-hosted administration:

- Token management.
- Reserved subdomain management.
- Active gateway tunnel visibility.
- Control-plane readiness status.

The dashboard is embedded in the control-plane binary and served at:

```text
/dashboard/
```

Authentication uses the self-hosted `PORTHOOK_CONTROL_ADMIN_TOKEN`. The browser stores the admin token in session storage for the current tab and sends it to the control-plane API as a bearer token. Logout clears the session storage value.

The dashboard displays plaintext agent tokens only from the token create response. Token list responses contain summaries without plaintext token values.

Token tables include `last_used_at` metadata, updated when the gateway successfully validates an agent token through the control plane.

The active tunnels view reads `GET /api/v1/tunnels` from the configured gateway URL. The default is `http://<dashboard-host>:8080` for the local Compose stack.

Future dashboard scope:

- Request logs.
- Self-hosted instance settings.
