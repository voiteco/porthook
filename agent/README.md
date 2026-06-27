# Agent

License: Apache-2.0

The agent is the local CLI users run on their machines.

Current command shape:

```sh
porthook http 3000 --server http://localhost:8081 --token dev-token --subdomain demo
```

Responsibilities:

- Authenticate with a Porthook gateway.
- Register local tunnels.
- Maintain a persistent outbound tunnel connection.
- Forward gateway requests to local services.
- Print public URLs and request logs.
- Reconnect with backoff after transient gateway disconnects.

The initial implementation is written in Go and uses a WebSocket connection to the gateway.

## Runtime Configuration

CLI flags take precedence for the command they configure. Environment variables provide defaults for local development and scripted runs.

| Environment variable | Default | Description |
| --- | --- | --- |
| `PORTHOOK_SERVER_URL` | `http://localhost:8081` | Gateway agent listener URL. |
| `PORTHOOK_TOKEN` | `dev-token` | Static agent authentication token. |
| `PORTHOOK_HANDSHAKE_TIMEOUT` | `10s` | WebSocket dial, authentication, and tunnel registration timeout. |
| `PORTHOOK_REQUEST_TIMEOUT` | `30s` | Local service request timeout. |
| `PORTHOOK_MAX_RESPONSE_BODY_BYTES` | `1048576` | Maximum local response body sent through the tunnel. |
| `PORTHOOK_WS_WRITE_TIMEOUT` | `10s` | WebSocket message write timeout. |
| `PORTHOOK_WS_PING_INTERVAL` | `15s` | WebSocket keepalive ping interval. |
| `PORTHOOK_WS_PONG_TIMEOUT` | `5s` | WebSocket keepalive pong timeout. |
| `PORTHOOK_RECONNECT_INITIAL_DELAY` | `500ms` | First retry delay after a transient connection failure. |
| `PORTHOOK_RECONNECT_MAX_DELAY` | `5s` | Maximum reconnect backoff delay. |
| `PORTHOOK_RECONNECT_JITTER` | `250ms` | Maximum random jitter added to reconnect delays. |

Duration values use Go duration syntax such as `500ms`, `10s`, or `1m`.

Authentication and tunnel registration errors stop the agent immediately. Transient dial and WebSocket disconnects are retried until the process is stopped.
