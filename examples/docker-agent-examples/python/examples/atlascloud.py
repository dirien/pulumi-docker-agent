# Generated from docker-agent v1.151.0 examples/atlascloud.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "atlascloud",
    models={
        "atlascloud_model": {"provider": "atlascloud", "model": "qwen/qwen3.8-max"},
    },
    agents=[
        {
            "name": "root",
            "model": "atlascloud_model",
            "description": "Assistant using Atlas Cloud",
            "instruction": """\
You are a helpful assistant.
""",
            "toolsets": [{"type": "filesystem"}, {"type": "shell"}, {"type": "think"}],
        },
    ],
)
