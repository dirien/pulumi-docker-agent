# Generated from docker-agent v1.151.0 examples/cloudflare-workers-ai.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "cloudflare-workers-ai",
    # Requires CLOUDFLARE_ACCOUNT_ID and CLOUDFLARE_API_TOKEN in the environment.
    # The account-scoped base URL is resolved from CLOUDFLARE_ACCOUNT_ID.
    models={
        "cloudflare_model": {
            "provider": "cloudflare-workers-ai",
            "model": "@cf/meta/llama-3.1-8b-instruct",
        },
    },
    agents=[
        {
            "name": "root",
            "model": "cloudflare_model",
            "description": "Assistant using Cloudflare Workers AI",
            "instruction": """\
You are a helpful assistant.
""",
            "toolsets": [{"type": "filesystem"}, {"type": "shell"}, {"type": "think"}],
        },
    ],
)
