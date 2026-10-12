// Generated from docker-agent v1.150.0 examples/openai-service-tier.yaml by `make generate_config_types`. Do not edit.
//
// Cost estimates use the actual response tier for supported OpenAI models.
// https://developers.openai.com/api/docs/guides/fast-mode
// https://developers.openai.com/api/docs/guides/ultrafast-mode

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("openai-service-tier", {
    agents: [
        {
            name: "root",
            model: "fast-gpt", // Switch to ultrafast-astra to use Ultrafast mode.
            description: "An assistant using OpenAI Fast mode.",
            instruction: "You are a helpful assistant.",
        },
    ],
    models: {
        "fast-gpt": {
            provider: "openai",
            model: "gpt-5.6",
            providerOpts: {
                service_tier: "fast", // priority is also accepted by OpenAI
            },
        },
        "ultrafast-astra": {
            provider: "openai",
            model: "gpt-6-astra",
            providerOpts: { service_tier: "ultrafast" },
        },
    },
});

export const yaml = config.yaml;
