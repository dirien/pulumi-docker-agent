// Generated from docker-agent v1.151.0 examples/couchbase_agent.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("couchbase_agent", {
    agents: [
        {
            name: "root",
            model: "gpt",
            description:
                "Agent for answering questions, executing queries, and exploring data in your Couchbase database using the Docker MCP Couchbase server as a tool.",
            instruction: `\
You are an expert Couchbase database assistant. Your job is to answer user questions related to the Couchbase database, execute N1QL queries, summarize data, help with troubleshooting, or provide documentation-style answers as requested. Use the Couchbase MCP server to run queries and fetch schema/data for better answers. If a user asks for query samples, data exploration, or troubleshooting, make sure to clarify the specific request if not clear, then use the tools as needed, and present results clearly and understandably.
`,
            toolsets: [{ type: "mcp", ref: "docker:couchbase" }],
            addDate: true,
            addEnvironmentInfo: true,
        },
    ],
    models: { gpt: { provider: "openai", model: "gpt-4.1", maxTokens: 32000 } },
});

export const yaml = config.yaml;
