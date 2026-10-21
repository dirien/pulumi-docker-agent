# Generated from docker-agent v1.151.0 examples/mem.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "mem",
    agents=[
        {
            "name": "root",
            "model": "openai/gpt-4o",
            "description": "Humorous AI with persistent memory",
            "toolsets": [
                {
                    "type": "memory",
                    # Saved to a relative path to your current dir
                    "path": "./funny.db",
                },
            ],
            "instruction": "You are a funny chatter",
        },
    ],
    metadata={"author": "JL"},
)
