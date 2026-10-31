# Generated from docker-agent v1.154.0 examples/first_available.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "first_available",
    agents=[
        {
            "name": "root",
            "model": "smart",
            "description": "An agent that uses the first model with configured credentials",
            "instruction": """\
You are a helpful assistant.
""",
        },
    ],
    models={
        # `smart` resolves at load time to the first candidate whose credentials are
        # configured. Candidates are tried top to bottom. The local dmr provider
        # needs no API key, so it acts as a guaranteed final fallback.
        "smart": {
            "first_available": [
                "anthropic/claude-sonnet-4-6",
                "openai/gpt-5",
                "google/gemini-2.5-flash",
                "dmr/ai/qwen3",
            ],
        },
    },
)
