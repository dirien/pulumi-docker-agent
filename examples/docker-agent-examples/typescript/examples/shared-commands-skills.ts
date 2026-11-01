// Generated from docker-agent v1.154.0 examples/shared-commands-skills.yaml by `make generate_config_types`. Do not edit.
//
// This example demonstrates the top-level `commands` and `skills` sections.
// Define reusable command and skill groups once, then reference them by name
// from multiple agents via `use_commands` / `use_skills`, avoiding duplication.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("shared-commands-skills", {
    models: { model: { provider: "anthropic", model: "claude-sonnet-5", maxTokens: 64000 } },

    // Reusable, named command groups.
    commands: {
        ci: { simpleCommands: { deploy: "Deploy the application", test: "Run the test suite" } },
        docs: {
            commands: {
                "build-docs": {
                    description: "Build the documentation site",
                    instruction: "Build the docs and report any broken links.",
                },
            },
        },
    },

    // Reusable, named skill groups (same value format as an agent's `skills`).
    skills: { base: { skills: ["local", "git"] } },
    agents: [
        {
            name: "root",
            model: "model",
            description: "Lead developer",
            instruction: `\
You are the lead developer. Coordinate the team.
`,
            subAgents: ["docs-writer"],

            // Pull in the "ci" command group and the "base" skill group by name.
            useCommands: ["ci"],
            useSkills: ["base"],

            // Inline commands are merged in and win on name conflicts.
            simpleCommands: { lint: "Run the linter" },
        },
        {
            name: "docs-writer",
            model: "model",
            description: "Documentation writer",
            instruction: `\
You write and maintain the project documentation.
`,

            // Reuse the same groups, plus the docs-specific commands.
            useCommands: ["ci", "docs"],
            useSkills: ["base"],
        },
    ],
});

export const yaml = config.yaml;
