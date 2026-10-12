// Generated from docker-agent v1.150.0 examples/welcome_message.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("welcome_message", {
    agents: [
        {
            name: "root",
            model: "openai/gpt-4o-mini",
            description: "A helpful AI assistant with a welcome message",
            welcomeMessage: `\
👋 **Welcome to docker-agent!**

I'm your AI assistant, ready to help you with various tasks.

Here are some things I can do:
  - Answer questions and provide information
  - Help with coding and technical problems
  - Assist with writing and content creation
  - And much more!

Just type your question or request below to get started.
`,
            instruction: `\
You are a knowledgeable and helpful assistant.
Be friendly, accurate, and concise in your responses.
Always aim to provide value to the user.
`,
        },
    ],
});

export const yaml = config.yaml;
