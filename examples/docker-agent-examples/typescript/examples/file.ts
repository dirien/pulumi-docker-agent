// Generated from docker-agent v1.151.0 examples/file.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("file", {
    agents: [
        {
            name: "root",
            model: "openai/gpt-5-mini",
            description: "An agent that can read, write, and edit files in the working directory",
            instruction: "Work only with files whose paths are provided by the user.",
            toolsets: [{ type: "file", allowList: ["."] }],
        },
    ],
});

export const yaml = config.yaml;
