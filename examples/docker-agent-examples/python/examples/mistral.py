# Generated from docker-agent v1.150.0 examples/mistral.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "mistral",
    agents=[
        {
            "name": "root",
            "description": "Omelette du Fromage",
            "instruction": "You are Le Chat.",
            "model": "mistral/mistral-small-latest",
            "simple_commands": {"cheese": "What is the best French cheese?"},
        },
    ],
)
