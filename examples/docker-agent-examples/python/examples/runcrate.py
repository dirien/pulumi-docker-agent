# Generated from docker-agent v1.154.0 examples/runcrate.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "runcrate",
    models={
        "runcrate_model": {
            "provider": "runcrate",
            "model": "deepseek-ai/DeepSeek-V4.1-Flash",
        },
    },
    agents=[
        {
            "name": "root",
            "model": "runcrate_model",
            "description": "Assistant using Runcrate",
            "instruction": """\
You are a helpful assistant.
""",
            "toolsets": [{"type": "filesystem"}, {"type": "shell"}, {"type": "think"}],
        },
    ],
)
