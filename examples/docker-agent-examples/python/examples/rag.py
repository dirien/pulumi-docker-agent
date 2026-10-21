# Generated from docker-agent v1.151.0 examples/rag.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "rag",
    agents=[
        {
            "name": "root",
            "model": "gpt-minimal",
            "description": "assistant that can search a knowledge base",
            "instruction": """\
You are a helpful assistant. You have access to a knowledge base you
can use when it makes sense to do so, based on the user's question.
If you receive sources from the knowledge base, always include them as
a markdown list of links to local files at the very end of your response.
""",
            "welcome_message": "Ask me anything about Blorks.",
            "toolsets": [{"type": "rag", "ref": "blork_knowledge_base"}],
        },
    ],
    models={
        "gpt-minimal": {
            "provider": "openai",
            "model": "gpt-5.6-sol",
            "thinking_budget": "minimal",
        },
    },
    rag={
        "blork_knowledge_base": {
            "tool": {"description": "documents about the blorks"},
            "docs": ["./rag/docs", "./rag/blork_field_guide.txt"],
            "strategies": [
                {
                    "type": "chunked-embeddings",
                    "embedding_model": "openai/text-embedding-3-small",
                    "database": "./rag/chunked_embeddings.db",
                    "vector_dimensions": 1536,
                },
            ],
        },
    },
)
