// Generated from docker-agent v1.166.0 examples/rapid-mlx.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("rapid-mlx", {
    // For an 8 GB Mac. The qwen3.5-4b-4bit alternative requires at least 16 GB.
    // Start the local server first: rapid-mlx serve qwen3-1.7b-4bit --port 8000
    providers: {
        "rapid-mlx": {
            apiType: "openai_chatcompletions",
            baseUrl: "http://127.0.0.1:8000/v1",

            // If the server uses RAPID_MLX_API_KEY, export the same key and uncomment:
            // token_key: RAPID_MLX_API_KEY
            providerOpts: { extra_body: { chat_template_kwargs: { enable_thinking: false } } },
        },
    },
    models: {
        local: {
            provider: "rapid-mlx",
            model: "qwen3-1.7b-4bit",
            temperature: 0.0,
            maxTokens: 512,
        },
    },
    agents: [
        {
            name: "root",
            model: "local",
            description: "Local chat assistant using Rapid-MLX on Apple Silicon",
            instruction: "You are a concise helpful assistant. Answer directly.",
        },
    ],
});

export const yaml = config.yaml;
