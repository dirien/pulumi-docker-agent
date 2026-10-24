// Generated from docker-agent v1.152.0 examples/custom-pricing.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("custom-pricing", {
    // Demonstrates the `cost` override that declares a model's token pricing
    // explicitly, in USD per one million tokens. It takes precedence over the
    // models.dev catalogue and — more importantly — prices models the catalogue
    // does not know at all: custom OpenAI-compatible providers, local models, and
    // private deployments otherwise bill $0 despite consuming tokens.
    //
    // A declared cost feeds per-turn cost computation, session cost tracking, the
    // /model picker, and the after_llm_call hook's `cost` field. Prices must not
    // be negative; an all-zero table means "priced, free" (distinct from unpriced).
    providers: {
        // A self-hosted OpenAI-compatible gateway with negotiated pricing.
        // models.dev knows nothing about the models it serves.
        "internal-llm": {
            apiType: "openai_chatcompletions",
            baseUrl: "https://llm.internal.example.com/v1",
            tokenKey: "INTERNAL_LLM_API_KEY",
        },
    },
    models: {
        // Custom endpoint: without `cost` every call would be recorded at $0.
        "internal-gpt": {
            provider: "internal-llm",
            model: "gpt-5.6-sol",
            cost: {
                input: 1.25, // USD per 1M input tokens
                output: 5.0, // USD per 1M output tokens
                cacheRead: 0.125, // USD per 1M cached input tokens
                cacheWrite: 1.5625, // USD per 1M cache-write tokens
            },
        },

        // Catalogued model with a discounted enterprise contract: the override
        // replaces the models.dev list price so recorded spend matches the invoice.
        "discounted-sonnet": {
            provider: "anthropic",
            model: "claude-sonnet-4-6",
            cost: { input: 2.4, output: 12.0 },
        },
    },
    agents: [
        {
            name: "root",
            model: "internal-gpt",
            description: "Assistant on a custom endpoint with accurate cost tracking",
            instruction: "You are a helpful assistant.",
        },
    ],
});

export const yaml = config.yaml;
