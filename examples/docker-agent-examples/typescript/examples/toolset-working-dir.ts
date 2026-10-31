// Generated from docker-agent v1.154.0 examples/toolset-working-dir.yaml by `make generate_config_types`. Do not edit.
//
// Example: using working_dir for MCP and LSP toolsets
//
// Some language servers and MCP tools must be started from a specific directory.
// For example, gopls must be started from the Go module root. Use `working_dir`
// to configure the launch directory for any MCP or LSP toolset.
//
// `working_dir` is:
//   - Optional (defaults to the agent's working directory when omitted)
//   - Resolved relative to the agent's working directory if it is a relative path

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("toolset-working-dir", {
    agents: [
        {
            name: "root",
            model: "openai/gpt-5-mini",
            description: "Example agent demonstrating working_dir for MCP and LSP toolsets",
            instruction: `\
You are a helpful coding assistant with access to language server and MCP tools
launched from their respective project directories.
`,
            toolsets: [
                // LSP server started from a subdirectory (e.g. a Go module in ./backend)
                { type: "lsp", command: "gopls", workingDir: "./backend" },

                // MCP server started from a specific tools directory
                { type: "mcp", command: "my-mcp-server", workingDir: "./tools/mcp" },
            ],
        },
    ],
});

export const yaml = config.yaml;
