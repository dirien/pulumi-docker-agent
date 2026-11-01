# Generated from docker-agent v1.154.0 examples/remote_mcp_allow_private_ips.yaml by `make generate_config_types`. Do not edit.
#
# Example: Remote MCP server whose OAuth endpoints intentionally resolve to
# internal/private addresses. Use allow_private_ips only for trusted servers.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "remote_mcp_allow_private_ips",
    agents=[
        {
            "name": "root",
            "model": "openai/gpt-4.1-mini",
            "description": "Assistant connected to a trusted internal remote MCP server",
            "instruction": "You are a helpful assistant with access to internal MCP tools.",
            "toolsets": [
                {
                    "type": "mcp",
                    "allow_private_ips": True,
                    "remote": {
                        "url": "https://internal-mcp.example.com/mcp",
                        "transport_type": "streamable",
                    },
                },
            ],
        },
    ],
)
