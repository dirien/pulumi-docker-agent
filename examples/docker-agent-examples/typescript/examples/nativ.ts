// Generated from docker-agent v1.150.0 examples/nativ.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("nativ", {
    // Download this small MLX model in Nativ and start its local server first.
    providers: {
        nativ: {
            apiType: "openai_chatcompletions",
            baseUrl: "http://127.0.0.1:8080/v1",

            // If server authentication is enabled, set NATIV_API_KEY and uncomment:
            // token_key: NATIV_API_KEY
            providerOpts: { extra_body: { enable_thinking: false } },
        },
    },
    models: {
        tiny: {
            provider: "nativ",
            model: "mlx-community/Qwen3-0.6B-4bit",
            temperature: 0.0,
            maxTokens: 256,
        },
    },
    agents: [
        {
            name: "root",
            model: "tiny",
            description: "Local chat assistant using Nativ on Apple Silicon",
            instruction: "You are a concise helpful assistant. Answer directly.",
        },
    ],
});

export const yaml = config.yaml;
