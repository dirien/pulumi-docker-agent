# Generated from docker-agent v1.150.0 examples/shell.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "shell",
    agents=[
        {
            "name": "root",
            "model": "openai/gpt-4o",
            "description": "Test agent",
            "instruction": "Use the `shell` tool to run the command the user asks you to",
            "toolsets": [{"type": "shell"}],
        },
    ],
)
