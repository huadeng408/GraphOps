from __future__ import annotations

from datetime import UTC, datetime

from graphops_orchestrator.models import (
    ActionPlan,
    Evidence,
    FinalReport,
    Hypothesis,
    VerificationPolicy,
    VerificationResult,
)


def build_evidence(source_type: str, items: list[dict], prefix: str) -> list[Evidence]:
    evidence: list[Evidence] = []
    for index, item in enumerate(items, start=1):
        evidence.append(
            Evidence(
                evidence_id=f"{prefix}-{index}",
                source_type=source_type,
                source_ref=item["source_ref"],
                summary=item["summary"],
                confidence=item["confidence"],
            )
        )
    return evidence


def plan_diagnosis(
    service_name: str,
    release_version: str,
    previous_version: str,
    change_evidence: list[Evidence],
    log_evidence: list[Evidence],
    dependency_evidence: list[Evidence],
) -> tuple[list[Hypothesis], ActionPlan | None]:
    text = " ".join([item.summary.lower() for item in change_evidence + log_evidence + dependency_evidence])

    release_signals: dict[str, bool] = {
        "recent_release": "release" in text or "deployed" in text,
        "config_change": "configuration bundle update" in text or "database dsn" in text,
        "db_auth_errors": "invalid connection string" in text or "authentication failures" in text,
        "local_blast_radius": "stays local to order-api" in text or "blast radius is currently limited to order-api" in text,
        "dependency_healthy": "no downstream error amplification" in text or "remains healthy" in text,
    }
    downstream_signals: dict[str, bool] = {
        "no_local_change": "no relevant order-api change" in text,
        "inventory_timeouts": "timeouts when calling inventory-service" in text or "upstream timeout" in text,
        "dependency_degraded": "inventory-service is degraded" in text,
        "database_pool_saturated": "saturated database connection pool" in text or "pool exhaustion" in text,
    }
    release_score = sum(1 for matched in release_signals.values() if matched)
    downstream_score = sum(1 for matched in downstream_signals.values() if matched)
    primary_ids = [item.evidence_id for item in change_evidence[:2] + log_evidence[:2]]

    if downstream_score >= 3 and downstream_score > release_score:
        hypotheses = [
            Hypothesis(
                hypothesis_id="hyp-1",
                cause="The primary fault is likely in inventory-service and is propagating to the caller.",
                support_evidence_ids=[item.evidence_id for item in log_evidence[:2] + dependency_evidence[:2]],
                confidence=min(0.99, 0.68 + downstream_score * 0.07),
            ),
            Hypothesis(
                hypothesis_id="hyp-2",
                cause="order-api is acting as the symptom carrier instead of the fault owner.",
                support_evidence_ids=[item.evidence_id for item in change_evidence[:1] + log_evidence[:1] + dependency_evidence[:2]],
                confidence=min(0.96, 0.62 + downstream_score * 0.06),
            ),
        ]
        return hypotheses, None

    hypotheses = [
        Hypothesis(
            hypothesis_id="hyp-1",
            cause="The latest release introduced a configuration regression in order-api database connectivity.",
            support_evidence_ids=primary_ids,
            confidence=min(0.99, 0.65 + release_score * 0.06),
        ),
        Hypothesis(
            hypothesis_id="hyp-2",
            cause="The error spike is local to order-api and started immediately after the release window.",
            support_evidence_ids=[item.evidence_id for item in log_evidence[:2] + dependency_evidence[:2]],
            confidence=min(0.95, 0.55 + release_score * 0.05),
        ),
    ]

    if release_score < 4:
        return hypotheses, None

    action = ActionPlan(
        action_type="rollback",
        target_service=service_name,
        current_revision=release_version,
        target_revision=previous_version,
        reason="Recent change timing and local database errors indicate a release regression; rollback is the safest first action.",
        risk_level="high",
        evidence_ids=primary_ids,
        verification_policy=VerificationPolicy(
            window_minutes=10,
            max_error_rate=1.0,
            max_p95_latency_ms=300,
            minimum_passing_signals=2,
        ),
        requires_approval=True,
    )
    return hypotheses, action


def build_final_report(
    service_name: str,
    hypotheses: list[Hypothesis],
    proposed_action: ActionPlan | None,
    verification_result: VerificationResult | None,
    approval_status: str,
) -> FinalReport:
    primary_cause = hypotheses[0].cause if hypotheses else "Insufficient evidence to determine root cause."
    anomaly_summary: list[str] = []
    handling_suggestions: list[str] = []
    metrics = []
    release_comparisons = []
    anomalies = []

    if proposed_action is None:
        if "inventory-service" in primary_cause.lower():
            recommended_action = "Do not rollback. Escalate to the downstream owner and continue manual investigation."
        else:
            recommended_action = "Do not rollback yet. Gather more release and runtime evidence before taking write actions."
    elif approval_status == "rejected":
        recommended_action = "Rollback was proposed but rejected during human review."
    else:
        recommended_action = f"Execute rollback for {proposed_action.target_service}."

    if verification_result is None:
        verification = "No rollback was executed. Diagnostic report only."
    elif verification_result.status == "recovered":
        verification = (
            f"Recovered: 5xx dropped to {verification_result.error_rate}% and P95 to "
            f"{verification_result.p95_latency_ms}ms. {verification_result.decision_basis}"
        )
    elif verification_result.status == "partial_recovered":
        verification = f"Partially recovered: {verification_result.summary}"
    else:
        verification = f"Not recovered: {verification_result.summary}"

    if verification_result is not None:
        metrics = list(verification_result.metrics)
        release_comparisons = list(verification_result.release_comparisons)
        anomalies = list(verification_result.anomalies)
        anomaly_summary = [f"[{item.severity}] {item.description}" for item in anomalies]
        seen_suggestions: set[str] = set()
        for item in anomalies:
            suggestion = item.handling_suggestion.strip()
            if suggestion and suggestion not in seen_suggestions:
                seen_suggestions.add(suggestion)
                handling_suggestions.append(suggestion)

    return FinalReport(
        summary=f"GraphOps completed a diagnostic run for {service_name}.",
        root_cause=primary_cause,
        recommended_action=recommended_action,
        verification=verification,
        anomaly_summary=anomaly_summary,
        handling_suggestions=handling_suggestions,
        metrics=metrics,
        release_comparisons=release_comparisons,
        anomalies=anomalies,
        generated_at=datetime.now(tz=UTC).isoformat(),
    )
