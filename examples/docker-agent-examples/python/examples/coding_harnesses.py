# Generated from docker-agent v1.150.0 examples/coding_harnesses.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "coding_harnesses",
    agents=[
        {
            "name": "root",
            "model": "anthropic/claude-sonnet-4-5",
            "description": "Orchestrator",
            "instruction": "Route coding tasks to the appropriate specialist.",
            "sub_agents": ["claude-coder", "codex-coder"],
        },
        {
            "name": "claude-coder",
            "description": "Claude Code for complex refactors",
            "instruction": "You are a senior software engineer.",
            "harness": {
                "type": "claude-code",
                "model": "claude-sonnet-4-5",  # optional; omit to use the Claude Code default
                "effort": "xhigh",  # low | medium | high | xhigh | max
            },
        },
        {
            "name": "codex-coder",
            "description": "Codex for code generation",
            "instruction": "You are a software engineer.",
            "harness": {"type": "codex"},
        },
    ],
)
