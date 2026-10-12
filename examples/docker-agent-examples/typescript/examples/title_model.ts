// Generated from docker-agent v1.150.0 examples/title_model.yaml by `make generate_config_types`. Do not edit.
//
// Title Model Example
//
// This example demonstrates how a model can delegate session-title
// generation to another, cheaper/faster model via the `title_model` field.
//
// Session titles are short summaries generated from the first user message.
// There is no need to spend a heavyweight reasoning model on that: point the
// primary model's `title_model` at a small model and docker-agent will use it
// for the title-generation call only. Everything else still runs on the
// primary model. When `title_model` is omitted, title generation reuses the
// agent's own model.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("title_model", {
    models: {
        primary: {
            provider: "anthropic",
            model: "claude-sonnet-5",

            // Generate session titles with the cheaper Haiku model instead of Sonnet.
            titleModel: "fast",
        },
        fast: { provider: "anthropic", model: "claude-haiku-4-5" },
    },
    agents: [
        {
            name: "root",
            model: "primary",
            description: "An assistant that generates session titles with a cheaper model.",
            instruction: "You are a helpful assistant.",
        },
    ],
});

export const yaml = config.yaml;
