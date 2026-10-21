# Generated from docker-agent v1.151.0 examples/yaml-anchors.yaml by `make generate_config_types`. Do not edit.
#
# This example demonstrates YAML anchors (&name), aliases (*name) and merge
# keys (<<). Declare a value once and reuse it across agents instead of
# copy-pasting the same block.
#
# An anchor has to sit on a real value inside a known section (here, real
# agents). A separate top-level block such as `defaults:` is rejected as an
# unknown field.

import ediri_docker_agent as dockeragent

house_rules = """\
You are part of the Acme engineering team.
Cite the files you looked at and keep changes minimal.
"""

specialist = {
    "model": "sonnet",
    "description": "Reviews code changes for correctness and clarity.",
    "instruction": house_rules,  # reuse the same instruction, declared once
    "toolsets": [{"type": "filesystem"}],
}

config = dockeragent.AgentConfig(
    "yaml-anchors",
    models={
        "sonnet": {
            "provider": "anthropic",
            "model": "claude-sonnet-5",
            "max_tokens": 64000,
        },
    },
    agents=[
        {
            "name": "root",
            "model": "sonnet",
            "description": "Coordinator that delegates to specialist sub-agents.",
            # `&house_rules` anchors this instruction so it can be reused verbatim.
            "instruction": house_rules,
            "sub_agents": ["reviewer", "documenter"],
        },
        # `&specialist` anchors this whole agent so it can be merged into others.
        {
            "name": "reviewer",
            **specialist,
        },
        # Merge everything from `reviewer` (model, instruction, toolsets), then
        # override a single field. Overriding a merged key works in `agents:`.
        {
            "name": "documenter",
            **specialist,
            "description": "Writes and updates project documentation.",
        },
    ],
)
