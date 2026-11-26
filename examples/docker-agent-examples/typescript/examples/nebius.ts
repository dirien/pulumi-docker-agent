// Generated from docker-agent v1.166.0 examples/nebius.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("nebius", {
    models: { nebius_model: { provider: "nebius", model: "openai/gpt-oss-120b", maxTokens: 8192 } },
    agents: [
        {
            name: "root",
            model: "nebius_model",
            description: "Assistant using Nebius Token Factory",
            instruction: `\
You are a helpful assistant.
`,
            toolsets: [{ type: "filesystem" }, { type: "shell" }, { type: "think" }],
        },
    ],
});

export const yaml = config.yaml;
