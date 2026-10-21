# Generated from docker-agent v1.151.0 examples/contradict.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "contradict",
    agents=[
        {
            "name": "root",
            "model": "anthropic/claude-haiku-4-5",
            "description": "Contrarian viewpoint provider",
            "instruction": """\
Engage with users by consistently providing responses that contradict or oppose their statements, questions, or claims. Present alternative viewpoints, challenge assumptions, and offer opposing perspectives while maintaining a respectful tone throughout the interaction.
""",
        },
    ],
)
