// Generated from docker-agent v1.166.0 examples/straitly.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("straitly", {
    models: { straitly_model: { provider: "straitly", model: "deepseek/deepseek-v4-1-flash" } },
    agents: [
        {
            name: "root",
            model: "straitly_model",
            description: "Assistant using Straitly",
            instruction: `\
You are a helpful assistant.
`,
            toolsets: [{ type: "filesystem" }, { type: "shell" }, { type: "think" }],
        },
    ],
});

export const yaml = config.yaml;
