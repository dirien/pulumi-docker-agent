// Generated from docker-agent v1.152.0 examples/deepseek.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("deepseek", {
    models: { deepseek_model: { provider: "deepseek", model: "deepseek-v4-pro" } },
    agents: [
        {
            name: "root",
            model: "deepseek_model",
            description: "Assistant using DeepSeek",
            instruction: `\
You are a helpful assistant.
`,
            toolsets: [{ type: "filesystem" }, { type: "shell" }, { type: "think" }],
        },
    ],
});

export const yaml = config.yaml;
