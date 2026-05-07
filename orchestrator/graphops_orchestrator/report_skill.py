from __future__ import annotations

from abc import ABC, abstractmethod
from datetime import UTC, datetime
from typing import Any

from graphops_orchestrator.agent_schemas import ReportDecision
from graphops_orchestrator.logic import build_final_report
from graphops_orchestrator.models import ActionPlan, FinalReport, Hypothesis, VerificationResult
from graphops_orchestrator.prompts import report_prompt


class BaseReportSkill(ABC):
    @abstractmethod
    async def generate(self, payload: dict[str, Any]) -> FinalReport:
        raise NotImplementedError


class RuleBasedReportSkill(BaseReportSkill):
    async def generate(self, payload: dict[str, Any]) -> FinalReport:
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


class LLMReportSkill(BaseReportSkill):
    def __init__(self, invoke_structured) -> None:
        self._invoke_structured = invoke_structured

    async def generate(self, payload: dict[str, Any]) -> FinalReport:
        report = await self._invoke_structured(report_prompt(payload), ReportDecision, agent_name="report_agent")
        return FinalReport(
            summary=report.summary,
            root_cause=report.root_cause,
            recommended_action=report.recommended_action,
            verification=report.verification,
            generated_at=datetime.now(tz=UTC).isoformat(),
        )
