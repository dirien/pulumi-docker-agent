# Builds a two-agent Docker Agent configuration, pushes it to an OCI registry and reads it back.

terraform {
  required_providers {
    # The `pulumi/` prefix selects the native Pulumi provider (any other source is bridged from
    # a Terraform provider). In your own projects pin an exact version, for example
    # `version = "0.1.0"`; Pulumi providers take a version, not a constraint. This example runs
    # the provider built from this repository (see Pulumi.yaml), so it leaves the version open.
    docker-agent = {
      source = "pulumi/docker-agent"
    }
  }
}

# The registry to push to. Set it with `pulumi config set registry <host:port>`.
variable "registry" {
  type        = string
  default     = "localhost:5000"
  description = "The registry to push to. The default is a local registry:2 container."
}

# A typed docker-agent configuration, validated during `pulumi preview`.
# docker-agent:index:AgentConfig -> docker-agent_agent_config. Property names are snake_case;
# lists of objects (agents, toolsets) are written as repeated nested blocks.
resource "docker-agent_agent_config" "assistant" {
  models = {
    claude = {
      provider   = "anthropic"
      model      = "claude-opus-5-5"
      max_tokens = 32000
      # The environment variable that holds the API key where the agent runs.
      token_key = "AGENT_ANTHROPIC_API_KEY"
    }
  }

  agents {
    name        = "root"
    model       = "claude"
    description = "Answers questions and delegates web research"
    instruction = "You are a helpful assistant. Delegate questions that need current information to the researcher agent, then summarise its findings."
    sub_agents  = ["researcher"]
  }

  agents {
    name        = "researcher"
    model       = "claude"
    description = "Searches the web with DuckDuckGo"
    instruction = "Search the web and answer with short summaries that cite their sources."
    add_date    = true

    # The DuckDuckGo MCP server from the Docker MCP Catalog.
    toolsets {
      type = "mcp"
      ref  = "docker:duckduckgo"
    }
  }

  metadata = {
    author      = "Pulumi Docker Agent examples"
    license     = "Apache-2.0"
    description = "A research assistant that delegates web searches to a sub-agent"
    version     = "1.0.0"
    tags        = ["research", "example"]
  }

  # Sandbox mode: `docker agent run` runs the agent in a sandbox by default and only lets it
  # reach the model API.
  runtime = {
    sandbox           = true
    network_allowlist = ["api.anthropic.com"]
    safety            = "balanced"
  }
}

# Share the configuration as an OCI artifact, like `docker agent share push`.
resource "docker-agent_agent_artifact" "artifact" {
  reference = "${var.registry}/examples/hcl-agent:v1"
  yaml      = docker-agent_agent_config.assistant.yaml
  # Remove the artifact from the registry on `pulumi destroy`.
  delete_behavior = "delete"
}

# Read the shared agent back from the registry, like `docker agent share pull`.
# docker-agent:index:getAgent -> data "docker-agent_agent" (the `get` prefix is dropped).
data "docker-agent_agent" "pulled" {
  reference = docker-agent_agent_artifact.artifact.pinned_reference
}

# Validate a hand-written agent file without pushing it.
data "docker-agent_validate_agent_config" "raw" {
  yaml = file("${path.module}/agent.yaml")
}

output "digest" {
  value = docker-agent_agent_artifact.artifact.digest
}

output "pinnedReference" {
  value = docker-agent_agent_artifact.artifact.pinned_reference
}

output "runCommand" {
  value = "docker agent run ${docker-agent_agent_artifact.artifact.pinned_reference}"
}

output "requiredEnvVars" {
  value = docker-agent_agent_config.assistant.required_env_vars
}

output "agentYaml" {
  value = docker-agent_agent_config.assistant.yaml
}

output "pulledYaml" {
  value = data.docker-agent_agent.pulled.yaml
}

output "pulledAgents" {
  value = data.docker-agent_agent.pulled.agent_names
}

output "pulledMetadata" {
  value = data.docker-agent_agent.pulled.metadata
}

output "rawConfigValid" {
  value = data.docker-agent_validate_agent_config.raw.valid
}

output "rawConfigAgents" {
  value = data.docker-agent_validate_agent_config.raw.agent_names
}

output "rawConfigErrors" {
  value = data.docker-agent_validate_agent_config.raw.errors
}
