// Generated from docker-agent v1.151.0 examples/cached_responses.yaml by `make generate_config_types`. Do not edit.
//
// Demonstrates the response cache.
//
// The first time a question is asked, the agent calls the model normally and
// stores the answer. The second time the same question is asked, the answer is
// replayed verbatim and the model is not invoked at all.
//
// Two normalization options control what "same question" means:
//   - case_sensitive: when false (the default), "Hello" and "hello" match.
//   - trim_spaces: when true, leading and trailing whitespace is ignored.
//
// Storage is in-memory by default. Set `path` to persist entries to a JSON
// file that is reloaded on startup so the cache survives restarts. Multiple
// processes can safely share the same file: an advisory lock on
// `<path>.lock` serializes writes, and Lookup reloads the in-memory map
// when the file changes externally.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("cached_responses", {
    agents: [
        {
            name: "root",
            model: "openai/gpt-5-mini",
            description: "A helpful AI assistant with a response cache",
            instruction: `\
You are a knowledgeable assistant that helps users with various tasks.
Be helpful, accurate, and concise in your responses.
`,
            cache: {
                enabled: true,
                caseSensitive: false, // "Hello" == "hello"
                trimSpaces: true, // "  hello  " == "hello"
                path: "./cache.json", // remove this line to keep the cache in memory only
            },
        },
    ],
});

export const yaml = config.yaml;
