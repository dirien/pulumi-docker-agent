// Generated from docker-agent v1.166.0 examples/obscurify.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("obscurify", {
    models: { obscurify_model: { provider: "obscurify", model: "deepseek/deepseek-v4-flash" } },
    agents: [
        {
            name: "root",
            model: "obscurify_model",
            description: "Assistant using Obscurify.ai",
            instruction: `\
You are a helpful assistant.
`,
            toolsets: [{ type: "filesystem" }, { type: "shell" }, { type: "think" }],
        },
    ],
});

export const yaml = config.yaml;
