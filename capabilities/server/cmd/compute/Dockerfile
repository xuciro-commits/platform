FROM golang:1.27-alpine AS build
ENV GOPROXY=https://goproxy.cn,direct CGO_ENABLED=0
WORKDIR /src
COPY contract/go contract/go
COPY capabilities/server capabilities/server
WORKDIR /src/capabilities/server
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go build -trimpath -o /wasm-worker ./cmd/wasm-worker && \
    go build -trimpath -o /code-builder ./cmd/code-builder

FROM alpine:3.22 AS worker
RUN mkdir -p /run/platform-compute && chown 65534:65534 /run/platform-compute && chmod 0770 /run/platform-compute
COPY --from=build /wasm-worker /usr/local/bin/wasm-worker
USER 65534:65534
ENTRYPOINT ["wasm-worker"]

FROM alpine:3.22 AS builder
RUN apk add --no-cache docker-cli && mkdir -p /run/platform-compute && chown 65534:65534 /run/platform-compute && chmod 0770 /run/platform-compute
COPY --from=build /code-builder /usr/local/bin/code-builder
# Only this fixed-command driver may create isolated offline compiler guests.
# Business hosts and the Wasm execution worker receive no daemon socket.
USER 0:65534
ENTRYPOINT ["code-builder"]
