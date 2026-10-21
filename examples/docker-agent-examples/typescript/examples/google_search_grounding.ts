// Generated from docker-agent v1.151.0 examples/google_search_grounding.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("google_search_grounding", {
    models: {
        gemini: {
            provider: "google",
            model: "gemini-3.1-flash-lite",
            providerOpts: { google_search: true },
        },
    },
    agents: [
        {
            name: "root",
            model: "gemini",
            description: "Gemini with Google Search",
            instruction: `\
You are a helpful assistant with access to the latest information via Google Search.
Use grounded search results to provide accurate, up-to-date answers.
`,
        },
    ],
});

export const yaml = config.yaml;
