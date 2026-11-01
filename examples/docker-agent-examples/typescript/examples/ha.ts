// Generated from docker-agent v1.154.0 examples/ha.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("ha", {
    agents: [
        {
            name: "root",
            model: "anthropic/claude-haiku-4-5",
            description: "Expert in Home Automation",
            instruction: `\
You are a Home Automation expert with deep knowledge in Home Assistant and various smart home technologies.
You help users set up and manage their smart home devices, create automation routines, and troubleshoot issues.
Provide clear, concise, and accurate information to assist users in optimizing their home automation systems.
`,
            toolsets: [
                {
                    type: "mcp",
                    remote: {
                        url: "http://homeassistant.local:8123/api/mcp",
                        transportType: "streamable",
                        headers: { Authorization: "Bearer ${env.HA_TOKEN}" },
                    },
                },
            ],
        },
    ],
});

export const yaml = config.yaml;
