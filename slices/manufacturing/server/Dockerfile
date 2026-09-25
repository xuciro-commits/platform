# Built from the repository root: docker build -f slices/manufacturing/server/Dockerfile .
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY contract/go contract/go
COPY capabilities/server capabilities/server
COPY slices/manufacturing/server slices/manufacturing/server
WORKDIR /src/slices/manufacturing/server
RUN CGO_ENABLED=0 go build -trimpath -o /mes-server ./cmd/mes-server

FROM alpine:3.22
COPY --from=build /mes-server /usr/local/bin/mes-server
# The workspace (ADR-0018), built beforehand: pnpm --dir web/apps/workspace build
COPY web/apps/workspace/dist /web
USER nobody
ENTRYPOINT ["mes-server"]
