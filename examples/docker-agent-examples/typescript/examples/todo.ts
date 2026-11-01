// Generated from docker-agent v1.154.0 examples/todo.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("todo", {
    agents: [
        {
            name: "root",
            model: "anthropic/claude-sonnet-4-5",
            description: "Expert developer",
            instruction: `\
You are an expert developer, your only purpose in life is to help the user with their query.

The user wants you to edit their code. Do not show the code, use the tools for file editing directly
`,
            toolsets: [{ type: "filesystem" }, { type: "todo" }],
        },
    ],
});

export const yaml = config.yaml;
