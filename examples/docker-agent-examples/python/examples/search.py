# Generated from docker-agent v1.150.0 examples/search.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "search",
    agents=[
        {
            "name": "root",
            "model": "anthropic/claude-sonnet-4-5",
            "description": "Search the web",
            "instruction": """\
You search the Web for information using Brave and fetch.
Always share your sources.
""",
            "toolsets": [
                {"type": "fetch"},
                {
                    "type": "mcp",
                    "ref": "docker:brave",
                    "env": {"brave.api_key": "${env.BRAVE_API_KEY}"},
                },
            ],
        },
    ],
)
