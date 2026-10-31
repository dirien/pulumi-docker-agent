# Generated from docker-agent v1.154.0 examples/sampling-opts.yaml by `make generate_config_types`. Do not edit.
#
# This example shows how to use provider_opts to pass sampling parameters
# like top_k and repetition_penalty to different providers.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "sampling-opts",
    agents=[
        {
            "name": "root",
            "model": "gpt",
            "description": "Assistant with custom sampling parameters",
            "instruction": """\
You are a helpful assistant running on a local model with tuned sampling parameters.
""",
        },
    ],
    models={
        "gpt": {
            "provider": "openai",
            "model": "gpt-5.6-sol",
            "temperature": 0.7,
            "top_p": 0.9,
            "provider_opts": {"top_k": 40, "repetition_penalty": 1.15, "min_p": 0.05},
        },
    },
)
