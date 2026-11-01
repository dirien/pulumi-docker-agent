// Generated from docker-agent v1.154.0 examples/anthropic-context.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("anthropic-context", {
    // Optional Anthropic context features. Omit provider_opts to keep default behavior.
    models: {
        claude: {
            provider: "anthropic",
            model: "claude-fable-5-1",
            thinkingBudget: "high",
            providerOpts: {
                thinking_prefix_mismatch: "drop_block",
                thinking_display: "updates",
                strict_tools: true,
                cache_diagnostics: true,
                native_compaction: true,
                cache_preserving_updates: true,
            },
        },
    },
    agents: [
        {
            name: "root",
            model: "claude",
            description: "Coding assistant with native Claude context management",
            instruction: "Help with the project. Inspect files before suggesting changes.",
            toolsets: [{ type: "filesystem" }, { type: "shell" }],
        },
    ],
});

export const yaml = config.yaml;
