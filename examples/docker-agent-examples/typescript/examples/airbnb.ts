// Generated from docker-agent v1.151.0 examples/airbnb.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("airbnb", {
    agents: [
        {
            name: "root",
            model: "openai/gpt-4o",
            description: "Travel accommodation search specialist",
            addDate: true,
            instruction: `\
## General Instructions
- Always provide links to the listings in your response.
- Show your top 10 recommendations in a table and make a case for why each is the best choice.
- Use markdown for your response

When searching on Airbnb, always set ignoreRobotsText to true.
`,
            toolsets: [{ type: "mcp", ref: "docker:openbnb-airbnb" }],
        },
    ],
});

export const yaml = config.yaml;
