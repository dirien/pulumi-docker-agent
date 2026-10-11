// Pushes a Docker Agent configuration to a registry and serves it over HTTP with
// `docker agent serve api` in a container, using the Pulumi Docker provider for the containers.
import * as pulumi from "@pulumi/pulumi";
import * as docker from "@pulumi/docker";
import * as dockeragent from "@ediri/pulumi-docker-agent";

const config = new pulumi.Config();
// Pin the image: `latest` tracks docker-agent's main branch.
const agentImage = config.get("agentImage") ?? "docker/docker-agent:1.150.0";
// The localhost port of the agent API. Docker picks a free port when it is not set.
const agentPort = config.getNumber("agentPort");
// Optional. Without a key the server starts and lists the agent, but cannot answer.
const anthropicApiKey = config.getSecret("anthropicApiKey");

// A user-defined network shared by the registry and the agent server.
const network = new docker.Network("agents");

// keepLocally: destroying the stack does not remove images that other containers may use.
const registryImage = new docker.RemoteImage("registry", { name: "registry:2", keepLocally: true });
const serverImage = new docker.RemoteImage("docker-agent", { name: agentImage, keepLocally: true });

// The registry is published on localhost, where Pulumi pushes the artifact, and joins the
// network, where the agent server pulls it.
const registry = new docker.Container("registry", {
    image: registryImage.imageId,
    envs: ["REGISTRY_STORAGE_DELETE_ENABLED=true"],
    networksAdvanced: [{ name: network.name }],
    ports: [{ internal: 5000, ip: "127.0.0.1" }],
    healthcheck: {
        tests: ["CMD", "wget", "-q", "-O", "/dev/null", "http://127.0.0.1:5000/v2/"],
        interval: "1s",
        timeout: "3s",
        retries: 30,
    },
    // Wait until the registry is healthy before the artifact is pushed.
    wait: true,
    waitTimeout: 60,
});

const registryHostPort = registry.ports.apply(ports => ports![0].external);
// docker-agent talks plain HTTP to registries on localhost and on private IP addresses, so
// the server pulls from the registry's address on the shared network.
const registryNetworkAddress = pulumi
    .all([registry.networkDatas, network.name])
    .apply(([networks, name]) => `${networks.find(n => n.networkName === name)!.ipAddress}:5000`);

// The same research assistant as the other examples.
const assistant = new dockeragent.AgentConfig("research-assistant", {
    models: {
        claude: {
            provider: "anthropic",
            model: "claude-opus-5-5",
            maxTokens: 32000,
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
});

// Push the configuration through the published port.
const artifact = new dockeragent.AgentArtifact("research-assistant", {
    reference: pulumi.interpolate`localhost:${registryHostPort}/examples/serve-agent:v1`,
    yaml: assistant.yaml,
});

// The same artifact, pinned by digest and addressed the way the server reaches the registry.
// A new push changes the digest, which replaces the server container.
const agentReference = pulumi.interpolate`${registryNetworkAddress}/examples/serve-agent@${artifact.digest}`;

const envs: pulumi.Input<string>[] = ["TELEMETRY_ENABLED=false"];
if (anthropicApiKey) {
    envs.push(pulumi.interpolate`AGENT_ANTHROPIC_API_KEY=${anthropicApiKey}`);
}

// `docker agent serve api <reference>` pulls the agent and serves the HTTP API.
const server = new docker.Container("agent-server", {
    image: serverImage.imageId,
    command: ["serve", "api", agentReference, "--listen", "0.0.0.0:8080"],
    envs,
    networksAdvanced: [{ name: network.name }],
    ports: [{ internal: 8080, external: agentPort, ip: "127.0.0.1" }],
    healthcheck: {
        tests: ["CMD", "wget", "-q", "-O", "/dev/null", "http://127.0.0.1:8080/health"],
        interval: "2s",
        timeout: "3s",
        retries: 30,
    },
    // Wait until /health answers.
    wait: true,
    waitTimeout: 120,
});

const apiUrl = server.ports.apply(ports => `http://localhost:${ports![0].external}`);

export const pinnedReference = artifact.pinnedReference;
export const serverAgentReference = agentReference;
export const url = apiUrl;
export const healthUrl = pulumi.interpolate`${apiUrl}/health`;
export const agentsUrl = pulumi.interpolate`${apiUrl}/api/agents`;
export const serverContainer = server.name;
