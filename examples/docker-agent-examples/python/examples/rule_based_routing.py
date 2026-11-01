# Generated from docker-agent v1.154.0 examples/rule_based_routing.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "rule_based_routing",
    models={
        "router": {
            "provider": "anthropic",
            "model": "claude-haiku-4-5",
            "routing": [
                # Simple greetings and quick questions → fast model
                {
                    "model": "fast",
                    "examples": [
                        "hello",
                        "hi there",
                        "good morning",
                        "how are you",
                        "thanks",
                        "bye",
                    ],
                },
                # Code-related questions → capable model
                {
                    "model": "capable",
                    "examples": [
                        "explain the algorithm",
                        "implement a function",
                        "debug this code",
                        "fix this error",
                        "write a program",
                        "refactor this code",
                    ],
                },
                # Complex analysis → capable model
                {
                    "model": "capable",
                    "examples": [
                        "analyze this document in detail",
                        "compare and contrast these approaches",
                        "explain the implications of this decision",
                        "provide a comprehensive review",
                    ],
                },
            ],
        },
        "capable": {"provider": "anthropic", "model": "claude-sonnet-5"},
        "fast": {"provider": "anthropic", "model": "claude-haiku-4-5"},
    },
    agents=[
        {
            "name": "root",
            "model": "router",
            "description": "An intelligent assistant that automatically selects the best model",
            "instruction": """\
You are a helpful AI assistant. Respond to user questions accurately and concisely.

For simple questions, be brief and direct.
For complex questions, provide detailed explanations.
For code questions, include examples and explanations.
""",
        },
    ],
)
