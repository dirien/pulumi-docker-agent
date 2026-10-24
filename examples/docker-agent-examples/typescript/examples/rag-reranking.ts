// Generated from docker-agent v1.152.0 examples/rag/reranking.yaml by `make generate_config_types`. Do not edit.
//
// This example demonstrates a chunked embeddings rag with final reranking of results
//
// Reranking re-scores the retrieved results using a specialized reranking model
// to improve relevance accuracy. The reranking happens after retrieval/fusion
// but before final limiting and deduplication.
//
// To use reranking with DMR:
// 1. Pull a reranking model: docker model pull hf.co/ggml-org/Qwen3-Reranker-0.6B-Q8_0-GGUF
// 2. The model will be loaded on-demand when the reranking endpoint is called
//
// If reranking fails (e.g., model not available), the RAG system will gracefully
// fall back to using the original retrieval scores.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("rag-reranking", {
    agents: [
        {
            name: "root",
            model: "openai-agent",
            description: "assistant with hybrid search",
            instruction: `\
You are a helpful assistant with access to hybrid retrieval
combining semantic and keyword search for comprehensive results.
`,
            toolsets: [{ type: "rag", ref: "knowledge_base" }],
        },
    ],
    rag: {
        knowledge_base: {
            tool: { description: "to be used to search for information about blorks" },
            docs: ["./docs", "./blork_field_guide.txt"],
            strategies: [
                {
                    type: "chunked-embeddings",
                    embeddingModel: "openai-embedder",
                    database: "./chunked_embeddings.db",
                    similarityMetric: "cosine_similarity",
                    chunking: { size: 1500, overlap: 75, respectWordBoundaries: true },
                    // Left out: docker-agent does not read the RAG strategy parameter `batch_size`
                    // (the number of chunks per embedding request is `embedding_batch_size`). The
                    // upstream line:
                    // batch_size: 10
                    maxEmbeddingConcurrency: 5,
                    vectorDimensions: 1536,
                },
                { type: "bm25", database: "./bm25.db" },
            ],
            results: {
                fusion: { strategy: "rrf" },
                reranking: {
                    model: "openai-rerank",

                    // Optional: Provide domain-specific criteria to guide relevance scoring
                    // The model receives metadata with each document (source path, chunk index, created_at)
                    // Use this context to guide scoring decisions.
                    // This is not considered when using DMR native reranking.
                    criteria: `\
When scoring relevance, prioritize:
- Content from official documentation (field_guide.txt) over blog posts
- Recent information (check created_at dates)
- Practical examples and field observations over theory
- Documents from docs/ directory when available
`,
                },
                deduplicate: true,
            },
        },
    },
    models: {
        // AGENT MODELS
        "openai-agent": {
            provider: "openai",
            model: "gpt-5.6-terra",
            thinkingBudget: "minimal",
            maxTokens: 64000,
        },

        // EMBEDDING MODELS
        "openai-embedder": { provider: "openai", model: "text-embedding-3-small" },
        "dmr-embedder": {
            provider: "dmr",
            model: "ai/embeddinggemma",
            maxTokens: 2048,
            providerOpts: { runtime_flags: "-ub 1024" },
        },

        // RERANKING MODELS
        "dmr-rerank": {
            provider: "dmr",
            model: "hf.co/ggml-org/qwen3-reranker-0.6b-q8_0-gguf",
            maxTokens: 32000,
        },
        "openai-rerank": { provider: "openai", model: "gpt-4.1-nano", maxTokens: 64000 },
        "gemini-rerank": { provider: "google", model: "gemini-2.5-flash", maxTokens: 64000 },
        "claude-rerank": { provider: "anthropic", model: "claude-sonnet-5", maxTokens: 64000 },
    },
});

export const yaml = config.yaml;
