package main

import (
	"context"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc"

	"remit-platform/ledger/internal/api"
	"remit-platform/ledger/internal/events"
	"remit-platform/ledger/internal/fx"
	"remit-platform/ledger/internal/store"
	ledgerpb "remit-platform/ledger/proto"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

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

	if queue := os.Getenv("SQS_QUEUE_NAME"); queue != "" {
		sqsClient, err := events.NewSQSClient(context.Background(), os.Getenv("SQS_ENDPOINT"))
		if err != nil {
			log.Fatalf("sqs client: %v", err)
		}
		queueURL, err := events.QueueURL(context.Background(), sqsClient, queue, os.Getenv("SQS_CREATE_QUEUE") == "true")
		if err != nil {
			log.Fatalf("sqs queue: %v", err)
		}
		interval := 2 * time.Second
		if v := os.Getenv("OUTBOX_RELAY_INTERVAL"); v != "" {
			d, err := time.ParseDuration(v)
			if err != nil || d <= 0 {
				log.Fatalf("OUTBOX_RELAY_INTERVAL must be a positive duration like 2s: %q", v)
			}
			interval = d
		}
		go events.RunRelay(context.Background(), pool, events.NewSQSPublisher(sqsClient, queueURL), interval)
		log.Printf("outbox relay started (queue %s)", queue)
	} else {
		log.Println("SQS_QUEUE_NAME not set: outbox relay disabled")
	}

	metricsAddr := os.Getenv("LEDGER_METRICS_ADDR")
	if metricsAddr == "" {
		metricsAddr = ":9102"
	}
	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.Handler())
		log.Printf("metrics listening on %s", metricsAddr)
		if err := http.ListenAndServe(metricsAddr, mux); err != nil {
			log.Printf("metrics server: %v", err)
		}
	}()

	addr := os.Getenv("LEDGER_GRPC_ADDR")
	if addr == "" {
		addr = ":50051"
	}

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("listen on %s: %v", addr, err)
	}

	srv := grpc.NewServer(grpc.UnaryInterceptor(api.LoggingInterceptor))
	ledgerpb.RegisterLedgerServer(srv, &api.Server{DB: pool, FX: fx.NewClient(fxURL, fxMaxAge)})

	log.Println("ready")
	if err := srv.Serve(lis); err != nil {
		log.Fatalf("serve: %v", err)
	}
}