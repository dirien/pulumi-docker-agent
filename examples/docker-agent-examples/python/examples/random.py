# Generated from docker-agent v1.150.0 examples/random.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "random",
    # The agent chooses both inclusive bounds on each random_int call.
    agents=[
        {
            "name": "root",
            "model": "openai/gpt-5-mini",
            "description": "Assistant that rolls dice and makes random choices",
            "instruction": """\
Use random_int for dice rolls and random choices instead of inventing
random numbers. Choose min and max to match the request: for a
six-sided die, use min=1 and max=6; for a coin flip, use min=0 and max=1.
Both bounds are inclusive.
""",
            "toolsets": [{"type": "random"}],
        },
    ],
)
