// Generated from docker-agent v1.154.0 examples/orcarouter.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("orcarouter", {
    models: { orcarouter_model: { provider: "orcarouter", model: "orcarouter/auto" } },
    agents: [
        {
            name: "root",
            model: "orcarouter_model",
            description: "Assistant using OrcaRouter",
            instruction: `\
You are a helpful assistant.
`,
            toolsets: [{ type: "filesystem" }, { type: "shell" }, { type: "think" }],
        },
    ],
});

export const yaml = config.yaml;
