# Generated from docker-agent v1.150.0 examples/moby.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "moby",
    agents=[
        {
            "name": "root",
            "model": "anthropic/claude-sonnet-4-5",
            "description": "Moby Project Expert",
            "instruction": "You are an AI assistant with expertise in the moby/moby project's documentation.",
            "toolsets": [
                {
                    "type": "mcp",
                    "remote": {
                        "url": "https://gitmcp.io/moby/moby",
                        "transport_type": "streamable",
                    },
                },
            ],
        },
    ],
)
