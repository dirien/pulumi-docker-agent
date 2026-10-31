// Generated from docker-agent v1.154.0 examples/lunaroute.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("lunaroute", {
    models: { lunaroute_glm: { provider: "lunaroute", model: "glm-5.3" } },
    agents: [
        {
            name: "root",
            model: "lunaroute_glm",
            description: "Assistant using Lunaroute",
            instruction: `\
You are a helpful assistant.
`,
            toolsets: [{ type: "filesystem" }, { type: "shell" }, { type: "think" }],
        },
    ],
});

export const yaml = config.yaml;
