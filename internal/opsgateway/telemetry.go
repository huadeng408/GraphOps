package opsgateway

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	toolCallDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "tool_call_duration_seconds",
			Help:    "Latency of tool and action calls exposed by ops-gateway.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"tool", "status"},
	)
	rollbackRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "rollback_requests_total",
			Help: "Total rollback requests by result.",
		},
		[]string{"result"},
	)
	recoveryVerificationTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "recovery_verification_total",
			Help: "Verification outcomes after rollback or observe-only flows.",
		},
		[]string{"status"},
	)
)

func observeToolCall(toolName, status string, started time.Time) {
	toolCallDuration.WithLabelValues(toolName, status).Observe(time.Since(started).Seconds())
}

func recordRollbackResult(result string) {
	rollbackRequestsTotal.WithLabelValues(result).Inc()
}

func recordVerificationStatus(status string) {
	recoveryVerificationTotal.WithLabelValues(status).Inc()
}
