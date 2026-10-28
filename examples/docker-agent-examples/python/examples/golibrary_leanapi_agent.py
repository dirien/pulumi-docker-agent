# Generated from docker-agent v1.152.0 examples/golibrary/leanapi/agent.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "golibrary-leanapi-agent",
    agents=[
        {
            "name": "root",
            "description": "Queries GitHub repository metadata without JavaScript.",
            "model": "anthropic/claude-sonnet-4-6",
            "instruction": "Answer questions about GitHub repositories using the API tool.",
            "toolsets": [
                {
                    "type": "api",
                    "api_config": {
                        "name": "repository",
                        "method": "GET",
                        "endpoint": "https://api.github.com/repos/${owner}/${repo}",
                        "args": {
                            "owner": {"type": "string"},
                            "repo": {"type": "string"},
                        },
                        "required": ["owner", "repo"],
                    },
                },
            ],
        },
    ],
)
