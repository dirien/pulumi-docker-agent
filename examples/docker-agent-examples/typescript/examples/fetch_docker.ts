// Generated from docker-agent v1.152.0 examples/fetch_docker.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("fetch_docker", {
    agents: [
        {
            name: "root",
            model: "anthropic/claude-sonnet-4-5",
            description: "An agent that fetches and summarizes web content",
            instruction: `\
You are a web content analyzer that fetches web pages and provides concise summaries.
When asked to summarize a website:
1. Use the fetch tool to retrieve the content
2. Analyze the content and extract key information
3. Provide a clear, structured summary
Be concise and focus on the most important information.
`,
            toolsets: [{ type: "fetch" }],
        },
    ],
});

export const yaml = config.yaml;
