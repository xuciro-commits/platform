# Built from the repository root: docker build -f solutions/sales/Dockerfile .
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY contract/go contract/go
COPY capabilities/server capabilities/server
COPY apps/hotel/server apps/hotel/server
COPY apps/crm/server apps/crm/server
COPY apps/hr/server apps/hr/server
COPY apps/helpdesk/server apps/helpdesk/server
COPY protocols/lodging protocols/lodging
COPY solutions/sales solutions/sales
WORKDIR /src/solutions/sales
RUN CGO_ENABLED=0 go build -trimpath -o /sales-server ./cmd/sales-server

FROM alpine:3.22
COPY --from=build /sales-server /usr/local/bin/sales-server
# The workspace (ADR-0018), built beforehand: pnpm --dir web/apps/workspace build
COPY web/apps/workspace/dist /web
USER nobody
ENTRYPOINT ["sales-server"]
