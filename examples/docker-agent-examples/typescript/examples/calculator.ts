// Generated from docker-agent v1.154.0 examples/calculator.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("calculator", {
    agents: [
        {
            name: "root",
            model: "openai/gpt-5-mini",
            description: "A helpful assistant with exact arithmetic",
            instruction: `\
Help the user solve numerical problems. Use calculate for all arithmetic
instead of calculating mentally. Explain the expression you used and
include units in your answer. If approximate is true, identify the decimal
as rounded and use the exact result for any further calculations.
`,
            toolsets: [{ type: "calculator" }],
        },
    ],
});

export const yaml = config.yaml;
