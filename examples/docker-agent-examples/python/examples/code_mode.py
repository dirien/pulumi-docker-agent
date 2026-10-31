# Generated from docker-agent v1.154.0 examples/code_mode.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "code_mode",
    agents=[
        {
            "name": "root",
            "model": "anthropic/claude-sonnet-4-5",
            "description": "Demonstrates the use of Code Mode with tools",
            "instruction": "Use your tool to help the user with their github requests.",
            "code_mode_tools": True,
            "simple_commands": {
                "demo": "How many issues in docker/docker-agent have a number that is prime?",
            },
            "toolsets": [{"type": "mcp", "ref": "docker:github-official"}],
        },
    ],
)
