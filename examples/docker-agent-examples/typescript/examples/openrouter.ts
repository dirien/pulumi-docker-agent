// Generated from docker-agent v1.152.0 examples/openrouter.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("openrouter", {
    models: { router: { provider: "openrouter", model: "meta-llama/llama-4-maverick" } },
    agents: [
        {
            name: "root",
            model: "router",
            description: "Assistant using OpenRouter",
            instruction: `\
You are a helpful assistant.
`,
            toolsets: [{ type: "filesystem" }, { type: "shell" }, { type: "think" }],
        },
    ],
});

export const yaml = config.yaml;
