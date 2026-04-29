from __future__ import annotations

from time import perf_counter
from typing import Any

import httpx

from graphops_orchestrator.models import (
    FinalReport,
    Incident,
    RollbackResponse,
    ToolResponse,
    VerificationPolicy,
    VerificationResult,
)
from graphops_orchestrator.telemetry import measure_span, tool_call_duration_seconds


class IncidentAPIClient:
    def __init__(self, base_url: str) -> None:
        self.base_url = base_url.rstrip("/")

    async def get_incident(self, incident_id: str) -> Incident:
        payload = await self._request("GET", f"/incidents/{incident_id}")
        return Incident.model_validate(payload)

    async def list_events(self, incident_id: str) -> list[dict[str, Any]]:
        payload = await self._request("GET", f"/incidents/{incident_id}/events")
        return list(payload.get("items", []))

    async def list_agent_runs(self, incident_id: str) -> list[dict[str, Any]]:
        payload = await self._request("GET", f"/incidents/{incident_id}/agent-runs")
        return list(payload.get("items", []))

    async def approve(self, incident_id: str, reviewer: str, comment: str) -> dict[str, Any]:
        return await self._request(
            "POST",
            f"/incidents/{incident_id}/approve",
            json={"reviewer": reviewer, "comment": comment},
        )

    async def reject(self, incident_id: str, reviewer: str, comment: str) -> dict[str, Any]:
        return await self._request(
            "POST",
            f"/incidents/{incident_id}/reject",
            json={"reviewer": reviewer, "comment": comment},
        )

    async def save_analysis(
        self,
        incident_id: str,
        evidence: list[dict[str, Any]],
        hypotheses: list[dict[str, Any]],
        proposed_action: dict[str, Any] | None,
    ) -> dict[str, Any]:
        return await self._request(
            "POST",
            f"/internal/incidents/{incident_id}/analysis",
            json={
                "evidence": evidence,
                "hypotheses": hypotheses,
                "proposed_action": proposed_action,
            },
        )

    async def save_report(self, incident_id: str, report: FinalReport) -> dict[str, Any]:
        return await self._request(
            "POST",
            f"/internal/incidents/{incident_id}/report",
            json={"report": report.model_dump(mode="json")},
        )

    async def add_event(
        self,
        incident_id: str,
        *,
        event_type: str,
        actor_type: str,
        actor_name: str,
        payload: dict[str, Any] | None = None,
    ) -> dict[str, Any]:
        return await self._request(
            "POST",
            f"/internal/incidents/{incident_id}/events",
            json={
                "event_type": event_type,
                "actor_type": actor_type,
                "actor_name": actor_name,
                "payload": payload or {},
            },
        )

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
        input_payload: dict[str, Any] | None = None,
        output_payload: dict[str, Any] | None = None,
    ) -> dict[str, Any]:
        return await self._request(
            "POST",
            f"/internal/incidents/{incident_id}/agent-runs",
            json={
                "node_name": node_name,
                "model_name": model_name,
                "prompt_version": prompt_version,
                "status": status,
                "latency_ms": latency_ms,
                "checkpoint_id": checkpoint_id,
                "input": input_payload or {},
                "output": output_payload or {},
            },
        )

    async def _request(self, method: str, path: str, json: dict[str, Any] | None = None) -> dict[str, Any]:
        async with httpx.AsyncClient(base_url=self.base_url, timeout=10.0, trust_env=False) as client:
            response = await client.request(method, path, json=json)
            response.raise_for_status()
            return response.json()


class OpsGatewayClient:
    def __init__(self, base_url: str) -> None:
        self.base_url = base_url.rstrip("/")

    async def query_changes(
        self,
        incident_id: str,
        service_name: str,
        playbook_key: str,
        incident_context: dict[str, Any],
    ) -> ToolResponse:
        return await self._query_tool(
            "changes",
            "/tools/changes/query",
            incident_id,
            service_name,
            playbook_key,
            incident_context,
        )

    async def query_logs(
        self,
        incident_id: str,
        service_name: str,
        playbook_key: str,
        incident_context: dict[str, Any],
    ) -> ToolResponse:
        return await self._query_tool(
            "logs",
            "/tools/logs/query",
            incident_id,
            service_name,
            playbook_key,
            incident_context,
        )

    async def query_dependencies(
        self,
        incident_id: str,
        service_name: str,
        playbook_key: str,
        incident_context: dict[str, Any],
    ) -> ToolResponse:
        return await self._query_tool(
            "dependency",
            "/tools/dependency/query",
            incident_id,
            service_name,
            playbook_key,
            incident_context,
        )

    async def rollback(
        self,
        incident_id: str,
        playbook_key: str,
        incident_context: dict[str, Any],
        target_service: str,
        current_revision: str,
        target_revision: str,
        risk_level: str,
        idempotency_key: str,
        requested_by: str,
        verification_policy: VerificationPolicy | None,
    ) -> RollbackResponse:
        payload = await self._request_with_metrics(
            tool_name="rollback",
            method="POST",
            path="/actions/rollback",
            json={
                "incident_id": incident_id,
                "playbook_key": playbook_key,
                "incident_context": incident_context,
                "target_service": target_service,
                "current_revision": current_revision,
                "target_revision": target_revision,
                "risk_level": risk_level,
                "idempotency_key": idempotency_key,
                "requested_by": requested_by,
                "verification_policy": verification_policy.model_dump() if verification_policy else None,
            },
        )
        return RollbackResponse.model_validate(payload)

    async def verify(
        self,
        incident_id: str,
        service_name: str,
        playbook_key: str,
        incident_context: dict[str, Any],
        verification_policy: VerificationPolicy | None,
    ) -> VerificationResult:
        payload = await self._request_with_metrics(
            tool_name="verify",
            method="POST",
            path="/actions/verify",
            json={
                "incident_id": incident_id,
                "service_name": service_name,
                "playbook_key": playbook_key,
                "incident_context": incident_context,
                "verification_policy": verification_policy.model_dump() if verification_policy else None,
            },
        )
        return VerificationResult.model_validate(payload)

    async def _query_tool(
        self,
        tool_name: str,
        path: str,
        incident_id: str,
        service_name: str,
        playbook_key: str,
        incident_context: dict[str, Any],
    ) -> ToolResponse:
        payload = await self._request_with_metrics(
            tool_name=tool_name,
            method="POST",
            path=path,
            json={
                "incident_id": incident_id,
                "service_name": service_name,
                "playbook_key": playbook_key,
                "incident_context": incident_context,
                "time_window_minutes": 120,
            },
        )
        return ToolResponse.model_validate(payload)

    async def _request_with_metrics(
        self,
        *,
        tool_name: str,
        method: str,
        path: str,
        json: dict[str, Any],
    ) -> dict[str, Any]:
        started = perf_counter()
        status = "ok"
        with measure_span("tool_call", tool_name=tool_name, method=method, path=path):
            try:
                return await self._request(method, path, json=json)
            except Exception:
                status = "error"
                raise
            finally:
                tool_call_duration_seconds.labels(tool_name, status).observe(perf_counter() - started)

    async def _request(self, method: str, path: str, json: dict[str, Any]) -> dict[str, Any]:
        async with httpx.AsyncClient(base_url=self.base_url, timeout=10.0, trust_env=False) as client:
            response = await client.request(method, path, json=json)
            response.raise_for_status()
            return response.json()
