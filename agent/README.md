# Agent

License: Apache-2.0

The agent is the local CLI users run on their machines.

Current command shape:

```sh
PORTHOOK_TOKEN=dev-token
printf '%s' "$PORTHOOK_TOKEN" | porthook login --server http://localhost:8081 --token-stdin
porthook http 3000 --subdomain demo
```

Responsibilities:

- Authenticate with a Porthook gateway.
- Register local tunnels.
- Maintain a persistent outbound tunnel connection.
- Forward gateway requests to local services.
- Cancel local requests when the gateway cancels a stream.
- Print public URLs and request logs.
- Reconnect with backoff after transient gateway disconnects.
- Persist self-hosted server and token defaults with `porthook login`.

The initial implementation is written in Go and uses a WebSocket connection to the gateway.

For scripts and shell history safety, prefer `porthook login --token-stdin`. The `--token` flag remains available for local development and one-off runs. If no token flag is provided in an interactive terminal, `porthook login` prompts for the token without echoing it.

The CLI also includes self-hosted control-plane token helpers:

```sh
printf '%s' "$PORTHOOK_CONTROL_ADMIN_TOKEN" | porthook tokens create --control-plane http://localhost:8082 --admin-token-stdin --name "local agent"
printf '%s' "$PORTHOOK_CONTROL_ADMIN_TOKEN" | porthook tokens list --control-plane http://localhost:8082 --admin-token-stdin
printf '%s' "$PORTHOOK_CONTROL_ADMIN_TOKEN" | porthook tokens revoke --control-plane http://localhost:8082 --admin-token-stdin tok_...
```

## Runtime Configuration

CLI flags take precedence for the command they configure. Environment variables provide defaults for local development and scripted runs.

| Environment variable | Default | Description |
| --- | --- | --- |
| `PORTHOOK_SERVER_URL` | `http://localhost:8081` | Gateway agent listener URL. |
| `PORTHOOK_TOKEN` | `dev-token` | Static agent authentication token. |
| `PORTHOOK_CONFIG_PATH` | OS config dir | Optional path for the JSON login config file. |
| `PORTHOOK_CONTROL_PLANE_URL` | empty | Default control-plane API URL for `porthook tokens` commands. |
| `PORTHOOK_CONTROL_ADMIN_TOKEN` | empty | Default admin token for `porthook tokens` commands. Prefer `--admin-token-stdin` for shell history safety. |
| `PORTHOOK_HANDSHAKE_TIMEOUT` | `10s` | WebSocket dial, authentication, and tunnel registration timeout. |
| `PORTHOOK_REQUEST_TIMEOUT` | `30s` | Local service request timeout. |
| `PORTHOOK_MAX_RESPONSE_BODY_BYTES` | `1048576` | Maximum local response body sent through the tunnel. |
| `PORTHOOK_STREAM_CHUNK_BYTES` | `32768` | Maximum HTTP body chunk size for tunnel messages. |
| `PORTHOOK_WS_WRITE_TIMEOUT` | `10s` | WebSocket message write timeout. |
| `PORTHOOK_WS_PING_INTERVAL` | `15s` | WebSocket keepalive ping interval. |
| `PORTHOOK_WS_PONG_TIMEOUT` | `5s` | WebSocket keepalive pong timeout. |
| `PORTHOOK_RECONNECT_INITIAL_DELAY` | `500ms` | First retry delay after a transient connection failure. |
| `PORTHOOK_RECONNECT_MAX_DELAY` | `5s` | Maximum reconnect backoff delay. |
| `PORTHOOK_RECONNECT_JITTER` | `250ms` | Maximum random jitter added to reconnect delays. |

Duration values use Go duration syntax such as `500ms`, `10s`, or `1m`.

Authentication and tunnel registration errors stop the agent immediately. Transient dial and WebSocket disconnects are retried until the process is stopped.

CLI flags override environment variables. Environment variables override saved login config. Saved login config overrides local development defaults.
