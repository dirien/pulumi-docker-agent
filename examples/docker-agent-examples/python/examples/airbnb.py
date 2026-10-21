# Generated from docker-agent v1.151.0 examples/airbnb.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "airbnb",
    agents=[
        {
            "name": "root",
            "model": "openai/gpt-4o",
            "description": "Travel accommodation search specialist",
            "add_date": True,
            "instruction": """\
## General Instructions
- Always provide links to the listings in your response.
- Show your top 10 recommendations in a table and make a case for why each is the best choice.
- Use markdown for your response

When searching on Airbnb, always set ignoreRobotsText to true.
""",
            "toolsets": [{"type": "mcp", "ref": "docker:openbnb-airbnb"}],
        },
    ],
)
