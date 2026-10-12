# Generated from docker-agent v1.150.0 examples/anthropic-context.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "anthropic-context",
    # Optional Anthropic context features. Omit provider_opts to keep default behavior.
    models={
        "claude": {
            "provider": "anthropic",
            "model": "claude-fable-5-1",
            "thinking_budget": "high",
            "provider_opts": {
                "thinking_prefix_mismatch": "drop_block",
                "thinking_display": "updates",
                "strict_tools": True,
                "cache_diagnostics": True,
                "native_compaction": True,
                "cache_preserving_updates": True,
            },
        },
    },
    agents=[
        {
            "name": "root",
            "model": "claude",
            "description": "Coding assistant with native Claude context management",
            "instruction": "Help with the project. Inspect files before suggesting changes.",
            "toolsets": [{"type": "filesystem"}, {"type": "shell"}],
        },
    ],
)
