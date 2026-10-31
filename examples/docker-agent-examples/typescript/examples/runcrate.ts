// Generated from docker-agent v1.154.0 examples/runcrate.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("runcrate", {
    models: { runcrate_model: { provider: "runcrate", model: "deepseek-ai/DeepSeek-V4.1-Flash" } },
    agents: [
        {
            name: "root",
            model: "runcrate_model",
            description: "Assistant using Runcrate",
            instruction: `\
You are a helpful assistant.
`,
            toolsets: [{ type: "filesystem" }, { type: "shell" }, { type: "think" }],
        },
    ],
});

export const yaml = config.yaml;
