# Built from the repository root: docker build -f solutions/sales/Dockerfile .
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY contract/go contract/go
COPY capabilities/server capabilities/server
COPY slices/hotel/server slices/hotel/server
COPY slices/crm/server slices/crm/server
COPY slices/hr/server slices/hr/server
COPY protocols/lodging protocols/lodging
COPY solutions/sales solutions/sales
WORKDIR /src/solutions/sales
RUN CGO_ENABLED=0 go build -trimpath -o /sales-server ./cmd/sales-server

FROM alpine:3.22
COPY --from=build /sales-server /usr/local/bin/sales-server
USER nobody
ENTRYPOINT ["sales-server"]
