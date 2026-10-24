// Generated from docker-agent v1.152.0 examples/atlascloud.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("atlascloud", {
    models: { atlascloud_model: { provider: "atlascloud", model: "qwen/qwen3.8-max" } },
    agents: [
        {
            name: "root",
            model: "atlascloud_model",
            description: "Assistant using Atlas Cloud",
            instruction: `\
You are a helpful assistant.
`,
            toolsets: [{ type: "filesystem" }, { type: "shell" }, { type: "think" }],
        },
    ],
});

export const yaml = config.yaml;
