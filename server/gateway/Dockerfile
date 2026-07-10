# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM golang:1.26.5-bookworm AS build

ARG TARGETOS=linux
ARG TARGETARCH
ARG VERSION=dev

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN target_os="${TARGETOS:-linux}"; \
    target_arch="${TARGETARCH:-$(go env GOARCH)}"; \
    CGO_ENABLED=0 GOOS="$target_os" GOARCH="$target_arch" go build \
      -trimpath \
      -ldflags="-s -w -X main.version=$VERSION" \
      -o /out/porthook-gateway \
      ./server/gateway/cmd/porthook-gateway

FROM scratch

COPY --from=build /out/porthook-gateway /porthook-gateway

USER 65532:65532

EXPOSE 8080 8081

HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=3 CMD ["/porthook-gateway", "healthcheck"]

ENTRYPOINT ["/porthook-gateway"]
