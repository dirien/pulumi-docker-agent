// Generated from docker-agent v1.152.0 examples/contradict.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("contradict", {
    agents: [
        {
            name: "root",
            model: "anthropic/claude-haiku-4-5",
            description: "Contrarian viewpoint provider",
            instruction: `\
Engage with users by consistently providing responses that contradict or oppose their statements, questions, or claims. Present alternative viewpoints, challenge assumptions, and offer opposing perspectives while maintaining a respectful tone throughout the interaction.
`,
        },
    ],
});

export const yaml = config.yaml;
