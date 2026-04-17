package opsgateway

import "fmt"

var scenarioData = buildScenarioData()

func buildScenarioData() map[string]ScenarioData {
	data := map[string]ScenarioData{
		"release_config_regression":   releaseRegressionScenario("release_config_regression", 0),
		"downstream_inventory_outage": downstreamScenario("downstream_inventory_outage", 0),
	}

	for i := 1; i <= 18; i++ {
		key := fmt.Sprintf("release_config_regression_%02d", i)
		data[key] = releaseRegressionScenario(key, i)
	}

	for i := 1; i <= 18; i++ {
		key := fmt.Sprintf("downstream_inventory_outage_%02d", i)
		data[key] = downstreamScenario(key, i)
	}

	return data
}

func releaseRegressionScenario(key string, index int) ScenarioData {
	suffix := ""
	if index > 0 {
		suffix = fmt.Sprintf("-%02d", index)
	}

	return ScenarioData{
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
				SourceRef:  fmt.Sprintf("logs/order-api#main%s", suffix),
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
			Status:       "not_recovered",
			ErrorRate:    12.4,
			P95LatencyMs: 980,
			Summary:      "5xx and latency are both above threshold before rollback.",
		},
		VerificationAfter: VerificationResult{
			Status:       "recovered",
			ErrorRate:    0.3,
			P95LatencyMs: 118,
			Summary:      "Metrics returned to the healthy baseline after rollback.",
		},
	}
}

func downstreamScenario(key string, index int) ScenarioData {
	suffix := ""
	if index > 0 {
		suffix = fmt.Sprintf("-%02d", index)
	}

	return ScenarioData{
		ChangeItems: []EvidenceItem{
			{
				SourceRef:  fmt.Sprintf("deploy/order-api@2026.04.17-0005%s", suffix),
				Summary:    "No relevant order-api change in the last 2 hours.",
				Confidence: 0.88,
			},
		},
		LogItems: []EvidenceItem{
			{
				SourceRef:  fmt.Sprintf("logs/order-api#timeouts%s", suffix),
				Summary:    "order-api errors are dominated by timeouts when calling inventory-service.",
				Confidence: 0.95,
			},
			{
				SourceRef:  fmt.Sprintf("logs/order-api#error-cluster-2%s", suffix),
				Summary:    "The top error pattern is upstream timeout rather than local configuration failure.",
				Confidence: 0.89,
			},
		},
		DependencyItems: []EvidenceItem{
			{
				SourceRef:  fmt.Sprintf("dep/order-api->inventory-service%s", suffix),
				Summary:    "inventory-service is degraded with database pool exhaustion; downstream propagation is likely.",
				Confidence: 0.96,
			},
			{
				SourceRef:  fmt.Sprintf("dep/inventory-service->postgres%s", suffix),
				Summary:    "inventory-service depends on a saturated database connection pool.",
				Confidence: 0.93,
			},
		},
		VerificationBefore: VerificationResult{
			Status:       "not_recovered",
			ErrorRate:    8.9,
			P95LatencyMs: 760,
			Summary:      "Order traffic is still impacted by downstream timeouts.",
		},
		VerificationAfter: VerificationResult{
			Status:       "not_recovered",
			ErrorRate:    8.7,
			P95LatencyMs: 748,
			Summary:      "Rollback does not recover the service because the issue is downstream.",
		},
	}
}
