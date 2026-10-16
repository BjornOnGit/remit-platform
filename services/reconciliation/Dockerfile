# Build from the repo root: docker build -f services/reconciliation/Dockerfile .
# Use the Go version from services/reconciliation/go.mod (or newer): --build-arg GO_VERSION=1.24
ARG GO_VERSION=1.26
FROM golang:${GO_VERSION}-alpine AS build
WORKDIR /src
COPY services/reconciliation/go.mod services/reconciliation/go.sum ./
RUN go mod download
COPY services/reconciliation/ ./
RUN CGO_ENABLED=0 go build -o /out/reconciliation ./cmd/reconciliation

FROM alpine:3.20
RUN apk add --no-cache ca-certificates && adduser -D -u 10001 app
USER app
COPY --from=build /out/reconciliation /usr/local/bin/reconciliation
ENTRYPOINT ["reconciliation"]