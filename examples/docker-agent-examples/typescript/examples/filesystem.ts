// Generated from docker-agent v1.152.0 examples/filesystem.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("filesystem", {
    agents: [
        {
            name: "root",
            model: "openai/gpt-4o",
            description: "Simple agent with filesystem access",
            instruction: "Use the tools to help the user with filesystem operations.",
            toolsets: [{ type: "filesystem" }],
        },
    ],
});

export const yaml = config.yaml;
