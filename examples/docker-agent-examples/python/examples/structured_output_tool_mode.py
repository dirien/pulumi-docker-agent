# Generated from docker-agent v1.151.0 examples/structured-output-tool-mode.yaml by `make generate_config_types`. Do not edit.
#
# Tool-mode structured output: instead of asking the provider for native
# structured output, the runtime exposes an internal tool whose parameters
# are exactly the configured schema. The model keeps using its regular tools
# and ends the conversation by calling that tool with the final JSON, which
# the runtime validates against the schema. Works with any tool-calling
# model, even without native structured-output support.

import ediri_docker_agent as dockeragent

config = dockeragent.AgentConfig(
    "structured-output-tool-mode",
    agents=[
        {
            "name": "root",
            "model": "openai/gpt-4o",
            "description": "Agent that inspects files and reports findings as JSON",
            "toolsets": [{"type": "filesystem"}],
            "instruction": """\
Inspect the files the user asks about, then report your findings.
Deliver the final report through the structured output tool.
""",
            "structured_output": {
                "mode": "tool",
                "name": "file_report",
                "description": "Findings about the inspected files",
                "schema": {
                    "type": "object",
                    "properties": {
                        "files": {
                            "type": "array",
                            "items": {
                                "type": "object",
                                "properties": {
                                    "path": {"type": "string"},
                                    "summary": {"type": "string"},
                                },
                                "required": ["path", "summary"],
                            },
                        },
                        "overall_summary": {"type": "string"},
                    },
                    "required": ["files", "overall_summary"],
                    "additionalProperties": False,
                },
            },
        },
    ],
)
