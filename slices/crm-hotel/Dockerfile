# Built from the repository root: docker build -f slices/crm-hotel/Dockerfile .
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY contract/go contract/go
COPY capabilities/server capabilities/server
COPY slices/hotel/server slices/hotel/server
COPY slices/crm/server slices/crm/server
COPY slices/crm-hotel slices/crm-hotel
WORKDIR /src/slices/crm-hotel
RUN CGO_ENABLED=0 go build -trimpath -o /sales-server ./cmd/sales-server

FROM alpine:3.22
COPY --from=build /sales-server /usr/local/bin/sales-server
USER nobody
ENTRYPOINT ["sales-server"]
