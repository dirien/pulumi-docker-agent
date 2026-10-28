# Generated from docker-agent v1.152.0 examples/moonshot.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "moonshot",
    models={"moonshot_model": {"provider": "moonshotai", "model": "kimi-k3"}},
    agents=[
        {
            "name": "root",
            "model": "moonshot_model",
            "description": "Assistant using Moonshot AI (Kimi)",
            "instruction": """\
You are a helpful assistant.
""",
            "toolsets": [{"type": "filesystem"}, {"type": "shell"}, {"type": "think"}],
        },
    ],
)
