package opsgateway

import "testing"

func TestRollbackIsIdempotent(t *testing.T) {
	store := NewStore()
	req := RollbackRequest{
		IncidentID:     "inc-000001",
		ScenarioKey:    "release_config_regression",
		TargetService:  "order-api",
		IdempotencyKey: "inc-000001:rollback:order-api",
		RequestedBy:    "oncall",
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
		IncidentID:  "inc-000001",
		ServiceName: "order-api",
		ScenarioKey: "release_config_regression",
	})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if verify.Status != "recovered" {
		t.Fatalf("expected recovered, got %s", verify.Status)
	}
}

func TestGeneratedReplayScenariosAreAvailable(t *testing.T) {
	store := NewStore()

	resp, err := store.QueryChanges(QueryRequest{
		IncidentID:  "inc-000002",
		ServiceName: "order-api",
		ScenarioKey: "release_config_regression_12",
	})
	if err != nil {
		t.Fatalf("query generated release scenario: %v", err)
	}
	if len(resp.Items) == 0 {
		t.Fatalf("expected generated release scenario to contain evidence items")
	}

	resp, err = store.QueryDependencies(QueryRequest{
		IncidentID:  "inc-000003",
		ServiceName: "order-api",
		ScenarioKey: "downstream_inventory_outage_18",
	})
	if err != nil {
		t.Fatalf("query generated downstream scenario: %v", err)
	}
	if len(resp.Items) == 0 {
		t.Fatalf("expected generated downstream scenario to contain evidence items")
	}
}
