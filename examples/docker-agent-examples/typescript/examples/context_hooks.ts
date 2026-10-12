// Generated from docker-agent v1.150.0 examples/context_hooks.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("context_hooks", {
    agents: [
        {
            name: "root",
            model: "anthropic/claude-sonnet-4-5",
            description: "Assistant with dependency-free session context.",
            instruction: "You are a helpful assistant.",
            hooks: {
                sessionStart: [
                    {
                        type: "builtin",
                        command: "add_context",
                        args: [
                            "Current session ID: {{ .SessionID }}",
                            "Agent: {{ .AgentName }}",
                            "Working directory: {{ .Cwd }}",
                        ],
                    },
                ],
            },
        },
    ],
});

export const yaml = config.yaml;
