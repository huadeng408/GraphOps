package incidentapi

import (
	"testing"
	"time"
)

func TestMemoryStoreReviewFlow(t *testing.T) {
	store := NewMemoryStore()
	incident, err := store.CreateIncident(CreateIncidentRequest{
		ServiceName:  "order-api",
		Severity:     "P1",
		AlertSummary: "5xx spike after deploy",
		PlaybookKey:  "release_config_regression",
		Context: &IncidentContext{
			Cluster:         "prod-cn",
			Namespace:       "checkout",
			Environment:     "production",
			AlertName:       "OrderApiHigh5xxAfterRelease",
			AlertStartedAt:  mustParseTestTime("2026-04-17T02:02:00Z"),
			ReleaseID:       "deploy-2026.04.17-0155",
			ReleaseVersion:  "order-api@2026.04.17-0155",
			PreviousVersion: "order-api@2026.04.17-0142",
			Labels:          map[string]string{"service": "order-api"},
		},
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
			ActionType:      "rollback",
			TargetService:   "order-api",
			CurrentRevision: "order-api@2026.04.17-0155",
			TargetRevision:  "order-api@2026.04.17-0142",
			Reason:          "recent release likely caused the incident",
			RiskLevel:       "high",
			EvidenceIDs:     []string{"e-1"},
			VerificationPolicy: &VerificationPolicy{
				WindowMinutes:         10,
				MaxErrorRate:          1.0,
				MaxP95LatencyMs:       300,
				MinimumPassingSignals: 2,
			},
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
	if current.Status != "waiting_for_approval" {
		t.Fatalf("expected waiting_for_approval, got %s", current.Status)
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

	if err := store.RecordEvent(incident.ID, RecordIncidentEventRequest{
		EventType: "incident_loaded",
		ActorType: "system",
		ActorName: "load_incident",
		Payload:   map[string]any{"playbook_key": "release_config_regression"},
	}); err != nil {
		t.Fatalf("record event: %v", err)
	}
	if err := store.RecordAgentRun(incident.ID, RecordAgentRunRequest{
		NodeName:      "triage_agent",
		ModelName:     "rule-engine",
		PromptVersion: "triage-v2",
		Input:         map[string]any{"service_name": "order-api"},
		Output:        map[string]any{"incident_type": "release_regression"},
		LatencyMs:     12,
		Status:        "completed",
		CheckpointID:  incident.ID,
	}); err != nil {
		t.Fatalf("record agent run: %v", err)
	}

	events, err := store.ListEvents(incident.ID)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	runs, err := store.ListAgentRuns(incident.ID)
	if err != nil {
		t.Fatalf("list agent runs: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("expected 1 agent run, got %d", len(runs))
	}
}

func mustParseTestTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return parsed
}
