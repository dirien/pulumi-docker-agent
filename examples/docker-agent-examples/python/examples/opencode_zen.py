# Generated from docker-agent v1.154.0 examples/opencode-zen.yaml by `make generate_config_types`. Do not edit.
#
# OpenCode Zen — Free Model Example
#
# Requirements:
#   1. Sign in to OpenCode Zen at https://opencode.ai/auth
#   2. Copy your API key and export it:
#      export OPENCODE_API_KEY="your-api-key"
#
# deepseek-v4-flash-free is a free model — no charges apply.
# Full model list: https://opencode.ai/zen/v1/models

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "opencode-zen",
    agents=[
        {
            "name": "root",
            "model": "opencode/deepseek-v4-flash-free",
            "description": "A helpful AI assistant powered by OpenCode Zen (free)",
            "instruction": """\
You are a helpful AI assistant powered by OpenCode Zen.
Be helpful, accurate, and concise in your responses.
""",
        },
    ],
)
