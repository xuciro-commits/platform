# Built from the repository root: docker build -f solutions/hospitality/Dockerfile .
FROM golang:1.27-alpine AS build
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
RUN CGO_ENABLED=0 go build -trimpath -o /hospitality-server ./cmd/hospitality-server

FROM alpine:3.22
COPY --from=build /hospitality-server /usr/local/bin/hospitality-server
# The workspace (ADR-0018), built beforehand: pnpm --dir web/apps/workspace build
COPY web/apps/workspace/dist /web
USER nobody
ENTRYPOINT ["hospitality-server"]
