# Generated from docker-agent v1.151.0 examples/typo.yaml by `make generate_config_types`. Do not edit.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "typo",
    agents=[
        {
            "name": "root",
            "model": "model",
            "description": "Fix typos.",
            "instruction": """\
Your goal is to fix typos in a project.
Read as many files as you can at once and fix typos in them.
Fix typos and don't print any summary.
""",
            "toolsets": [
                {
                    "type": "filesystem",
                    "tools": [
                        "list_directory",
                        "edit_file",
                        "read_multiple_files",
                        "write_file",
                    ],
                },
            ],
            "simple_commands": {
                "fix": "Fix every typo, in every file, in the current directory.",
            },
        },
    ],
    models={"model": {"provider": "anthropic", "model": "claude-sonnet-5"}},
)
