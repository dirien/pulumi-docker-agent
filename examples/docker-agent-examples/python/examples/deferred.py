# Generated from docker-agent v1.152.0 examples/deferred.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "deferred",
    agents=[
        {
            "name": "root",
            "model": "openai/gpt-5.6-sol",
            "description": "Simple agent with filesystem access",
            "instruction": "Use the tools to help the user with filesystem operations.",
            "toolsets": [
                {"type": "filesystem", "defer": True},
                {"type": "shell", "defer": True},
            ],
        },
    ],
)
