# Generated from docker-agent v1.154.0 examples/api-route.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "api-route",
    models={"api_route_model": {"provider": "api-route", "model": "gpt-6.1-sol"}},
    agents=[
        {
            "name": "root",
            "model": "api_route_model",
            "description": "Assistant using API Route",
            "instruction": "You are a helpful assistant.",
            "toolsets": [{"type": "filesystem"}, {"type": "shell"}, {"type": "think"}],
        },
    ],
)
