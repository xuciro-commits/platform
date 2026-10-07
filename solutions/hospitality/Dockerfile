# Built from the repository root: docker build -f solutions/hospitality/Dockerfile .
FROM golang:1.27-alpine AS build
# Modules through the mirror, and the module and build caches kept by BuildKit across
# builds and images: a build downloads nothing it has before and compiles only what changed.
ENV GOPROXY=https://goproxy.cn,direct CGO_ENABLED=0
WORKDIR /src
COPY contract/go contract/go
COPY capabilities/server capabilities/server
COPY apps/pms/server apps/pms/server
COPY apps/crm/server apps/crm/server
COPY apps/hcm/server apps/hcm/server
COPY apps/csm/server apps/csm/server
COPY protocols/lodging protocols/lodging
COPY solutions/hospitality solutions/hospitality
WORKDIR /src/solutions/hospitality
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go build -trimpath -o /hospitality-server ./cmd/hospitality-server

FROM alpine:3.22
RUN apk add --no-cache tzdata
ARG PLATFORM_REVISION
LABEL org.opencontainers.image.revision=$PLATFORM_REVISION
COPY --from=build /hospitality-server /usr/local/bin/hospitality-server
# The workspace (ADR-0018), built beforehand: pnpm --dir web/apps/workspace build
COPY web/apps/workspace/dist /web
USER nobody
ENTRYPOINT ["hospitality-server"]
