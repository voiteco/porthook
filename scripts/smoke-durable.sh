#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="${VERSION:-smoke-durable}"
POSTGRES_IMAGE="${PORTHOOK_SMOKE_POSTGRES_IMAGE:-postgres:16-alpine}"
POSTGRES_PASSWORD="${PORTHOOK_SMOKE_POSTGRES_PASSWORD:-smoke-postgres-password}"
POSTGRES_CONTAINER="porthook-smoke-postgres-$$"

cleanup() {
	status=$?
	if docker ps -a --format '{{.Names}}' | grep -qx "${POSTGRES_CONTAINER}"; then
		if [[ "${status}" -ne 0 ]]; then
			echo "Durable smoke Postgres logs:" >&2
			docker logs "${POSTGRES_CONTAINER}" >&2 || true
		fi
		docker stop "${POSTGRES_CONTAINER}" >/dev/null 2>&1 || true
	fi
	exit "${status}"
}
trap cleanup EXIT

if ! command -v docker >/dev/null 2>&1; then
	echo "Required command not found: docker" >&2
	exit 1
fi

docker run -d --rm \
	--name "${POSTGRES_CONTAINER}" \
	-e POSTGRES_DB=porthook \
	-e POSTGRES_USER=porthook \
	-e POSTGRES_PASSWORD="${POSTGRES_PASSWORD}" \
	-p 127.0.0.1::5432 \
	"${POSTGRES_IMAGE}" >/dev/null

for _ in $(seq 1 120); do
	if docker exec "${POSTGRES_CONTAINER}" pg_isready -U porthook -d porthook >/dev/null 2>&1; then
		break
	fi
	sleep 0.5
done

if ! docker exec "${POSTGRES_CONTAINER}" pg_isready -U porthook -d porthook >/dev/null 2>&1; then
	echo "Timed out waiting for smoke Postgres" >&2
	exit 1
fi

POSTGRES_PORT="$(docker port "${POSTGRES_CONTAINER}" 5432/tcp | sed 's/.*://')"
if [[ -z "${POSTGRES_PORT}" ]]; then
	echo "Could not determine smoke Postgres host port" >&2
	exit 1
fi

DATABASE_URL="postgres://porthook:${POSTGRES_PASSWORD}@127.0.0.1:${POSTGRES_PORT}/porthook?sslmode=disable"

PORTHOOK_SMOKE_DATABASE_URL="${DATABASE_URL}" \
PORTHOOK_SMOKE_REQUEST_LOG_DATABASE_URL="${DATABASE_URL}" \
PORTHOOK_SMOKE_DURABLE_RESTART=1 \
VERSION="${VERSION}" \
	"${ROOT_DIR}/scripts/smoke-control-plane.sh"

echo "Durable smoke test passed"
