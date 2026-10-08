# Built from the repository root: docker build -f solutions/manufacturing/Dockerfile .
FROM golang:1.27-alpine AS build
# Modules through the mirror, and the module and build caches kept by BuildKit across
# builds and images: a build downloads nothing it has before and compiles only what changed.
ENV GOPROXY=https://goproxy.cn,direct CGO_ENABLED=0
WORKDIR /src
COPY contract/go contract/go
COPY capabilities/server capabilities/server
COPY apps/mes/server apps/mes/server
COPY apps/erp/server apps/erp/server
COPY protocols/production protocols/production
COPY solutions/manufacturing solutions/manufacturing
WORKDIR /src/solutions/manufacturing
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go build -trimpath -o /manufacturing-server ./cmd/manufacturing-server

FROM alpine:3.22
RUN apk add --no-cache tzdata
ARG PLATFORM_REVISION
LABEL org.opencontainers.image.revision=$PLATFORM_REVISION
COPY --from=build /manufacturing-server /usr/local/bin/manufacturing-server
# The workspace (ADR-0018), built beforehand: pnpm --dir web/apps/workspace build
COPY web/apps/workspace/dist /web
USER nobody
ENTRYPOINT ["manufacturing-server"]
