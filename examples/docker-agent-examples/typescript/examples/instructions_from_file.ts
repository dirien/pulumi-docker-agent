// Generated from docker-agent v1.152.0 examples/instructions_from_file.hcl by `make generate_config_types`. Do not edit.

import * as fs from "fs";
import * as path from "path";
import * as dockeragent from "@ediri/pulumi-docker-agent";

// The HCL configuration reads this value with docker-agent's file() helper, which renders
// instructions_from_file.md as a template. The program reads the copy next to it and replaces each
// ${name} placeholder the same way.
const instructionVars: Record<string, string> = { audience: "developers", style: "concise" };
const instruction = fs
    .readFileSync(path.join(__dirname, "instructions_from_file.md"), "utf8")
    .replace(/\$\{(\w+)\}/g, (_match, name: string) => instructionVars[name]);

export const config = new dockeragent.AgentConfig("instructions_from_file", {
    agents: [
        {
            name: "root",
            description: "Agent that loads its system prompt from a separate file",
            model: "auto",

            // The file() helper reads a text file and injects its contents here.
            // Relative paths are resolved from this HCL file's directory.
            // Passing an object as a second argument renders the file as a template:
            // each key becomes a ${key} variable inside the file.
            instruction,
            welcomeMessage: "Hi! My instructions were loaded from instructions_from_file.md",
        },
    ],
});

export const yaml = config.yaml;
