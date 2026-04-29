from graphops_orchestrator.logic import plan_diagnosis
from graphops_orchestrator.models import Evidence


def test_plan_diagnosis_recommends_rollback_for_release_regression() -> None:
    changes = [
        Evidence(
            evidence_id="chg-1",
            source_type="change",
            source_ref="deploy/order-api",
            summary="order-api released with a configuration bundle update and database DSN change.",
            confidence=0.95,
        )
    ]
    logs = [
        Evidence(
            evidence_id="log-1",
            source_type="log",
            source_ref="logs/order-api",
            summary="High-frequency errors show invalid connection string and authentication failures.",
            confidence=0.97,
        )
    ]
    dependencies = [
        Evidence(
            evidence_id="dep-1",
            source_type="dependency",
            source_ref="dep/order-api->inventory-service",
            summary="No downstream error amplification detected.",
            confidence=0.8,
        )
    ]

    hypotheses, action = plan_diagnosis(
        "order-api",
        "order-api@2026.04.17-0155",
        "order-api@2026.04.17-0142",
        changes,
        logs,
        dependencies,
    )

    assert hypotheses[0].cause.startswith("The latest release introduced")
    assert action is not None
    assert action.action_type == "rollback"
    assert action.current_revision == "order-api@2026.04.17-0155"
    assert action.target_revision == "order-api@2026.04.17-0142"
    assert action.verification_policy is not None


def test_plan_diagnosis_skips_rollback_when_release_evidence_is_inconclusive() -> None:
    changes = [
        Evidence(
            evidence_id="chg-1",
            source_type="change",
            source_ref="deploy/order-api",
            summary="order-api was deployed earlier in the day, but no configuration changes were confirmed.",
            confidence=0.88,
        )
    ]
    logs = [
        Evidence(
            evidence_id="log-1",
            source_type="log",
            source_ref="logs/order-api",
            summary="order-api error volume increased, but log patterns do not confirm local database or config failures.",
            confidence=0.95,
        )
    ]
    dependencies = [
        Evidence(
            evidence_id="dep-1",
            source_type="dependency",
            source_ref="dep/order-api->inventory-service",
            summary="Dependency health is inconclusive for the incident window.",
            confidence=0.96,
        )
    ]

    hypotheses, action = plan_diagnosis(
        "order-api",
        "order-api@2026.04.17-0155",
        "order-api@2026.04.17-0142",
        changes,
        logs,
        dependencies,
    )

    assert hypotheses[0].cause.startswith("The latest release introduced")
    assert action is None


def test_plan_diagnosis_identifies_downstream_dependency_without_rollback() -> None:
    changes = [
        Evidence(
            evidence_id="chg-1",
            source_type="change",
            source_ref="deploy/order-api",
            summary="No relevant order-api change in the last 2 hours.",
            confidence=0.93,
        )
    ]
    logs = [
        Evidence(
            evidence_id="log-1",
            source_type="log",
            source_ref="logs/order-api",
            summary="order-api errors are dominated by timeouts when calling inventory-service.",
            confidence=0.96,
        ),
        Evidence(
            evidence_id="log-2",
            source_type="log",
            source_ref="logs/order-api",
            summary="The top error pattern is upstream timeout rather than local configuration failure.",
            confidence=0.91,
        ),
    ]
    dependencies = [
        Evidence(
            evidence_id="dep-1",
            source_type="dependency",
            source_ref="dep/inventory-service",
            summary="inventory-service is degraded with database pool exhaustion; downstream propagation is likely.",
            confidence=0.97,
        ),
        Evidence(
            evidence_id="dep-2",
            source_type="dependency",
            source_ref="dep/inventory-service#db",
            summary="inventory-service depends on a saturated database connection pool.",
            confidence=0.92,
        ),
    ]

    hypotheses, action = plan_diagnosis(
        "order-api",
        "order-api@2026.04.16-2210",
        "order-api@2026.04.16-2150",
        changes,
        logs,
        dependencies,
    )

    assert hypotheses[0].cause.startswith("The primary fault is likely in inventory-service")
    assert action is None
