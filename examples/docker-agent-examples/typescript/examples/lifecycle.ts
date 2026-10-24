// Generated from docker-agent v1.152.0 examples/lifecycle.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("lifecycle", {
    agents: [
        {
            name: "root",
            model: "openai/gpt-5.6-terra",
            description: "An example agent that demonstrates per-toolset lifecycle policies.",
            instruction: `\
Use the configured tools to answer questions.
`,
            toolsets: [
                // 1. Strict profile: marks gopls as critical so a future eager-startup
                //    phase will refuse to start the agent if it can't come up. Today
                //    the agent still boots without it and surfaces a warning.
                {
                    type: "lsp",
                    command: "gopls",
                    fileTypes: [".go"],
                    lifecycle: {
                        profile: "strict", // required=true (informational), no auto-restart
                        startupTimeout: "30s", // informational; not yet enforced
                    },
                },

                // 2. Resilient profile (the default if 'lifecycle' is omitted).
                //    Auto-restart with exponential backoff and a higher restart budget.
                {
                    type: "mcp",
                    ref: "docker:context7",
                    lifecycle: {
                        profile: "resilient",
                        maxRestarts: 10,
                        backoff: { initial: "500ms", max: "1m", multiplier: 2.0, jitter: 0.2 },
                    },
                },

                // 3. Best-effort profile: single attempt, no restart, optional.
                //    Good for experimental MCPs whose flakiness shouldn't amplify into a
                //    restart loop.
                {
                    type: "mcp",
                    ref: "docker:openbnb-airbnb",
                    lifecycle: { profile: "best-effort" },
                },
            ],
        },
    ],
    models: { "openai/gpt-5.6-terra": { provider: "openai", model: "gpt-5.6-terra" } },
    metadata: {
        description:
            "Demonstrates per-toolset lifecycle profiles (strict / resilient / best-effort)",
    },
});

export const yaml = config.yaml;
