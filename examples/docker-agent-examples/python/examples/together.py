# Generated from docker-agent v1.151.0 examples/together.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "together",
    models={
        "together_model": {
            "provider": "togetherai",
            "model": "meta-llama/Llama-3.3-70B-Instruct-Turbo",
        },
    },
    agents=[
        {
            "name": "root",
            "model": "together_model",
            "description": "Assistant using Together AI",
            "instruction": """\
You are a helpful assistant.
""",
            "toolsets": [{"type": "filesystem"}, {"type": "shell"}, {"type": "think"}],
        },
    ],
)
