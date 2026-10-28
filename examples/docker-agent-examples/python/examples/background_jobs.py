# Generated from docker-agent v1.152.0 examples/background_jobs.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "background_jobs",
    agents=[
        {
            "name": "root",
            "model": "openai/gpt-4o",
            "description": "Run and manage long-running shell commands.",
            "instruction": """\
Use the `background_jobs` tools for servers, watchers, and other commands
that should continue running while you work. Use `shell` for short commands.
""",
            "toolsets": [{"type": "shell"}, {"type": "background_jobs"}],
        },
    ],
)
