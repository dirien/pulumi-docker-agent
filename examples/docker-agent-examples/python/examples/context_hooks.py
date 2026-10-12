# Generated from docker-agent v1.150.0 examples/context_hooks.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "context_hooks",
    agents=[
        {
            "name": "root",
            "model": "anthropic/claude-sonnet-4-5",
            "description": "Assistant with dependency-free session context.",
            "instruction": "You are a helpful assistant.",
            "hooks": {
                "session_start": [
                    {
                        "type": "builtin",
                        "command": "add_context",
                        "args": [
                            "Current session ID: {{ .SessionID }}",
                            "Agent: {{ .AgentName }}",
                            "Working directory: {{ .Cwd }}",
                        ],
                    },
                ],
            },
        },
    ],
)
