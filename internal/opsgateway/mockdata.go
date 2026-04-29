package opsgateway

import "fmt"

var scenarioData = buildScenarioData()

func buildScenarioData() map[string]ScenarioData {
	data := map[string]ScenarioData{
		"release_config_regression":    releaseRegressionScenario("release_config_regression", 0),
		"downstream_inventory_outage": downstreamInventoryScenario(),
	}

	for i := 1; i <= 18; i++ {
		key := fmt.Sprintf("release_config_regression_%02d", i)
		data[key] = releaseRegressionScenario(key, i)
	}

	return data
}

func releaseRegressionScenario(key string, index int) ScenarioData {
	suffix := ""
	targetRevision := "order-api@2026.04.17-0142"
	currentRevision := "order-api@2026.04.17-0155"
	if index > 0 {
		suffix = fmt.Sprintf("-%02d", index)
		targetRevision = fmt.Sprintf("%s%s", targetRevision, suffix)
		currentRevision = fmt.Sprintf("%s%s", currentRevision, suffix)
	}

	beforeChecks := []SignalCheck{
		{
			Name:          "error_rate",
			QueryRef:      "promql:error_rate:order-api",
			ObservedValue: 12.4,
			Threshold:     "<= 1.0",
			Passed:        false,
			Summary:       "HTTP 5xx ratio is above the rollback recovery threshold.",
		},
		{
			Name:          "p95_latency_ms",
			QueryRef:      "promql:p95_latency:order-api",
			ObservedValue: 980,
			Threshold:     "<= 300",
			Passed:        false,
			Summary:       "P95 latency is elevated and confirms user-facing degradation.",
		},
	}
	afterChecks := []SignalCheck{
		{
			Name:          "error_rate",
			QueryRef:      "promql:error_rate:order-api",
			ObservedValue: 0.3,
			Threshold:     "<= 1.0",
			Passed:        true,
			Summary:       "5xx ratio returned to the healthy baseline after rollback.",
		},
		{
			Name:          "p95_latency_ms",
			QueryRef:      "promql:p95_latency:order-api",
			ObservedValue: 118,
			Threshold:     "<= 300",
			Passed:        true,
			Summary:       "P95 latency recovered after reverting the bad release.",
		},
	}

	return ScenarioData{
		CurrentRevision: currentRevision,
		TargetRevision:  targetRevision,
		ChangeItems: []EvidenceItem{
			{
				SourceRef:  fmt.Sprintf("deploy/order-api@2026.04.17-0155%s", suffix),
				Summary:    "order-api released 8 minutes before the alert with a configuration bundle update.",
				Confidence: 0.94,
			},
			{
				SourceRef:  fmt.Sprintf("config/order-api/db-dsn%s", suffix),
				Summary:    "Database DSN and pool settings changed in the same release window.",
				Confidence: 0.92,
			},
		},
		LogItems: []EvidenceItem{
			{
				SourceRef:  fmt.Sprintf("logs/order-api#db-auth%s", suffix),
				Summary:    "High-frequency errors show invalid connection string and database authentication failures.",
				Confidence: 0.97,
			},
			{
				SourceRef:  fmt.Sprintf("logs/order-api#error-cluster-1%s", suffix),
				Summary:    "The error spike starts immediately after the release and stays local to order-api.",
				Confidence: 0.90,
			},
		},
		DependencyItems: []EvidenceItem{
			{
				SourceRef:  fmt.Sprintf("dep/order-api->inventory-service%s", suffix),
				Summary:    "No downstream error amplification detected on inventory-service.",
				Confidence: 0.78,
			},
			{
				SourceRef:  fmt.Sprintf("dep/order-api->payment-service%s", suffix),
				Summary:    "payment-service remains healthy; the blast radius is currently limited to order-api.",
				Confidence: 0.82,
			},
		},
		VerificationBefore: VerificationResult{
			Status:        "not_recovered",
			ErrorRate:     12.4,
			P95LatencyMs:  980,
			WindowMinutes: 10,
			QueryRefs: []string{
				"promql:error_rate:order-api",
				"promql:p95_latency:order-api",
			},
			SignalChecks:  beforeChecks,
			DecisionBasis: "0 of 2 recovery signals passed before rollback, so the service is still considered unhealthy.",
			Summary:       "5xx and latency are both above threshold before rollback.",
		},
		VerificationAfter: VerificationResult{
			Status:        "recovered",
			ErrorRate:     0.3,
			P95LatencyMs:  118,
			WindowMinutes: 10,
			QueryRefs: []string{
				"promql:error_rate:order-api",
				"promql:p95_latency:order-api",
			},
			SignalChecks:  afterChecks,
			DecisionBasis: "2 of 2 recovery signals passed after rollback, so the service is considered recovered.",
			Summary:       "Metrics returned to the healthy baseline after rollback.",
		},
	}
}

func downstreamInventoryScenario() ScenarioData {
	beforeChecks := []SignalCheck{
		{
			Name:          "error_rate",
			QueryRef:      "promql:error_rate:order-api",
			ObservedValue: 8.7,
			Threshold:     "<= 1.0",
			Passed:        false,
			Summary:       "Order traffic remains degraded while inventory-service is timing out.",
		},
		{
			Name:          "p95_latency_ms",
			QueryRef:      "promql:p95_latency:order-api",
			ObservedValue: 840,
			Threshold:     "<= 300",
			Passed:        false,
			Summary:       "P95 latency remains elevated because the downstream dependency is still unhealthy.",
		},
	}
	afterChecks := []SignalCheck{
		{
			Name:          "error_rate",
			QueryRef:      "promql:error_rate:order-api",
			ObservedValue: 8.2,
			Threshold:     "<= 1.0",
			Passed:        false,
			Summary:       "Rolling back order-api does not remove the upstream timeout source.",
		},
		{
			Name:          "p95_latency_ms",
			QueryRef:      "promql:p95_latency:order-api",
			ObservedValue: 801,
			Threshold:     "<= 300",
			Passed:        false,
			Summary:       "Latency remains degraded because inventory-service is still saturated.",
		},
	}

	return ScenarioData{
		CurrentRevision: "order-api@2026.04.16-2210",
		TargetRevision:  "order-api@2026.04.16-2150",
		ChangeItems: []EvidenceItem{
			{
				SourceRef:  "deploy/order-api@2026.04.16-2210",
				Summary:    "No relevant order-api change in the last 2 hours.",
				Confidence: 0.93,
			},
		},
		LogItems: []EvidenceItem{
			{
				SourceRef:  "logs/order-api#inventory-timeouts",
				Summary:    "order-api errors are dominated by timeouts when calling inventory-service.",
				Confidence: 0.96,
			},
			{
				SourceRef:  "logs/order-api#error-cluster-2",
				Summary:    "The top error pattern is upstream timeout rather than local configuration failure.",
				Confidence: 0.91,
			},
		},
		DependencyItems: []EvidenceItem{
			{
				SourceRef:  "dep/inventory-service#pool-exhaustion",
				Summary:    "inventory-service is degraded with database pool exhaustion; downstream propagation is likely.",
				Confidence: 0.97,
			},
			{
				SourceRef:  "dep/inventory-service#db-pool",
				Summary:    "inventory-service depends on a saturated database connection pool.",
				Confidence: 0.92,
			},
		},
		VerificationBefore: VerificationResult{
			Status:        "not_recovered",
			ErrorRate:     8.7,
			P95LatencyMs:  840,
			WindowMinutes: 10,
			QueryRefs: []string{
				"promql:error_rate:order-api",
				"promql:p95_latency:order-api",
			},
			SignalChecks:  beforeChecks,
			DecisionBasis: "0 of 2 recovery signals passed because the downstream dependency is still degraded.",
			Summary:       "order-api remains degraded while inventory-service is unhealthy.",
		},
		VerificationAfter: VerificationResult{
			Status:        "not_recovered",
			ErrorRate:     8.2,
			P95LatencyMs:  801,
			WindowMinutes: 10,
			QueryRefs: []string{
				"promql:error_rate:order-api",
				"promql:p95_latency:order-api",
			},
			SignalChecks:  afterChecks,
			DecisionBasis: "0 of 2 recovery signals passed after the wrong action because inventory-service remains degraded.",
			Summary:       "Rolling back order-api does not address the downstream fault owner.",
		},
	}
}
