// Generated from docker-agent v1.152.0 examples/structured-output.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("structured-output", {
    models: {
        gpt4_structured: {
            provider: "openai",
            model: "gpt-5.6-sol",
            temperature: 0.7,
            maxTokens: 1000,
        },
    },
    agents: [
        {
            name: "root",
            model: "gpt4_structured",
            description: "Agent that extracts structured information about people",
            instruction: `\
You are an assistant that extracts structured information about people from conversations.
Always respond with valid JSON matching the specified schema.
`,
            simpleCommands: {
                demo: "My name is John Doe, I am 30 years old, my email is john.doe@example.com, and I enjoy hiking and photography.",
            },
            structuredOutput: {
                name: "person_info",
                description: "Information about a person",
                strict: true,
                schema: {
                    type: "object",
                    properties: {
                        name: { type: "string", description: "The person's full name" },
                        age: { type: "integer", description: "The person's age" },
                        email: { type: "string", description: "The person's email address" },
                        hobbies: {
                            type: "array",
                            items: { type: "string" },
                            description: "List of the person's hobbies",
                        },
                    },
                    required: ["name", "age", "email", "hobbies"],
                    additionalProperties: false,
                },
            },
        },
    ],
});

export const yaml = config.yaml;
