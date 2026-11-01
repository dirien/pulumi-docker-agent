// Generated from docker-agent v1.154.0 examples/instruction_file.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("instruction_file", {
    agents: [
        {
            name: "coordinator",
            model: "openai/gpt-5-mini",
            description: "Routes work between specialist agents",

            // Instead of inlining a long prompt here, point at a Markdown file kept
            // next to the config. The path is relative to this config file's directory
            // and its contents are loaded as the agent's instruction at startup.
            instructionFile: ["instructions/coordinator.md"],
            subAgents: ["writer"],
        },
        {
            name: "writer",
            model: "openai/gpt-5-mini",
            description: "Drafts and edits written content",

            // instruction_file also accepts a list of files. Their contents are
            // concatenated in order (separated by a blank line), so a shared preamble
            // can be reused across agents while each appends its own specifics.
            instructionFile: ["instructions/shared-preamble.md", "instructions/writer.md"],
        },
    ],

    // `instructionFile` is read relative to the agent file when docker-agent loads it. The provider
    // validates the rendered YAML in memory, where there are no files, so validation is skipped.
    skipValidation: true,
});

export const yaml = config.yaml;
