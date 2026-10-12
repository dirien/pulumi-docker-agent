// Generated from docker-agent v1.150.0 examples/moby.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("moby", {
    agents: [
        {
            name: "root",
            model: "anthropic/claude-sonnet-4-5",
            description: "Moby Project Expert",
            instruction:
                "You are an AI assistant with expertise in the moby/moby project's documentation.",
            toolsets: [
                {
                    type: "mcp",
                    remote: { url: "https://gitmcp.io/moby/moby", transportType: "streamable" },
                },
            ],
        },
    ],
});

export const yaml = config.yaml;
