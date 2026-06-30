GO ?= go
BIN_DIR ?= bin
DIST_DIR ?= dist
VERSION ?= dev
LDFLAGS ?= -s -w -X main.version=$(VERSION)
RELEASE_TARGETS ?= linux/amd64 linux/arm64 darwin/amd64 darwin/arm64

.PHONY: build clean compose-config docker-build docker-build-control-plane docker-build-gateway fmt fmt-check release-build release-checksums smoke-control-plane smoke-local test vet

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
	docker compose -f deploy/compose/docker-compose.yml config
	docker compose --env-file deploy/compose/.env.control-plane.example -f deploy/compose/docker-compose.control-plane.yml config

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

smoke-local:
	VERSION=$(VERSION) ./scripts/smoke-local.sh

smoke-control-plane:
	VERSION=$(VERSION) ./scripts/smoke-control-plane.sh

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...
