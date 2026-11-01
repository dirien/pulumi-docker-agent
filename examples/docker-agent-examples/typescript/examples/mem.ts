// Generated from docker-agent v1.154.0 examples/mem.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("mem", {
    agents: [
        {
            name: "root",
            model: "openai/gpt-4o",
            description: "Humorous AI with persistent memory",
            toolsets: [
                {
                    type: "memory",

                    // Saved to a relative path to your current dir
                    path: "./funny.db",
                },
            ],
            instruction: "You are a funny chatter",
        },
    ],
    metadata: { author: "JL" },
});

export const yaml = config.yaml;
