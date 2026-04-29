package opsgateway

import (
	"testing"
	"time"
)

func TestRollbackIsIdempotent(t *testing.T) {
	store := NewStore()
	req := RollbackRequest{
		IncidentID:      "inc-000001",
		PlaybookKey:     "release_config_regression",
		IncidentContext: testIncidentContext(),
		TargetService:   "order-api",
		CurrentRevision: "order-api@2026.04.17-0155",
		TargetRevision:  "order-api@2026.04.17-0142",
		RiskLevel:       "high",
		IdempotencyKey:  "inc-000001:rollback:order-api",
		RequestedBy:     "oncall",
		VerificationPolicy: &VerificationPolicy{
			WindowMinutes:         10,
			MaxErrorRate:          1.0,
			MaxP95LatencyMs:       300,
			MinimumPassingSignals: 2,
		},
	}

	first, err := store.Rollback(req)
	if err != nil {
		t.Fatalf("first rollback: %v", err)
	}

	second, err := store.Rollback(req)
	if err != nil {
		t.Fatalf("second rollback: %v", err)
	}

	if first.Receipt.ReceiptID != second.Receipt.ReceiptID {
		t.Fatalf("expected same receipt id, got %s and %s", first.Receipt.ReceiptID, second.Receipt.ReceiptID)
	}

	verify, err := store.Verify(VerifyRequest{
		IncidentID:         "inc-000001",
		ServiceName:        "order-api",
		PlaybookKey:        "release_config_regression",
		IncidentContext:    testIncidentContext(),
		VerificationPolicy: req.VerificationPolicy,
	})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if verify.Status != "recovered" {
		t.Fatalf("expected recovered, got %s", verify.Status)
	}

	afterVerify, err := store.Rollback(req)
	if err != nil {
		t.Fatalf("rollback after verify: %v", err)
	}
	if afterVerify.Receipt.VerificationStatus != "recovered" {
		t.Fatalf("expected receipt verification status to be recovered, got %q", afterVerify.Receipt.VerificationStatus)
	}
}

func TestGeneratedReplayScenariosAreAvailable(t *testing.T) {
	store := NewStore()

	resp, err := store.QueryChanges(QueryRequest{
		IncidentID:      "inc-000002",
		ServiceName:     "order-api",
		PlaybookKey:     "release_config_regression_12",
		IncidentContext: testIncidentContext(),
	})
	if err != nil {
		t.Fatalf("query generated release scenario: %v", err)
	}
	if len(resp.Items) == 0 {
		t.Fatalf("expected generated release scenario to contain evidence items")
	}
}

func TestDownstreamScenarioIsAvailable(t *testing.T) {
	store := NewStore()

	resp, err := store.QueryDependencies(QueryRequest{
		IncidentID:      "inc-000003",
		ServiceName:     "order-api",
		PlaybookKey:     "downstream_inventory_outage",
		IncidentContext: testIncidentContext(),
	})
	if err != nil {
		t.Fatalf("query downstream scenario: %v", err)
	}
	if len(resp.Items) == 0 {
		t.Fatalf("expected downstream scenario to contain dependency evidence")
	}
}

func testIncidentContext() *IncidentContext {
	return &IncidentContext{
		Cluster:         "prod-cn",
		Namespace:       "checkout",
		Environment:     "production",
		AlertName:       "OrderApiHigh5xxAfterRelease",
		AlertStartedAt:  mustParseOpsTime("2026-04-17T02:02:00Z"),
		ReleaseID:       "deploy-2026.04.17-0155",
		ReleaseVersion:  "order-api@2026.04.17-0155",
		PreviousVersion: "order-api@2026.04.17-0142",
		Labels:          map[string]string{"service": "order-api"},
	}
}

func mustParseOpsTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return parsed
}
