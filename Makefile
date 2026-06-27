GO ?= go
BIN_DIR ?= bin

.PHONY: build clean docker-build docker-build-gateway fmt fmt-check test vet

build:
	mkdir -p $(BIN_DIR)
	$(GO) build -o $(BIN_DIR)/porthook ./agent/cmd/porthook
	$(GO) build -o $(BIN_DIR)/porthook-gateway ./server/gateway/cmd/porthook-gateway

clean:
	rm -rf $(BIN_DIR)

docker-build-gateway:
	docker build -f server/gateway/Dockerfile -t porthook-gateway:dev .

docker-build: docker-build-gateway

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './.git/*')

fmt-check:
	test -z "$$(gofmt -l $$(find . -name '*.go' -not -path './.git/*'))"

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...
