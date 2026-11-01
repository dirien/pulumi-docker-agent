// Generated from docker-agent v1.154.0 examples/code_mode.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("code_mode", {
    agents: [
        {
            name: "root",
            model: "anthropic/claude-sonnet-4-5",
            description: "Demonstrates the use of Code Mode with tools",
            instruction: "Use your tool to help the user with their github requests.",
            codeModeTools: true,
            simpleCommands: {
                demo: "How many issues in docker/docker-agent have a number that is prime?",
            },
            toolsets: [{ type: "mcp", ref: "docker:github-official" }],
        },
    ],
});

export const yaml = config.yaml;
