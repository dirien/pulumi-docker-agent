// Generated from docker-agent v1.151.0 examples/agent_switching_commands.yaml by `make generate_config_types`. Do not edit.
//
// This example demonstrates slash commands that switch the active agent.
// Type "/plan" in the TUI to hand the conversation off to the planner sub-agent,
// "/review" to switch to the reviewer, and "/back" to return to the root agent.
//
// Anything typed after the slash command (e.g. "/plan add a logout button") is
// forwarded as the first user prompt to the target agent.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("agent_switching_commands", {
    models: { claude: { provider: "anthropic", model: "claude-sonnet-5" } },
    agents: [
        {
            name: "root",
            model: "claude",
            description: "The default agent. Implements the work once a plan exists.",
            instruction: `\
You are the implementation agent. You write and edit code.
If the user asks for a plan or design discussion, use the /plan command
to switch to the planner sub-agent.
`,
            subAgents: ["planner", "reviewer"],
            toolsets: [{ type: "filesystem" }, { type: "shell" }],
            commands: {
                plan: { description: "Hand off to the planner sub-agent", agent: "planner" },
                review: { description: "Hand off to the reviewer sub-agent", agent: "reviewer" },
            },
        },
        {
            name: "planner",
            model: "claude",
            description: "Plans the work before implementation.",
            instruction: `\
You are the planning agent. Ask clarifying questions, then produce a
step-by-step plan in markdown. Do not write code.
`,
            subAgents: ["root"],
            commands: { back: { description: "Return to the root agent", agent: "root" } },
        },
        {
            name: "reviewer",
            model: "claude",
            description: "Reviews local changes for quality and security.",
            instruction: `\
You review the local Git changes and provide concise, actionable feedback
on code quality, security, and maintainability.
`,
            subAgents: ["root"],
            toolsets: [{ type: "shell" }],
            commands: { back: { description: "Return to the root agent", agent: "root" } },
        },
    ],
});

export const yaml = config.yaml;
