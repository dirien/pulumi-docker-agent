// Generated from docker-agent v1.152.0 examples/huggingface.yaml by `make generate_config_types`. Do not edit.

import * as dockeragent from "@ediri/pulumi-docker-agent";

export const config = new dockeragent.AgentConfig("huggingface", {
    models: {
        huggingface_model: { provider: "huggingface", model: "meta-llama/Llama-3.3-70B-Instruct" },
    },
    agents: [
        {
            name: "root",
            model: "huggingface_model",
            description: "Assistant using Hugging Face Inference Providers",
            instruction: `\
You are a helpful assistant.
`,
            toolsets: [{ type: "filesystem" }, { type: "shell" }, { type: "think" }],
        },
    ],
});

export const yaml = config.yaml;
