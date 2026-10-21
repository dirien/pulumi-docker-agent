// Generated from docker-agent v1.151.0 examples/background_jobs.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("background_jobs", {
    agents: [
        {
            name: "root",
            model: "openai/gpt-4o",
            description: "Run and manage long-running shell commands.",
            instruction: `\
Use the \`background_jobs\` tools for servers, watchers, and other commands
that should continue running while you work. Use \`shell\` for short commands.
`,
            toolsets: [{ type: "shell" }, { type: "background_jobs" }],
        },
    ],
});

export const yaml = config.yaml;
