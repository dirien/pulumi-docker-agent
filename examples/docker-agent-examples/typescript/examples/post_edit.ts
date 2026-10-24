// Generated from docker-agent v1.152.0 examples/post_edit.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("post_edit", {
    agents: [
        {
            name: "root",
            description: "A Go developer agent with post-edit formatting",
            model: "openai/gpt-4o",
            instruction: "You are a Go developer assistant.",
            toolsets: [{ type: "filesystem", postEdit: [{ path: "*.go", cmd: "gofmt -w $path" }] }],
        },
    ],
});

export const yaml = config.yaml;
