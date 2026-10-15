package api

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"remit-platform/ledger/internal/corr"
)

const correlationHeader = "x-correlation-id"

// LoggingInterceptor puts the caller's correlation id (or a new one) into the context
// and writes one structured log line per request.
func LoggingInterceptor(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	id := ""
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if v := md.Get(correlationHeader); len(v) > 0 {
			id = v[0]
		}
	}
	if id == "" {
		id = uuid.NewString()
	}

	start := time.Now()
	resp, err := handler(corr.With(ctx, id), req)
	slog.Info("grpc request",
		"method", info.FullMethod,
		"correlation_id", id,
		"code", status.Code(err).String(),
		"duration_ms", time.Since(start).Milliseconds())
	return resp, err
}