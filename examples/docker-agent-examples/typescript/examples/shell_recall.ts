// Generated from docker-agent v1.150.0 examples/shell_recall.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("shell_recall", {
    agents: [
        {
            name: "root",
            model: "openai/gpt-4o",
            description: "Run long shell jobs and resume automatically when they finish.",
            instruction: `\
Use background-job recall for finite long-running commands, such as builds or tests.
Avoid recall for servers and watchers that are meant to keep running.
`,
            toolsets: [{ type: "shell" }, { type: "background_jobs", recall: true }],
        },
    ],
});

export const yaml = config.yaml;
