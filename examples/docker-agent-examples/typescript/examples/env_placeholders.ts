// Generated from docker-agent v1.151.0 examples/env_placeholders.yaml by `make generate_config_types`. Do not edit.
//
// Example demonstrating environment variable placeholder expansion in commands
//
// This example shows how to use ${env.VARIABLE_NAME} placeholders in command
// definitions, which are automatically replaced with environment variable
// values when the agent is loaded.
//
// ${env.X} is the canonical syntax for command, instruction, description, and
// header fields (it is evaluated by a JavaScript template engine, so you also
// get defaults via `|| 'fallback'`, ternaries, etc.). The shell-style $VAR /
// ${VAR} form is NOT expanded in these fields.
//
// Usage:
//   export USER=alice
//   export PROJECT_NAME=myproject
//   export ENVIRONMENT=production
//
//   docker agent run env_placeholders.yaml -c greet
//   docker agent run env_placeholders.yaml -c analyze
//   docker agent run env_placeholders.yaml -c status
//
// Note: Undefined environment variables expand to empty strings without causing errors,
// allowing you to only define the variables needed for the commands you actually use.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("env_placeholders", {
    agents: [
        {
            name: "root",
            model: "anthropic/claude-sonnet-4-5",
            description: "Agent demonstrating environment variable placeholder expansion",
            instruction: `\
You are a helpful assistant that responds to user requests.
Provide clear, concise, and actionable responses.
`,
            simpleCommands: {
                greet: "Say hello to ${env.USER} and ask how their day is going",
                analyze:
                    "Analyze the project named ${env.PROJECT_NAME} in the ${env.ENVIRONMENT} environment and provide insights",
                status: "Check the status of the service ${env.SERVICE_NAME} and report any issues",
                report: "Generate a comprehensive report for project ${env.PROJECT_NAME} owned by ${env.OWNER} deployed in ${env.LOCATION}",
                inspect:
                    "Inspect the contents of the directory at ${env.WORKING_DIR} and summarize what you find",
                help: "Explain what you can do and how to use environment variable placeholders in commands",
            },
            toolsets: [{ type: "filesystem" }, { type: "shell" }],
        },
    ],
});

export const yaml = config.yaml;
