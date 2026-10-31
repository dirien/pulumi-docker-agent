// Generated from docker-agent v1.154.0 examples/hook_routing_command.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("hook_routing_command", {
    // Offline routing with a command selector: no evaluator service or API call for routing.
    //
    // The command receives the hook input as JSON on stdin (see the `input`, `previous_output`
    // and `conversation` fields) and prints a transition. Empty output means "no route": the
    // current agent runs normally. Malformed output or a target outside allowed_agents stops
    // the run instead of continuing with the wrong agent.
    //
    // This keyword match is deliberately naive; it also sees earlier conversation turns.
    agents: [
        {
            name: "root",
            model: "openai/gpt-5-mini",
            description: "Routes by keyword, then answers the rest itself",
            instruction: "Answer the request.",
            routing: { allowedAgents: ["specialist"] },
            hooks: {
                beforeAgentRun: [
                    {
                        type: "command",
                        timeout: 5,
                        command: `\
if grep -qiE 'deadlock|race condition|architecture'; then
  echo '{"hook_specific_output":{"transition":{"action":"route","agent":"specialist"}}}'
fi
`,
                    },
                ],
            },
        },
        {
            name: "specialist",
            model: "openai/gpt-5",
            description: "Handles architecture and concurrency questions",
            instruction: "Investigate the complex task and explain the evidence.",
        },
    ],
});

export const yaml = config.yaml;
