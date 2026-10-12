# Generated from docker-agent v1.150.0 examples/anthropic-fallbacks.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "anthropic-fallbacks",
    # Demonstrates Anthropic's server-side fallbacks (beta): when the primary
    # model refuses a request (e.g. Claude Fable 5's safety classifiers), the API
    # retries with the listed fallback models in order, in a single round trip.
    models={
        "fable": {
            "provider": "anthropic",
            "model": "claude-fable-5-1",
            "provider_opts": {"fallbacks": ["claude-opus-4-8"]},
        },
    },
    agents=[
        {
            "name": "root",
            "model": "fable",
            "description": "Assistant on Claude Fable 5 with a server-side fallback model",
            "instruction": "You are a helpful assistant.",
        },
    ],
)
