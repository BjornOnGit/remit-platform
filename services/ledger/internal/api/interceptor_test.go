package api

import (
	"context"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"remit-platform/ledger/internal/corr"
)

func TestLoggingInterceptor(t *testing.T) {
	info := &grpc.UnaryServerInfo{FullMethod: transferMethod}
	var seen string
	ok := func(ctx context.Context, _ any) (any, error) {
		seen = corr.From(ctx)
		return "done", nil
	}

	// Uses the caller's correlation id.
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs(correlationHeader, "abc-123"))
	if _, err := LoggingInterceptor(ctx, nil, info, ok); err != nil {
		t.Fatal(err)
	}
	if seen != "abc-123" {
		t.Fatalf("correlation id = %q, want abc-123", seen)
	}

	// Generates one when the caller sent none.
	seen = ""
	if _, err := LoggingInterceptor(context.Background(), nil, info, ok); err != nil {
		t.Fatal(err)
	}
	if seen == "" {
		t.Fatal("no correlation id generated")
	}

	// Errors pass through unchanged (and are counted as failed transfers).
	fail := func(context.Context, any) (any, error) { return nil, status.Error(codes.Internal, "boom") }
	if _, err := LoggingInterceptor(context.Background(), nil, info, fail); status.Code(err) != codes.Internal {
		t.Fatalf("got %v, want Internal", err)
	}
}