// Generated from docker-agent v1.151.0 examples/remote_mcp_oauth.yaml by `make generate_config_types`. Do not edit.
//
// Example: Remote MCP server with explicit OAuth credentials.
// Use this when connecting to MCP servers that do NOT support
// Dynamic Client Registration (RFC 7591), such as Slack or GitHub.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("remote_mcp_oauth", {
    agents: [
        {
            name: "root",
            model: "openai/gpt-4.1-mini",
            description: "Assistant with remote MCP tools using explicit OAuth credentials",
            instruction: "You are a helpful assistant with access to remote tools.",
            toolsets: [
                {
                    type: "mcp",
                    remote: {
                        url: "https://mcp.example.com/mcp",
                        transportType: "streamable",
                        oauth: {
                            clientId: "your-client-id",
                            clientSecret: "your-client-secret",
                            callbackPort: 8080,
                            scopes: ["read", "write"],
                        },
                    },
                },
            ],
        },
    ],
});

export const yaml = config.yaml;
