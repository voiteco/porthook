# Dashboard

License: AGPL-3.0-only

The dashboard provides a self-hosted web interface for control-plane administration.

The first dashboard scope is token management:

- Token management.
- Control-plane readiness status.

The dashboard is embedded in the control-plane binary and served at:

```text
/dashboard/
```

Authentication uses the self-hosted `PORTHOOK_CONTROL_ADMIN_TOKEN`. The browser stores the admin token in session storage for the current tab and sends it to the control-plane API as a bearer token. Logout clears the session storage value.

The dashboard displays plaintext agent tokens only from the token create response. Token list responses contain summaries without plaintext token values.

Future dashboard scope:

- Active tunnels.
- Request logs.
- Reserved subdomains.
- Self-hosted instance settings.
