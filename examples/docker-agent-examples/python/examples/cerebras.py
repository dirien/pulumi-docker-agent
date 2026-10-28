# Generated from docker-agent v1.152.0 examples/cerebras.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "cerebras",
    models={"cerebras_model": {"provider": "cerebras", "model": "gpt-oss-120b"}},
    agents=[
        {
            "name": "root",
            "model": "cerebras_model",
            "description": "Assistant using Cerebras",
            "instruction": """\
You are a helpful assistant.
""",
            "toolsets": [{"type": "filesystem"}, {"type": "shell"}, {"type": "think"}],
        },
    ],
)
