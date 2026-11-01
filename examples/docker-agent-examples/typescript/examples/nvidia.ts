// Generated from docker-agent v1.154.0 examples/nvidia.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("nvidia", {
    models: { nemotron: { provider: "nvidia", model: "nvidia/nemotron-3.5-super-vl-120b-a12b" } },
    agents: [
        {
            name: "root",
            model: "nemotron",
            description: "Assistant using NVIDIA NIM",
            instruction: `\
You are a helpful assistant.
`,
            toolsets: [{ type: "filesystem" }, { type: "shell" }, { type: "think" }],
        },
    ],
});

export const yaml = config.yaml;
