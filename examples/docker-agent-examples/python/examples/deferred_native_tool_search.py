# Generated from docker-agent v1.154.0 examples/deferred_native_tool_search.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "deferred_native_tool_search",
    agents=[
        {
            "name": "root",
            "model": "openai/gpt-5.6-sol",
            "description": "Agent whose deferred tools are discovered by OpenAI's hosted tool search",
            "instruction": "Use native tool search to find filesystem and shell tools, then call them directly.",
            "toolsets": [
                {"type": "filesystem", "defer": True},
                {"type": "shell", "defer": True},
            ],
        },
    ],
    models={
        "openai/gpt-5.6-sol": {
            "provider": "openai",
            "model": "gpt-5.6-sol",
            "provider_opts": {
                # Declare every deferred tool to OpenAI with defer_loading and let the
                # server-side tool_search tool load the ones the model needs. Ignored
                # (legacy search_tool/add_tool behaviour) on models without support.
                "native_tool_search": True,
                # Also available independently of native search.
                "preserve_reasoning": True,
                # Run with --debug to inspect cache reuse between turns.
                "cache_diagnostics": True,
            },
        },
    },
)
