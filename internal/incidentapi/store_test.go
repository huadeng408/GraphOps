package incidentapi

import "testing"

func TestMemoryStoreReviewFlow(t *testing.T) {
	store := NewMemoryStore()
	incident, err := store.CreateIncident(CreateIncidentRequest{
		ServiceName:  "order-api",
		Severity:     "P1",
		AlertSummary: "5xx spike after deploy",
		ScenarioKey:  "release_config_regression",
	})
	if err != nil {
		t.Fatalf("create incident: %v", err)
	}

	_, err = store.SaveAnalysis(incident.ID, UpsertAnalysisRequest{
		Evidence: []Evidence{
			{EvidenceID: "e-1", SourceType: "change", SourceRef: "deploy-1", Summary: "recent deploy", Confidence: 0.9},
		},
		Hypotheses: []Hypothesis{
			{HypothesisID: "h-1", Cause: "config regression", SupportEvidenceIDs: []string{"e-1"}, Confidence: 0.91},
		},
		ProposedAction: &ActionPlan{
			ActionType:       "rollback",
			TargetService:    "order-api",
			Reason:           "recent release likely caused the incident",
			EvidenceIDs:      []string{"e-1"},
			RequiresApproval: true,
		},
	})
	if err != nil {
		t.Fatalf("save analysis: %v", err)
	}

	current, err := store.GetIncident(incident.ID)
	if err != nil {
		t.Fatalf("get incident: %v", err)
	}
	if current.Status != "awaiting_approval" {
		t.Fatalf("expected awaiting_approval, got %s", current.Status)
	}

	current, err = store.ReviewIncident(incident.ID, "approved", ReviewIncidentRequest{
		Reviewer: "oncall",
		Comment:  "rollback the broken release",
	})
	if err != nil {
		t.Fatalf("review incident: %v", err)
	}
	if current.Status != "approved" {
		t.Fatalf("expected approved, got %s", current.Status)
	}
	if current.Approval == nil || current.Approval.Reviewer != "oncall" {
		t.Fatalf("approval reviewer not persisted")
	}
}
