// Generated from docker-agent v1.150.0 examples/deferred.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("deferred", {
    agents: [
        {
            name: "root",
            model: "openai/gpt-5.6-sol",
            description: "Simple agent with filesystem access",
            instruction: "Use the tools to help the user with filesystem operations.",
            toolsets: [
                { type: "filesystem", defer: true },
                { type: "shell", defer: true },
            ],
        },
    ],
});

export const yaml = config.yaml;
