# Generated from docker-agent v1.150.0 examples/notion-expert.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "notion-expert",
    agents=[
        {
            "name": "root",
            "model": "anthropic/claude-sonnet-4-5",
            "description": "Notion Expert",
            "instruction": "You are an AI assistant with expertise in the Notion project's documentation.",
            "toolsets": [
                {
                    "type": "mcp",
                    "remote": {
                        "url": "https://mcp.notion.com/mcp",
                        "transport_type": "streamable",
                    },
                },
            ],
        },
    ],
)
