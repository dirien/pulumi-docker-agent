# Generated from docker-agent v1.151.0 examples/vercel.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "vercel",
    models={"vercel_model": {"provider": "vercel", "model": "openai/gpt-5.6-sol"}},
    agents=[
        {
            "name": "root",
            "model": "vercel_model",
            "description": "Assistant using Vercel AI Gateway",
            "instruction": """\
You are a helpful assistant.
""",
            "toolsets": [{"type": "filesystem"}, {"type": "shell"}, {"type": "think"}],
        },
    ],
)
