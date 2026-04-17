from __future__ import annotations

from datetime import UTC, datetime

from graphops_orchestrator.models import ActionPlan, Evidence, FinalReport, Hypothesis, VerificationResult


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
    change_evidence: list[Evidence],
    log_evidence: list[Evidence],
    dependency_evidence: list[Evidence],
) -> tuple[list[Hypothesis], ActionPlan | None]:
    text = " ".join(
        [item.summary.lower() for item in change_evidence + log_evidence + dependency_evidence]
    )

    local_change_signal = any(
        keyword in text
        for keyword in [
            "configuration bundle update",
            "database dsn",
            "invalid connection string",
            "authentication failures",
        ]
    )
    downstream_signal = any(
        keyword in text
        for keyword in [
            "downstream propagation is likely",
            "timeouts when calling inventory-service",
            "saturated database connection pool",
            "inventory-service is degraded",
        ]
    )

    if downstream_signal and not local_change_signal:
        primary_ids = [item.evidence_id for item in dependency_evidence[:2] + log_evidence[:1]]
        return (
            [
                Hypothesis(
                    hypothesis_id="hyp-1",
                    cause="The primary fault is likely in inventory-service and is propagating to the caller.",
                    support_evidence_ids=primary_ids,
                    confidence=0.93,
                ),
                Hypothesis(
                    hypothesis_id="hyp-2",
                    cause="order-api is acting as the symptom carrier instead of the fault owner.",
                    support_evidence_ids=[item.evidence_id for item in log_evidence[:1]],
                    confidence=0.76,
                ),
            ],
            None,
        )

    primary_ids = [item.evidence_id for item in change_evidence[:2] + log_evidence[:2]]
    hypotheses = [
        Hypothesis(
            hypothesis_id="hyp-1",
            cause="The latest release introduced a configuration regression in order-api database connectivity.",
            support_evidence_ids=primary_ids,
            confidence=0.95,
        ),
        Hypothesis(
            hypothesis_id="hyp-2",
            cause="The error spike is local to order-api and started immediately after the release window.",
            support_evidence_ids=[item.evidence_id for item in log_evidence[:2] + dependency_evidence[:1]],
            confidence=0.81,
        ),
    ]
    action = ActionPlan(
        action_type="rollback",
        target_service=service_name,
        reason="Recent change timing and local database errors indicate a release regression; rollback is the safest first action.",
        evidence_ids=primary_ids,
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

    if proposed_action is None:
        recommended_action = "Do not rollback. Escalate to the downstream owner and continue manual investigation."
    elif approval_status == "rejected":
        recommended_action = "Rollback was proposed but rejected during human review."
    else:
        recommended_action = f"Execute {proposed_action.action_type} for {proposed_action.target_service}."

    if verification_result is None:
        verification = "No recovery action was executed. Diagnostic report only."
    elif verification_result.status == "recovered":
        verification = (
            f"Recovered: 5xx dropped to {verification_result.error_rate}% and P95 to "
            f"{verification_result.p95_latency_ms}ms."
        )
    elif verification_result.status == "partial_recovered":
        verification = verification_result.summary
    else:
        verification = f"Not recovered: {verification_result.summary}"

    return FinalReport(
        summary=f"GraphOps completed a diagnostic run for {service_name}.",
        root_cause=primary_cause,
        recommended_action=recommended_action,
        verification=verification,
        generated_at=datetime.now(tz=UTC).isoformat(),
    )
