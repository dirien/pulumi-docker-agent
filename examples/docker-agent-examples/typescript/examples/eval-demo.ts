// Generated from docker-agent v1.152.0 examples/eval/demo.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("eval-demo", {
    agents: [
        {
            name: "root",
            model: "openai/gpt-4o",
            description: "Test agent",
            instruction: "You know how to read/write and list files.",
            toolsets: [{ type: "filesystem" }],
        },
    ],
});

export const yaml = config.yaml;
