package main

import (
	"fmt"
	"log"
	"net"
	"os"

	"google.golang.org/grpc"
)

func main() {
	addr := os.Getenv("LEDGER_GRPC_ADDR")
	if addr == "" {
		addr = ":50051"
	}

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("listen on %s: %v", addr, err)
	}

	srv := grpc.NewServer()

	fmt.Println("ready")
	if err := srv.Serve(lis); err != nil {
		log.Fatalf("serve: %v", err)
	}
}