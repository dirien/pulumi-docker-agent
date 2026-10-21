// Generated from docker-agent v1.151.0 examples/opencode-go.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("opencode-go", {
    agents: [
        {
            name: "root",
            model: "opencode-go/deepseek-v4-flash",
            description: "A helpful AI assistant powered by OpenCode Go",
            instruction: `\
You are a helpful AI assistant powered by OpenCode Go.
Be helpful, accurate, and concise in your responses.
You are powered by open-source models served through OpenCode Go.
`,
        },
    ],
});

export const yaml = config.yaml;
