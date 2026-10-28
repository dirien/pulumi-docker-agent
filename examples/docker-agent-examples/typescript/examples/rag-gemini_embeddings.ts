// Generated from docker-agent v1.152.0 examples/rag/gemini_embeddings.yaml by `make generate_config_types`. Do not edit.
//
// This example demonstrates using a Google Gemini embedding model for RAG.
//
// Requires GOOGLE_API_KEY (or GEMINI_API_KEY). Vertex AI works too via
// GOOGLE_GENAI_USE_VERTEXAI / project + location provider_opts.
//
// Only text is embedded, and no task_type is sent: queries and documents share
// the same vector space without automatic task prefixes.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("rag-gemini_embeddings", {
    models: {
        "gemini-embed": {
            provider: "google",

            // gemini-embedding-001 also works; vectors from different models are not
            // comparable, so rebuild the index when switching.
            model: "gemini-embedding-2",
            providerOpts: {
                // Optional: truncate vectors (Matryoshka). Omit to keep the native size (3072).
                // Must match vector_dimensions below.
                output_dimensionality: 768,
            },
        },
    },
    agents: [
        {
            name: "root",
            model: "google/gemini-2.5-flash",
            description: "assistant with RAG backed by Gemini embeddings",
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
                    embeddingModel: "gemini-embed",
                    database: "./gemini_embeddings.db",
                    vectorDimensions: 768,
                    chunking: { size: 1000, overlap: 100 },
                },
            ],
            results: { limit: 5 },
        },
    },
});

export const yaml = config.yaml;
