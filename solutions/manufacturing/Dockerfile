# Built from the repository root: docker build -f solutions/manufacturing/Dockerfile .
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY contract/go contract/go
COPY capabilities/server capabilities/server
COPY apps/mes/server apps/mes/server
COPY apps/erp/server apps/erp/server
COPY apps/erpadapter/server apps/erpadapter/server
COPY protocols/production protocols/production
COPY solutions/manufacturing solutions/manufacturing
WORKDIR /src/solutions/manufacturing
RUN CGO_ENABLED=0 go build -trimpath -o /manufacturing-server ./cmd/manufacturing-server

FROM alpine:3.22
COPY --from=build /manufacturing-server /usr/local/bin/manufacturing-server
# The workspace (ADR-0018), built beforehand: pnpm --dir web/apps/workspace build
COPY web/apps/workspace/dist /web
USER nobody
ENTRYPOINT ["manufacturing-server"]
