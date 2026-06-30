# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM golang:1.24-bookworm AS build

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
      -o /out/porthook-control-plane \
      ./server/control-plane/cmd/porthook-control-plane

FROM scratch

COPY --from=build /out/porthook-control-plane /porthook-control-plane

USER 65532:65532

EXPOSE 8082

ENTRYPOINT ["/porthook-control-plane"]
