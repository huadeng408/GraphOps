import asyncio

from langgraph.checkpoint.sqlite.aio import AsyncSqliteSaver

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
        labels={"service": "order-api"},
    )


class PersistenceIncidentClient:
    def __init__(self, incident: Incident) -> None:
        self.incident = incident
        self.analysis_payload: dict | None = None
        self.report_payload: FinalReport | None = None

    async def get_incident(self, incident_id: str) -> Incident:
        return self.incident

    async def list_events(self, incident_id: str) -> list[dict]:
        return []

    async def list_agent_runs(self, incident_id: str) -> list[dict]:
        return []

    async def approve(self, incident_id: str, reviewer: str, comment: str) -> dict:
        return {"status": "approved", "reviewer": reviewer, "comment": comment}

    async def reject(self, incident_id: str, reviewer: str, comment: str) -> dict:
        return {"status": "rejected", "reviewer": reviewer, "comment": comment}

    async def save_analysis(
        self,
        incident_id: str,
        evidence: list[dict],
        hypotheses: list[dict],
        proposed_action: dict | None,
    ) -> dict:
        self.analysis_payload = {
            "evidence": evidence,
            "hypotheses": hypotheses,
            "proposed_action": proposed_action,
        }
        return self.analysis_payload

    async def save_report(self, incident_id: str, report: FinalReport) -> dict:
        self.report_payload = report
        return {"report": report.model_dump(mode="json")}

    async def add_event(self, incident_id: str, **kwargs) -> dict:
        return {"status": "ok"}

    async def add_agent_run(self, incident_id: str, **kwargs) -> dict:
        return {"status": "ok"}


class PersistenceOpsClient:
    def __init__(self) -> None:
        self.rollback_calls: list[dict] = []

    async def query_changes(
        self,
        incident_id: str,
        service_name: str,
        playbook_key: str,
        incident_context: dict,
    ) -> ToolResponse:
        return ToolResponse(
            items=[
                ToolItem(
                    source_ref="deploy/order-api",
                    summary="order-api release 2026.04.17-0155 was deployed 8 minutes before the alert and introduced a configuration bundle update.",
                    confidence=0.95,
                )
            ]
        )

    async def query_logs(
        self,
        incident_id: str,
        service_name: str,
        playbook_key: str,
        incident_context: dict,
    ) -> ToolResponse:
        return ToolResponse(
            items=[
                ToolItem(
                    source_ref="logs/order-api",
                    summary="High-frequency errors show invalid connection string and authentication failures immediately after the new release.",
                    confidence=0.97,
                )
            ]
        )

    async def query_dependencies(
        self,
        incident_id: str,
        service_name: str,
        playbook_key: str,
        incident_context: dict,
    ) -> ToolResponse:
        return ToolResponse(
            items=[
                ToolItem(
                    source_ref="dep/order-api->inventory-service",
                    summary="No downstream error amplification is visible on inventory-service during the incident window.",
                    confidence=0.8,
                )
            ]
        )

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
        self.rollback_calls.append({"idempotency_key": idempotency_key, "requested_by": requested_by})
        return RollbackResponse(
            receipt=ActionReceipt(
                receipt_id="receipt-persist",
                idempotency_key=idempotency_key,
                action_type="rollback",
                target_service=target_service,
                executor=requested_by,
                from_revision=current_revision,
                to_revision=target_revision,
                status="executed",
                status_detail="Rollback completed in the persistence fake adapter.",
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
        return VerificationResult(
            status="recovered",
            error_rate=0.2,
            p95_latency_ms=101,
            window_minutes=10,
            query_refs=["promql:error_rate:order-api", "promql:p95_latency:order-api"],
            signal_checks=[],
            decision_basis="2 of 2 recovery checks passed.",
            summary="Recovered after rollback.",
        )


def test_resume_survives_runner_restart(tmp_path) -> None:
    async def scenario() -> tuple[object, PersistenceOpsClient]:
        db_path = tmp_path / "langgraph.sqlite"
        incident = Incident(
            id="inc-persist",
            service_name="order-api",
            severity="P1",
            alert_summary="5xx spike after deploy",
            playbook_key="release_config_regression",
            context=incident_context(),
            status="created",
        )
        incident_client = PersistenceIncidentClient(incident)
        ops_client = PersistenceOpsClient()

        async with AsyncSqliteSaver.from_conn_string(str(db_path)) as saver:
            runner = GraphRunner(
                incident_client=incident_client,
                ops_client=ops_client,
                reasoner=RuleBasedReasoner(),
                checkpointer=saver,
            )
            initial = await runner.run(incident.id)
            assert initial.status == "waiting_for_approval"

        async with AsyncSqliteSaver.from_conn_string(str(db_path)) as saver:
            runner = GraphRunner(
                incident_client=incident_client,
                ops_client=ops_client,
                reasoner=RuleBasedReasoner(),
                checkpointer=saver,
            )
            resumed = await runner.resume(incident.id, approved=True, reviewer="oncall", comment="resume")
        return resumed, ops_client

    resumed, ops_client = asyncio.run(scenario())

    assert resumed.status == "recovered"
    assert resumed.verification_result is not None
    assert resumed.verification_result.status == "recovered"
    assert len(ops_client.rollback_calls) == 1
