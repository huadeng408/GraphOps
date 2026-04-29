from __future__ import annotations

import json
from typing import Any


PROMPT_VERSIONS = {
    "triage_agent": "triage-v2",
    "change_agent": "change-v2",
    "log_agent": "log-v2",
    "dependency_agent": "dependency-v2",
    "planner_agent": "planner-v2",
    "critic_agent": "critic-v2",
    "policy_agent": "policy-v2",
    "report_agent": "report-v2",
}


def _payload(data: dict[str, Any]) -> str:
    return json.dumps(data, ensure_ascii=False, indent=2, sort_keys=True)


def triage_prompt(data: dict[str, Any]) -> str:
    return (
        "You are the Triage Agent for a production release-regression workflow.\n"
        "Decide whether the alert should enter the post-release rollback workflow.\n"
        "Classify the incident conservatively and explain your reason using the incident context.\n"
        "Only use information in the payload.\n\n"
        f"Payload:\n{_payload(data)}"
    )


def evidence_prompt(agent_name: str, data: dict[str, Any]) -> str:
    return (
        f"You are the {agent_name}. Convert raw tool output into the most useful structured evidence.\n"
        "Keep summaries concise, factual, and grounded in the payload. Do not invent source references.\n"
        "Highlight evidence that helps prove or disprove a local release regression.\n\n"
        f"Payload:\n{_payload(data)}"
    )


def planner_prompt(data: dict[str, Any]) -> str:
    return (
        "You are the Planner Agent for a production release-regression workflow.\n"
        "Use the structured evidence to propose up to 2 root-cause hypotheses and, if justified,\n"
        "a rollback action that names the current revision, target revision, risk level, and verification policy.\n"
        "Only propose rollback when evidence strongly points to a local release regression.\n"
        "If evidence is insufficient, return no action.\n"
        "Support every conclusion with evidence IDs from the payload.\n\n"
        f"Payload:\n{_payload(data)}"
    )


def critic_prompt(data: dict[str, Any]) -> str:
    return (
        "You are the Critic Agent reviewing the planner's diagnosis.\n"
        "Check for weak evidence, conflicting evidence, or rollback plans that are missing revision or verification details.\n"
        "Approve the plan only when the evidence is strong enough. Otherwise request a replan.\n\n"
        f"Payload:\n{_payload(data)}"
    )


def policy_prompt(data: dict[str, Any]) -> str:
    return (
        "You are the Policy Agent for an incident response system.\n"
        "Evaluate the proposed action using the severity, evidence strength, revision rollback scope, and safety rules.\n"
        "Use require_human_approval for risky write actions like rollback.\n"
        "Use deny when the evidence is not sufficient for action.\n\n"
        f"Payload:\n{_payload(data)}"
    )


def report_prompt(data: dict[str, Any]) -> str:
    return (
        "You are the Report Agent for incident response.\n"
        "Write a crisp final report grounded only in the payload. Summarize the diagnosis,\n"
        "state the primary root cause, recommended action, and recovery outcome.\n\n"
        f"Payload:\n{_payload(data)}"
    )
