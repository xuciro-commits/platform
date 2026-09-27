# Built from the repository root: docker build -f capabilities/server/cmd/webhook-sink/Dockerfile .
FROM golang:1.27-alpine AS build
# Modules through the mirror, and the module and build caches kept by BuildKit across
# builds and images: a build downloads nothing it has before and compiles only what changed.
ENV GOPROXY=https://goproxy.cn,direct CGO_ENABLED=0
WORKDIR /src
COPY contract/go contract/go
COPY capabilities/server capabilities/server
WORKDIR /src/capabilities/server
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go build -trimpath -o /webhook-sink ./cmd/webhook-sink

FROM alpine:3.22
COPY --from=build /webhook-sink /usr/local/bin/webhook-sink
USER nobody
ENTRYPOINT ["webhook-sink"]
