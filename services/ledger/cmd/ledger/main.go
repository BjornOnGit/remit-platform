package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"time"

	"google.golang.org/grpc"

	"remit-platform/ledger/internal/api"
	"remit-platform/ledger/internal/fx"
	"remit-platform/ledger/internal/store"
	ledgerpb "remit-platform/ledger/proto"
)

func main() {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL is required")
	}
	pool, err := store.Connect(context.Background(), dbURL)
	if err != nil {
		log.Fatalf("database connection failed: %v", err)
	}
	defer pool.Close()

	fxURL := os.Getenv("FX_SERVICE_URL")
	if fxURL == "" {
		fxURL = "http://localhost:3001"
	}
	fxMaxAge := 10 * time.Minute
	if v := os.Getenv("FX_MAX_AGE"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			log.Fatalf("FX_MAX_AGE must be a positive duration like 10m: %q", v)
		}
		fxMaxAge = d
	}
	log.Println("connected to database")

	addr := os.Getenv("LEDGER_GRPC_ADDR")
	if addr == "" {
		addr = ":50051"
	}

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("listen on %s: %v", addr, err)
	}

	srv := grpc.NewServer()
	ledgerpb.RegisterLedgerServer(srv, &api.Server{DB: pool, FX: fx.NewClient(fxURL, fxMaxAge)})

	fmt.Println("ready")
	if err := srv.Serve(lis); err != nil {
		log.Fatalf("serve: %v", err)
	}
}