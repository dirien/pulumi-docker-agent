# Generated from docker-agent v1.152.0 examples/google_search_grounding.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "google_search_grounding",
    models={
        "gemini": {
            "provider": "google",
            "model": "gemini-3.1-flash-lite",
            "provider_opts": {"google_search": True},
        },
    },
    agents=[
        {
            "name": "root",
            "model": "gemini",
            "description": "Gemini with Google Search",
            "instruction": """\
You are a helpful assistant with access to the latest information via Google Search.
Use grounded search results to provide accurate, up-to-date answers.
""",
        },
    ],
)
