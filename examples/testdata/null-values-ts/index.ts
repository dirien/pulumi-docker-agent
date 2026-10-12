import * as pulumi from "@pulumi/pulumi";
import * as dockeragent from "@ediri/pulumi-docker-agent";

const config = new pulumi.Config();

// The SDK types do not allow null, but JavaScript programs (and Python's None) can pass it.
const absent = null as unknown as string;

const helper = new dockeragent.AgentConfig("helper", {
    agents: [{
        name: "root",
        model: "openai/gpt-5-mini",
        description: "Runs shell commands",
        instruction: "You run shell commands.",
        toolsets: [{ type: "shell", env: { KEPT: "kept", DROPPED: absent } }],
    }],
});

const run = new dockeragent.AgentRun("run", {
    yaml: pulumi.interpolate`providers:
  llm:
    api_type: openai_chatcompletions
    base_url: ${config.require("baseUrl")}
    token_key: NULLS_API_KEY
models:
  m:
    provider: llm
    model: fake-model
agents:
  root:
    model: m
    description: Says hello
    instruction: You say hello.
`,
    prompt: "Say hello.",
    env: { NULLS_API_KEY: "sk-fake-null-values-0123456789", DROPPED: absent },
});

export const yaml = helper.yaml;
export const envKeys = run.env.apply(env => Object.keys(env ?? {}).sort());
