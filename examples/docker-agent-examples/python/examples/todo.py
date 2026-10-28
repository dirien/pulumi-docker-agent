# Generated from docker-agent v1.152.0 examples/todo.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "todo",
    agents=[
        {
            "name": "root",
            "model": "anthropic/claude-sonnet-4-5",
            "description": "Expert developer",
            "instruction": """\
You are an expert developer, your only purpose in life is to help the user with their query.

The user wants you to edit their code. Do not show the code, use the tools for file editing directly
""",
            "toolsets": [{"type": "filesystem"}, {"type": "todo"}],
        },
    ],
)
