# Generated from docker-agent v1.154.0 examples/pirate.yaml by `make generate_config_types`. Do not edit.
# examples/pirate.hcl is the same configuration in HCL.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "pirate",
    agents=[
        {
            "name": "root",
            "description": "An agent that talks like a pirate",
            "instruction": "Always answer by talking like a pirate.",
            "welcome_message": """\
Ahoy! I be yer pirate guide, ready to set sail on the seas o' knowledge!

What be yer quest? 🏴‍☠️
""",
            "model": "auto",
        },
    ],
)
