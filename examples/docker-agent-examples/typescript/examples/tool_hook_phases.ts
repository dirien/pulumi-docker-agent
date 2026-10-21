// Generated from docker-agent v1.151.0 examples/tool_hook_phases.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("tool_hook_phases", {
    // Requires python3 for the command hooks. Try with --yolo: the guard still runs.
    agents: [
        {
            name: "root",
            model: "openai/gpt-5-mini",
            description: "Separate input normalization, mandatory checks, and approval assistance",
            instruction: "Help with shell commands. Respect tool policy rejections.",
            toolsets: [{ type: "shell" }],
            hooks: {
                toolInputTransform: [
                    {
                        matcher: "shell",
                        hooks: [
                            {
                                name: "normalize whitespace",
                                type: "command",
                                strictOutput: true,
                                onError: "block",
                                command: `\
python3 -c '
import json, sys
payload = json.load(sys.stdin)
command = payload["tool_input"].get("cmd", "")
print(json.dumps({"hook_specific_output": {
    "updated_input": {"cmd": command.strip()}
}}))
'
`,
                            },
                        ],
                    },
                ],
                toolGuard: [
                    {
                        matcher: "shell",
                        hooks: [
                            {
                                name: "require confirmation for project commands",
                                type: "command",
                                strictOutput: true,
                                timeout: 5,
                                command: `\
python3 -c '
import json, sys
payload = json.load(sys.stdin)
command = payload["tool_input"].get("cmd", "")
# Exact matches, not a shell-prefix allowlist.
decision = "allow" if command in ("pwd", "git status") else "ask"
print(json.dumps({"hook_specific_output": {
    "permission_decision": decision,
    "permission_decision_reason": "Review commands beyond the read-only allowlist",
    "metadata": {"policy": "project command review"}
}}))
'
`,
                            },
                        ],
                    },
                ],

                // This judge only runs when the guard falls through and safety mode asks.
                // A guard's "ask" skips it; an "allow" here cannot undo a guard denial.
                preToolUse: [
                    {
                        matcher: "shell",
                        hooks: [
                            {
                                name: "approval assistant",
                                type: "model",
                                model: "openai/gpt-4o-mini",
                                timeout: 15,
                                schema: "pre_tool_use_decision",
                                prompt: `\
Decide allow, ask, or deny for this tool call.
Allow only clearly read-only operations; ask when uncertain.
Tool: {{ .ToolName }}
Arguments: {{ .ToolInput | toJSON }}
`,
                            },
                        ],
                    },
                ],
            },
        },
    ],
});

export const yaml = config.yaml;
