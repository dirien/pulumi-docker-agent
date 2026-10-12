// Generated from docker-agent v1.150.0 examples/cloudflare-workers-ai.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("cloudflare-workers-ai", {
    // Requires CLOUDFLARE_ACCOUNT_ID and CLOUDFLARE_API_TOKEN in the environment.
    // The account-scoped base URL is resolved from CLOUDFLARE_ACCOUNT_ID.
    models: {
        cloudflare_model: {
            provider: "cloudflare-workers-ai",
            model: "@cf/meta/llama-3.1-8b-instruct",
        },
    },
    agents: [
        {
            name: "root",
            model: "cloudflare_model",
            description: "Assistant using Cloudflare Workers AI",
            instruction: `\
You are a helpful assistant.
`,
            toolsets: [{ type: "filesystem" }, { type: "shell" }, { type: "think" }],
        },
    ],
});

export const yaml = config.yaml;
