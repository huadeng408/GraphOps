package incidentapi

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

type Incident struct {
	ID           string            `json:"id"`
	ServiceName  string            `json:"service_name"`
	Severity     string            `json:"severity"`
	AlertSummary string            `json:"alert_summary"`
	PlaybookKey  string            `json:"playbook_key,omitempty"`
	Context      *IncidentContext  `json:"context,omitempty"`
	Status       string            `json:"status"`
	Approval     *ApprovalDecision `json:"approval,omitempty"`
	Analysis     *AnalysisSnapshot `json:"analysis,omitempty"`
	Report       *FinalReport      `json:"report,omitempty"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
}

type IncidentEvent struct {
	ID         int64          `json:"id"`
	IncidentID string         `json:"incident_id"`
	EventType  string         `json:"event_type"`
	ActorType  string         `json:"actor_type"`
	ActorName  string         `json:"actor_name"`
	Payload    map[string]any `json:"payload"`
	CreatedAt  time.Time      `json:"created_at"`
}

type AgentRun struct {
	ID            int64          `json:"id"`
	IncidentID    string         `json:"incident_id"`
	NodeName      string         `json:"node_name"`
	ModelName     string         `json:"model_name"`
	PromptVersion string         `json:"prompt_version"`
	Input         map[string]any `json:"input"`
	Output        map[string]any `json:"output"`
	LatencyMs     int64          `json:"latency_ms"`
	Status        string         `json:"status"`
	CheckpointID  string         `json:"checkpoint_id"`
	CreatedAt     time.Time      `json:"created_at"`
}

type ApprovalDecision struct {
	Status    string    `json:"status"`
	Reviewer  string    `json:"reviewer"`
	Comment   string    `json:"comment,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

type AnalysisSnapshot struct {
	Evidence       []Evidence   `json:"evidence"`
	Hypotheses     []Hypothesis `json:"hypotheses"`
	ProposedAction *ActionPlan  `json:"proposed_action,omitempty"`
}

type Evidence struct {
	EvidenceID string  `json:"evidence_id"`
	SourceType string  `json:"source_type"`
	SourceRef  string  `json:"source_ref"`
	Summary    string  `json:"summary"`
	Confidence float64 `json:"confidence"`
}

type Hypothesis struct {
	HypothesisID       string   `json:"hypothesis_id"`
	Cause              string   `json:"cause"`
	SupportEvidenceIDs []string `json:"support_evidence_ids"`
	Confidence         float64  `json:"confidence"`
}

type ActionPlan struct {
	ActionType         string              `json:"action_type"`
	TargetService      string              `json:"target_service"`
	CurrentRevision    string              `json:"current_revision"`
	TargetRevision     string              `json:"target_revision"`
	Reason             string              `json:"reason"`
	RiskLevel          string              `json:"risk_level"`
	EvidenceIDs        []string            `json:"evidence_ids"`
	VerificationPolicy *VerificationPolicy `json:"verification_policy,omitempty"`
	RequiresApproval   bool                `json:"requires_approval"`
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

type FinalReport struct {
	Summary           string         `json:"summary"`
	RootCause         string         `json:"root_cause"`
	RecommendedAction string         `json:"recommended_action"`
	Verification      string         `json:"verification"`
	ActionReceipt     *ActionReceipt `json:"action_receipt,omitempty"`
	GeneratedAt       time.Time      `json:"generated_at"`
}

type CreateIncidentRequest struct {
	ServiceName  string           `json:"service_name"`
	Severity     string           `json:"severity"`
	AlertSummary string           `json:"alert_summary"`
	PlaybookKey  string           `json:"playbook_key,omitempty"`
	Context      *IncidentContext `json:"context,omitempty"`
}

type ListIncidentsRequest struct {
	ServiceName string `json:"service_name,omitempty"`
	Status      string `json:"status,omitempty"`
	Limit       int    `json:"limit,omitempty"`
}

type ReviewIncidentRequest struct {
	Reviewer string `json:"reviewer"`
	Comment  string `json:"comment"`
}

type UpsertAnalysisRequest struct {
	Evidence       []Evidence   `json:"evidence"`
	Hypotheses     []Hypothesis `json:"hypotheses"`
	ProposedAction *ActionPlan  `json:"proposed_action,omitempty"`
}

type UpsertReportRequest struct {
	Report *FinalReport `json:"report"`
}

type RecordIncidentEventRequest struct {
	EventType string         `json:"event_type"`
	ActorType string         `json:"actor_type"`
	ActorName string         `json:"actor_name"`
	Payload   map[string]any `json:"payload"`
}

type RecordAgentRunRequest struct {
	NodeName      string         `json:"node_name"`
	ModelName     string         `json:"model_name"`
	PromptVersion string         `json:"prompt_version"`
	Input         map[string]any `json:"input"`
	Output        map[string]any `json:"output"`
	LatencyMs     int64          `json:"latency_ms"`
	Status        string         `json:"status"`
	CheckpointID  string         `json:"checkpoint_id"`
}
