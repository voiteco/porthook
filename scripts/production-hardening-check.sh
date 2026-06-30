#!/usr/bin/env sh
set -eu

root_dir="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
cd "$root_dir"

default_config="$(mktemp)"
allowlist_config="$(mktemp)"
trap 'rm -f "$default_config" "$allowlist_config"' EXIT

docker compose \
  --env-file deploy/compose/.env.production.example \
  -f deploy/compose/docker-compose.production.yml \
  config --format json > "$default_config"

PORTHOOK_CADDYFILE_PATH=../reverse-proxy/caddy/Caddyfile.control-allowlist \
  docker compose \
    --env-file deploy/compose/.env.production.example \
    -f deploy/compose/docker-compose.production.yml \
    config --format json > "$allowlist_config"

python3 - "$default_config" "$allowlist_config" <<'PY'
import json
import sys


def fail(message):
    print(f"production-hardening-check: {message}", file=sys.stderr)
    sys.exit(1)


def load(path):
    with open(path, "r", encoding="utf-8") as fh:
        return json.load(fh)


def require_service(config, name):
    try:
        return config["services"][name]
    except KeyError:
        fail(f"missing service {name!r}")


def require_no_host_ports(service, name):
    if service.get("ports"):
        fail(f"{name} must not publish host ports in production compose")


def require_exposes(service, name, expected):
    got = {str(port) for port in service.get("expose", [])}
    missing = set(expected) - got
    if missing:
        fail(f"{name} missing expose ports: {sorted(missing)}")


def require_hardened_service(service, name):
    if service.get("read_only") is not True:
        fail(f"{name} must use read_only: true")
    if service.get("cap_drop") != ["ALL"]:
        fail(f"{name} must drop all Linux capabilities")
    if "no-new-privileges:true" not in service.get("security_opt", []):
        fail(f"{name} must set no-new-privileges")


def volume_source_for(service, target):
    for volume in service.get("volumes", []):
        if volume.get("target") == target:
            return volume.get("source"), volume.get("read_only")
    fail(f"missing volume target {target!r}")


default = load(sys.argv[1])
allowlist = load(sys.argv[2])

gateway = require_service(default, "porthook-gateway")
control = require_service(default, "porthook-control-plane")
postgres = require_service(default, "postgres")
proxy = require_service(default, "reverse-proxy")

require_no_host_ports(gateway, "porthook-gateway")
require_no_host_ports(control, "porthook-control-plane")
require_no_host_ports(postgres, "postgres")
require_exposes(gateway, "porthook-gateway", {"8080", "8081"})
require_exposes(control, "porthook-control-plane", {"8082"})
require_hardened_service(gateway, "porthook-gateway")
require_hardened_service(control, "porthook-control-plane")

ports = {(str(port.get("published")), int(port.get("target", 0))) for port in proxy.get("ports", [])}
if ports != {("80", 80), ("443", 443)}:
    fail(f"reverse-proxy must publish only 80:80 and 443:443, got {sorted(ports)}")
if proxy.get("cap_drop") != ["ALL"]:
    fail("reverse-proxy must drop all Linux capabilities")
if proxy.get("cap_add") != ["NET_BIND_SERVICE"]:
    fail("reverse-proxy must add only NET_BIND_SERVICE")
if "no-new-privileges:true" not in proxy.get("security_opt", []):
    fail("reverse-proxy must set no-new-privileges")

source, read_only = volume_source_for(proxy, "/etc/caddy/Caddyfile")
if not str(source).endswith("/deploy/reverse-proxy/caddy/Caddyfile"):
    fail(f"default Caddyfile source is unexpected: {source}")
if read_only is not True:
    fail("default Caddyfile mount must be read-only")

allowlist_proxy = require_service(allowlist, "reverse-proxy")
source, read_only = volume_source_for(allowlist_proxy, "/etc/caddy/Caddyfile")
if not str(source).endswith("/deploy/reverse-proxy/caddy/Caddyfile.control-allowlist"):
    fail(f"allowlist Caddyfile source is unexpected: {source}")
if read_only is not True:
    fail("allowlist Caddyfile mount must be read-only")
if not allowlist_proxy.get("environment", {}).get("PORTHOOK_CONTROL_ALLOWED_IPS"):
    fail("reverse-proxy must receive PORTHOOK_CONTROL_ALLOWED_IPS")

gateway_env = gateway.get("environment", {})
if gateway_env.get("PORTHOOK_CONTROL_PLANE_URL") != "http://porthook-control-plane:8082":
    fail("gateway must use the internal control-plane URL")
if gateway_env.get("PORTHOOK_PUBLIC_URL") != "https://tunnels.example.com":
    fail("production example must use HTTPS public URLs")

print("production hardening checks passed")
PY

