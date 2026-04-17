from __future__ import annotations

import json
from typing import Any


PROMPT_VERSIONS = {
    "triage_agent": "triage-v1",
    "change_agent": "change-v1",
    "log_agent": "log-v1",
    "dependency_agent": "dependency-v1",
    "planner_agent": "planner-v1",
    "critic_agent": "critic-v1",
    "policy_agent": "policy-v1",
    "report_agent": "report-v1",
}


def _payload(data: dict[str, Any]) -> str:
    return json.dumps(data, ensure_ascii=False, indent=2, sort_keys=True)


def triage_prompt(data: dict[str, Any]) -> str:
    return (
        "You are the Triage Agent in a production incident workflow.\n"
        "Decide whether the alert should enter the post-release incident workflow.\n"
        "Classify the incident type conservatively and explain your reason.\n"
        "Only use information in the payload.\n\n"
        f"Payload:\n{_payload(data)}"
    )


def evidence_prompt(agent_name: str, data: dict[str, Any]) -> str:
    return (
        f"You are the {agent_name}. Convert raw tool output into the most useful structured evidence.\n"
        "Keep summaries concise, factual, and grounded in the payload. Do not invent source references.\n"
        "Return only the strongest evidence items for incident diagnosis.\n\n"
        f"Payload:\n{_payload(data)}"
    )


def planner_prompt(data: dict[str, Any]) -> str:
    return (
        "You are the Planner Agent for a production incident workflow.\n"
        "Use the structured evidence to propose up to 2 root-cause hypotheses and an action plan.\n"
        "Only propose rollback when evidence strongly points to a local release regression.\n"
        "If evidence is insufficient, use a conservative action or return no action.\n"
        "Support every conclusion with evidence IDs from the payload.\n\n"
        f"Payload:\n{_payload(data)}"
    )


def critic_prompt(data: dict[str, Any]) -> str:
    return (
        "You are the Critic Agent reviewing the planner's diagnosis.\n"
        "Check for weak evidence, conflicting evidence, or unsafe rollback recommendations.\n"
        "Approve the plan only when the evidence is strong enough. Otherwise request a replan.\n\n"
        f"Payload:\n{_payload(data)}"
    )


def policy_prompt(data: dict[str, Any]) -> str:
    return (
        "You are the Policy Agent for an incident response system.\n"
        "Evaluate the proposed action using the severity, evidence strength, and safety rules.\n"
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
