"""Builds a two-agent Docker Agent configuration, pushes it to an OCI registry and reads it back."""

import pathlib

import pulumi
import ediri_docker_agent as dockeragent

config = pulumi.Config()
# The registry to push to. The default is a local registry:2 container.
registry = config.get("registry") or "localhost:5000"

# A typed docker-agent configuration, validated during `pulumi preview`.
assistant = dockeragent.AgentConfig(
    "research-assistant",
    models={
        "claude": {
            "provider": "anthropic",
            "model": "claude-opus-5-5",
            "max_tokens": 32000,
            # The environment variable that holds the API key where the agent runs.
            "token_key": "AGENT_ANTHROPIC_API_KEY",
        },
    },
    agents=[
        {
            "name": "root",
            "model": "claude",
            "description": "Answers questions and delegates web research",
            "instruction": "You are a helpful assistant. Delegate questions that need current "
            "information to the researcher agent, then summarise its findings.",
            "sub_agents": ["researcher"],
        },
        {
            "name": "researcher",
            "model": "claude",
            "description": "Searches the web with DuckDuckGo",
            "instruction": "Search the web and answer with short summaries that cite their sources.",
            "add_date": True,
            # The DuckDuckGo MCP server from the Docker MCP Catalog.
            "toolsets": [{"type": "mcp", "ref": "docker:duckduckgo"}],
        },
    ],
    metadata={
        "author": "Pulumi Docker Agent examples",
        "license": "Apache-2.0",
        "description": "A research assistant that delegates web searches to a sub-agent",
        "version": "1.0.0",
        "tags": ["research", "example"],
    },
    # Sandbox mode: `docker agent run` runs the agent in a sandbox by default and only lets it
    # reach the model API.
    runtime={
        "sandbox": True,
        "network_allowlist": ["api.anthropic.com"],
        "safety": "balanced",
    },
)

# Share the configuration as an OCI artifact, like `docker agent share push`.
artifact = dockeragent.AgentArtifact(
    "research-assistant",
    reference=f"{registry}/examples/python-agent:v1",
    yaml=assistant.yaml,
    # Remove the artifact from the registry on `pulumi destroy`.
    delete_behavior=dockeragent.DeleteBehavior.DELETE,
)

# Read the shared agent back from the registry, like `docker agent share pull`.
pulled = dockeragent.get_agent_output(reference=artifact.pinned_reference)

# Validate a hand-written agent file without pushing it.
raw_config = dockeragent.validate_agent_config_output(
    yaml=(pathlib.Path(__file__).parent / "agent.yaml").read_text(encoding="utf-8"),
)

pulumi.export("digest", artifact.digest)
pulumi.export("pinnedReference", artifact.pinned_reference)
pulumi.export("runCommand", pulumi.Output.concat("docker agent run ", artifact.pinned_reference))
pulumi.export("requiredEnvVars", assistant.required_env_vars)
pulumi.export("agentYaml", assistant.yaml)
pulumi.export("pulledYaml", pulled.yaml)
pulumi.export("pulledAgents", pulled.agent_names)
pulumi.export("pulledMetadata", pulled.metadata)
pulumi.export("rawConfigValid", raw_config.valid)
pulumi.export("rawConfigAgents", raw_config.agent_names)
pulumi.export("rawConfigErrors", raw_config.errors)
