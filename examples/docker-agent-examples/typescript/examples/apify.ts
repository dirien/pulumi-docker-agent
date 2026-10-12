// Generated from docker-agent v1.150.0 examples/apify.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("apify", {
    agents: [
        {
            name: "root",
            model: "openai/gpt-4o",
            description: "Agent knowledgeable in Apify.",
            instruction: `\
You are an AI assistant with a deep understanding of Apify.
Your responses should be clear, concise, and focused on providing accurate information about
Apify concepts, best practices, and usage.
`,
            toolsets: [{ type: "mcp", ref: "docker:apify" }],
        },
    ],
});

export const yaml = config.yaml;
