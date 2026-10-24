// Generated from docker-agent v1.152.0 examples/custom_provider.yaml by `make generate_config_types`. Do not edit.
//
// Example: Custom Provider Configuration
//
// This example demonstrates how to define and use providers in docker-agent.
// Providers allow you to group reusable configuration (base URLs, API tokens,
// model defaults) that models can inherit by referencing the provider name.
//
// Providers support any provider type: openai, anthropic, google, amazon-bedrock, etc.
// When the 'provider' field is not set, it defaults to 'openai' for backward compatibility.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("custom_provider", {
    // Define providers with reusable configuration
    providers: {
        // Example: An OpenAI Chat Completions compatible API gateway (default behavior)
        my_gateway: {
            apiType: "openai_chatcompletions", // Use the Chat Completions API schema
            baseUrl: "https://api.example.com/",
            tokenKey: "API_KEY_ENV_VAR_NAME", // Environment variable containing the API token
        },

        // Example: An OpenAI Responses compatible API gateway
        responses_provider: {
            apiType: "openai_responses",
            baseUrl: "https://responses.example.com/",
            tokenKey: "API_KEY_ENV_VAR_NAME",
        },

        // Example: Anthropic provider with shared defaults
        my_anthropic: {
            provider: "anthropic",
            tokenKey: "MY_ANTHROPIC_KEY",
            maxTokens: 16384,
            thinkingBudget: "high",
        },

        // Example: Google provider with shared defaults
        my_google: { provider: "google", temperature: 0.7 },
    },

    // Define models that use the providers
    models: {
        // Model using the custom gateway provider
        gateway_gpt: {
            provider: "my_gateway",
            model: "gpt-5.6-sol",
            maxTokens: 32768,
            temperature: 0.7,
        },

        // Model using the responses provider
        responses_model: { provider: "responses_provider", model: "gpt-5.6-sol", maxTokens: 16000 },

        // Model using the Anthropic provider - inherits max_tokens and thinking_budget
        claude_model: { provider: "my_anthropic", model: "claude-sonnet-5" },

        // Model overriding the provider's thinking_budget
        claude_fast: { provider: "my_anthropic", model: "claude-haiku-4-5", thinkingBudget: "low" },
    },

    // Define agents that use the models
    agents: [
        {
            name: "root",
            model: "responses_model",
            description: "Main assistant using the custom gateway",
            instruction: `\
You are a helpful AI assistant. Be concise and helpful in your responses.
`,
        },

        // Example using shorthand syntax: provider_name/model_name
        // The provider defaults (base_url, token_key, api_type) are automatically applied
        {
            name: "subagent",
            model: "my_gateway/gpt-5.6-terra",
            description: "Sub-agent for specialized tasks",
            instruction: `\
You are a specialized assistant for specific tasks.
`,
        },

        // Example using Anthropic provider with shorthand
        {
            name: "researcher",
            model: "my_anthropic/claude-sonnet-5",
            description: "Research assistant",
            instruction: `\
You are a research assistant that provides thorough analysis.
`,
        },
    ],
});

export const yaml = config.yaml;
