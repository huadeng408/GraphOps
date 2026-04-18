from __future__ import annotations

import time
from typing import Any, TypedDict

from langgraph.checkpoint.memory import MemorySaver
from langgraph.graph import END, START, StateGraph
from langgraph.types import Command, interrupt
from pydantic import BaseModel

from graphops_orchestrator.clients import IncidentAPIClient, OpsGatewayClient
from graphops_orchestrator.llm import BaseReasoner, build_reasoner_from_env
from graphops_orchestrator.logic import build_evidence
from graphops_orchestrator.models import (
    ActionPlan,
    ActionReceipt,
    Evidence,
    FinalReport,
    Hypothesis,
    RunResponse,
    VerificationResult,
)
from graphops_orchestrator.prompts import PROMPT_VERSIONS
from graphops_orchestrator.telemetry import (
    approval_wait_duration_seconds,
    graph_node_duration_seconds,
    incident_runs_total,
    measure_span,
)


class GraphState(TypedDict, total=False):
    incident_id: str
    service_name: str
    severity: str
    alert_summary: str
    scenario_key: str
    triage_decision: dict[str, Any] | None
    change_evidence: list[dict[str, Any]]
    log_evidence: list[dict[str, Any]]
    dependency_evidence: list[dict[str, Any]]
    hypotheses: list[dict[str, Any]]
    proposed_action: dict[str, Any] | None
    planner_notes: str
    planner_feedback: str
    critic_verdict: dict[str, Any] | None
    policy_decision: dict[str, Any] | None
    replan_count: int
    approval_status: str
    approval_reviewer: str
    action_receipt: dict[str, Any] | None
    verification_result: dict[str, Any] | None
    final_report: dict[str, Any] | None


class GraphRunner:
    def __init__(
        self,
        incident_api_url: str | None = None,
        ops_gateway_url: str | None = None,
        *,
        incident_client: Any | None = None,
        ops_client: Any | None = None,
        checkpointer: Any | None = None,
        reasoner: BaseReasoner | None = None,
        runtime_coordinator: Any | None = None,
    ) -> None:
        if incident_client is None:
            if incident_api_url is None:
                raise ValueError("incident_api_url is required when incident_client is not provided")
            incident_client = IncidentAPIClient(incident_api_url)
        if ops_client is None:
            if ops_gateway_url is None:
                raise ValueError("ops_gateway_url is required when ops_client is not provided")
            ops_client = OpsGatewayClient(ops_gateway_url)

        self.incident_client = incident_client
        self.ops_client = ops_client
        self.reasoner = reasoner or build_reasoner_from_env()
        self.runtime_coordinator = runtime_coordinator
        self.graph = self._build_graph(checkpointer or MemorySaver())

    async def run(self, incident_id: str) -> RunResponse:
        async with self._hold_run_lock(incident_id):
            config = self._config(incident_id)
            result = await self.graph.ainvoke({"incident_id": incident_id}, config)
            snapshot = await self.graph.aget_state(config)
            return self._to_run_response(incident_id, result, snapshot.values, snapshot.interrupts)

    async def resume(self, incident_id: str, approved: bool, reviewer: str, comment: str) -> RunResponse:
        async with self._hold_run_lock(incident_id):
            if approved:
                await self.incident_client.approve(incident_id, reviewer, comment)
            else:
                await self.incident_client.reject(incident_id, reviewer, comment)

            await self._record_event(
                incident_id,
                event_type="approval_reviewed",
                actor_type="human",
                actor_name=reviewer,
                payload={"approved": approved, "comment": comment},
            )

            config = self._config(incident_id)
            result = await self.graph.ainvoke(
                Command(resume={"approved": approved, "reviewer": reviewer, "comment": comment}),
                config,
            )
            snapshot = await self.graph.aget_state(config)
            return self._to_run_response(incident_id, result, snapshot.values, snapshot.interrupts)

    def _config(self, incident_id: str) -> dict[str, Any]:
        return {"configurable": {"thread_id": incident_id}}

    def _scenario_type(self, values: dict[str, Any]) -> str:
        triage_decision = values.get("triage_decision") or {}
        return triage_decision.get("incident_type") or values.get("scenario_key", "unknown")

    def _record_run_metric(self, values: dict[str, Any], interrupt_payload: dict[str, Any] | None) -> str:
        status = determine_status(values, interrupt_payload)
        incident_runs_total.labels(status, self._scenario_type(values)).inc()
        return status

    def _hold_run_lock(self, incident_id: str):
        if self.runtime_coordinator is None:
            return _NullAsyncContextManager()
        return self.runtime_coordinator.hold_run_lock(incident_id)

    async def _record_event(
        self,
        incident_id: str,
        *,
        event_type: str,
        actor_type: str,
        actor_name: str,
        payload: dict[str, Any] | None = None,
    ) -> None:
        try:
            await self.incident_client.add_event(
                incident_id,
                event_type=event_type,
                actor_type=actor_type,
                actor_name=actor_name,
                payload=self._to_jsonable(payload or {}),
            )
        except Exception:
            # Audit must not break the incident workflow.
            return

    async def _record_agent_run(
        self,
        incident_id: str,
        *,
        node_name: str,
        prompt_version: str,
        status: str,
        latency_ms: int,
        input_payload: dict[str, Any],
        output_payload: dict[str, Any],
    ) -> None:
        try:
            await self.incident_client.add_agent_run(
                incident_id,
                node_name=node_name,
                model_name=self.reasoner.model_name,
                prompt_version=prompt_version,
                status=status,
                latency_ms=latency_ms,
                checkpoint_id=incident_id,
                input_payload=self._to_jsonable(input_payload),
                output_payload=self._to_jsonable(output_payload),
            )
        except Exception:
            return

    async def _run_agent(
        self,
        incident_id: str,
        *,
        node_name: str,
        prompt_version: str,
        input_payload: dict[str, Any],
        fn,
    ) -> Any:
        started = time.perf_counter()
        await self._record_event(
            incident_id,
            event_type="agent_started",
            actor_type="agent",
            actor_name=node_name,
            payload={"prompt_version": prompt_version},
        )
        try:
            with measure_span("graph_node", node_name=node_name):
                output = await fn()
            latency_ms = int((time.perf_counter() - started) * 1000)
            graph_node_duration_seconds.labels(node_name, "ok").observe(time.perf_counter() - started)
            await self._record_agent_run(
                incident_id,
                node_name=node_name,
                prompt_version=prompt_version,
                status="completed",
                latency_ms=latency_ms,
                input_payload=input_payload,
                output_payload=self._to_jsonable(output),
            )
            await self._record_event(
                incident_id,
                event_type="agent_completed",
                actor_type="agent",
                actor_name=node_name,
                payload={"latency_ms": latency_ms},
            )
            return output
        except Exception as exc:
            latency_ms = int((time.perf_counter() - started) * 1000)
            graph_node_duration_seconds.labels(node_name, "error").observe(time.perf_counter() - started)
            await self._record_agent_run(
                incident_id,
                node_name=node_name,
                prompt_version=prompt_version,
                status="failed",
                latency_ms=latency_ms,
                input_payload=input_payload,
                output_payload={"error": str(exc)},
            )
            await self._record_event(
                incident_id,
                event_type="agent_failed",
                actor_type="agent",
                actor_name=node_name,
                payload={"latency_ms": latency_ms, "error": str(exc)},
            )
            raise

    def _to_jsonable(self, value: Any) -> Any:
        if isinstance(value, BaseModel):
            return value.model_dump(mode="json")
        if isinstance(value, list):
            return [self._to_jsonable(item) for item in value]
        if isinstance(value, tuple):
            return [self._to_jsonable(item) for item in value]
        if isinstance(value, dict):
            return {str(key): self._to_jsonable(item) for key, item in value.items()}
        return value

    def _build_graph(self, checkpointer: Any):
        builder = StateGraph(GraphState)

        async def load_incident(state: GraphState) -> GraphState:
            incident = await self.incident_client.get_incident(state["incident_id"])
            await self._record_event(
                state["incident_id"],
                event_type="incident_loaded",
                actor_type="system",
                actor_name="load_incident",
                payload={"scenario_key": incident.scenario_key},
            )
            return {
                "service_name": incident.service_name,
                "severity": incident.severity,
                "alert_summary": incident.alert_summary,
                "scenario_key": incident.scenario_key,
                "replan_count": 0,
            }

        async def triage(state: GraphState) -> GraphState:
            payload = {
                "incident_id": state["incident_id"],
                "service_name": state["service_name"],
                "severity": state["severity"],
                "alert_summary": state["alert_summary"],
                "scenario_key": state["scenario_key"],
            }
            decision = await self._run_agent(
                state["incident_id"],
                node_name="triage_agent",
                prompt_version=PROMPT_VERSIONS["triage_agent"],
                input_payload=payload,
                fn=lambda: self.reasoner.triage(payload),
            )
            return {"triage_decision": self._to_jsonable(decision)}

        async def change_agent(state: GraphState) -> GraphState:
            response = await self.ops_client.query_changes(
                state["incident_id"], state["service_name"], state["scenario_key"]
            )
            payload = {
                "service_name": state["service_name"],
                "alert_summary": state["alert_summary"],
                "scenario_key": state["scenario_key"],
                "triage_decision": state.get("triage_decision"),
                "items": [item.model_dump() for item in response.items],
            }
            result = await self._run_agent(
                state["incident_id"],
                node_name="change_agent",
                prompt_version=PROMPT_VERSIONS["change_agent"],
                input_payload=payload,
                fn=lambda: self.reasoner.analyze_evidence("change", payload),
            )
            evidence = build_evidence(
                "change",
                [item.model_dump() for item in result.findings],
                "chg",
            )
            return {"change_evidence": [item.model_dump() for item in evidence]}

        async def log_agent(state: GraphState) -> GraphState:
            response = await self.ops_client.query_logs(
                state["incident_id"], state["service_name"], state["scenario_key"]
            )
            payload = {
                "service_name": state["service_name"],
                "alert_summary": state["alert_summary"],
                "scenario_key": state["scenario_key"],
                "triage_decision": state.get("triage_decision"),
                "items": [item.model_dump() for item in response.items],
            }
            result = await self._run_agent(
                state["incident_id"],
                node_name="log_agent",
                prompt_version=PROMPT_VERSIONS["log_agent"],
                input_payload=payload,
                fn=lambda: self.reasoner.analyze_evidence("log", payload),
            )
            evidence = build_evidence(
                "log",
                [item.model_dump() for item in result.findings],
                "log",
            )
            return {"log_evidence": [item.model_dump() for item in evidence]}

        async def dependency_agent(state: GraphState) -> GraphState:
            response = await self.ops_client.query_dependencies(
                state["incident_id"], state["service_name"], state["scenario_key"]
            )
            payload = {
                "service_name": state["service_name"],
                "alert_summary": state["alert_summary"],
                "scenario_key": state["scenario_key"],
                "triage_decision": state.get("triage_decision"),
                "items": [item.model_dump() for item in response.items],
            }
            result = await self._run_agent(
                state["incident_id"],
                node_name="dependency_agent",
                prompt_version=PROMPT_VERSIONS["dependency_agent"],
                input_payload=payload,
                fn=lambda: self.reasoner.analyze_evidence("dependency", payload),
            )
            evidence = build_evidence(
                "dependency",
                [item.model_dump() for item in result.findings],
                "dep",
            )
            return {"dependency_evidence": [item.model_dump() for item in evidence]}

        async def planner_agent(state: GraphState) -> GraphState:
            payload = {
                "incident_id": state["incident_id"],
                "service_name": state["service_name"],
                "severity": state["severity"],
                "triage_decision": state.get("triage_decision"),
                "change_evidence": state.get("change_evidence", []),
                "log_evidence": state.get("log_evidence", []),
                "dependency_evidence": state.get("dependency_evidence", []),
                "planner_feedback": state.get("planner_feedback", ""),
                "replan_count": state.get("replan_count", 0),
            }
            decision = await self._run_agent(
                state["incident_id"],
                node_name="planner_agent",
                prompt_version=PROMPT_VERSIONS["planner_agent"],
                input_payload=payload,
                fn=lambda: self.reasoner.plan(payload),
            )
            hypotheses = [
                Hypothesis(
                    hypothesis_id=f"hyp-{index}",
                    cause=item.cause,
                    support_evidence_ids=list(item.support_evidence_ids),
                    confidence=item.confidence,
                )
                for index, item in enumerate(decision.hypotheses, start=1)
            ]
            action = None
            if decision.proposed_action and decision.proposed_action.action_type != "none":
                action = ActionPlan(
                    action_type=decision.proposed_action.action_type,
                    target_service=decision.proposed_action.target_service,
                    reason=decision.proposed_action.reason,
                    evidence_ids=list(decision.proposed_action.evidence_ids),
                    requires_approval=decision.proposed_action.requires_approval,
                )
            evidence_payload = [
                item
                for item in [
                    *state.get("change_evidence", []),
                    *state.get("log_evidence", []),
                    *state.get("dependency_evidence", []),
                ]
            ]
            await self.incident_client.save_analysis(
                state["incident_id"],
                evidence_payload,
                [item.model_dump() for item in hypotheses],
                action.model_dump() if action else None,
            )
            return {
                "hypotheses": [item.model_dump() for item in hypotheses],
                "proposed_action": action.model_dump() if action else None,
                "planner_notes": decision.investigation_notes,
                "planner_feedback": "",
            }

        async def critic_agent(state: GraphState) -> GraphState:
            payload = {
                "service_name": state["service_name"],
                "severity": state["severity"],
                "triage_decision": state.get("triage_decision"),
                "change_evidence": state.get("change_evidence", []),
                "log_evidence": state.get("log_evidence", []),
                "dependency_evidence": state.get("dependency_evidence", []),
                "hypotheses": state.get("hypotheses", []),
                "proposed_action": state.get("proposed_action"),
                "planner_notes": state.get("planner_notes", ""),
                "replan_count": state.get("replan_count", 0),
            }
            verdict = await self._run_agent(
                state["incident_id"],
                node_name="critic_agent",
                prompt_version=PROMPT_VERSIONS["critic_agent"],
                input_payload=payload,
                fn=lambda: self.reasoner.critique(payload),
            )
            next_replan = state.get("replan_count", 0)
            if verdict.decision == "request_replan":
                next_replan += 1
            return {
                "critic_verdict": verdict.model_dump(),
                "planner_feedback": verdict.reason,
                "replan_count": next_replan,
            }

        async def policy_agent(state: GraphState) -> GraphState:
            payload = {
                "service_name": state["service_name"],
                "severity": state["severity"],
                "hypotheses": state.get("hypotheses", []),
                "proposed_action": state.get("proposed_action"),
                "critic_verdict": state.get("critic_verdict"),
                "replan_count": state.get("replan_count", 0),
            }
            decision = await self._run_agent(
                state["incident_id"],
                node_name="policy_agent",
                prompt_version=PROMPT_VERSIONS["policy_agent"],
                input_payload=payload,
                fn=lambda: self.reasoner.policy(payload),
            )
            return {"policy_decision": decision.model_dump()}

        async def approval_gate(state: GraphState) -> GraphState:
            started = time.perf_counter()
            decision = interrupt(
                {
                    "incident_id": state["incident_id"],
                    "service_name": state["service_name"],
                    "proposed_action": state["proposed_action"],
                    "policy_decision": state.get("policy_decision"),
                }
            )
            approval_wait_duration_seconds.observe(time.perf_counter() - started)
            return {
                "approval_status": "approved" if decision["approved"] else "rejected",
                "approval_reviewer": decision["reviewer"],
            }

        async def rollback_action(state: GraphState) -> GraphState:
            action = ActionPlan.model_validate(state["proposed_action"])
            reviewer = state.get("approval_reviewer", "system")
            idempotency_key = f"{state['incident_id']}:{action.action_type}:{action.target_service}"
            await self._record_event(
                state["incident_id"],
                event_type="action_requested",
                actor_type="system",
                actor_name="rollback_action",
                payload={"idempotency_key": idempotency_key},
            )
            response = await self.ops_client.rollback(
                incident_id=state["incident_id"],
                scenario_key=state["scenario_key"],
                target_service=action.target_service,
                idempotency_key=idempotency_key,
                requested_by=reviewer,
            )
            receipt = response.receipt.model_copy(update={"verification_status": "pending"})
            return {"action_receipt": receipt.model_dump()}

        async def verify_recovery(state: GraphState) -> GraphState:
            result = await self.ops_client.verify(
                state["incident_id"], state["service_name"], state["scenario_key"]
            )
            receipt_payload = state.get("action_receipt")
            if receipt_payload:
                receipt = ActionReceipt.model_validate(receipt_payload)
                receipt = receipt.model_copy(update={"verification_status": result.status})
                receipt_payload = receipt.model_dump()
            return {
                "action_receipt": receipt_payload,
                "verification_result": result.model_dump(),
            }

        async def finalize_report(state: GraphState) -> GraphState:
            payload = {
                "service_name": state["service_name"],
                "severity": state["severity"],
                "triage_decision": state.get("triage_decision"),
                "hypotheses": state.get("hypotheses", []),
                "proposed_action": state.get("proposed_action"),
                "verification_result": state.get("verification_result"),
                "approval_status": state.get("approval_status", ""),
                "policy_decision": state.get("policy_decision"),
                "critic_verdict": state.get("critic_verdict"),
            }
            report = await self._run_agent(
                state["incident_id"],
                node_name="report_agent",
                prompt_version=PROMPT_VERSIONS["report_agent"],
                input_payload=payload,
                fn=lambda: self.reasoner.report(payload),
            )
            if state.get("action_receipt"):
                report = report.model_copy(
                    update={"action_receipt": ActionReceipt.model_validate(state["action_receipt"])}
                )
            await self.incident_client.save_report(state["incident_id"], report)
            await self._record_event(
                state["incident_id"],
                event_type="report_saved",
                actor_type="system",
                actor_name="finalize_report",
                payload={"status": state.get("approval_status", "")},
            )
            return {"final_report": report.model_dump(mode="json")}

        builder.add_node("load_incident", load_incident)
        builder.add_node("triage", triage)
        builder.add_node("change_agent", change_agent)
        builder.add_node("log_agent", log_agent)
        builder.add_node("dependency_agent", dependency_agent)
        builder.add_node("planner_agent", planner_agent)
        builder.add_node("critic_agent", critic_agent)
        builder.add_node("policy_agent", policy_agent)
        builder.add_node("approval_gate", approval_gate)
        builder.add_node("rollback_action", rollback_action)
        builder.add_node("verify_recovery", verify_recovery)
        builder.add_node("finalize_report", finalize_report)

        builder.add_edge(START, "load_incident")
        builder.add_edge("load_incident", "triage")
        builder.add_edge("triage", "change_agent")
        builder.add_edge("triage", "log_agent")
        builder.add_edge("triage", "dependency_agent")
        builder.add_edge(["change_agent", "log_agent", "dependency_agent"], "planner_agent")
        builder.add_edge("planner_agent", "critic_agent")
        builder.add_conditional_edges(
            "critic_agent",
            route_after_critic,
            {
                "planner_agent": "planner_agent",
                "policy_agent": "policy_agent",
            },
        )
        builder.add_conditional_edges(
            "policy_agent",
            route_after_policy,
            {
                "approval_gate": "approval_gate",
                "rollback_action": "rollback_action",
                "finalize_report": "finalize_report",
            },
        )
        builder.add_conditional_edges(
            "approval_gate",
            route_after_approval,
            {
                "rollback_action": "rollback_action",
                "finalize_report": "finalize_report",
            },
        )
        builder.add_edge("rollback_action", "verify_recovery")
        builder.add_edge("verify_recovery", "finalize_report")
        builder.add_edge("finalize_report", END)

        return builder.compile(checkpointer=checkpointer)

    def _to_run_response(
        self,
        incident_id: str,
        result: dict[str, Any],
        values: dict[str, Any],
        interrupts: tuple[Any, ...],
    ) -> RunResponse:
        interrupt_payload = None
        if interrupts:
            interrupt_payload = interrupts[0].value
        elif "__interrupt__" in result:
            interrupt_payload = result["__interrupt__"][0].value

        return RunResponse(
            incident_id=incident_id,
            status=self._record_run_metric(values, interrupt_payload),
            hypotheses=[Hypothesis.model_validate(item) for item in values.get("hypotheses", [])],
            proposed_action=(
                ActionPlan.model_validate(values["proposed_action"])
                if values.get("proposed_action")
                else None
            ),
            action_receipt=(
                ActionReceipt.model_validate(values["action_receipt"])
                if values.get("action_receipt")
                else None
            ),
            verification_result=(
                VerificationResult.model_validate(values["verification_result"])
                if values.get("verification_result")
                else None
            ),
            final_report=(
                FinalReport.model_validate(values["final_report"])
                if values.get("final_report")
                else None
            ),
            triage_decision=values.get("triage_decision"),
            critic_verdict=values.get("critic_verdict"),
            policy_decision=values.get("policy_decision"),
            interrupt=interrupt_payload,
        )


class _NullAsyncContextManager:
    async def __aenter__(self):
        return None

    async def __aexit__(self, exc_type, exc, tb):
        return False
def route_after_critic(state: GraphState) -> str:
    verdict = state.get("critic_verdict") or {}
    if verdict.get("decision") == "request_replan" and state.get("replan_count", 0) <= 1:
        return "planner_agent"
    return "policy_agent"


def route_after_policy(state: GraphState) -> str:
    decision = state.get("policy_decision") or {}
    if state.get("proposed_action") is None or decision.get("decision") == "deny":
        return "finalize_report"
    if decision.get("decision") == "allow":
        return "rollback_action"
    return "approval_gate"


def route_after_approval(state: GraphState) -> str:
    if state.get("approval_status") == "approved":
        return "rollback_action"
    return "finalize_report"


def determine_status(values: dict[str, Any], interrupt_payload: dict[str, Any] | None) -> str:
    if interrupt_payload is not None:
        return "waiting_for_approval"
    if values.get("final_report"):
        return "completed"
    return "running"
