import asyncio

from langgraph.checkpoint.memory import MemorySaver

from graphops_orchestrator.graph import GraphRunner
from graphops_orchestrator.llm import RuleBasedReasoner
from graphops_orchestrator.models import (
    ActionReceipt,
    FinalReport,
    Incident,
    IncidentContext,
    RollbackResponse,
    ToolItem,
    ToolResponse,
    VerificationPolicy,
    VerificationResult,
)


def incident_context() -> IncidentContext:
    return IncidentContext(
        cluster="prod-cn",
        namespace="checkout",
        environment="production",
        alert_name="OrderApiHigh5xxAfterRelease",
        alert_started_at="2026-04-17T02:02:00Z",
        release_id="deploy-2026.04.17-0155",
        release_version="order-api@2026.04.17-0155",
        previous_version="order-api@2026.04.17-0142",
        labels={"service": "order-api", "team": "payments"},
    )


def downstream_incident_context() -> IncidentContext:
    return IncidentContext(
        cluster="prod-cn",
        namespace="checkout",
        environment="production",
        alert_name="OrderApiTimeoutsToInventory",
        alert_started_at="2026-04-17T03:14:00Z",
        release_id="deploy-2026.04.16-2210",
        release_version="order-api@2026.04.16-2210",
        previous_version="order-api@2026.04.16-2150",
        labels={"service": "order-api", "team": "payments"},
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

    async def list_events(self, incident_id: str) -> list[dict]:
        assert incident_id == self.incident.id
        return self.events

    async def list_agent_runs(self, incident_id: str) -> list[dict]:
        assert incident_id == self.incident.id
        return self.agent_runs

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

    async def query_changes(
        self,
        incident_id: str,
        service_name: str,
        playbook_key: str,
        incident_context: dict,
    ) -> ToolResponse:
        return ToolResponse(items=self.change_items)

    async def query_logs(
        self,
        incident_id: str,
        service_name: str,
        playbook_key: str,
        incident_context: dict,
    ) -> ToolResponse:
        return ToolResponse(items=self.log_items)

    async def query_dependencies(
        self,
        incident_id: str,
        service_name: str,
        playbook_key: str,
        incident_context: dict,
    ) -> ToolResponse:
        return ToolResponse(items=self.dependency_items)

    async def rollback(
        self,
        incident_id: str,
        playbook_key: str,
        incident_context: dict,
        target_service: str,
        current_revision: str,
        target_revision: str,
        risk_level: str,
        idempotency_key: str,
        requested_by: str,
        verification_policy: VerificationPolicy | None,
    ) -> RollbackResponse:
        self.rollback_calls.append(
            {
                "incident_id": incident_id,
                "playbook_key": playbook_key,
                "target_service": target_service,
                "current_revision": current_revision,
                "target_revision": target_revision,
                "risk_level": risk_level,
                "idempotency_key": idempotency_key,
                "requested_by": requested_by,
                "verification_policy": verification_policy.model_dump() if verification_policy else None,
            }
        )
        return RollbackResponse(
            receipt=ActionReceipt(
                receipt_id="receipt-1",
                idempotency_key=idempotency_key,
                action_type="rollback",
                target_service=target_service,
                executor=requested_by,
                from_revision=current_revision,
                to_revision=target_revision,
                status="executed",
                status_detail="Rollback completed in the fake adapter.",
                executed_at="2026-04-17T02:03:00Z",
                verification_status="",
            )
        )

    async def verify(
        self,
        incident_id: str,
        service_name: str,
        playbook_key: str,
        incident_context: dict,
        verification_policy: VerificationPolicy | None,
    ) -> VerificationResult:
        if self.rollback_calls:
            return self.verify_after
        return self.verify_before


class RoutedRuleReasoner(RuleBasedReasoner):
    model_name = "qwen3:4b"

    def model_name_for_agent(self, agent_name: str) -> str:
        if agent_name in {"change_agent", "log_agent", "dependency_agent"}:
            return "qwen3:1.7b"
        return "qwen3:4b"


def recovered_result() -> VerificationResult:
    return VerificationResult(
        status="recovered",
        error_rate=0.3,
        p95_latency_ms=118,
        window_minutes=10,
        query_refs=["promql:error_rate:order-api", "promql:p95_latency:order-api"],
        signal_checks=[],
        decision_basis="2 of 2 checks passed after rollback.",
        summary="Recovered after rollback.",
    )


def unrecovered_result() -> VerificationResult:
    return VerificationResult(
        status="not_recovered",
        error_rate=12.4,
        p95_latency_ms=980,
        window_minutes=10,
        query_refs=["promql:error_rate:order-api", "promql:p95_latency:order-api"],
        signal_checks=[],
        decision_basis="0 of 2 checks passed before rollback.",
        summary="Not healthy before rollback.",
    )


def test_graph_runner_release_scenario_waits_for_approval_then_recovers() -> None:
    incident = Incident(
        id="inc-main",
        service_name="order-api",
        severity="P1",
        alert_summary="5xx spike after deploy",
        playbook_key="release_config_regression",
        context=incident_context(),
        status="created",
    )
    incident_client = FakeIncidentClient(incident)
    ops_client = FakeOpsClient(
        change_items=[
            ToolItem(
                source_ref="deploy/order-api",
                summary="order-api release 2026.04.17-0155 was deployed 8 minutes before the alert and introduced a configuration bundle update.",
                confidence=0.95,
            )
        ],
        log_items=[
            ToolItem(
                source_ref="logs/order-api",
                summary="High-frequency errors show invalid connection string and authentication failures immediately after the new release.",
                confidence=0.97,
            )
        ],
        dependency_items=[
            ToolItem(
                source_ref="dep/order-api->inventory-service",
                summary="No downstream error amplification is visible on inventory-service during the incident window.",
                confidence=0.8,
            )
        ],
        verify_before=unrecovered_result(),
        verify_after=recovered_result(),
    )
    runner = GraphRunner(
        incident_client=incident_client,
        ops_client=ops_client,
        reasoner=RoutedRuleReasoner(),
        checkpointer=MemorySaver(),
    )

    initial = asyncio.run(runner.run(incident.id))

    assert initial.status == "waiting_for_approval"
    assert initial.interrupt is not None
    assert initial.proposed_action is not None
    assert initial.proposed_action.action_type == "rollback"
    assert initial.proposed_action.current_revision == incident.context.release_version
    assert initial.proposed_action.target_revision == incident.context.previous_version
    assert initial.proposed_action.verification_policy is not None
    assert initial.critic_verdict is not None
    assert initial.policy_decision is not None
    assert incident_client.analysis_payload is not None
    assert len(incident_client.agent_runs) >= 6

    resumed = asyncio.run(runner.resume(incident.id, approved=True, reviewer="oncall", comment="rollback"))

    assert resumed.status == "recovered"
    assert resumed.verification_result is not None
    assert resumed.verification_result.status == "recovered"
    assert resumed.action_receipt is not None
    assert resumed.action_receipt.verification_status == "recovered"
    assert len(ops_client.rollback_calls) == 1
    assert incident_client.report_payload is not None
    assert any(event["event_type"] == "approval_reviewed" for event in incident_client.events)
    model_names = {item["node_name"]: item["model_name"] for item in incident_client.agent_runs}
    assert model_names["change_agent"] == "qwen3:1.7b"
    assert model_names["log_agent"] == "qwen3:1.7b"
    assert model_names["dependency_agent"] == "qwen3:1.7b"
    assert model_names["planner_agent"] == "qwen3:4b"
    assert model_names["critic_agent"] == "qwen3:4b"
    assert model_names["policy_agent"] == "qwen3:4b"


def test_graph_runner_inconclusive_release_scenario_stops_without_rollback() -> None:
    incident = Incident(
        id="inc-inconclusive",
        service_name="order-api",
        severity="P2",
        alert_summary="5xx spike after deploy",
        playbook_key="release_config_regression",
        context=incident_context(),
        status="created",
    )
    incident_client = FakeIncidentClient(incident)
    ops_client = FakeOpsClient(
        change_items=[
            ToolItem(
                source_ref="deploy/order-api",
                summary="order-api was deployed in the last hour.",
                confidence=0.7,
            )
        ],
        log_items=[
            ToolItem(
                source_ref="logs/order-api",
                summary="Error volume increased but no database or configuration signatures were confirmed.",
                confidence=0.6,
            )
        ],
        dependency_items=[
            ToolItem(
                source_ref="dep/order-api->inventory-service",
                summary="Dependency health is inconclusive for the incident window.",
                confidence=0.5,
            )
        ],
        verify_before=unrecovered_result(),
        verify_after=recovered_result(),
    )
    runner = GraphRunner(
        incident_client=incident_client,
        ops_client=ops_client,
        reasoner=RuleBasedReasoner(),
        checkpointer=MemorySaver(),
    )

    result = asyncio.run(runner.run(incident.id))

    assert result.status == "diagnosed"
    assert result.interrupt is None
    assert result.proposed_action is None
    assert result.action_receipt is None
    assert result.verification_result is None
    assert result.final_report is not None
    assert "Do not rollback yet" in result.final_report.recommended_action
    assert result.policy_decision is not None
    assert ops_client.rollback_calls == []


def test_graph_runner_downstream_dependency_generates_report_without_rollback() -> None:
    incident = Incident(
        id="inc-downstream",
        service_name="order-api",
        severity="P2",
        alert_summary="timeouts to inventory",
        playbook_key="downstream_inventory_outage",
        context=downstream_incident_context(),
        status="created",
    )
    incident_client = FakeIncidentClient(incident)
    ops_client = FakeOpsClient(
        change_items=[
            ToolItem(
                source_ref="deploy/order-api",
                summary="No relevant order-api change in the last 2 hours.",
                confidence=0.93,
            )
        ],
        log_items=[
            ToolItem(
                source_ref="logs/order-api#inventory-timeouts",
                summary="order-api errors are dominated by timeouts when calling inventory-service.",
                confidence=0.96,
            ),
            ToolItem(
                source_ref="logs/order-api#upstream",
                summary="The top error pattern is upstream timeout rather than local configuration failure.",
                confidence=0.91,
            ),
        ],
        dependency_items=[
            ToolItem(
                source_ref="dep/inventory-service#pool",
                summary="inventory-service is degraded with database pool exhaustion; downstream propagation is likely.",
                confidence=0.97,
            ),
            ToolItem(
                source_ref="dep/inventory-service#db",
                summary="inventory-service depends on a saturated database connection pool.",
                confidence=0.92,
            ),
        ],
        verify_before=unrecovered_result(),
        verify_after=recovered_result(),
    )
    runner = GraphRunner(
        incident_client=incident_client,
        ops_client=ops_client,
        reasoner=RuleBasedReasoner(),
        checkpointer=MemorySaver(),
    )

    result = asyncio.run(runner.run(incident.id))

    assert result.status == "diagnosed"
    assert result.interrupt is None
    assert result.proposed_action is None
    assert result.action_receipt is None
    assert result.verification_result is None
    assert result.final_report is not None
    assert result.triage_decision is not None
    assert result.triage_decision["incident_type"] == "downstream_dependency"
    assert "inventory-service" in result.final_report.root_cause
    assert result.final_report.recommended_action == (
        "Do not rollback. Escalate to the downstream owner and continue manual investigation."
    )
    assert ops_client.rollback_calls == []
