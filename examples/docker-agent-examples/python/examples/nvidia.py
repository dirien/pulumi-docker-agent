# Generated from docker-agent v1.151.0 examples/nvidia.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "nvidia",
    models={
        "nemotron": {
            "provider": "nvidia",
            "model": "nvidia/nemotron-3-super-120b-a12b",
        },
    },
    agents=[
        {
            "name": "root",
            "model": "nemotron",
            "description": "Assistant using NVIDIA NIM",
            "instruction": """\
You are a helpful assistant.
""",
            "toolsets": [{"type": "filesystem"}, {"type": "shell"}, {"type": "think"}],
        },
    ],
)
