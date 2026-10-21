# Generated from docker-agent v1.151.0 examples/fireworks.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "fireworks",
    models={
        "fireworks_model": {
            "provider": "fireworks-ai",
            "model": "accounts/fireworks/models/kimi-k3",
        },
    },
    agents=[
        {
            "name": "root",
            "model": "fireworks_model",
            "description": "Assistant using Fireworks AI",
            "instruction": """\
You are a helpful assistant.
""",
            "toolsets": [{"type": "filesystem"}, {"type": "shell"}, {"type": "think"}],
        },
    ],
)
