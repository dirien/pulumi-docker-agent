# Generated from docker-agent v1.166.0 examples/hopbase.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "hopbase",
    models={"hopbase_model": {"provider": "hopbase", "model": "glm-5.3"}},
    agents=[
        {
            "name": "root",
            "model": "hopbase_model",
            "description": "Assistant using HopBase",
            "instruction": """\
You are a helpful assistant.
""",
            "toolsets": [{"type": "filesystem"}, {"type": "shell"}, {"type": "think"}],
        },
    ],
)
