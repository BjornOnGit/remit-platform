# Build from the repo root: docker build -f services/ledger/Dockerfile .
# Use the Go version from services/ledger/go.mod (or newer): --build-arg GO_VERSION=1.24
ARG GO_VERSION=1.26
FROM golang:${GO_VERSION}-alpine AS build
WORKDIR /src
COPY services/ledger/go.mod services/ledger/go.sum ./
RUN go mod download
COPY services/ledger/ ./
RUN CGO_ENABLED=0 go build -o /out/ledger ./cmd/ledger

FROM alpine:3.20
RUN apk add --no-cache ca-certificates && adduser -D -u 10001 app
USER app
COPY --from=build /out/ledger /usr/local/bin/ledger
EXPOSE 50051 9102
HEALTHCHECK --interval=15s --timeout=3s --start-period=10s --retries=3 \
  CMD wget -qO- http://127.0.0.1:9102/metrics > /dev/null || exit 1
ENTRYPOINT ["ledger"]