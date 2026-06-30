# Domains and TLS

Porthook routes public HTTP traffic by hostname:

```text
{subdomain}.{PORTHOOK_ROOT_DOMAIN}
```

For self-hosted deployments, `PORTHOOK_ROOT_DOMAIN` is the domain suffix you own and delegate to the Porthook gateway. For example, with:

```text
PORTHOOK_ROOT_DOMAIN=tunnels.example.com
PORTHOOK_PUBLIC_URL=https://tunnels.example.com
```

a tunnel registered as `demo` is announced as:

```text
https://demo.tunnels.example.com
```

The current public self-hosted implementation supports custom root domains for wildcard tunnel routing. It does not yet support arbitrary per-tunnel custom domains such as `preview.customer.com`.

## Recommended Hostnames

Use separate hostnames for each external surface:

| Surface | Example | Purpose |
| --- | --- | --- |
| Wildcard tunnel traffic | `*.tunnels.example.com` | Public HTTP requests forwarded through active tunnels. |
| Agent WebSocket | `agent.example.com` | `porthook` agents connect to `/agent/connect`. |
| Control plane and dashboard | `control.example.com` | Admin API and `/dashboard/`; keep private or externally protected. |

The gateway public listener and agent listener are intentionally separate. Do not point agents at the wildcard tunnel hostname unless your reverse proxy explicitly routes that hostname to the agent listener.

## DNS

Create DNS records that send the wildcard tunnel domain and agent hostname to the reverse proxy or load balancer in front of the gateway:

```text
*.tunnels.example.com  A      203.0.113.10
agent.example.com      A      203.0.113.10
control.example.com    A      203.0.113.10
```

Use `AAAA` records when the edge has IPv6. A wildcard `CNAME` can also work when your DNS provider supports it:

```text
*.tunnels.example.com  CNAME  porthook-edge.example.net.
agent.example.com      CNAME  porthook-edge.example.net.
```

Operational notes:

- Lower TTLs before a cutover, for example 60-300 seconds, then raise them once the deployment is stable.
- Keep the wildcard record scoped to the tunnel root, such as `*.tunnels.example.com`, instead of `*.example.com`.
- The control-plane hostname should resolve only where operators need it if you use a private DNS zone or VPN.
- DNS only brings traffic to the edge. The reverse proxy must still route wildcard tunnel traffic to the gateway public listener and agent traffic to the gateway agent listener.

Verify records before testing tunnels:

```sh
dig +short 'demo.tunnels.example.com'
dig +short agent.example.com
dig +short control.example.com
```

## TLS Certificates

The reverse proxy must present certificates that cover the hostnames it serves:

| Hostname | Certificate coverage |
| --- | --- |
| `demo.tunnels.example.com` and other tunnel names | `*.tunnels.example.com` |
| `agent.example.com` | `agent.example.com` |
| `control.example.com` | `control.example.com` |

Wildcard certificates normally require DNS-01 validation or a certificate issued outside the reverse proxy. A certificate for `*.tunnels.example.com` does not cover `tunnels.example.com` itself, so include the root name as a separate SAN if your proxy serves redirects, health checks, or landing content on the root hostname.

The checked-in Caddy and Traefik examples assume mounted certificate files:

```text
/certs/fullchain.pem
/certs/privkey.pem
```

For the production Compose stack, set the host paths in:

```text
PORTHOOK_TLS_CERT_PATH=/etc/letsencrypt/live/tunnels.example.com/fullchain.pem
PORTHOOK_TLS_KEY_PATH=/etc/letsencrypt/live/tunnels.example.com/privkey.pem
```

Use filesystem permissions that allow only the reverse proxy process to read the private key.

## Porthook Configuration

Production gateway settings should match the public DNS and TLS shape:

```text
PORTHOOK_ROOT_DOMAIN=tunnels.example.com
PORTHOOK_PUBLIC_URL=https://tunnels.example.com
```

`PORTHOOK_PUBLIC_URL` supplies the URL scheme and optional port used when the gateway announces tunnel URLs. The gateway replaces the host with `{subdomain}.{PORTHOOK_ROOT_DOMAIN}`. If `PORTHOOK_PUBLIC_URL` includes a port, announced tunnel URLs include that port too.

Agents should connect to the dedicated agent hostname:

```sh
printf '%s' '<agent-token>' | porthook login \
  --server https://agent.example.com \
  --token-stdin
```

Then register a tunnel:

```sh
porthook http 3000 --subdomain demo
```

The agent should print:

```text
https://demo.tunnels.example.com
```

## Verification

After starting the gateway and reverse proxy, verify the public listener through a wildcard hostname:

```sh
curl -i https://check.tunnels.example.com/healthz
```

Verify that the agent hostname reaches the agent listener by running:

```sh
printf '%s' '<agent-token>' | porthook login \
  --server https://agent.example.com \
  --token-stdin
```

Then start a local service and complete an HTTP round trip:

```sh
python3 -m http.server 3000 --bind 127.0.0.1
porthook http 3000 --subdomain demo
curl -i https://demo.tunnels.example.com/
```

For control-plane-backed deployments, reserve `demo` for that token before starting the requested-subdomain tunnel.

## Common Misconfigurations

| Symptom | Likely cause |
| --- | --- |
| Agent login fails before authentication | `agent.example.com` routes to the public listener instead of the agent listener. |
| Tunnel URL points to the wrong host or scheme | `PORTHOOK_ROOT_DOMAIN` or `PORTHOOK_PUBLIC_URL` does not match the public DNS/TLS setup. |
| Browser certificate error on tunnel URLs | Certificate does not cover `*.tunnels.example.com` or the proxy is serving a default certificate. |
| Requested subdomain is rejected | Control-plane mode requires a reserved subdomain owned by the agent token. |
| `control.example.com` is publicly reachable | DNS, firewall, VPN, or reverse-proxy access rules are too broad for the control-plane surface. |

