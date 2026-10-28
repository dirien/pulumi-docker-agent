# Generated from docker-agent v1.152.0 examples/ovhcloud.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "ovhcloud",
    models={"ovhcloud_model": {"provider": "ovhcloud", "model": "Qwen3.5-397B-A17B"}},
    agents=[
        {
            "name": "root",
            "model": "ovhcloud_model",
            "description": "Assistant using OVHcloud AI Endpoints",
            "instruction": """\
You are a helpful assistant.
""",
            "toolsets": [{"type": "filesystem"}, {"type": "shell"}, {"type": "think"}],
        },
    ],
)
