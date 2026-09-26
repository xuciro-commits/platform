# Built from the repository root: docker build -f solutions/plant/Dockerfile .
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY contract/go contract/go
COPY capabilities/server capabilities/server
COPY apps/manufacturing/server apps/manufacturing/server
COPY apps/erp/server apps/erp/server
COPY apps/erplink/server apps/erplink/server
COPY protocols/production protocols/production
COPY solutions/plant solutions/plant
WORKDIR /src/solutions/plant
RUN CGO_ENABLED=0 go build -trimpath -o /plant-server ./cmd/plant-server

FROM alpine:3.22
COPY --from=build /plant-server /usr/local/bin/plant-server
# The workspace (ADR-0018), built beforehand: pnpm --dir web/apps/workspace build
COPY web/apps/workspace/dist /web
USER nobody
ENTRYPOINT ["plant-server"]
