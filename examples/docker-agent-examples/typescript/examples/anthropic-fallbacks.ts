// Generated from docker-agent v1.152.0 examples/anthropic-fallbacks.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("anthropic-fallbacks", {
    // Demonstrates Anthropic's server-side fallbacks (beta): when the primary
    // model refuses a request, the API retries with the listed fallback models in
    // order, in a single round trip.
    //
    // Fallback chains that include a Claude 5.x model are rejected until
    // https://github.com/docker/docker-agent/issues/4584 lands.
    models: {
        opus: {
            provider: "anthropic",
            model: "claude-opus-4-8",
            providerOpts: { fallbacks: ["claude-opus-4-7"] },
        },
    },
    agents: [
        {
            name: "root",
            model: "opus",
            description: "Assistant on Claude Opus 4.8 with a server-side fallback model",
            instruction: "You are a helpful assistant.",
        },
    ],
});

export const yaml = config.yaml;
