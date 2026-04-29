package opsgateway

import "time"

type IncidentContext struct {
	Cluster         string            `json:"cluster"`
	Namespace       string            `json:"namespace"`
	Environment     string            `json:"environment"`
	AlertName       string            `json:"alert_name"`
	AlertStartedAt  time.Time         `json:"alert_started_at"`
	ReleaseID       string            `json:"release_id"`
	ReleaseVersion  string            `json:"release_version"`
	PreviousVersion string            `json:"previous_version"`
	Labels          map[string]string `json:"labels,omitempty"`
}

type VerificationPolicy struct {
	WindowMinutes         int     `json:"window_minutes"`
	MaxErrorRate          float64 `json:"max_error_rate"`
	MaxP95LatencyMs       int     `json:"max_p95_latency_ms"`
	MinimumPassingSignals int     `json:"minimum_passing_signals"`
}

type SignalCheck struct {
	Name          string  `json:"name"`
	QueryRef      string  `json:"query_ref"`
	ObservedValue float64 `json:"observed_value"`
	Threshold     string  `json:"threshold"`
	Passed        bool    `json:"passed"`
	Summary       string  `json:"summary"`
}

type QueryRequest struct {
	IncidentID        string           `json:"incident_id"`
	ServiceName       string           `json:"service_name"`
	PlaybookKey       string           `json:"playbook_key,omitempty"`
	IncidentContext   *IncidentContext `json:"incident_context,omitempty"`
	TimeWindowMinutes int              `json:"time_window_minutes"`
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
	IncidentID         string              `json:"incident_id"`
	PlaybookKey        string              `json:"playbook_key,omitempty"`
	IncidentContext    *IncidentContext    `json:"incident_context,omitempty"`
	TargetService      string              `json:"target_service"`
	CurrentRevision    string              `json:"current_revision"`
	TargetRevision     string              `json:"target_revision"`
	RiskLevel          string              `json:"risk_level"`
	IdempotencyKey     string              `json:"idempotency_key"`
	RequestedBy        string              `json:"requested_by"`
	VerificationPolicy *VerificationPolicy `json:"verification_policy,omitempty"`
}

type ActionReceipt struct {
	ReceiptID          string    `json:"receipt_id"`
	IdempotencyKey     string    `json:"idempotency_key"`
	ActionType         string    `json:"action_type"`
	TargetService      string    `json:"target_service"`
	Executor           string    `json:"executor"`
	FromRevision       string    `json:"from_revision"`
	ToRevision         string    `json:"to_revision"`
	Status             string    `json:"status"`
	StatusDetail       string    `json:"status_detail"`
	ExecutedAt         time.Time `json:"executed_at"`
	VerificationStatus string    `json:"verification_status"`
}

type RollbackResponse struct {
	Receipt ActionReceipt `json:"receipt"`
}

type VerifyRequest struct {
	IncidentID         string              `json:"incident_id"`
	ServiceName        string              `json:"service_name"`
	PlaybookKey        string              `json:"playbook_key,omitempty"`
	IncidentContext    *IncidentContext    `json:"incident_context,omitempty"`
	VerificationPolicy *VerificationPolicy `json:"verification_policy,omitempty"`
}

type VerificationResult struct {
	Status        string        `json:"status"`
	ErrorRate     float64       `json:"error_rate"`
	P95LatencyMs  int           `json:"p95_latency_ms"`
	WindowMinutes int           `json:"window_minutes"`
	QueryRefs     []string      `json:"query_refs"`
	SignalChecks  []SignalCheck `json:"signal_checks"`
	DecisionBasis string        `json:"decision_basis"`
	Summary       string        `json:"summary"`
}

type ScenarioData struct {
	CurrentRevision    string
	TargetRevision     string
	ChangeItems        []EvidenceItem
	LogItems           []EvidenceItem
	DependencyItems    []EvidenceItem
	VerificationBefore VerificationResult
	VerificationAfter  VerificationResult
}
