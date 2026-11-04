# Generated from docker-agent v1.156.2 examples/cloudgpu.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "cloudgpu",
    models={"cloudgpu_model": {"provider": "cloudgpu", "model": "deepseek-v4.1-flash"}},
    agents=[
        {
            "name": "root",
            "model": "cloudgpu_model",
            "description": "Assistant using CloudGPU",
            "instruction": """\
You are a helpful assistant.
""",
            "toolsets": [{"type": "filesystem"}, {"type": "shell"}, {"type": "think"}],
        },
    ],
)
