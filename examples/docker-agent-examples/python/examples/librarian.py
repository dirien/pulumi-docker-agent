# Generated from docker-agent v1.151.0 examples/librarian.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "librarian",
    agents=[
        {
            "name": "root",
            "model": "anthropic/claude-haiku-4-5",
            "description": "Expert in searching the web",
            "instruction": """\
You are the librarian, your job is to look for relevant documentation to help the golang developer agent.
When given a query, search the internet for relevant documentation, articles, or resources that can assist in completing the task.
Use context7 for searching documentation and duckduckgo for general web searches.
""",
            "toolsets": [
                {"type": "mcp", "ref": "docker:context7"},
                {"type": "mcp", "ref": "docker:duckduckgo"},
                {"type": "fetch"},
            ],
        },
    ],
)
