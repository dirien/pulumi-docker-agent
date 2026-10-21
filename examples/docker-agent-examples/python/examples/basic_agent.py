# Generated from docker-agent v1.151.0 examples/basic_agent.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "basic_agent",
    agents=[
        {
            "name": "root",
            "model": "openai/gpt-5-mini",
            "description": "A helpful AI assistant",
            "instruction": """\
You are a knowledgeable assistant that helps users with various tasks.
Be helpful, accurate, and concise in your responses.
""",
        },
    ],
)
