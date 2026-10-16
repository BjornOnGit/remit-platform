# Build from the repo root: docker build -f services/settlement-worker/Dockerfile .
# Use the Go version from services/settlement-worker/go.mod (or newer): --build-arg GO_VERSION=1.24
ARG GO_VERSION=1.26
FROM golang:${GO_VERSION}-alpine AS build
WORKDIR /src
COPY services/settlement-worker/go.mod services/settlement-worker/go.sum ./
RUN go mod download
COPY services/settlement-worker/ ./
RUN CGO_ENABLED=0 go build -o /out/settlement-worker ./cmd/settlement-worker

FROM alpine:3.20
RUN apk add --no-cache ca-certificates && adduser -D -u 10001 app
USER app
COPY --from=build /out/settlement-worker /usr/local/bin/settlement-worker
ENTRYPOINT ["settlement-worker"]