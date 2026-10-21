// Generated from docker-agent v1.151.0 examples/first_available.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("first_available", {
    agents: [
        {
            name: "root",
            model: "smart",
            description: "An agent that uses the first model with configured credentials",
            instruction: `\
You are a helpful assistant.
`,
        },
    ],
    models: {
        // `smart` resolves at load time to the first candidate whose credentials are
        // configured. Candidates are tried top to bottom. The local dmr provider
        // needs no API key, so it acts as a guaranteed final fallback.
        smart: {
            firstAvailable: [
                "anthropic/claude-sonnet-4-6",
                "openai/gpt-5",
                "google/gemini-2.5-flash",
                "dmr/ai/qwen3",
            ],
        },
    },
});

export const yaml = config.yaml;
