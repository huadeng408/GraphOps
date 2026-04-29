from graphops_orchestrator.llm import select_ollama_model_for_agent


def test_parallel_agents_use_parallel_model() -> None:
    parallel_agents = {"change_agent", "log_agent", "dependency_agent"}

    for agent_name in parallel_agents:
        assert select_ollama_model_for_agent(agent_name, "qwen3:4b", "qwen3:1.7b") == "qwen3:1.7b"


def test_non_parallel_agents_use_main_model() -> None:
    main_agents = {"triage_agent", "planner_agent", "critic_agent", "policy_agent", "report_agent"}

    for agent_name in main_agents:
        assert select_ollama_model_for_agent(agent_name, "qwen3:4b", "qwen3:1.7b") == "qwen3:4b"
