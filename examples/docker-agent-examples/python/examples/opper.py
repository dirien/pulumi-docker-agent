# Generated from docker-agent v1.154.0 examples/opper.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "opper",
    models={"opper_model": {"provider": "opper", "model": "claude-sonnet-4-6"}},
    agents=[
        {
            "name": "root",
            "model": "opper_model",
            "description": "Assistant using Opper",
            "instruction": """\
You are a helpful assistant.
""",
            "toolsets": [{"type": "filesystem"}, {"type": "shell"}, {"type": "think"}],
        },
    ],
)
