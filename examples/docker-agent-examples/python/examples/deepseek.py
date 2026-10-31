# Generated from docker-agent v1.154.0 examples/deepseek.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "deepseek",
    models={"deepseek_model": {"provider": "deepseek", "model": "deepseek-v4-pro"}},
    agents=[
        {
            "name": "root",
            "model": "deepseek_model",
            "description": "Assistant using DeepSeek",
            "instruction": """\
You are a helpful assistant.
""",
            "toolsets": [{"type": "filesystem"}, {"type": "shell"}, {"type": "think"}],
        },
    ],
)
