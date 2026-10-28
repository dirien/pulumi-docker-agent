# Generated from docker-agent v1.152.0 examples/github-toon.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "github-toon",
    agents=[
        {
            "name": "root",
            "model": "anthropic/claude-sonnet-4-5",
            "description": "GitHub Agent - Help with GitHub using MCP tools",
            "instruction": "You are a helpful assistant that can help with all things GitHub",
            "toolsets": [
                {
                    "type": "mcp",
                    "ref": "docker:github-official",
                    "toon": ".*",  # toonify all the tools
                },
            ],
        },
    ],
)
