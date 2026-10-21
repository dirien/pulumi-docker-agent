// Generated from docker-agent v1.151.0 examples/extra_body.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("extra_body", {
    // Run an OpenAI-compatible local server with this model loaded first.
    // The server and model template must support chat_template_kwargs.enable_thinking.
    providers: {
        local: { apiType: "openai_chatcompletions", baseUrl: "http://localhost:8080/v1" },
    },
    models: {
        qwen: {
            provider: "local",
            model: "mlx-community/Qwen3.6-35B-A3B-8bit",
            temperature: 0.0,
            providerOpts: { extra_body: { chat_template_kwargs: { enable_thinking: false } } },
        },
    },
    agents: [
        {
            name: "root",
            model: "qwen",
            description: "Local assistant with thinking disabled",
            instruction: "Answer questions clearly and concisely.",
        },
    ],
});

export const yaml = config.yaml;
