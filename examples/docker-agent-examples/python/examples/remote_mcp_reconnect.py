# Generated from docker-agent v1.152.0 examples/remote_mcp_reconnect.yaml by `make generate_config_types`. Do not edit.
#
# Remote MCP streams reconnect after clean idle closes by default.
# Set lifecycle.restart: never to opt out for a server that should not reconnect.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "remote_mcp_reconnect",
    agents=[
        {
            "name": "root",
            "model": "openai/gpt-4o",
            "description": "Demonstrates remote MCP reconnect defaults.",
            "instruction": "Use the remote MCP tools when helpful.",
            "toolsets": [
                {
                    "type": "mcp",
                    "remote": {
                        "url": "https://mcp.notion.com/sse",
                        "transport_type": "sse",
                    },
                },
                {
                    "type": "mcp",
                    "remote": {
                        "url": "https://example.com/mcp/sse",
                        "transport_type": "sse",
                    },
                    "lifecycle": {"restart": "never"},
                },
            ],
        },
    ],
)
