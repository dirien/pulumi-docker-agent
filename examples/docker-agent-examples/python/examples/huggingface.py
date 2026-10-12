# Generated from docker-agent v1.150.0 examples/huggingface.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "huggingface",
    models={
        "huggingface_model": {
            "provider": "huggingface",
            "model": "meta-llama/Llama-3.3-70B-Instruct",
        },
    },
    agents=[
        {
            "name": "root",
            "model": "huggingface_model",
            "description": "Assistant using Hugging Face Inference Providers",
            "instruction": """\
You are a helpful assistant.
""",
            "toolsets": [{"type": "filesystem"}, {"type": "shell"}, {"type": "think"}],
        },
    ],
)
