# Generated from docker-agent v1.152.0 examples/instruction_file.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "instruction_file",
    agents=[
        {
            "name": "coordinator",
            "model": "openai/gpt-5-mini",
            "description": "Routes work between specialist agents",
            # Instead of inlining a long prompt here, point at a Markdown file kept
            # next to the config. The path is relative to this config file's directory
            # and its contents are loaded as the agent's instruction at startup.
            "instruction_file": ["instructions/coordinator.md"],
            "sub_agents": ["writer"],
        },
        {
            "name": "writer",
            "model": "openai/gpt-5-mini",
            "description": "Drafts and edits written content",
            # instruction_file also accepts a list of files. Their contents are
            # concatenated in order (separated by a blank line), so a shared preamble
            # can be reused across agents while each appends its own specifics.
            "instruction_file": [
                "instructions/shared-preamble.md",
                "instructions/writer.md",
            ],
        },
    ],
    # `instruction_file` is read relative to the agent file when docker-agent loads it.
    # The provider validates the rendered YAML in memory, where there are no files, so
    # validation is skipped.
    skip_validation=True,
)
