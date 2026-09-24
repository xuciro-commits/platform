# Built from the repository root: docker build -f capabilities/server/cmd/webhook-sink/Dockerfile .
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY contract/go contract/go
COPY capabilities/server capabilities/server
WORKDIR /src/capabilities/server
RUN CGO_ENABLED=0 go build -trimpath -o /webhook-sink ./cmd/webhook-sink

FROM alpine:3.22
COPY --from=build /webhook-sink /usr/local/bin/webhook-sink
USER nobody
ENTRYPOINT ["webhook-sink"]
