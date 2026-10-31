// Generated from docker-agent v1.154.0 examples/yaml-anchors.yaml by `make generate_config_types`. Do not edit.
//
// This example demonstrates YAML anchors (&name), aliases (*name) and merge
// keys (<<). Declare a value once and reuse it across agents instead of
// copy-pasting the same block.
//
// An anchor has to sit on a real value inside a known section (here, real
// agents). A separate top-level block such as `defaults:` is rejected as an
// unknown field.

import * as dockeragent from "@ediri/pulumi-docker-agent";

const houseRules = `\
You are part of the Acme engineering team.
Cite the files you looked at and keep changes minimal.
`;

const specialist = {
    model: "sonnet",
    description: "Reviews code changes for correctness and clarity.",
    instruction: houseRules, // reuse the same instruction, declared once
    toolsets: [{ type: "filesystem" }],
};

export const config = new dockeragent.AgentConfig("yaml-anchors", {
    models: { sonnet: { provider: "anthropic", model: "claude-sonnet-5", maxTokens: 64000 } },
    agents: [
        {
            name: "root",
            model: "sonnet",
            description: "Coordinator that delegates to specialist sub-agents.",

            // `&house_rules` anchors this instruction so it can be reused verbatim.
            instruction: houseRules,
            subAgents: ["reviewer", "documenter"],
        },

        // `&specialist` anchors this whole agent so it can be merged into others.
        {
            name: "reviewer",
            ...specialist,
        },

        // Merge everything from `reviewer` (model, instruction, toolsets), then
        // override a single field. Overriding a merged key works in `agents:`.
        {
            name: "documenter",
            ...specialist,
            description: "Writes and updates project documentation.",
        },
    ],
});

export const yaml = config.yaml;
