# Generated from docker-agent v1.151.0 examples/file.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "file",
    agents=[
        {
            "name": "root",
            "model": "openai/gpt-5-mini",
            "description": "An agent that can read, write, and edit files in the working directory",
            "instruction": "Work only with files whose paths are provided by the user.",
            "toolsets": [{"type": "file", "allow_list": ["."]}],
        },
    ],
)
