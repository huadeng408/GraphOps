package opsgateway

import "time"

type QueryRequest struct {
	IncidentID        string `json:"incident_id"`
	ServiceName       string `json:"service_name"`
	ScenarioKey       string `json:"scenario_key"`
	TimeWindowMinutes int    `json:"time_window_minutes"`
}

type EvidenceItem struct {
	SourceRef  string  `json:"source_ref"`
	Summary    string  `json:"summary"`
	Confidence float64 `json:"confidence"`
}

type QueryResponse struct {
	Items []EvidenceItem `json:"items"`
}

type RollbackRequest struct {
	IncidentID     string `json:"incident_id"`
	ScenarioKey    string `json:"scenario_key"`
	TargetService  string `json:"target_service"`
	IdempotencyKey string `json:"idempotency_key"`
	RequestedBy    string `json:"requested_by"`
}

type ActionReceipt struct {
	ReceiptID          string    `json:"receipt_id"`
	IdempotencyKey     string    `json:"idempotency_key"`
	ActionType         string    `json:"action_type"`
	TargetService      string    `json:"target_service"`
	Status             string    `json:"status"`
	ExecutedAt         time.Time `json:"executed_at"`
	VerificationStatus string    `json:"verification_status"`
}

type RollbackResponse struct {
	Receipt ActionReceipt `json:"receipt"`
}

type VerifyRequest struct {
	IncidentID  string `json:"incident_id"`
	ServiceName string `json:"service_name"`
	ScenarioKey string `json:"scenario_key"`
}

type VerificationResult struct {
	Status       string  `json:"status"`
	ErrorRate    float64 `json:"error_rate"`
	P95LatencyMs int     `json:"p95_latency_ms"`
	Summary      string  `json:"summary"`
}

type ScenarioData struct {
	ChangeItems        []EvidenceItem
	LogItems           []EvidenceItem
	DependencyItems    []EvidenceItem
	VerificationBefore VerificationResult
	VerificationAfter  VerificationResult
}
