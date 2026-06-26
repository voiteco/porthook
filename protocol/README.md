# Protocol

License: Apache-2.0

This directory is for shared protocol definitions, transport abstractions, and future SDK packages.

The first protocol version is expected to use WebSocket over TLS with JSON control messages and stream-oriented request forwarding.

Protocol goals:

- Simple enough to debug manually.
- Stable enough for third-party clients.
- Independent from private hosted-service features.
- Safe for concurrent HTTP request streams.

See [../docs/SPEC.md](../docs/SPEC.md) for the current protocol draft.
