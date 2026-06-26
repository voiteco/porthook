# Agent

License: Apache-2.0

The agent is the local CLI users run on their machines.

Initial command shape:

```sh
porthook http 3000
```

Responsibilities:

- Authenticate with a Porthook gateway.
- Register local tunnels.
- Maintain a persistent outbound tunnel connection.
- Forward gateway requests to local services.
- Print public URLs and request logs.
- Reconnect cleanly when possible.

The initial implementation is expected to be written in Go.

