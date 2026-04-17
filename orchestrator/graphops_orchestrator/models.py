from __future__ import annotations

from pydantic import BaseModel, Field


class Incident(BaseModel):
    id: str
    service_name: str
    severity: str
    alert_summary: str
    scenario_key: str
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


class ActionPlan(BaseModel):
    action_type: str
    target_service: str
    reason: str
    evidence_ids: list[str] = Field(default_factory=list)
    requires_approval: bool


class ActionReceipt(BaseModel):
    receipt_id: str
    idempotency_key: str
    action_type: str
    target_service: str
    status: str
    executed_at: str
    verification_status: str = ""


class VerificationResult(BaseModel):
    status: str
    error_rate: float
    p95_latency_ms: int
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
