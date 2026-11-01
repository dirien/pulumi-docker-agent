// Generated from docker-agent v1.154.0 examples/pythonista.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("pythonista", {
    agents: [
        {
            name: "root",
            model: "claude",
            description: "Expert Python developer and assistant",
            instruction: `\
You are an AI assistant with an expertise in Python development.

Your responses should be clear, concise, and focused on providing accurate information about Python programming,
libraries, and best practices.
`,
            toolsets: [{ type: "filesystem" }, { type: "shell" }],
        },
    ],
    models: { claude: { provider: "anthropic", model: "claude-sonnet-5", maxTokens: 64000 } },
});

export const yaml = config.yaml;
