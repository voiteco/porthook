GO ?= go
BIN_DIR ?= bin
DIST_DIR ?= dist
VERSION ?= dev
LDFLAGS ?= -s -w -X main.version=$(VERSION)
RELEASE_TARGETS ?= linux/amd64 linux/arm64 darwin/amd64 darwin/arm64

.PHONY: build clean docker-build docker-build-gateway fmt fmt-check release-build release-checksums smoke-local test vet

build:
	mkdir -p $(BIN_DIR)
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/porthook ./agent/cmd/porthook
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/porthook-gateway ./server/gateway/cmd/porthook-gateway

clean:
	rm -rf $(BIN_DIR) $(DIST_DIR)

docker-build-gateway:
	docker build --build-arg VERSION=$(VERSION) -f server/gateway/Dockerfile -t porthook-gateway:dev .

docker-build: docker-build-gateway

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
	done

release-checksums:
	cd $(DIST_DIR) && set -eu; \
	files="$$(printf '%s\n' porthook-gateway_* porthook_* | sort)"; \
	if command -v sha256sum >/dev/null 2>&1; then \
		sha256sum $$files > SHA256SUMS; \
	else \
		shasum -a 256 $$files > SHA256SUMS; \
	fi

smoke-local:
	VERSION=$(VERSION) ./scripts/smoke-local.sh

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...
