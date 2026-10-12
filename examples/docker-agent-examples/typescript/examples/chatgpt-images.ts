// Generated from docker-agent v1.150.0 examples/chatgpt-images.yaml by `make generate_config_types`. Do not edit.
//
// ChatGPT image input with automatic capability detection.
// Requires ChatGPT sign-in and access to the selected model.
// Image input for gpt-6.1-sol is inferred from the matching OpenAI catalogue entry.
//
// From the repository root:
//   docker agent run examples/chatgpt-images.yaml --exec \
//     --attach examples/chatgpt-images.png "Describe the shapes and colors."
// The sample image is a red square centered on a blue background.
// With a source build, use ./bin/docker-agent instead of docker agent.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("chatgpt-images", {
    models: { vision: { provider: "chatgpt", model: "gpt-6.1-sol" } },
    agents: [
        {
            name: "root",
            model: "vision",
            description: "ChatGPT image-input example",
            instruction: "Describe the attached image accurately and concisely.",
        },
    ],
});

export const yaml = config.yaml;
