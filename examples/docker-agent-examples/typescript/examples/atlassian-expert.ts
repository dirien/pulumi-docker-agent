// Generated from docker-agent v1.151.0 examples/atlassian-expert.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("atlassian-expert", {
    models: { sonnet: { provider: "anthropic", model: "claude-sonnet-4-6" } },
    agents: [
        {
            name: "root",
            model: "sonnet",
            description: "Atlassian Expert",
            instruction:
                "You are an AI assistant with expertise in the Atlassian suite of products (Jira, Confluence).",
            toolsets: [
                {
                    type: "mcp",
                    remote: {
                        url: "https://mcp.atlassian.com/v1/mcp/authv2",
                        transportType: "streamable",
                        oauth: {
                            callbackRedirectURL:
                                "https://ai-backend-service.docker.com/callback?port=${callbackPort}",
                        },
                    },
                },
            ],
        },
    ],
});

export const yaml = config.yaml;
