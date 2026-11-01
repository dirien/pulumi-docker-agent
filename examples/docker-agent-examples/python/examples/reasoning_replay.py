# Generated from docker-agent v1.154.0 examples/reasoning_replay.yaml by `make generate_config_types`. Do not edit.
#
# Example: Replaying reasoning to an OpenAI-compatible model
#
# Docker Agent captures reasoning from reasoning models (delta.reasoning_content
# or delta.reasoning on OpenAI-compatible streams) and replays it on later turns,
# so the model sees its own earlier reasoning instead of re-deriving it after
# every tool call. The replay is sent as a reasoning_content field on the
# assistant message.
#
# Replay defaults on for Chat Completions. Set
# provider_opts.replay_reasoning: false when an endpoint rejects or must not
# receive the non-standard field. Replayed reasoning counts against the context
# window, so a model that reasons at length is also the case where opting out
# saves the most tokens.
#
# This option is Chat Completions only. The Responses API has a separate
# preserve_reasoning option for its encrypted reasoning items; neither option
# applies to the other API.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "reasoning_replay",
    providers={
        # A local server fronting an open-weight reasoning model. Replay is already
        # on for this destination; the explicit setting is shown for contrast.
        "local_vllm": {
            "provider": "openai",
            "api_type": "openai_chatcompletions",
            "base_url": "http://localhost:8000/v1",
        },
    },
    models={
        "qwen_replay": {
            "provider": "local_vllm",
            "model": "qwen3-30b-a3b",
            "provider_opts": {"replay_reasoning": True},
        },
        # Same server, but this model should never receive stored reasoning.
        "qwen_no_replay": {
            "provider": "local_vllm",
            "model": "qwen3-30b-a3b",
            "provider_opts": {"replay_reasoning": False},
        },
    },
    agents=[
        {
            "name": "root",
            "model": "qwen_replay",
            "description": "Main assistant, replaying its own reasoning",
            "instruction": """\
You are a helpful AI assistant. Be concise and helpful in your responses.
""",
        },
        {
            "name": "analyst",
            "model": "qwen_no_replay",
            "description": "Sub-agent that starts each turn from scratch",
            "instruction": """\
You are a specialized assistant for specific tasks.
""",
        },
    ],
)
