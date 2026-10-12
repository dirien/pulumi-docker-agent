# Generated from docker-agent v1.150.0 examples/fetch_escape_html.yaml by `make generate_config_types`. Do not edit.
#
# Restore legacy HTML escaping in multi-URL JSON results.
# Omit escape_html (or set it to false) for the token-efficient default.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "fetch_escape_html",
    agents=[
        {
            "name": "root",
            "model": "anthropic/claude-sonnet-4-5",
            "description": "Fetches pages using legacy JSON encoding.",
            "instruction": """\
Fetch the user's URLs together in one call and summarize the content.
""",
            "toolsets": [{"type": "fetch", "escape_html": True}],
        },
    ],
)
