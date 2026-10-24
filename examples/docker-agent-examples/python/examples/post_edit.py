# Generated from docker-agent v1.152.0 examples/post_edit.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "post_edit",
    agents=[
        {
            "name": "root",
            "description": "A Go developer agent with post-edit formatting",
            "model": "openai/gpt-4o",
            "instruction": "You are a Go developer assistant.",
            "toolsets": [
                {
                    "type": "filesystem",
                    "post_edit": [{"path": "*.go", "cmd": "gofmt -w $path"}],
                },
            ],
        },
    ],
)
