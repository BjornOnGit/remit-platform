package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	TransferRequests = promauto.NewCounter(prometheus.CounterOpts{
		Name: "transfer_requests_total",
		Help: "Total Transfer RPCs handled by the ledger",
	})
	TransferFailed = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "transfer_failed_total",
		Help: "Transfer RPCs that returned an error, by gRPC code",
	}, []string{"code"})
)