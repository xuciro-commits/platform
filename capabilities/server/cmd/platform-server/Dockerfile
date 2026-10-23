# Built from the repository root: docker build -f capabilities/server/cmd/platform-server/Dockerfile .
FROM golang:1.27-alpine AS build
# Modules through the mirror, and the module and build caches kept by BuildKit across
# builds and images: a build downloads nothing it has before and compiles only what changed.
ENV GOPROXY=https://goproxy.cn,direct CGO_ENABLED=0
WORKDIR /src
COPY contract/go contract/go
COPY capabilities/server capabilities/server
WORKDIR /src/capabilities/server
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    GOMAXPROCS=2 go build -trimpath -o /platform-server ./cmd/platform-server

FROM alpine:3.22
RUN apk add --no-cache tzdata
ARG PLATFORM_REVISION
LABEL org.opencontainers.image.revision=$PLATFORM_REVISION
COPY --from=build /platform-server /usr/local/bin/platform-server
# The workspace, built beforehand: pnpm --dir web/apps/workspace build
COPY web/apps/workspace/dist /web
USER nobody
ENTRYPOINT ["platform-server"]
