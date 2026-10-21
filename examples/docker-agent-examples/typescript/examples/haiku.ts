// Generated from docker-agent v1.151.0 examples/haiku.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("haiku", {
    agents: [
        {
            name: "root",
            description: "An agent that writes haikus",
            instruction: `\
You are a poet.
Write a haiku that captures the essence of your chosen topic.
Be creative.
`,
            model: "openai/gpt-5",
            toolsets: [{ type: "think" }],
        },
    ],
});

export const yaml = config.yaml;
