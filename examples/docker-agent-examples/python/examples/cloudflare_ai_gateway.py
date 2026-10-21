# Generated from docker-agent v1.151.0 examples/cloudflare-ai-gateway.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "cloudflare-ai-gateway",
    # Requires CLOUDFLARE_ACCOUNT_ID, CLOUDFLARE_GATEWAY_ID and CLOUDFLARE_API_TOKEN
    # in the environment. The gateway-scoped base URL is resolved from
    # CLOUDFLARE_ACCOUNT_ID and CLOUDFLARE_GATEWAY_ID. Model IDs use the gateway's
    # provider/model form (see the Cloudflare AI Gateway docs).
    #
    # The token is sent as "Authorization: Bearer", so this works with a gateway
    # that has authentication disabled (the default), routing to Workers AI models.
    # An authenticated gateway (unified billing) needs the cf-aig-authorization
    # header, which this alias does not send. See docs/providers/cloudflare-ai-gateway.
    models={
        "cloudflare_model": {
            "provider": "cloudflare-ai-gateway",
            "model": "workers-ai/@cf/meta/llama-3.1-8b-instruct",
        },
    },
    agents=[
        {
            "name": "root",
            "model": "cloudflare_model",
            "description": "Assistant using Cloudflare AI Gateway",
            "instruction": """\
You are a helpful assistant.
""",
            "toolsets": [{"type": "filesystem"}, {"type": "shell"}, {"type": "think"}],
        },
    ],
)
