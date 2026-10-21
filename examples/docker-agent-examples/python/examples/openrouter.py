# Generated from docker-agent v1.151.0 examples/openrouter.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "openrouter",
    models={
        "router": {"provider": "openrouter", "model": "meta-llama/llama-4-maverick"},
    },
    agents=[
        {
            "name": "root",
            "model": "router",
            "description": "Assistant using OpenRouter",
            "instruction": """\
You are a helpful assistant.
""",
            "toolsets": [{"type": "filesystem"}, {"type": "shell"}, {"type": "think"}],
        },
    ],
)
