from __future__ import annotations

from pydantic import BaseModel, Field


class IncidentContext(BaseModel):
    cluster: str
    namespace: str
    environment: str
    alert_name: str
    alert_started_at: str
    release_id: str
    release_version: str
    previous_version: str
    labels: dict[str, str] = Field(default_factory=dict)


class Incident(BaseModel):
    id: str
    service_name: str
    severity: str
    alert_summary: str
    playbook_key: str = ""
    context: IncidentContext | None = None
    status: str


class Evidence(BaseModel):
    evidence_id: str
    source_type: str
    source_ref: str
    summary: str
    confidence: float


class Hypothesis(BaseModel):
    hypothesis_id: str
    cause: str
    support_evidence_ids: list[str] = Field(default_factory=list)
    confidence: float


class VerificationPolicy(BaseModel):
    window_minutes: int
    max_error_rate: float
    max_p95_latency_ms: int
    minimum_passing_signals: int


class ActionPlan(BaseModel):
    action_type: str
    target_service: str
    current_revision: str
    target_revision: str
    reason: str
    risk_level: str
    evidence_ids: list[str] = Field(default_factory=list)
    verification_policy: VerificationPolicy | None = None
    requires_approval: bool


class ActionReceipt(BaseModel):
    receipt_id: str
    idempotency_key: str
    action_type: str
    target_service: str
    executor: str
    from_revision: str
    to_revision: str
    status: str
    status_detail: str
    executed_at: str
    verification_status: str = ""


class SignalCheck(BaseModel):
    name: str
    query_ref: str
    observed_value: float
    threshold: str
    passed: bool
    summary: str


class VerificationResult(BaseModel):
    status: str
    error_rate: float
    p95_latency_ms: int
    window_minutes: int
    query_refs: list[str] = Field(default_factory=list)
    signal_checks: list[SignalCheck] = Field(default_factory=list)
    decision_basis: str
    summary: str


class FinalReport(BaseModel):
    summary: str
    root_cause: str
    recommended_action: str
    verification: str
    action_receipt: ActionReceipt | None = None
    generated_at: str = ""


class ToolItem(BaseModel):
    source_ref: str
    summary: str
    confidence: float


class ToolResponse(BaseModel):
    items: list[ToolItem]


class RollbackResponse(BaseModel):
    receipt: ActionReceipt


class RunResponse(BaseModel):
    incident_id: str
    status: str
    hypotheses: list[Hypothesis] = Field(default_factory=list)
    proposed_action: ActionPlan | None = None
    action_receipt: ActionReceipt | None = None
    verification_result: VerificationResult | None = None
    final_report: FinalReport | None = None
    triage_decision: dict | None = None
    critic_verdict: dict | None = None
    policy_decision: dict | None = None
    interrupt: dict | None = None


class ResumeRequest(BaseModel):
    approved: bool
    reviewer: str
    comment: str = ""
