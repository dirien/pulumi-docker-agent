// Generated from docker-agent v1.150.0 examples/moonshot.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("moonshot", {
    models: { moonshot_model: { provider: "moonshotai", model: "kimi-k3" } },
    agents: [
        {
            name: "root",
            model: "moonshot_model",
            description: "Assistant using Moonshot AI (Kimi)",
            instruction: `\
You are a helpful assistant.
`,
            toolsets: [{ type: "filesystem" }, { type: "shell" }, { type: "think" }],
        },
    ],
});

export const yaml = config.yaml;
