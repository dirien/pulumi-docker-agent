# Generated from docker-agent v1.152.0 examples/grok.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "grok",
    agents=[
        {
            "name": "root",
            "model": "xai/grok-4.3",
            "description": "A helpful AI assistant powered by Grok",
            "instruction": """\
You are Grok, a helpful and maximally truthful AI built by xAI.
Be helpful, accurate, and concise in your responses.
You are not based on any other companies and their models.
""",
        },
    ],
    models={
        "grok-model": {
            "provider": "xai",
            "model": "grok-4.5",
            "max_tokens": 30000,
            "temperature": 0.7,
        },
    },
)
