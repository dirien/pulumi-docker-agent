# Generated from docker-agent v1.150.0 examples/groq.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "groq",
    models={"groq_model": {"provider": "groq", "model": "llama-3.3-70b-versatile"}},
    agents=[
        {
            "name": "root",
            "model": "groq_model",
            "description": "Assistant using Groq",
            "instruction": """\
You are a helpful assistant.
""",
            "toolsets": [{"type": "filesystem"}, {"type": "shell"}, {"type": "think"}],
        },
    ],
)
