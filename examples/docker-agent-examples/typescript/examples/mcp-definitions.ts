// Generated from docker-agent v1.151.0 examples/mcp-definitions.yaml by `make generate_config_types`. Do not edit.
//
// This example demonstrates how to use the top-level `mcps` section to define
// MCP servers once and reference them across multiple agents, avoiding duplication.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("mcp-definitions", {
    models: { model: { provider: "anthropic", model: "claude-sonnet-5", maxTokens: 64000 } },

    // Define MCP servers once at the top level.
    // Each entry can use `command`, `remote`, or `ref` (just like inline MCP toolsets).
    mcps: {
        context7: { ref: "docker:context7" },
        github: {
            ref: "docker:github-official",
            env: { GITHUB_PERSONAL_ACCESS_TOKEN: "${env.GITHUB_PERSONAL_ACCESS_TOKEN}" },
            tools: [
                "create_or_update_file",
                "search_repositories",
                "create_repository",
                "get_file_contents",
                "push_files",
                "create_issue",
                "create_pull_request",
                "list_issues",
            ],
        },
    },
    agents: [
        {
            name: "root",
            model: "model",
            description: "Lead developer",
            instruction: `\
You are the lead developer. Coordinate the team.
`,
            subAgents: ["frontend", "backend"],
            toolsets: [
                { type: "filesystem" },
                { type: "think" },

                // Reference the MCP definition by name — no need to repeat the full config.
                { type: "mcp", ref: "context7" },
                { type: "mcp", ref: "github" },
            ],
        },
        {
            name: "frontend",
            model: "model",
            description: "Frontend engineer",
            instruction: `\
You are a frontend engineer.
`,
            toolsets: [
                { type: "filesystem" },
                { type: "shell" },

                // Same MCP server, referenced by name.
                { type: "mcp", ref: "context7" },

                // Reference github MCP but override the tool list for this agent.
                { type: "mcp", ref: "github", tools: ["get_file_contents", "search_repositories"] },
            ],
        },
        {
            name: "backend",
            model: "model",
            description: "Backend engineer",
            instruction: `\
You are a backend engineer.
`,
            toolsets: [
                { type: "filesystem" },
                { type: "shell" },
                { type: "mcp", ref: "context7" },
                { type: "mcp", ref: "github" },
            ],
        },
    ],
});

export const yaml = config.yaml;
