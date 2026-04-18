from __future__ import annotations

import os
from abc import ABC, abstractmethod
from datetime import UTC, datetime
from time import perf_counter
from typing import Any

from graphops_orchestrator.agent_schemas import (
    CriticVerdict,
    EvidenceAgentResult,
    PlannerDecision,
    PolicyDecision,
    ReportDecision,
    TriageDecision,
)
from graphops_orchestrator.logic import build_evidence, build_final_report, plan_diagnosis
from graphops_orchestrator.models import ActionPlan, Evidence, FinalReport, Hypothesis, VerificationResult
from graphops_orchestrator.prompts import (
    critic_prompt,
    evidence_prompt,
    planner_prompt,
    policy_prompt,
    report_prompt,
    triage_prompt,
)
from graphops_orchestrator.telemetry import llm_calls_total, llm_duration_seconds, measure_span


class BaseReasoner(ABC):
    provider: str = "unknown"
    model_name: str = "unknown"

    @abstractmethod
    async def triage(self, payload: dict[str, Any]) -> TriageDecision:
        raise NotImplementedError

    @abstractmethod
    async def analyze_evidence(self, source_type: str, payload: dict[str, Any]) -> EvidenceAgentResult:
        raise NotImplementedError

    @abstractmethod
    async def plan(self, payload: dict[str, Any]) -> PlannerDecision:
        raise NotImplementedError

    @abstractmethod
    async def critique(self, payload: dict[str, Any]) -> CriticVerdict:
        raise NotImplementedError

    @abstractmethod
    async def policy(self, payload: dict[str, Any]) -> PolicyDecision:
        raise NotImplementedError

    @abstractmethod
    async def report(self, payload: dict[str, Any]) -> FinalReport:
        raise NotImplementedError


class RuleBasedReasoner(BaseReasoner):
    provider = "rules"
    model_name = "rule-engine"

    async def triage(self, payload: dict[str, Any]) -> TriageDecision:
        summary = str(payload.get("alert_summary", "")).lower()
        if "deploy" in summary or "release" in summary:
            incident_type = "release_regression"
        elif "timeout" in summary or "downstream" in summary:
            incident_type = "downstream_dependency"
        else:
            incident_type = "unknown"
        return TriageDecision(
            workflow="post_release_incident",
            incident_type=incident_type,
            reason="Rule-based triage inferred the incident type from alert keywords.",
        )

    async def analyze_evidence(self, source_type: str, payload: dict[str, Any]) -> EvidenceAgentResult:
        raw_items = payload.get("items", [])
        evidence = build_evidence(source_type, list(raw_items), source_type[:3])
        return EvidenceAgentResult(
            findings=[
                {
                    "source_ref": item.source_ref,
                    "summary": item.summary,
                    "confidence": item.confidence,
                }
                for item in evidence
            ],
            overall_signal=f"Rule-based {source_type} agent normalized {len(evidence)} evidence items.",
        )

    async def plan(self, payload: dict[str, Any]) -> PlannerDecision:
        service_name = payload["service_name"]
        change_evidence = [Evidence.model_validate(item) for item in payload.get("change_evidence", [])]
        log_evidence = [Evidence.model_validate(item) for item in payload.get("log_evidence", [])]
        dependency_evidence = [Evidence.model_validate(item) for item in payload.get("dependency_evidence", [])]
        hypotheses, action = plan_diagnosis(
            service_name,
            change_evidence,
            log_evidence,
            dependency_evidence,
        )
        proposed_action = None
        if action is not None:
            proposed_action = {
                "action_type": action.action_type,
                "target_service": action.target_service,
                "reason": action.reason,
                "evidence_ids": list(action.evidence_ids),
                "requires_approval": action.requires_approval,
            }
        return PlannerDecision(
            hypotheses=[
                {
                    "cause": item.cause,
                    "support_evidence_ids": list(item.support_evidence_ids),
                    "confidence": item.confidence,
                }
                for item in hypotheses
            ],
            proposed_action=proposed_action,
            investigation_notes="Rule-based planner matched evidence against known release-regression and downstream-failure patterns.",
        )

    async def critique(self, payload: dict[str, Any]) -> CriticVerdict:
        proposed_action = payload.get("proposed_action")
        evidence_count = len(payload.get("change_evidence", [])) + len(payload.get("log_evidence", []))
        if proposed_action and evidence_count < 2:
            return CriticVerdict(
                decision="request_replan",
                reason="Rollback was proposed without enough supporting change/log evidence.",
                missing_evidence=["Additional log or change evidence is required before rollback."],
                risk_flags=["rollback_without_enough_evidence"],
            )
        return CriticVerdict(
            decision="approve_plan",
            reason="Rule-based critic accepted the plan.",
        )

    async def policy(self, payload: dict[str, Any]) -> PolicyDecision:
        proposed_action = payload.get("proposed_action")
        if proposed_action is None:
            return PolicyDecision(decision="allow", reason="No write action was proposed.")
        action = ActionPlan.model_validate(proposed_action)
        if action.action_type == "rollback":
            return PolicyDecision(
                decision="require_human_approval" if action.requires_approval else "allow",
                reason="Rollback is treated as a risky write action.",
            )
        return PolicyDecision(decision="deny", reason="Unsupported action type in rule-based policy.")

    async def report(self, payload: dict[str, Any]) -> FinalReport:
        hypotheses = [Hypothesis.model_validate(item) for item in payload.get("hypotheses", [])]
        proposed_action = (
            ActionPlan.model_validate(payload["proposed_action"])
            if payload.get("proposed_action")
            else None
        )
        verification = (
            VerificationResult.model_validate(payload["verification_result"])
            if payload.get("verification_result")
            else None
        )
        report = build_final_report(
            service_name=payload["service_name"],
            hypotheses=hypotheses,
            proposed_action=proposed_action,
            verification_result=verification,
            approval_status=payload.get("approval_status", ""),
        )
        return report.model_copy(update={"generated_at": datetime.now(tz=UTC).isoformat()})


class OllamaReasoner(BaseReasoner):
    provider = "ollama"

    def __init__(
        self,
        *,
        model_name: str | None = None,
        base_url: str | None = None,
        temperature: float = 0.0,
    ) -> None:
        from langchain_ollama import ChatOllama

        resolved_model = model_name or os.getenv("OLLAMA_MODEL", "qwen3:4b")
        resolved_base_url = base_url or os.getenv("OLLAMA_BASE_URL", "http://127.0.0.1:11434")
        self.model_name = resolved_model
        self._llm = ChatOllama(
            model=resolved_model,
            base_url=resolved_base_url,
            temperature=temperature,
            num_predict=1024,
        )

    async def _invoke_structured(self, prompt: str, schema: type[Any], *, agent_name: str) -> Any:
        runnable = self._llm.with_structured_output(schema, method="json_schema")
        started = perf_counter()
        status = "ok"
        try:
            with measure_span("llm_call", agent_name=agent_name, model_name=self.model_name):
                return await runnable.ainvoke(prompt)
        except Exception as exc:
            status = "error"
            raise RuntimeError(
                f"Ollama model '{self.model_name}' failed during structured generation. "
                "Check that the model can run locally, that Ollama has enough memory, "
                "or temporarily switch REASONER_PROVIDER=rules for offline workflow testing."
            ) from exc
        finally:
            llm_calls_total.labels(agent_name, self.model_name, status).inc()
            llm_duration_seconds.labels(agent_name, self.model_name, status).observe(
                perf_counter() - started
            )

    async def triage(self, payload: dict[str, Any]) -> TriageDecision:
        return await self._invoke_structured(triage_prompt(payload), TriageDecision, agent_name="triage_agent")

    async def analyze_evidence(self, source_type: str, payload: dict[str, Any]) -> EvidenceAgentResult:
        return await self._invoke_structured(
            evidence_prompt(f"{source_type.title()} Agent", payload),
            EvidenceAgentResult,
            agent_name=f"{source_type}_agent",
        )

    async def plan(self, payload: dict[str, Any]) -> PlannerDecision:
        return await self._invoke_structured(planner_prompt(payload), PlannerDecision, agent_name="planner_agent")

    async def critique(self, payload: dict[str, Any]) -> CriticVerdict:
        return await self._invoke_structured(critic_prompt(payload), CriticVerdict, agent_name="critic_agent")

    async def policy(self, payload: dict[str, Any]) -> PolicyDecision:
        return await self._invoke_structured(policy_prompt(payload), PolicyDecision, agent_name="policy_agent")

    async def report(self, payload: dict[str, Any]) -> FinalReport:
        report = await self._invoke_structured(report_prompt(payload), ReportDecision, agent_name="report_agent")
        return FinalReport(
            summary=report.summary,
            root_cause=report.root_cause,
            recommended_action=report.recommended_action,
            verification=report.verification,
            generated_at=datetime.now(tz=UTC).isoformat(),
        )


def build_reasoner_from_env() -> BaseReasoner:
    provider = os.getenv("REASONER_PROVIDER", "ollama").lower()
    if provider == "rules":
        return RuleBasedReasoner()
    return OllamaReasoner()
