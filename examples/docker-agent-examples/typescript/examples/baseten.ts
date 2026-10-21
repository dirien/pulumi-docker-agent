// Generated from docker-agent v1.151.0 examples/baseten.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("baseten", {
    models: { baseten_model: { provider: "baseten", model: "deepseek-ai/DeepSeek-V3.1" } },
    agents: [
        {
            name: "root",
            model: "baseten_model",
            description: "Assistant using Baseten",
            instruction: `\
You are a helpful assistant.
`,
            toolsets: [{ type: "filesystem" }, { type: "shell" }, { type: "think" }],
        },
    ],
});

export const yaml = config.yaml;
