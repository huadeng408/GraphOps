from __future__ import annotations

import os
from contextlib import asynccontextmanager

import httpx
from fastapi import FastAPI, HTTPException
from prometheus_client import make_asgi_app

from graphops_orchestrator.checkpointer import build_checkpointer_context
from graphops_orchestrator.graph import GraphRunner
from graphops_orchestrator.models import ResumeRequest, RunResponse
from graphops_orchestrator.redis_runtime import RedisRuntimeCoordinator, RunLockError


def create_app() -> FastAPI:
    incident_api_url = os.getenv("INCIDENT_API_URL", "http://127.0.0.1:8082")
    ops_gateway_url = os.getenv("OPS_GATEWAY_URL", "http://127.0.0.1:8085")

    @asynccontextmanager
    async def lifespan(app: FastAPI):
        coordinator = await RedisRuntimeCoordinator.from_env()
        async with build_checkpointer_context() as checkpointer:
            app.state.runner = GraphRunner(
                incident_api_url=incident_api_url,
                ops_gateway_url=ops_gateway_url,
                checkpointer=checkpointer,
                runtime_coordinator=coordinator,
            )
            yield
        if coordinator is not None:
            await coordinator.close()

    app = FastAPI(title="GraphOps Orchestrator", version="0.2.0", lifespan=lifespan)
    app.mount("/metrics", make_asgi_app())

    @app.get("/healthz")
    async def healthz() -> dict[str, str]:
        return {"status": "ok"}

    @app.post("/runs/incidents/{incident_id}", response_model=RunResponse)
    async def run_incident(incident_id: str) -> RunResponse:
        try:
            return await app.state.runner.run(incident_id)
        except RunLockError as exc:
            raise HTTPException(status_code=409, detail=str(exc)) from exc
        except httpx.HTTPStatusError as exc:
            raise HTTPException(status_code=exc.response.status_code, detail=exc.response.text) from exc

    @app.post("/runs/incidents/{incident_id}/resume", response_model=RunResponse)
    async def resume_incident(incident_id: str, request: ResumeRequest) -> RunResponse:
        try:
            return await app.state.runner.resume(
                incident_id=incident_id,
                approved=request.approved,
                reviewer=request.reviewer,
                comment=request.comment,
            )
        except RunLockError as exc:
            raise HTTPException(status_code=409, detail=str(exc)) from exc
        except httpx.HTTPStatusError as exc:
            raise HTTPException(status_code=exc.response.status_code, detail=exc.response.text) from exc

    return app


app = create_app()
