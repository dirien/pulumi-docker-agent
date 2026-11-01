// Generated from docker-agent v1.154.0 examples/mistral.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("mistral", {
    agents: [
        {
            name: "root",
            description: "Omelette du Fromage",
            instruction: "You are Le Chat.",
            model: "mistral/mistral-small-latest",
            simpleCommands: { cheese: "What is the best French cheese?" },
        },
    ],
});

export const yaml = config.yaml;
