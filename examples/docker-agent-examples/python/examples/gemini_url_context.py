# Generated from docker-agent v1.154.0 examples/gemini_url_context.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "gemini_url_context",
    models={
        "reader": {
            "provider": "google",
            "model": "gemini-3.8-flash",
            "provider_opts": {"url_context": True},
        },
    },
    agents=[
        {
            "name": "root",
            "model": "reader",
            "description": "Read public documentation with Gemini URL Context",
            "instruction": """\
Read the public URLs supplied by the user and answer using their content.
Explain when a page cannot be retrieved; do not invent its contents.
""",
        },
    ],
)
