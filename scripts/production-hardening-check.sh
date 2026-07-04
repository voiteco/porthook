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
from pathlib import Path
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


def require_healthcheck(service, name, expected_test):
    healthcheck = service.get("healthcheck")
    if not healthcheck:
        fail(f"{name} must define a container healthcheck")
    if healthcheck.get("test") != expected_test:
        fail(f"{name} healthcheck command is unexpected: {healthcheck.get('test')}")
    if healthcheck.get("disable") is True:
        fail(f"{name} healthcheck must not be disabled")


def require_depends_on_healthy(service, name, dependency):
    depends_on = service.get("depends_on", {})
    dependency_config = depends_on.get(dependency)
    if dependency_config is None:
        fail(f"{name} must depend on {dependency}")
    if dependency_config.get("condition") != "service_healthy":
        fail(f"{name} must wait for {dependency} to be healthy")


def volume_source_for(service, target):
    for volume in service.get("volumes", []):
        if volume.get("target") == target:
            return volume.get("source"), volume.get("read_only")
    fail(f"missing volume target {target!r}")


def require_caddy_control_headers(path):
    text = Path(path).read_text(encoding="utf-8")
    for header in (
        "X-Content-Type-Options nosniff",
        "X-Frame-Options DENY",
        "Referrer-Policy no-referrer",
    ):
        if header not in text:
            fail(f"{path} must set control-plane security header {header!r}")


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
require_healthcheck(gateway, "porthook-gateway", ["CMD", "/porthook-gateway", "healthcheck"])
require_healthcheck(control, "porthook-control-plane", ["CMD", "/porthook-control-plane", "healthcheck"])
require_depends_on_healthy(gateway, "porthook-gateway", "porthook-control-plane")
require_depends_on_healthy(proxy, "reverse-proxy", "porthook-gateway")
require_depends_on_healthy(proxy, "reverse-proxy", "porthook-control-plane")

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

require_caddy_control_headers("deploy/reverse-proxy/caddy/Caddyfile")
require_caddy_control_headers("deploy/reverse-proxy/caddy/Caddyfile.control-allowlist")
allowlist_text = Path("deploy/reverse-proxy/caddy/Caddyfile.control-allowlist").read_text(encoding="utf-8")
if "@control_denied not remote_ip {$PORTHOOK_CONTROL_ALLOWED_IPS}" not in allowlist_text:
    fail("allowlist Caddyfile must deny requests outside PORTHOOK_CONTROL_ALLOWED_IPS")

gateway_env = gateway.get("environment", {})
if gateway_env.get("PORTHOOK_CONTROL_PLANE_URL") != "http://porthook-control-plane:8082":
    fail("gateway must use the internal control-plane URL")
if gateway_env.get("PORTHOOK_PUBLIC_URL") != "https://tunnels.example.com":
    fail("production example must use HTTPS public URLs")

print("production hardening checks passed")
PY
