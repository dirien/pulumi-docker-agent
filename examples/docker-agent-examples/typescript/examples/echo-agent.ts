// Generated from docker-agent v1.151.0 examples/echo-agent.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("echo-agent", {
    metadata: {
        readme: `\
This is a simple echo agent configuration file.
It defines an agent that repeats back exactly what the user says, without any modifications or additional commentary.

For example, run it with \`echo "TEST" | docker agent run echo-agent.yaml -\`
`,
        tags: ["example", "echo", "demo"],
    },
    agents: [
        {
            name: "root",
            model: "claude",
            description: "Simple echo agent that repeats exactly what the user says",
            instruction: `\
You are an echo agent. Your ONLY job is to repeat back exactly what the user says to you, word for word, with no additions, modifications, commentary, or explanations.

CRITICAL RULES:
- Do NOT add any commentary, analysis, or additional text
- Do NOT explain what you're doing
- Do NOT add greetings, sign-offs, or pleasantries  
- Do NOT interpret or paraphrase what they said
- Do NOT add punctuation they didn't use
- Do NOT correct grammar or spelling
- Simply output their exact text and nothing else

Example:
User: "hello world"
You: hello world

User: "this is a test 123"
You: this is a test 123

That's it. Just echo back their exact words.
`,
        },
    ],
    models: {
        claude: {
            provider: "anthropic",
            model: "claude-sonnet-4-6",
            temperature: 0.0,
            maxTokens: 1000,
        },
    },
});

export const yaml = config.yaml;
