// Generated from docker-agent v1.154.0 examples/groq.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("groq", {
    models: { groq_model: { provider: "groq", model: "llama-3.3-70b-versatile" } },
    agents: [
        {
            name: "root",
            model: "groq_model",
            description: "Assistant using Groq",
            instruction: `\
You are a helpful assistant.
`,
            toolsets: [{ type: "filesystem" }, { type: "shell" }, { type: "think" }],
        },
    ],
});

export const yaml = config.yaml;
