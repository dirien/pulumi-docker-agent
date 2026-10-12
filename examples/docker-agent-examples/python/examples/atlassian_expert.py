# Generated from docker-agent v1.150.0 examples/atlassian-expert.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "atlassian-expert",
    models={"sonnet": {"provider": "anthropic", "model": "claude-sonnet-4-6"}},
    agents=[
        {
            "name": "root",
            "model": "sonnet",
            "description": "Atlassian Expert",
            "instruction": "You are an AI assistant with expertise in the Atlassian suite of products (Jira, Confluence).",
            "toolsets": [
                {
                    "type": "mcp",
                    "remote": {
                        "url": "https://mcp.atlassian.com/v1/mcp/authv2",
                        "transport_type": "streamable",
                        "oauth": {
                            "callback_redirect_url": "https://ai-backend-service.docker.com/callback?port=${callbackPort}",
                        },
                    },
                },
            ],
        },
    ],
)
