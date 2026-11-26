// Generated from docker-agent v1.166.0 examples/popnow.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("popnow", {
    models: { popnow_model: { provider: "popnow", model: "deepseek/v4-flash/chat" } },
    agents: [
        {
            name: "root",
            model: "popnow_model",
            description: "Assistant using PopNow",
            instruction: `\
You are a helpful assistant.
`,
            toolsets: [{ type: "filesystem" }, { type: "shell" }, { type: "think" }],
        },
    ],
});

export const yaml = config.yaml;
