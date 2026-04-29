from __future__ import annotations

from typing import Literal

from pydantic import BaseModel, Field


class TriageDecision(BaseModel):
    workflow: Literal["post_release_incident", "dependency_investigation", "observe_only"] = "post_release_incident"
    incident_type: Literal["release_regression", "downstream_dependency", "unknown"] = "unknown"
    reason: str


class AgentEvidenceItem(BaseModel):
    source_ref: str
    summary: str
    confidence: float = Field(ge=0.0, le=1.0)


class EvidenceAgentResult(BaseModel):
    findings: list[AgentEvidenceItem] = Field(default_factory=list)
    overall_signal: str = ""


class PlannerHypothesis(BaseModel):
    cause: str
    support_evidence_ids: list[str] = Field(default_factory=list)
    confidence: float = Field(ge=0.0, le=1.0)


class PlannerAction(BaseModel):
    action_type: Literal["rollback", "none"]
    target_service: str
    current_revision: str
    target_revision: str
    reason: str
    risk_level: Literal["medium", "high"] = "high"
    evidence_ids: list[str] = Field(default_factory=list)
    verification_policy: dict | None = None
    requires_approval: bool = False


class PlannerDecision(BaseModel):
    hypotheses: list[PlannerHypothesis] = Field(default_factory=list)
    proposed_action: PlannerAction | None = None
    investigation_notes: str = ""


class CriticVerdict(BaseModel):
    decision: Literal["approve_plan", "request_replan"] = "approve_plan"
    reason: str
    missing_evidence: list[str] = Field(default_factory=list)
    risk_flags: list[str] = Field(default_factory=list)


class PolicyDecision(BaseModel):
    decision: Literal["allow", "require_human_approval", "deny"] = "allow"
    reason: str


class ReportDecision(BaseModel):
    summary: str
    root_cause: str
    recommended_action: str
    verification: str
