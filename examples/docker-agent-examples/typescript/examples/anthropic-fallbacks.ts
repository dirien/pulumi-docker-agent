// Generated from docker-agent v1.154.0 examples/anthropic-fallbacks.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("anthropic-fallbacks", {
    // Demonstrates Anthropic's server-side fallbacks (beta, Claude API only):
    // when the primary model refuses a request, the API retries on a fallback
    // model in a single round trip. `fallbacks: default` uses the model Anthropic
    // recommends for the refusal's category; a list (up to three) is tried in
    // order.
    models: {
        opus: {
            provider: "anthropic",
            model: "claude-opus-4-8",
            providerOpts: { fallbacks: ["claude-opus-4-7"] },
        },
        "opus-default": {
            provider: "anthropic",
            model: "claude-opus-4-8",
            providerOpts: { fallbacks: "default" },
        },
    },
    agents: [
        {
            name: "root",
            model: "opus",
            description: "Assistant on Claude Opus 4.8 with a server-side fallback model",
            instruction: "You are a helpful assistant.",
        },
        {
            name: "default-routing",
            model: "opus-default",
            description: "Assistant whose refused requests use Anthropic's recommended fallback",
            instruction: "You are a helpful assistant.",
        },
    ],
});

export const yaml = config.yaml;
