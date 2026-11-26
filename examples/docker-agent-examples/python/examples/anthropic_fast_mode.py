# Generated from docker-agent v1.166.0 examples/anthropic-fast-mode.yaml by `make generate_config_types`. Do not edit.
#
# Fast mode runs the same model at up to 2.5x output speed at premium pricing.
# Claude API only; supported models and rate limits are listed in the docs.
# Cost estimates use the actual response speed (usage.speed).
# https://platform.claude.com/docs/en/build-with-claude/fast-mode

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "anthropic-fast-mode",
    agents=[
        {
            "name": "root",
            "model": "fast-opus",
            "description": "An assistant using Anthropic fast mode.",
            "instruction": "You are a helpful assistant.",
        },
    ],
    models={
        "fast-opus": {
            "provider": "anthropic",
            "model": "claude-opus-5-5",
            "provider_opts": {
                "speed": "fast",  # standard (or omitting the option) uses the default speed
            },
        },
    },
)
