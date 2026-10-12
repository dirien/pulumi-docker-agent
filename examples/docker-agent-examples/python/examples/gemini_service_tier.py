# Generated from docker-agent v1.150.0 examples/gemini_service_tier.yaml by `make generate_config_types`. Do not edit.
#
# Gemini service tiers: flex (discounted, queued), standard, or priority.
# Flex requests may wait up to 15 minutes before the first byte.
# Set models.gemini-flex.cost to your tier's rates for accurate cost estimates.
# https://ai.google.dev/gemini-api/docs/service-tiers

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "gemini_service_tier",
    agents=[
        {
            "name": "root",
            "model": "gemini-flex",
            "description": "An assistant using the Gemini Flex service tier.",
            "instruction": "You are a helpful assistant.",
        },
    ],
    models={
        "gemini-flex": {
            "provider": "google",
            "model": "gemini-2.5-flash",
            "provider_opts": {
                "service_tier": "flex",  # standard and priority are also accepted
            },
        },
    },
)
