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
	serviceObservationValue = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "service_observation_value",
			Help: "Structured service and middleware observations captured during verification.",
		},
		[]string{"service", "metric", "phase", "source_mode", "unit"},
	)
	serviceObservationAbnormal = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "service_observation_abnormal",
			Help: "Whether a structured observation is currently abnormal (1) or healthy (0).",
		},
		[]string{"service", "metric", "phase", "source_mode"},
	)
	releaseComparisonDeltaValue = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "release_comparison_delta_value",
			Help: "Absolute release-window deltas for selected incident metrics.",
		},
		[]string{"service", "metric", "unit"},
	)
	releaseComparisonDeltaRatio = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "release_comparison_delta_ratio",
			Help: "Relative release-window deltas for selected incident metrics in percentage.",
		},
		[]string{"service", "metric"},
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

func recordVerificationSnapshot(serviceName string, result VerificationResult) {
	for _, item := range result.Metrics {
		serviceObservationValue.WithLabelValues(
			serviceName,
			item.Key,
			item.Phase,
			firstNonEmpty(item.SourceMode, "simulated"),
			item.Unit,
		).Set(item.Value)
		if item.Abnormal {
			serviceObservationAbnormal.WithLabelValues(
				serviceName,
				item.Key,
				item.Phase,
				firstNonEmpty(item.SourceMode, "simulated"),
			).Set(1)
		} else {
			serviceObservationAbnormal.WithLabelValues(
				serviceName,
				item.Key,
				item.Phase,
				firstNonEmpty(item.SourceMode, "simulated"),
			).Set(0)
		}
	}

	for _, item := range result.ReleaseComparisons {
		releaseComparisonDeltaValue.WithLabelValues(serviceName, item.Key, item.Unit).Set(item.DeltaValue)
		releaseComparisonDeltaRatio.WithLabelValues(serviceName, item.Key).Set(item.DeltaRatio)
	}
}
