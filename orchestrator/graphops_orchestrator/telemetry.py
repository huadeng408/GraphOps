from __future__ import annotations

from contextlib import contextmanager
from time import perf_counter

from opentelemetry import trace
from opentelemetry.sdk.trace import TracerProvider
from prometheus_client import Counter, Histogram

if type(trace.get_tracer_provider()).__name__ == "ProxyTracerProvider":
    trace.set_tracer_provider(TracerProvider())

tracer = trace.get_tracer("graphops.orchestrator")

graph_node_duration_seconds = Histogram(
    "graph_node_duration_seconds",
    "Latency of graph node execution.",
    labelnames=("node", "status"),
)
tool_call_duration_seconds = Histogram(
    "tool_call_duration_seconds",
    "Latency of external tool and action calls.",
    labelnames=("tool", "status"),
)
approval_wait_duration_seconds = Histogram(
    "approval_wait_duration_seconds",
    "Time spent waiting for human approval.",
)
graph_interrupts_total = Counter(
    "graph_interrupts_total",
    "Total graph interrupts raised for human approval.",
    labelnames=("node",),
)
graph_replans_total = Counter(
    "graph_replans_total",
    "Total replan requests issued by the critic agent.",
    labelnames=("reason",),
)
evidence_items_total = Counter(
    "evidence_items_total",
    "Total structured evidence items produced by evidence agents.",
    labelnames=("source_type",),
)
incident_runs_total = Counter(
    "incident_runs_total",
    "Total incident runs by final status.",
    labelnames=("status", "scenario_type"),
)
llm_calls_total = Counter(
    "llm_calls_total",
    "Total LLM calls by agent and model.",
    labelnames=("agent", "model", "status"),
)
llm_duration_seconds = Histogram(
    "llm_duration_seconds",
    "LLM call latency by agent and model.",
    labelnames=("agent", "model", "status"),
)
audit_write_failures_total = Counter(
    "audit_write_failures_total",
    "Total audit writes that failed but were downgraded to best effort.",
    labelnames=("record_type",),
)


@contextmanager
def measure_span(span_name: str, **attributes: str):
    with tracer.start_as_current_span(span_name) as span:
        for key, value in attributes.items():
            span.set_attribute(key, value)
        yield span


@contextmanager
def measure_duration(metric: Histogram, *labels: str):
    started = perf_counter()
    status = "ok"
    try:
        yield
    except Exception:
        status = "error"
        raise
    finally:
        metric.labels(*labels, status).observe(perf_counter() - started)
