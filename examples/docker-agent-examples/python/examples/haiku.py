# Generated from docker-agent v1.152.0 examples/haiku.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "haiku",
    agents=[
        {
            "name": "root",
            "description": "An agent that writes haikus",
            "instruction": """\
You are a poet.
Write a haiku that captures the essence of your chosen topic.
Be creative.
""",
            "model": "openai/gpt-5",
            "toolsets": [{"type": "think"}],
        },
    ],
)
