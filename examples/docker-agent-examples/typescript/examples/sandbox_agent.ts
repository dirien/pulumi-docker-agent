// Generated from docker-agent v1.154.0 examples/sandbox_agent.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("sandbox_agent", {
    runtime: {
        // Always run this agent inside a Docker sandbox so callers don't need
        // to remember --sandbox on the command line. An explicit
        // --sandbox=false on the CLI still wins.
        sandbox: true,

        // Hosts the agent's tools need to reach. Unioned with the
        // auto-discovered tool-install hosts and the configured models
        // gateway, then added to the sandbox proxy's default-deny
        // allowlist. Use this for endpoints the aqua-based resolver
        // can't infer.
        networkAllowlist: ["api.example.com", "registry.npmjs.org"],
    },
    agents: [
        {
            name: "root",
            model: "openai/gpt-4o",
            description: `\
A helpful assistant that runs shell commands in a sandboxed environment.
All commands execute inside a Docker container with limited filesystem access.
The agent declares runtime.sandbox: true above, so:
  docker agent run examples/sandbox_agent.yaml
is equivalent to:
  docker agent run --sandbox examples/sandbox_agent.yaml
`,
            instruction:
                "You are a helpful assistant with access to a sandboxed shell environment.",
            toolsets: [{ type: "shell" }],
        },
    ],
});

export const yaml = config.yaml;
