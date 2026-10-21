# Generated from docker-agent v1.151.0 examples/filesystem.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "filesystem",
    agents=[
        {
            "name": "root",
            "model": "openai/gpt-4o",
            "description": "Simple agent with filesystem access",
            "instruction": "Use the tools to help the user with filesystem operations.",
            "toolsets": [{"type": "filesystem"}],
        },
    ],
)
