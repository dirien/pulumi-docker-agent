# Generated from docker-agent v1.151.0 examples/baseten.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "baseten",
    models={
        "baseten_model": {"provider": "baseten", "model": "deepseek-ai/DeepSeek-V3.1"},
    },
    agents=[
        {
            "name": "root",
            "model": "baseten_model",
            "description": "Assistant using Baseten",
            "instruction": """\
You are a helpful assistant.
""",
            "toolsets": [{"type": "filesystem"}, {"type": "shell"}, {"type": "think"}],
        },
    ],
)
