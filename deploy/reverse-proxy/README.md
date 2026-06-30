# Reverse Proxy Examples

These examples show the intended self-hosted edge layout for internet-facing Porthook deployments.

The gateway has two listeners:

- public HTTP listener for tunneled application traffic
- agent WebSocket listener for `porthook` agent connections

The control plane serves the API and dashboard. Keep the control-plane listener private or behind an additional access boundary.

See [../../docs/DOMAINS_TLS.md](../../docs/DOMAINS_TLS.md) for the full domain, wildcard DNS, and TLS guide.

## Hostnames

Use separate names for each surface:

| Surface | Example | Upstream |
| --- | --- | --- |
| Public tunnels | `*.tunnels.example.com` | `porthook-gateway:8080` |
| Agent WebSocket | `agent.example.com` | `porthook-gateway:8081` |
| Control plane and dashboard | `control.example.com` | `porthook-control-plane:8082` |

Set Porthook accordingly:

```sh
PORTHOOK_ROOT_DOMAIN=tunnels.example.com
PORTHOOK_PUBLIC_URL=https://tunnels.example.com
```

Agents should connect to the agent hostname:

```sh
porthook login --server https://agent.example.com --token-stdin
```

## Files

- [caddy/Caddyfile](./caddy/Caddyfile) contains a Caddy reverse-proxy example using a mounted wildcard certificate.
- [traefik/traefik.yml](./traefik/traefik.yml) contains a Traefik static configuration example.
- [traefik/dynamic.yml](./traefik/dynamic.yml) contains Traefik routers and services for Porthook.

Wildcard certificates normally require DNS-01 validation or a certificate issued outside the reverse proxy. The examples assume the proxy can read a wildcard certificate and private key mounted into the container.

Do not expose the control-plane/dashboard hostname without a separate access boundary such as a private network, VPN, reverse-proxy authentication, or an allowlist.
