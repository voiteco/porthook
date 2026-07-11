GO ?= go
BIN_DIR ?= bin
DIST_DIR ?= dist
VERSION ?= dev
LDFLAGS ?= -s -w -X main.version=$(VERSION)
RELEASE_TARGETS ?= linux/amd64 linux/arm64 darwin/amd64 darwin/arm64
COMPOSE ?= docker compose
CONTROL_COMPOSE_FILE ?= deploy/compose/docker-compose.control-plane.yml
CONTROL_COMPOSE_ENV ?= deploy/compose/.env.control-plane
PRODUCTION_COMPOSE_FILE ?= deploy/compose/docker-compose.production.yml
PRODUCTION_COMPOSE_ENV ?= deploy/compose/.env.production
BACKUP_DIR ?= backups
BACKUP_FILE ?= $(BACKUP_DIR)/porthook_$(shell date -u +%Y%m%dT%H%M%SZ).sql

.PHONY: build clean compose-backup compose-config compose-down compose-logs compose-ps compose-up compose-up-detached configcheck configcheck-production docker-build docker-build-control-plane docker-build-gateway fmt fmt-check production-hardening-check race release-build release-checksums release-verify smoke-control-plane smoke-durable smoke-local test vet vulncheck

build:
	mkdir -p $(BIN_DIR)
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/porthook ./agent/cmd/porthook
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/porthook-gateway ./server/gateway/cmd/porthook-gateway
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/porthook-control-plane ./server/control-plane/cmd/porthook-control-plane

clean:
	rm -rf $(BIN_DIR) $(DIST_DIR)

docker-build-gateway:
	docker build --build-arg VERSION=$(VERSION) -f server/gateway/Dockerfile -t porthook-gateway:dev .

docker-build-control-plane:
	docker build --build-arg VERSION=$(VERSION) -f server/control-plane/Dockerfile -t porthook-control-plane:dev .

docker-build: docker-build-gateway docker-build-control-plane

compose-config:
	$(COMPOSE) -f deploy/compose/docker-compose.yml config
	$(COMPOSE) --env-file deploy/compose/.env.control-plane.example -f deploy/compose/docker-compose.control-plane.yml config
	$(COMPOSE) --env-file deploy/compose/.env.production.example -f deploy/compose/docker-compose.production.yml config

compose-up:
	$(COMPOSE) --env-file $(CONTROL_COMPOSE_ENV) -f $(CONTROL_COMPOSE_FILE) up --build

compose-up-detached:
	$(COMPOSE) --env-file $(CONTROL_COMPOSE_ENV) -f $(CONTROL_COMPOSE_FILE) up --build -d

compose-down:
	$(COMPOSE) --env-file $(CONTROL_COMPOSE_ENV) -f $(CONTROL_COMPOSE_FILE) down

compose-ps:
	$(COMPOSE) --env-file $(CONTROL_COMPOSE_ENV) -f $(CONTROL_COMPOSE_FILE) ps

compose-logs:
	$(COMPOSE) --env-file $(CONTROL_COMPOSE_ENV) -f $(CONTROL_COMPOSE_FILE) logs -f

compose-backup:
	mkdir -p $(BACKUP_DIR)
	$(COMPOSE) --env-file $(CONTROL_COMPOSE_ENV) -f $(CONTROL_COMPOSE_FILE) exec -T postgres pg_dump -U porthook -d porthook > $(BACKUP_FILE)
	test -s $(BACKUP_FILE)
	@echo "Backup written to $(BACKUP_FILE)"

configcheck:
	$(GO) run ./server/gateway/cmd/porthook-gateway configcheck
	$(GO) run ./server/control-plane/cmd/porthook-control-plane configcheck

configcheck-production:
	PORTHOOK_ROOT_DOMAIN=tunnels.example.com PORTHOOK_PUBLIC_URL=https://tunnels.example.com PORTHOOK_CONTROL_PLANE_URL=http://control-plane:8082 PORTHOOK_CONTROL_PLANE_TOKEN=validator-secret PORTHOOK_REQUEST_LOG_DATABASE_URL=postgres://porthook:secret@postgres:5432/porthook?sslmode=disable $(GO) run ./server/gateway/cmd/porthook-gateway configcheck --production
	PORTHOOK_CONTROL_ADMIN_TOKEN=admin-secret PORTHOOK_CONTROL_VALIDATOR_TOKEN=validator-secret PORTHOOK_DATABASE_URL=postgres://porthook:secret@postgres:5432/porthook?sslmode=disable $(GO) run ./server/control-plane/cmd/porthook-control-plane configcheck --production

production-hardening-check:
	sh scripts/production-hardening-check.sh

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './.git/*')

fmt-check:
	test -z "$$(gofmt -l $$(find . -name '*.go' -not -path './.git/*'))"

release-build:
	mkdir -p $(DIST_DIR)
	set -eu; for target in $(RELEASE_TARGETS); do \
		os=$${target%/*}; \
		arch=$${target#*/}; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch $(GO) build -ldflags "$(LDFLAGS)" -trimpath -o $(DIST_DIR)/porthook_$${os}_$${arch} ./agent/cmd/porthook; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch $(GO) build -ldflags "$(LDFLAGS)" -trimpath -o $(DIST_DIR)/porthook-gateway_$${os}_$${arch} ./server/gateway/cmd/porthook-gateway; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch $(GO) build -ldflags "$(LDFLAGS)" -trimpath -o $(DIST_DIR)/porthook-control-plane_$${os}_$${arch} ./server/control-plane/cmd/porthook-control-plane; \
	done

release-checksums:
	cd $(DIST_DIR) && set -eu; \
	files="$$(printf '%s\n' porthook-control-plane_* porthook-gateway_* porthook_* | sort)"; \
	if [ "$$(uname -s)" = "Darwin" ]; then \
		shasum -a 256 $$files > SHA256SUMS; \
	elif command -v sha256sum >/dev/null 2>&1; then \
		sha256sum $$files > SHA256SUMS; \
	else \
		shasum -a 256 $$files > SHA256SUMS; \
	fi

release-verify:
	VERSION=$(VERSION) DIST_DIR=$(DIST_DIR) ./scripts/release-verify.sh

smoke-local:
	VERSION=$(VERSION) ./scripts/smoke-local.sh

smoke-control-plane:
	VERSION=$(VERSION) ./scripts/smoke-control-plane.sh

smoke-durable:
	VERSION=$(VERSION) ./scripts/smoke-durable.sh

test:
	$(GO) test ./...

race:
	$(GO) test -race ./...

vulncheck:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@v1.5.0 ./...

vet:
	$(GO) vet ./...
