from __future__ import annotations

import asyncio
import json
import os
import re
import subprocess
from abc import ABC, abstractmethod
from datetime import UTC, datetime
from time import perf_counter
from typing import Any

from pydantic import ValidationError

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

PARALLEL_AGENT_NAMES = frozenset({"change_agent", "log_agent", "dependency_agent"})


def select_ollama_model_for_agent(agent_name: str, main_model: str, parallel_model: str) -> str:
    if agent_name in PARALLEL_AGENT_NAMES:
        return parallel_model
    return main_model


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

    def model_name_for_agent(self, agent_name: str) -> str:
        return self.model_name


class RuleBasedReasoner(BaseReasoner):
    provider = "rules"
    model_name = "rule-engine"

    async def triage(self, payload: dict[str, Any]) -> TriageDecision:
        summary = str(payload.get("alert_summary", "")).lower()
        release_version = str(payload.get("incident_context", {}).get("release_version", "")).lower()
        previous_version = str(payload.get("incident_context", {}).get("previous_version", "")).lower()
        if "inventory" in summary and "timeout" in summary:
            incident_type = "downstream_dependency"
        elif ("deploy" in summary or "release" in summary or "config" in summary) and release_version and previous_version:
            incident_type = "release_regression"
        else:
            incident_type = "unknown"
        if incident_type == "release_regression":
            workflow = "post_release_incident"
        elif incident_type == "downstream_dependency":
            workflow = "dependency_investigation"
        else:
            workflow = "observe_only"
        return TriageDecision(
            workflow=workflow,
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
        incident_context = payload.get("incident_context") or {}
        change_evidence = [Evidence.model_validate(item) for item in payload.get("change_evidence", [])]
        log_evidence = [Evidence.model_validate(item) for item in payload.get("log_evidence", [])]
        dependency_evidence = [Evidence.model_validate(item) for item in payload.get("dependency_evidence", [])]
        hypotheses, action = plan_diagnosis(
            service_name,
            str(incident_context.get("release_version", "")),
            str(incident_context.get("previous_version", "")),
            change_evidence,
            log_evidence,
            dependency_evidence,
        )
        proposed_action = None
        if action is not None:
            proposed_action = {
                "action_type": action.action_type,
                "target_service": action.target_service,
                "current_revision": action.current_revision,
                "target_revision": action.target_revision,
                "reason": action.reason,
                "risk_level": action.risk_level,
                "evidence_ids": list(action.evidence_ids),
                "verification_policy": (
                    action.verification_policy.model_dump() if action.verification_policy is not None else None
                ),
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
            investigation_notes="Rule-based planner scored release timing, config change evidence, local error signatures, and dependency isolation before recommending rollback.",
        )

    async def critique(self, payload: dict[str, Any]) -> CriticVerdict:
        proposed_action = payload.get("proposed_action")
        evidence_count = len(payload.get("change_evidence", [])) + len(payload.get("log_evidence", []))
        if proposed_action and evidence_count < 3:
            return CriticVerdict(
                decision="request_replan",
                reason="Rollback was proposed without enough supporting release and log evidence.",
                missing_evidence=["At least three strong change/log evidence items are required before rollback."],
                risk_flags=["rollback_without_enough_evidence"],
            )
        if proposed_action and (
            not proposed_action.get("current_revision")
            or not proposed_action.get("target_revision")
            or not proposed_action.get("verification_policy")
        ):
            return CriticVerdict(
                decision="request_replan",
                reason="Rollback plan is missing revision or verification details.",
                missing_evidence=["Rollback plans must name both revisions and a verification policy."],
                risk_flags=["rollback_plan_incomplete"],
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
                decision="require_human_approval" if action.requires_approval or action.risk_level == "high" else "allow",
                reason="Rollback is treated as a risky write action and must carry human approval when confidence is high enough to act.",
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
        parallel_model_name: str | None = None,
        base_url: str | None = None,
        temperature: float = 0.0,
        num_ctx: int | None = None,
        parallel_num_ctx: int | None = None,
        num_predict: int | None = None,
    ) -> None:
        resolved_model = model_name or os.getenv("OLLAMA_MAIN_MODEL") or os.getenv("OLLAMA_MODEL", "qwen3:4b")
        resolved_parallel_model = parallel_model_name or os.getenv("OLLAMA_PARALLEL_MODEL", "qwen3:1.7b")
        resolved_base_url = base_url or os.getenv("OLLAMA_BASE_URL", "http://127.0.0.1:11434")
        resolved_num_ctx = num_ctx or int(os.getenv("OLLAMA_NUM_CTX", "8192"))
        resolved_parallel_num_ctx = parallel_num_ctx or int(
            os.getenv("OLLAMA_PARALLEL_NUM_CTX", str(resolved_num_ctx))
        )
        resolved_num_predict = num_predict or int(os.getenv("OLLAMA_NUM_PREDICT", "1024"))
        self.model_name = resolved_model
        self.parallel_model_name = resolved_parallel_model
        self.base_url = resolved_base_url.rstrip("/")
        self.num_ctx = resolved_num_ctx
        self.parallel_num_ctx = resolved_parallel_num_ctx
        self.temperature = temperature
        self.num_predict = resolved_num_predict
        self.curl_bin = os.getenv("OLLAMA_CURL_BIN", "curl.exe" if os.name == "nt" else "curl")

    def model_name_for_agent(self, agent_name: str) -> str:
        return select_ollama_model_for_agent(agent_name, self.model_name, self.parallel_model_name)

    def _num_ctx_for_agent(self, agent_name: str) -> int:
        if agent_name in PARALLEL_AGENT_NAMES:
            return self.parallel_num_ctx
        return self.num_ctx

    async def _invoke_structured(self, prompt: str, schema: type[Any], *, agent_name: str) -> Any:
        model_name = self.model_name_for_agent(agent_name)
        num_ctx = self._num_ctx_for_agent(agent_name)
        started = perf_counter()
        status = "ok"
        try:
            with measure_span("llm_call", agent_name=agent_name, model_name=model_name):
                response = await asyncio.to_thread(
                    self._call_ollama_api,
                    model_name=model_name,
                    prompt=prompt,
                    schema=schema,
                    num_ctx=num_ctx,
                )
                content = self._extract_message_content(response)
                return self._parse_structured_content(schema, content)
        except Exception as exc:
            status = "error"
            raise RuntimeError(
                f"Ollama model '{model_name}' failed during structured generation. "
                "Check that the model can run locally, that Ollama has enough memory, "
                "or temporarily switch REASONER_PROVIDER=rules for offline workflow testing. "
                f"Configured num_ctx={num_ctx}."
            ) from exc
        finally:
            llm_calls_total.labels(agent_name, model_name, status).inc()
            llm_duration_seconds.labels(agent_name, model_name, status).observe(
                perf_counter() - started
            )

    def _call_ollama_api(self, *, model_name: str, prompt: str, schema: type[Any], num_ctx: int) -> dict[str, Any]:
        payload = json.dumps(
            {
                "model": model_name,
                "messages": [{"role": "user", "content": prompt}],
                "stream": False,
                "think": False,
                "format": schema.model_json_schema(),
                "options": {
                    "temperature": self.temperature,
                    "num_ctx": num_ctx,
                    "num_predict": self.num_predict,
                },
            },
            ensure_ascii=False,
        )
        cmd = [
            self.curl_bin,
            "-s",
            f"{self.base_url}/api/chat",
            "-H",
            "Content-Type: application/json",
            "-d",
            payload,
        ]
        proc = subprocess.run(
            cmd,
            capture_output=True,
            text=True,
            encoding="utf-8",
            errors="ignore",
        )
        if proc.returncode != 0:
            raise RuntimeError(proc.stderr.strip() or "curl invocation failed")
        if not proc.stdout.strip():
            raise RuntimeError("empty response from Ollama API")
        response = json.loads(proc.stdout)
        if response.get("error"):
            raise RuntimeError(str(response["error"]))
        return response

    def _extract_message_content(self, response: dict[str, Any]) -> str:
        message = response.get("message") or {}
        content = message.get("content")
        if not isinstance(content, str) or not content.strip():
            raise RuntimeError("structured response did not contain assistant content")
        return content.strip()

    def _parse_structured_content(self, schema: type[Any], content: str) -> Any:
        try:
            return schema.model_validate_json(content)
        except ValidationError:
            match = re.search(r"\{.*\}", content, flags=re.DOTALL)
            if match:
                return schema.model_validate_json(match.group(0))
            raise

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
