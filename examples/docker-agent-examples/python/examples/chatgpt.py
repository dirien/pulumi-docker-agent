# Generated from docker-agent v1.152.0 examples/chatgpt.yaml by `make generate_config_types`. Do not edit.
#
# Example: ChatGPT account (subscription) provider
#
# Use your ChatGPT Plus/Pro/Business subscription instead of an
# OPENAI_API_KEY. Sign in once with (pick chatgpt in the provider list):
#
#   docker agent setup
#
# The browser sign-in stores an OAuth credential that docker-agent refreshes
# automatically; check it anytime with `docker agent doctor`. Requests go to
# the ChatGPT Codex backend, which serves the gpt-5 model family over the
# Responses API.
#
# Run with:
#
#   docker agent run examples/chatgpt.yaml

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "chatgpt",
    models={
        "gpt": {"provider": "chatgpt", "model": "gpt-5.6", "thinking_budget": "medium"},
    },
    agents=[
        {
            "name": "root",
            "model": "gpt",
            "description": "A helpful assistant running on your ChatGPT subscription",
            "instruction": """\
You are a helpful assistant. Answer concisely.
""",
        },
    ],
)
