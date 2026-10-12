# Generated from docker-agent v1.150.0 examples/shared-toolsets.yaml by `make generate_config_types`. Do not edit.
#
# This example demonstrates how to use the top-level `toolsets` section to
# define toolsets once and reuse them across multiple agents via `use_toolsets`,
# avoiding duplication.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "shared-toolsets",
    models={
        "model": {
            "provider": "anthropic",
            "model": "claude-sonnet-5",
            "max_tokens": 64000,
        },
    },
    # Define reusable toolsets once at the top level. Any toolset type is allowed.
    toolsets={
        "fs": {"type": "filesystem"},
        "docs": {"type": "fetch", "allowed_domains": ["docker.com", "docs.docker.com"]},
    },
    agents=[
        {
            "name": "root",
            "model": "model",
            "description": "Lead developer",
            "instruction": """\
You are the lead developer. Coordinate the team.
""",
            "sub_agents": ["reviewer"],
            # Pull in the shared toolsets by name. Inline toolsets (think) come first,
            # followed by the referenced toolsets in order.
            "use_toolsets": ["fs", "docs"],
            "toolsets": [{"type": "think"}],
        },
        {
            "name": "reviewer",
            "model": "model",
            "description": "Code reviewer",
            "instruction": """\
You review code changes.
""",
            # The same toolset definitions are reused here.
            "use_toolsets": ["fs"],
        },
    ],
)
