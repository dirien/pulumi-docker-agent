# Generated from docker-agent v1.154.0 examples/github.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "github",
    agents=[
        {
            "name": "root",
            "model": "claude",
            "description": "GitHub Agent - Help with GitHub using MCP tools",
            "instruction": """\
You are a helpful assistant that can help with all things GitHub by leveraging GitHub MCP tools
""",
            "toolsets": [
                {
                    "type": "mcp",
                    "ref": "docker:github-official",
                    "instruction": """\
{ORIGINAL_INSTRUCTIONS}
Use these tools to help users with their GitHub-related tasks effectively.
""",
                },
            ],
            "simple_commands": {"whoami": "Who am I on GitHub?"},
        },
    ],
    models={
        "claude": {
            "provider": "anthropic",
            "model": "claude-sonnet-5",
            "max_tokens": 64000,
        },
    },
)
