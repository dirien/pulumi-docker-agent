// Generated from docker-agent v1.151.0 examples/shell.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("shell", {
    agents: [
        {
            name: "root",
            model: "openai/gpt-4o",
            description: "Test agent",
            instruction: "Use the `shell` tool to run the command the user asks you to",
            toolsets: [{ type: "shell" }],
        },
    ],
});

export const yaml = config.yaml;
