// Generated from docker-agent v1.151.0 examples/task_budget.yaml by `make generate_config_types`. Do not edit.
//
// Anthropic `task_budget` caps the total tokens a model spends across a
// multi-step agentic task (thinking + tool calls + final output). docker-agent
// forwards it as `output_config.task_budget` and automatically attaches the
// `task-budgets-2026-03-13` beta header.
//
// It can be set on any Claude model; Opus 4.7 and newer actually honor it. See:
// https://platform.claude.com/docs/en/build-with-claude/task-budgets
//
// Run the demo command with:  docker agent run task_budget.yaml -c demo

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("task_budget", {
    // The provider is declared explicitly here purely for illustration; for
    // catalog-known models like claude-opus-5 you can set `task_budget` directly
    // under `models.<name>` with the default `anthropic` provider.
    providers: { "anthropic-opus-47": { provider: "anthropic", tokenKey: "ANTHROPIC_API_KEY" } },
    agents: [
        {
            name: "root",
            model: "opus-bounded",
            description: "a helpful assistant with a bounded task token budget",
            instruction: "Stay within the configured task token budget.",
            simpleCommands: {
                demo: "design and sketch a small Python CLI that fetches weather data",
            },
            toolsets: [{ type: "shell" }],
        },
    ],
    models: {
        // Integer shorthand = a "tokens" budget of 128k for the whole task.
        "opus-bounded": {
            provider: "anthropic-opus-47",
            model: "claude-opus-5",
            taskBudget: 128000,
            thinkingBudget: "adaptive", // task_budget pairs well with adaptive thinking
        },

        // Explicit object form, equivalent to `task_budget: 64000`.
        "opus-bounded-tight": {
            provider: "anthropic-opus-47",
            model: "claude-opus-5",
            taskBudget: 64000,
        },
    },
});

export const yaml = config.yaml;
