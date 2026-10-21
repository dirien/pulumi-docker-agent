# Generated from docker-agent v1.151.0 examples/daoxe.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "daoxe",
    models={"daoxe_model": {"provider": "daoxe", "model": "claude-sonnet-4-6"}},
    agents=[
        {
            "name": "root",
            "model": "daoxe_model",
            "description": "Assistant using DaoXE",
            "instruction": """\
You are a helpful assistant.
""",
            "toolsets": [{"type": "filesystem"}, {"type": "shell"}, {"type": "think"}],
        },
    ],
)
