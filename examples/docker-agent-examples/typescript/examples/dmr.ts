// Generated from docker-agent v1.154.0 examples/dmr.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("dmr", {
    agents: [
        {
            name: "root",
            model: "qwen",

            // model: qwen_speculative
            description: "Pirate-themed AI assistant",
            instruction: "Talk like a pirate",
            simpleCommands: { demo: "Hey tell me a story about docker containers" },
        },
    ],
    models: {
        qwen: {
            provider: "dmr",
            model: "ai/qwen3",
            // base_url defaults to http://localhost:12434/engines/llama.cpp/v1
            // use http://model-runner.docker.internal/engines/v1 if you run docker-agent from a container
        },

        // try this model for faster inference if you have enough memory
        qwen_speculative: {
            provider: "dmr",
            model: "ai/qwen3",

            // The draft model should be a smaller, faster variant of the main model with low latency
            providerOpts: {
                speculative_draft_model: "ai/qwen3:0.6B-Q4_K_M",
                speculative_num_tokens: 16, // (this is the llama.cpp default if omitted)
                speculative_acceptance_rate: 0.8, // (this is the llama.cpp default if omitted)
            },
        },

        // A vision-capable local model. DMR-hosted models are not in the models.dev
        // catalog, so docker-agent cannot detect multimodal support automatically and
        // defaults to text-only (image/PDF attachments are dropped). Declare the
        // capabilities explicitly so attachments are forwarded to the model.
        qwen_vision: {
            provider: "dmr",
            model: "ai/qwen2.5-vl",
            providerOpts: { supports_images: true, supports_pdf: false },
        },
    },
});

export const yaml = config.yaml;
