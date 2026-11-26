// Generated from docker-agent v1.166.0 examples/model_context_window.yaml by `make generate_config_types`. Do not edit.
//
// Declare capacity for a custom endpoint and its named compaction model.
// context_window is accounting metadata, not a request or DMR engine setting.
// To size the DMR engine too, set provider_opts.context_size to the same value.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("model_context_window", {
    providers: {
        gateway: {
            apiType: "openai_responses",
            baseUrl: "https://gateway.example.com/v1",
            tokenKey: "GATEWAY_API_KEY",
        },
    },
    models: {
        primary: {
            provider: "gateway",
            model: "my-hosted-model",

            // Window of the hosted model, unknown to models.dev.
            contextWindow: 200000,
            compactionModel: "summarizer",
        },
        summarizer: {
            provider: "dmr",
            model: "ai/qwen3",

            // Metadata only: the DMR engine keeps its own allocation.
            contextWindow: 32768,
        },
    },
    agents: [
        {
            name: "root",
            model: "primary",
            description: "An assistant on a custom endpoint whose context window is declared.",
            instruction: "You are a helpful assistant.",
        },
    ],
});

export const yaml = config.yaml;
