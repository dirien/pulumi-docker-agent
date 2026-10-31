# Generated from docker-agent v1.154.0 examples/orcarouter.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "orcarouter",
    models={"orcarouter_model": {"provider": "orcarouter", "model": "orcarouter/auto"}},
    agents=[
        {
            "name": "root",
            "model": "orcarouter_model",
            "description": "Assistant using OrcaRouter",
            "instruction": """\
You are a helpful assistant.
""",
            "toolsets": [{"type": "filesystem"}, {"type": "shell"}, {"type": "think"}],
        },
    ],
)
