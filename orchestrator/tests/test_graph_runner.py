import asyncio

from langgraph.checkpoint.memory import MemorySaver

from graphops_orchestrator.graph import GraphRunner
from graphops_orchestrator.llm import RuleBasedReasoner
from graphops_orchestrator.models import (
    ActionReceipt,
    FinalReport,
    Incident,
    RollbackResponse,
    ToolItem,
    ToolResponse,
    VerificationResult,
)


class FakeIncidentClient:
    def __init__(self, incident: Incident) -> None:
        self.incident = incident
        self.analysis_payload: dict | None = None
        self.report_payload: FinalReport | None = None
        self.approval_status: str | None = None
        self.reviewer: str | None = None
        self.events: list[dict] = []
        self.agent_runs: list[dict] = []

    async def get_incident(self, incident_id: str) -> Incident:
        assert incident_id == self.incident.id
        return self.incident

    async def approve(self, incident_id: str, reviewer: str, comment: str) -> dict:
        assert incident_id == self.incident.id
        self.approval_status = "approved"
        self.reviewer = reviewer
        return {"status": "approved", "reviewer": reviewer, "comment": comment}

    async def reject(self, incident_id: str, reviewer: str, comment: str) -> dict:
        assert incident_id == self.incident.id
        self.approval_status = "rejected"
        self.reviewer = reviewer
        return {"status": "rejected", "reviewer": reviewer, "comment": comment}

    async def save_analysis(
        self,
        incident_id: str,
        evidence: list[dict],
        hypotheses: list[dict],
        proposed_action: dict | None,
    ) -> dict:
        assert incident_id == self.incident.id
        self.analysis_payload = {
            "evidence": evidence,
            "hypotheses": hypotheses,
            "proposed_action": proposed_action,
        }
        return self.analysis_payload

    async def save_report(self, incident_id: str, report: FinalReport) -> dict:
        assert incident_id == self.incident.id
        self.report_payload = report
        return {"report": report.model_dump(mode="json")}

    async def add_event(
        self,
        incident_id: str,
        *,
        event_type: str,
        actor_type: str,
        actor_name: str,
        payload: dict | None = None,
    ) -> dict:
        assert incident_id == self.incident.id
        item = {
            "event_type": event_type,
            "actor_type": actor_type,
            "actor_name": actor_name,
            "payload": payload or {},
        }
        self.events.append(item)
        return item

    async def add_agent_run(
        self,
        incident_id: str,
        *,
        node_name: str,
        model_name: str,
        prompt_version: str,
        status: str,
        latency_ms: int,
        checkpoint_id: str,
        input_payload: dict | None = None,
        output_payload: dict | None = None,
    ) -> dict:
        assert incident_id == self.incident.id
        item = {
            "node_name": node_name,
            "model_name": model_name,
            "prompt_version": prompt_version,
            "status": status,
            "latency_ms": latency_ms,
            "checkpoint_id": checkpoint_id,
            "input": input_payload or {},
            "output": output_payload or {},
        }
        self.agent_runs.append(item)
        return item


class FakeOpsClient:
    def __init__(
        self,
        *,
        change_items: list[ToolItem],
        log_items: list[ToolItem],
        dependency_items: list[ToolItem],
        verify_before: VerificationResult,
        verify_after: VerificationResult,
    ) -> None:
        self.change_items = change_items
        self.log_items = log_items
        self.dependency_items = dependency_items
        self.verify_before = verify_before
        self.verify_after = verify_after
        self.rollback_calls: list[dict] = []

    async def query_changes(self, incident_id: str, service_name: str, scenario_key: str) -> ToolResponse:
        return ToolResponse(items=self.change_items)

    async def query_logs(self, incident_id: str, service_name: str, scenario_key: str) -> ToolResponse:
        return ToolResponse(items=self.log_items)

    async def query_dependencies(self, incident_id: str, service_name: str, scenario_key: str) -> ToolResponse:
        return ToolResponse(items=self.dependency_items)

    async def rollback(
        self,
        incident_id: str,
        scenario_key: str,
        target_service: str,
        idempotency_key: str,
        requested_by: str,
    ) -> RollbackResponse:
        self.rollback_calls.append(
            {
                "incident_id": incident_id,
                "scenario_key": scenario_key,
                "target_service": target_service,
                "idempotency_key": idempotency_key,
                "requested_by": requested_by,
            }
        )
        return RollbackResponse(
            receipt=ActionReceipt(
                receipt_id="receipt-1",
                idempotency_key=idempotency_key,
                action_type="rollback",
                target_service=target_service,
                status="executed",
                executed_at="2026-04-17T02:03:00Z",
                verification_status="",
            )
        )

    async def verify(self, incident_id: str, service_name: str, scenario_key: str) -> VerificationResult:
        if self.rollback_calls:
            return self.verify_after
        return self.verify_before


def test_graph_runner_main_scenario_waits_for_approval_then_recovers() -> None:
    incident = Incident(
        id="inc-main",
        service_name="order-api",
        severity="P1",
        alert_summary="5xx spike after deploy",
        scenario_key="release_config_regression",
        status="created",
    )
    incident_client = FakeIncidentClient(incident)
    ops_client = FakeOpsClient(
        change_items=[
            ToolItem(
                source_ref="deploy/order-api",
                summary="order-api released with a configuration bundle update and database DSN change.",
                confidence=0.95,
            )
        ],
        log_items=[
            ToolItem(
                source_ref="logs/order-api",
                summary="High-frequency errors show invalid connection string and authentication failures.",
                confidence=0.97,
            )
        ],
        dependency_items=[
            ToolItem(
                source_ref="dep/order-api->inventory-service",
                summary="No downstream error amplification detected.",
                confidence=0.8,
            )
        ],
        verify_before=VerificationResult(
            status="not_recovered",
            error_rate=12.4,
            p95_latency_ms=980,
            summary="Not healthy before rollback.",
        ),
        verify_after=VerificationResult(
            status="recovered",
            error_rate=0.3,
            p95_latency_ms=118,
            summary="Recovered after rollback.",
        ),
    )
    runner = GraphRunner(
        incident_client=incident_client,
        ops_client=ops_client,
        reasoner=RuleBasedReasoner(),
        checkpointer=MemorySaver(),
    )

    initial = asyncio.run(runner.run(incident.id))

    assert initial.status == "waiting_for_approval"
    assert initial.interrupt is not None
    assert initial.proposed_action is not None
    assert initial.proposed_action.action_type == "rollback"
    assert initial.critic_verdict is not None
    assert initial.policy_decision is not None
    assert incident_client.analysis_payload is not None
    assert len(incident_client.agent_runs) >= 6

    resumed = asyncio.run(runner.resume(incident.id, approved=True, reviewer="oncall", comment="rollback"))

    assert resumed.status == "completed"
    assert resumed.verification_result is not None
    assert resumed.verification_result.status == "recovered"
    assert resumed.action_receipt is not None
    assert resumed.action_receipt.verification_status == "recovered"
    assert len(ops_client.rollback_calls) == 1
    assert incident_client.report_payload is not None
    assert any(event["event_type"] == "approval_reviewed" for event in incident_client.events)


def test_graph_runner_secondary_scenario_finishes_without_rollback() -> None:
    incident = Incident(
        id="inc-secondary",
        service_name="order-api",
        severity="P1",
        alert_summary="timeouts to inventory",
        scenario_key="downstream_inventory_outage",
        status="created",
    )
    incident_client = FakeIncidentClient(incident)
    ops_client = FakeOpsClient(
        change_items=[
            ToolItem(
                source_ref="deploy/order-api",
                summary="No relevant order-api change in the last 2 hours.",
                confidence=0.88,
            )
        ],
        log_items=[
            ToolItem(
                source_ref="logs/order-api",
                summary="order-api errors are dominated by timeouts when calling inventory-service.",
                confidence=0.95,
            )
        ],
        dependency_items=[
            ToolItem(
                source_ref="dep/order-api->inventory-service",
                summary="inventory-service is degraded with database pool exhaustion; downstream propagation is likely.",
                confidence=0.96,
            )
        ],
        verify_before=VerificationResult(
            status="not_recovered",
            error_rate=8.9,
            p95_latency_ms=760,
            summary="Still unhealthy.",
        ),
        verify_after=VerificationResult(
            status="not_recovered",
            error_rate=8.8,
            p95_latency_ms=750,
            summary="Rollback would not help.",
        ),
    )
    runner = GraphRunner(
        incident_client=incident_client,
        ops_client=ops_client,
        reasoner=RuleBasedReasoner(),
        checkpointer=MemorySaver(),
    )

    result = asyncio.run(runner.run(incident.id))

    assert result.status == "completed"
    assert result.interrupt is None
    assert result.proposed_action is None
    assert result.action_receipt is None
    assert result.verification_result is None
    assert result.final_report is not None
    assert "Do not rollback" in result.final_report.recommended_action
    assert result.policy_decision is not None
    assert ops_client.rollback_calls == []
