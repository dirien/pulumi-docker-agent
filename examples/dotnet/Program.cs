// Builds a two-agent Docker Agent configuration, pushes it to an OCI registry and reads it back.
using System.Collections.Generic;
using System.IO;
using Pulumi;
using ediri.DockerAgent;
using ediri.DockerAgent.Inputs;

return await Deployment.RunAsync(() =>
{
    var config = new Pulumi.Config();
    // The registry to push to. The default is a local registry:2 container.
    var registry = config.Get("registry") ?? "localhost:5000";

    // A typed docker-agent configuration, validated during `pulumi preview`.
    var assistant = new AgentConfig("research-assistant", new AgentConfigArgs
    {
        Models =
        {
            ["claude"] = new ModelDefArgs
            {
                Provider = "anthropic",
                Model = "claude-opus-5-5",
                MaxTokens = 32000,
                // The environment variable that holds the API key where the agent runs.
                TokenKey = "AGENT_ANTHROPIC_API_KEY",
            },
        },
        Agents =
        {
            new AgentArgs
            {
                Name = "root",
                Model = "claude",
                Description = "Answers questions and delegates web research",
                Instruction = "You are a helpful assistant. Delegate questions that need current " +
                    "information to the researcher agent, then summarise its findings.",
                SubAgents = { "researcher" },
            },
            new AgentArgs
            {
                Name = "researcher",
                Model = "claude",
                Description = "Searches the web with DuckDuckGo",
                Instruction = "Search the web and answer with short summaries that cite their sources.",
                AddDate = true,
                // The DuckDuckGo MCP server from the Docker MCP Catalog.
                Toolsets =
                {
                    new ToolsetArgs { Type = "mcp", Ref = "docker:duckduckgo" },
                },
            },
        },
        Metadata = new MetadataArgs
        {
            Author = "Pulumi Docker Agent examples",
            License = "Apache-2.0",
            Description = "A research assistant that delegates web searches to a sub-agent",
            Version = "1.0.0",
            Tags = { "research", "example" },
        },
        // Sandbox mode: `docker agent run` runs the agent in a sandbox by default and only lets
        // it reach the model API.
        Runtime = new RuntimeDefaultsArgs
        {
            Sandbox = true,
            NetworkAllowlist = { "api.anthropic.com" },
            Safety = SafetyMode.Balanced,
        },
    });

    // Share the configuration as an OCI artifact, like `docker agent share push`.
    var artifact = new AgentArtifact("research-assistant", new AgentArtifactArgs
    {
        Reference = $"{registry}/examples/dotnet-agent:v1",
        Yaml = assistant.Yaml,
        // Remove the artifact from the registry on `pulumi destroy`.
        DeleteBehavior = DeleteBehavior.Delete,
    });

    // Read the shared agent back from the registry, like `docker agent share pull`.
    var pulled = GetAgent.Invoke(new GetAgentInvokeArgs
    {
        Reference = artifact.PinnedReference,
    });

    // Validate a hand-written agent file without pushing it.
    var rawConfig = ValidateAgentConfig.Invoke(new ValidateAgentConfigInvokeArgs
    {
        Yaml = File.ReadAllText("agent.yaml"),
    });

    return new Dictionary<string, object?>
    {
        ["digest"] = artifact.Digest,
        ["pinnedReference"] = artifact.PinnedReference,
        ["runCommand"] = Output.Format($"docker agent run {artifact.PinnedReference}"),
        ["requiredEnvVars"] = assistant.RequiredEnvVars,
        ["agentYaml"] = assistant.Yaml,
        ["pulledYaml"] = pulled.Apply(result => result.Yaml),
        ["pulledAgents"] = pulled.Apply(result => result.AgentNames),
        ["pulledMetadata"] = pulled.Apply(result => result.Metadata),
        ["rawConfigValid"] = rawConfig.Apply(result => result.Valid),
        ["rawConfigAgents"] = rawConfig.Apply(result => result.AgentNames),
        ["rawConfigErrors"] = rawConfig.Apply(result => result.Errors),
    };
});
