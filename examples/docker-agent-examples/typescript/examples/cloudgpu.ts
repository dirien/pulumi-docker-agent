// Generated from docker-agent v1.157.1 examples/cloudgpu.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("cloudgpu", {
    models: { cloudgpu_model: { provider: "cloudgpu", model: "deepseek-v4.1-flash" } },
    agents: [
        {
            name: "root",
            model: "cloudgpu_model",
            description: "Assistant using CloudGPU",
            instruction: `\
You are a helpful assistant.
`,
            toolsets: [{ type: "filesystem" }, { type: "shell" }, { type: "think" }],
        },
    ],
});

export const yaml = config.yaml;
