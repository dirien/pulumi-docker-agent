// Generated from docker-agent v1.151.0 examples/deferred_native_tool_search.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("deferred_native_tool_search", {
    agents: [
        {
            name: "root",
            model: "openai/gpt-5.6-sol",
            description: "Agent whose deferred tools are discovered by OpenAI's hosted tool search",
            instruction:
                "Use native tool search to find filesystem and shell tools, then call them directly.",
            toolsets: [
                { type: "filesystem", defer: true },
                { type: "shell", defer: true },
            ],
        },
    ],
    models: {
        "openai/gpt-5.6-sol": {
            provider: "openai",
            model: "gpt-5.6-sol",
            providerOpts: {
                // Declare every deferred tool to OpenAI with defer_loading and let the
                // server-side tool_search tool load the ones the model needs. Ignored
                // (legacy search_tool/add_tool behaviour) on models without support.
                native_tool_search: true,

                // Also available independently of native search.
                preserve_reasoning: true,

                // Run with --debug to inspect cache reuse between turns.
                cache_diagnostics: true,
            },
        },
    },
});

export const yaml = config.yaml;
