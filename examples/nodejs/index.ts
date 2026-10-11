import * as fs from "fs";
import * as path from "path";
import * as pulumi from "@pulumi/pulumi";
import * as dockeragent from "@ediri/pulumi-docker-agent";

const config = new pulumi.Config();
// The registry to push to. The default is a local registry:2 container.
const registry = config.get("registry") ?? "localhost:5000";

// A typed docker-agent configuration, validated during `pulumi preview`.
const assistant = new dockeragent.AgentConfig("research-assistant", {
    models: {
        claude: {
            provider: "anthropic",
            model: "claude-opus-5-5",
            maxTokens: 32000,
            // The environment variable that holds the API key where the agent runs.
            tokenKey: "AGENT_ANTHROPIC_API_KEY",
        },
    },
    agents: [
        {
            name: "root",
            model: "claude",
            description: "Answers questions and delegates web research",
            instruction: "You are a helpful assistant. Delegate questions that need current " +
                "information to the researcher agent, then summarise its findings.",
            subAgents: ["researcher"],
        },
        {
            name: "researcher",
            model: "claude",
            description: "Searches the web with DuckDuckGo",
            instruction: "Search the web and answer with short summaries that cite their sources.",
            addDate: true,
            // The DuckDuckGo MCP server from the Docker MCP Catalog.
            toolsets: [{ type: "mcp", ref: "docker:duckduckgo" }],
        },
    ],
    metadata: {
        author: "Pulumi Docker Agent examples",
        license: "Apache-2.0",
        description: "A research assistant that delegates web searches to a sub-agent",
        version: "1.0.0",
        tags: ["research", "example"],
    },
    // Sandbox mode: `docker agent run` runs the agent in a sandbox by default and only lets it
    // reach the model API.
    runtime: {
        sandbox: true,
        networkAllowlist: ["api.anthropic.com"],
        safety: "balanced",
    },
});

// Share the configuration as an OCI artifact, like `docker agent share push`.
const artifact = new dockeragent.AgentArtifact("research-assistant", {
    reference: `${registry}/examples/nodejs-agent:v1`,
    yaml: assistant.yaml,
    // Remove the artifact from the registry on `pulumi destroy`.
    deleteBehavior: "delete",
});

// Read the shared agent back from the registry, like `docker agent share pull`.
const pulled = dockeragent.getAgentOutput({ reference: artifact.pinnedReference });

// Validate a hand-written agent file without pushing it.
const rawConfig = dockeragent.validateAgentConfigOutput({
    yaml: fs.readFileSync(path.join(__dirname, "agent.yaml"), "utf8"),
});

export const digest = artifact.digest;
export const pinnedReference = artifact.pinnedReference;
export const runCommand = pulumi.interpolate`docker agent run ${artifact.pinnedReference}`;
export const requiredEnvVars = assistant.requiredEnvVars;
export const agentYaml = assistant.yaml;
export const pulledYaml = pulled.yaml;
export const pulledAgents = pulled.agentNames;
export const pulledMetadata = pulled.metadata;
export const rawConfigValid = rawConfig.valid;
export const rawConfigAgents = rawConfig.agentNames;
export const rawConfigErrors = rawConfig.errors;
