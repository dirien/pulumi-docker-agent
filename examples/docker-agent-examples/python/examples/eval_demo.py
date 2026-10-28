# Generated from docker-agent v1.152.0 examples/eval/demo.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "eval-demo",
    agents=[
        {
            "name": "root",
            "model": "openai/gpt-4o",
            "description": "Test agent",
            "instruction": "You know how to read/write and list files.",
            "toolsets": [{"type": "filesystem"}],
        },
    ],
)
