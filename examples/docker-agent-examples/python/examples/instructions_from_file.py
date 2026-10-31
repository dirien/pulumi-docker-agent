# Generated from docker-agent v1.154.0 examples/instructions_from_file.hcl by `make generate_config_types`. Do not edit.

import pathlib
import re

import ediri_docker_agent as dockeragent

# The HCL configuration reads this value with docker-agent's file() helper, which
# renders instructions_from_file.md as a template. The program reads the copy next to it
# and replaces each ${name} placeholder the same way.
instruction_file = pathlib.Path(__file__).parent / "instructions_from_file.md"
instruction_vars = {"audience": "developers", "style": "concise"}
instruction = re.sub(
    r"\$\{(\w+)\}",
    lambda match: instruction_vars[match.group(1)],
    instruction_file.read_text(encoding="utf-8"),
)

config = dockeragent.AgentConfig(
    "instructions_from_file",
    agents=[
        {
            "name": "root",
            "description": "Agent that loads its system prompt from a separate file",
            "model": "auto",
            # The file() helper reads a text file and injects its contents here.
            # Relative paths are resolved from this HCL file's directory.
            # Passing an object as a second argument renders the file as a template:
            # each key becomes a ${key} variable inside the file.
            "instruction": instruction,
            "welcome_message": "Hi! My instructions were loaded from instructions_from_file.md",
        },
    ],
)
