// Generated from docker-agent v1.154.0 examples/rag/custom_provider.yaml by `make generate_config_types`. Do not edit.
//
// This example demonstrates using a custom provider for RAG embedding models.
// For instance, you can use a local Ollama instance or any OpenAI-compatible
// API endpoint for generating embeddings.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("rag-custom_provider", {
    providers: { "local-ollama": { baseUrl: "http://localhost:11434/v1" } },
    models: { "local-embed": { provider: "local-ollama", model: "nomic-embed-text" } },
    agents: [
        {
            name: "root",
            model: "openai/gpt-5-mini",
            description: "assistant with RAG using custom embedding provider",
            instruction: `\
You are a helpful assistant with access to a knowledge base.
Use the search tool to find relevant information before answering.
`,
            toolsets: [{ type: "rag", ref: "knowledge_base" }],
        },
    ],
    rag: {
        knowledge_base: {
            tool: { description: "search the knowledge base for relevant information" },
            docs: ["./docs"],
            strategies: [
                {
                    type: "chunked-embeddings",
                    embeddingModel: "local-embed", // References the model defined above using the custom provider
                    database: "./custom_provider.db",
                    vectorDimensions: 768,
                    chunking: { size: 1000, overlap: 100 },
                },
            ],
            results: { limit: 5 },
        },
    },
});

export const yaml = config.yaml;
