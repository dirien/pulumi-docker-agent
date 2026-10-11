---
title: Docker Agent
meta_desc: Use the Pulumi Docker Agent provider to define, validate and share Docker Agent AI agent configurations as OCI artifacts.
layout: package
---

The Docker Agent provider for Pulumi lets you manage [Docker Agent](https://docs.docker.com/ai/docker-agent/) agents as code. You define multi-agent configurations (models, sub-agents, MCP and built-in toolsets) with typed, validated inputs, publish them to any OCI registry as artifacts that are byte-compatible with `docker agent share push`, and read shared agents back from a registry. Anyone can then run the published agent with `docker agent run <reference>`.

Configurations are validated offline during `pulumi preview` with the same loader the `docker agent` CLI uses, so typos, unknown fields and references to undefined models or sub-agents fail before anything is pushed.

## Installation

{{< chooser language "typescript,python,go,csharp,java,yaml,hcl" >}}
{{% choosable language typescript %}}

```bash
npm install @ediri/pulumi-docker-agent
```

{{% /choosable %}}
{{% choosable language python %}}

```bash
pip install ediri_docker_agent
```

{{% /choosable %}}
{{% choosable language go %}}

```bash
go get github.com/dirien/pulumi-docker-agent/sdk/go/dockeragent
```

{{% /choosable %}}
{{% choosable language csharp %}}

```bash
dotnet add package ediri.DockerAgent
```

{{% /choosable %}}
{{% choosable language java %}}

No Java SDK is published. Generate one locally from your project directory:

```bash
pulumi package add docker-agent
```

{{% /choosable %}}
{{% choosable language yaml %}}

```bash
pulumi package add docker-agent
```

{{% /choosable %}}
{{% choosable language hcl %}}

Declare the provider in the `required_providers` block of your program. The `pulumi/` prefix selects the native Pulumi provider, and the version must be exact:

```hcl
terraform {
  required_providers {
    docker-agent = {
      source  = "pulumi/docker-agent"
      version = "0.1.0"
    }
  }
}
```

Then install the plugin from its GitHub release and run `pulumi install`:

```bash
pulumi plugin install resource docker-agent 0.1.0 --server github://api.github.com/dirien/pulumi-docker-agent
pulumi install
```

{{% /choosable %}}
{{< /chooser >}}

The SDKs download the `docker-agent` provider plugin automatically from the [GitHub releases](https://github.com/dirien/pulumi-docker-agent/releases) on first use. An HCL program names the package only by its `source`, so install the plugin once as shown, or commit the package descriptor described in [Installation & Configuration](https://www.pulumi.com/registry/packages/docker-agent/installation-configuration/#hcl) so that `pulumi install` alone fetches it.

## Example Usage

This program defines a two-agent configuration: a `root` agent that delegates web research to a `researcher` sub-agent, which uses the DuckDuckGo MCP server from the Docker MCP Catalog. It then pushes the configuration to a registry as an OCI artifact, and reads it back with `getAgent`.

The example pushes to a local registry. Start one with `docker run -d -p 5000:5000 registry:2`, or change the reference to a registry you are logged in to, such as `ghcr.io/<user>/research-assistant:v1` or `docker.io/<user>/research-assistant:v1`.

{{< chooser language "typescript,python,go,csharp,yaml,hcl" >}}
{{% choosable language typescript %}}

```typescript
import * as dockeragent from "@ediri/pulumi-docker-agent";

// A typed docker-agent configuration, validated during `pulumi preview`.
const assistant = new dockeragent.AgentConfig("research-assistant", {
    models: {
        fast: { provider: "openai", model: "gpt-5-mini", maxTokens: 16000 },
    },
    agents: [
        {
            name: "root",
            model: "fast",
            description: "Answers questions and delegates web research",
            instruction: "You are a helpful assistant. Delegate questions that need current information to the researcher agent, then summarise its findings.",
            subAgents: ["researcher"],
        },
        {
            name: "researcher",
            model: "fast",
            description: "Searches the web with DuckDuckGo",
            instruction: "Search the web and answer with short summaries that cite their sources.",
            toolsets: [{ type: "mcp", ref: "docker:duckduckgo" }],
        },
    ],
    metadata: {
        author: "Jane Doe",
        license: "Apache-2.0",
        version: "1.0.0",
        tags: ["research", "demo"],
    },
});

// Share the configuration as an OCI artifact, like `docker agent share push`.
const artifact = new dockeragent.AgentArtifact("research-assistant", {
    reference: "localhost:5000/agents/research-assistant:v1",
    yaml: assistant.yaml,
});

// Read the shared agent back from the registry, like `docker agent share pull`.
const pulled = dockeragent.getAgentOutput({ reference: artifact.pinnedReference });

export const digest = artifact.digest;
export const pinnedReference = artifact.pinnedReference;
export const requiredEnvVars = assistant.requiredEnvVars;
export const pulledAgents = pulled.agentNames;
```

{{% /choosable %}}
{{% choosable language python %}}

```python
import pulumi
import ediri_docker_agent as dockeragent

# A typed docker-agent configuration, validated during `pulumi preview`.
assistant = dockeragent.AgentConfig(
    "research-assistant",
    models={
        "fast": {"provider": "openai", "model": "gpt-5-mini", "max_tokens": 16000},
    },
    agents=[
        {
            "name": "root",
            "model": "fast",
            "description": "Answers questions and delegates web research",
            "instruction": "You are a helpful assistant. Delegate questions that need current "
            "information to the researcher agent, then summarise its findings.",
            "sub_agents": ["researcher"],
        },
        {
            "name": "researcher",
            "model": "fast",
            "description": "Searches the web with DuckDuckGo",
            "instruction": "Search the web and answer with short summaries that cite their sources.",
            "toolsets": [{"type": "mcp", "ref": "docker:duckduckgo"}],
        },
    ],
    metadata={
        "author": "Jane Doe",
        "license": "Apache-2.0",
        "version": "1.0.0",
        "tags": ["research", "demo"],
    },
)

# Share the configuration as an OCI artifact, like `docker agent share push`.
artifact = dockeragent.AgentArtifact(
    "research-assistant",
    reference="localhost:5000/agents/research-assistant:v1",
    yaml=assistant.yaml,
)

# Read the shared agent back from the registry, like `docker agent share pull`.
pulled = dockeragent.get_agent_output(reference=artifact.pinned_reference)

pulumi.export("digest", artifact.digest)
pulumi.export("pinnedReference", artifact.pinned_reference)
pulumi.export("requiredEnvVars", assistant.required_env_vars)
pulumi.export("pulledAgents", pulled.agent_names)
```

{{% /choosable %}}
{{% choosable language go %}}

```go
package main

import (
	"github.com/dirien/pulumi-docker-agent/sdk/go/dockeragent"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		// A typed docker-agent configuration, validated during `pulumi preview`.
		assistant, err := dockeragent.NewAgentConfig(ctx, "research-assistant", &dockeragent.AgentConfigArgs{
			Models: dockeragent.ModelDefMap{
				"fast": dockeragent.ModelDefArgs{
					Provider:  pulumi.String("openai"),
					Model:     pulumi.String("gpt-5-mini"),
					MaxTokens: pulumi.Int(16000),
				},
			},
			Agents: dockeragent.AgentArray{
				dockeragent.AgentArgs{
					Name:        pulumi.String("root"),
					Model:       pulumi.String("fast"),
					Description: pulumi.String("Answers questions and delegates web research"),
					Instruction: pulumi.String("You are a helpful assistant. Delegate questions that need current " +
						"information to the researcher agent, then summarise its findings."),
					SubAgents: pulumi.StringArray{pulumi.String("researcher")},
				},
				dockeragent.AgentArgs{
					Name:        pulumi.String("researcher"),
					Model:       pulumi.String("fast"),
					Description: pulumi.String("Searches the web with DuckDuckGo"),
					Instruction: pulumi.String("Search the web and answer with short summaries that cite their sources."),
					Toolsets: dockeragent.ToolsetArray{
						dockeragent.ToolsetArgs{
							Type: pulumi.String("mcp"),
							Ref:  pulumi.String("docker:duckduckgo"),
						},
					},
				},
			},
			Metadata: &dockeragent.MetadataArgs{
				Author:  pulumi.String("Jane Doe"),
				License: pulumi.String("Apache-2.0"),
				Version: pulumi.String("1.0.0"),
				Tags:    pulumi.StringArray{pulumi.String("research"), pulumi.String("demo")},
			},
		})
		if err != nil {
			return err
		}

		// Share the configuration as an OCI artifact, like `docker agent share push`.
		artifact, err := dockeragent.NewAgentArtifact(ctx, "research-assistant", &dockeragent.AgentArtifactArgs{
			Reference: pulumi.String("localhost:5000/agents/research-assistant:v1"),
			Yaml:      assistant.Yaml,
		})
		if err != nil {
			return err
		}

		// Read the shared agent back from the registry, like `docker agent share pull`.
		pulled := dockeragent.GetAgentOutput(ctx, dockeragent.GetAgentOutputArgs{
			Reference: artifact.PinnedReference,
		})

		ctx.Export("digest", artifact.Digest)
		ctx.Export("pinnedReference", artifact.PinnedReference)
		ctx.Export("requiredEnvVars", assistant.RequiredEnvVars)
		ctx.Export("pulledAgents", pulled.AgentNames())
		return nil
	})
}
```

{{% /choosable %}}
{{% choosable language csharp %}}

```csharp
using System.Collections.Generic;
using Pulumi;
using ediri.DockerAgent;
using ediri.DockerAgent.Inputs;

return await Deployment.RunAsync(() =>
{
    // A typed docker-agent configuration, validated during `pulumi preview`.
    var assistant = new AgentConfig("research-assistant", new AgentConfigArgs
    {
        Models =
        {
            ["fast"] = new ModelDefArgs
            {
                Provider = "openai",
                Model = "gpt-5-mini",
                MaxTokens = 16000,
            },
        },
        Agents =
        {
            new AgentArgs
            {
                Name = "root",
                Model = "fast",
                Description = "Answers questions and delegates web research",
                Instruction = "You are a helpful assistant. Delegate questions that need current information to the researcher agent, then summarise its findings.",
                SubAgents = { "researcher" },
            },
            new AgentArgs
            {
                Name = "researcher",
                Model = "fast",
                Description = "Searches the web with DuckDuckGo",
                Instruction = "Search the web and answer with short summaries that cite their sources.",
                Toolsets =
                {
                    new ToolsetArgs { Type = "mcp", Ref = "docker:duckduckgo" },
                },
            },
        },
        Metadata = new MetadataArgs
        {
            Author = "Jane Doe",
            License = "Apache-2.0",
            Version = "1.0.0",
            Tags = { "research", "demo" },
        },
    });

    // Share the configuration as an OCI artifact, like `docker agent share push`.
    var artifact = new AgentArtifact("research-assistant", new AgentArtifactArgs
    {
        Reference = "localhost:5000/agents/research-assistant:v1",
        Yaml = assistant.Yaml,
    });

    // Read the shared agent back from the registry, like `docker agent share pull`.
    var pulled = GetAgent.Invoke(new GetAgentInvokeArgs
    {
        Reference = artifact.PinnedReference,
    });

    return new Dictionary<string, object?>
    {
        ["digest"] = artifact.Digest,
        ["pinnedReference"] = artifact.PinnedReference,
        ["requiredEnvVars"] = assistant.RequiredEnvVars,
        ["pulledAgents"] = pulled.Apply(result => result.AgentNames),
    };
});
```

{{% /choosable %}}
{{% choosable language yaml %}}

```yaml
name: research-assistant
runtime: yaml

resources:
  # A typed docker-agent configuration, validated during `pulumi preview`.
  assistant:
    type: docker-agent:index:AgentConfig
    properties:
      models:
        fast:
          provider: openai
          model: gpt-5-mini
          maxTokens: 16000
      agents:
        - name: root
          model: fast
          description: Answers questions and delegates web research
          instruction: >-
            You are a helpful assistant. Delegate questions that need current
            information to the researcher agent, then summarise its findings.
          subAgents:
            - researcher
        - name: researcher
          model: fast
          description: Searches the web with DuckDuckGo
          instruction: Search the web and answer with short summaries that cite their sources.
          toolsets:
            - type: mcp
              ref: docker:duckduckgo
      metadata:
        author: Jane Doe
        license: Apache-2.0
        version: 1.0.0
        tags:
          - research
          - demo

  # Share the configuration as an OCI artifact, like `docker agent share push`.
  artifact:
    type: docker-agent:index:AgentArtifact
    properties:
      reference: localhost:5000/agents/research-assistant:v1
      yaml: ${assistant.yaml}

variables:
  # Read the shared agent back from the registry, like `docker agent share pull`.
  pulled:
    fn::invoke:
      function: docker-agent:index:getAgent
      arguments:
        reference: ${artifact.pinnedReference}

outputs:
  digest: ${artifact.digest}
  pinnedReference: ${artifact.pinnedReference}
  requiredEnvVars: ${assistant.requiredEnvVars}
  pulledAgents: ${pulled.agentNames}
```

{{% /choosable %}}
{{% choosable language hcl %}}

```hcl
terraform {
  required_providers {
    docker-agent = {
      source  = "pulumi/docker-agent"
      version = "0.1.0"
    }
  }
}

# A typed docker-agent configuration, validated during `pulumi preview`.
resource "docker-agent_agent_config" "assistant" {
  models = {
    fast = {
      provider   = "openai"
      model      = "gpt-5-mini"
      max_tokens = 16000
    }
  }

  agents {
    name        = "root"
    model       = "fast"
    description = "Answers questions and delegates web research"
    instruction = "You are a helpful assistant. Delegate questions that need current information to the researcher agent, then summarise its findings."
    sub_agents  = ["researcher"]
  }

  agents {
    name        = "researcher"
    model       = "fast"
    description = "Searches the web with DuckDuckGo"
    instruction = "Search the web and answer with short summaries that cite their sources."

    toolsets {
      type = "mcp"
      ref  = "docker:duckduckgo"
    }
  }

  metadata = {
    author  = "Jane Doe"
    license = "Apache-2.0"
    version = "1.0.0"
    tags    = ["research", "demo"]
  }
}

# Share the configuration as an OCI artifact, like `docker agent share push`.
resource "docker-agent_agent_artifact" "artifact" {
  reference = "localhost:5000/agents/research-assistant:v1"
  yaml      = docker-agent_agent_config.assistant.yaml
}

# Read the shared agent back from the registry, like `docker agent share pull`.
data "docker-agent_agent" "pulled" {
  reference = docker-agent_agent_artifact.artifact.pinned_reference
}

output "digest" {
  value = docker-agent_agent_artifact.artifact.digest
}

output "pinnedReference" {
  value = docker-agent_agent_artifact.artifact.pinned_reference
}

output "requiredEnvVars" {
  value = docker-agent_agent_config.assistant.required_env_vars
}

output "pulledAgents" {
  value = data.docker-agent_agent.pulled.agent_names
}
```

In HCL, `AgentConfig` and `AgentArtifact` are the `docker-agent_agent_config` and `docker-agent_agent_artifact` resources, `getAgent` and `validateAgentConfig` are the `docker-agent_agent` and `docker-agent_validate_agent_config` data sources, and property names are snake_case. Lists of objects, such as `agents` and an agent's `toolsets`, are written as repeated nested blocks.

{{% /choosable %}}
{{< /chooser >}}

A local registry needs no configuration. To push to a registry that requires credentials, either run `docker login <registry>` (the provider uses your Docker credentials) or set them on the provider:

```bash
pulumi config set --path 'docker-agent:registryAuth[0].address' ghcr.io
pulumi config set --path 'docker-agent:registryAuth[0].username' <github-user>
pulumi config set --secret --path 'docker-agent:registryAuth[0].password' <github-token>
```

### Run the shared agent

The pushed artifact is a regular Docker Agent artifact, so the `docker agent` CLI can run it directly. The `requiredEnvVars` output lists the model credentials the agent needs:

```bash
export OPENAI_API_KEY=<your-openai-api-key>
docker agent run localhost:5000/agents/research-assistant:v1
```

`docker agent share pull <reference>` and `docker agent serve api <reference>` accept the same reference. MCP servers referenced as `docker:<name>` come from the [Docker MCP Catalog](https://docs.docker.com/ai/mcp-catalog-and-toolkit/) and run through the Docker MCP Gateway on the machine that runs the agent.

## Sandbox mode and every other configuration key

Every key of the [Docker Agent configuration](https://docs.docker.com/ai/docker-agent/configuration/overview/) is a typed `AgentConfig` input, including [sandbox mode](https://docs.docker.com/ai/docker-agent/configuration/sandbox/) (`runtime`), hooks, commands, skills, MCP and RAG definitions, budgets, model routing, evaluators, structured output and flavors. The inputs are camelCase (snake_case in HCL) and render as the snake_case keys of the agent YAML. Settings under `runtime` are defaults for the people who run the agent:

- `sandbox: true` runs the agent in a sandbox unless they pass `--sandbox=false`.
- `networkAllowlist` adds hosts to the sandbox's default-deny network proxy.
- `safety` sets the default safety mode for new sessions.

{{< chooser language "typescript,python,go,csharp,yaml,hcl" >}}
{{% choosable language typescript %}}

```typescript
import * as dockeragent from "@ediri/pulumi-docker-agent";

const fixer = new dockeragent.AgentConfig("test-fixer", {
    runtime: {
        sandbox: true,
        safety: "restricted",
        networkAllowlist: ["api.github.com", "registry.npmjs.org:443"],
    },
    agents: [{
        name: "root",
        model: "anthropic/claude-sonnet-4-5",
        instruction: "Run the test suite and fix the failing tests.",
        toolsets: [{ type: "shell" }, { type: "filesystem" }],
        hooks: { sessionStart: [{ type: "builtin", command: "add_environment_info" }] },
        simpleCommands: { test: "Run the tests and summarise the failures" },
    }],
    budget: { maxCost: 5, maxTime: "30m" },
});

export const agentYaml = fixer.yaml;
```

{{% /choosable %}}
{{% choosable language python %}}

```python
import pulumi
import ediri_docker_agent as dockeragent

fixer = dockeragent.AgentConfig("test-fixer",
    runtime={
        "sandbox": True,
        "safety": "restricted",
        "network_allowlist": ["api.github.com", "registry.npmjs.org:443"],
    },
    agents=[{
        "name": "root",
        "model": "anthropic/claude-sonnet-4-5",
        "instruction": "Run the test suite and fix the failing tests.",
        "toolsets": [{"type": "shell"}, {"type": "filesystem"}],
        "hooks": {"session_start": [{"type": "builtin", "command": "add_environment_info"}]},
        "simple_commands": {"test": "Run the tests and summarise the failures"},
    }],
    budget={"max_cost": 5, "max_time": "30m"})

pulumi.export("agentYaml", fixer.yaml)
```

{{% /choosable %}}
{{% choosable language go %}}

```go
package main

import (
	"github.com/dirien/pulumi-docker-agent/sdk/go/dockeragent"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		fixer, err := dockeragent.NewAgentConfig(ctx, "test-fixer", &dockeragent.AgentConfigArgs{
			Runtime: &dockeragent.RuntimeDefaultsArgs{
				Sandbox:          pulumi.Bool(true),
				Safety:           pulumi.String("restricted"),
				NetworkAllowlist: pulumi.StringArray{pulumi.String("api.github.com"), pulumi.String("registry.npmjs.org:443")},
			},
			Agents: dockeragent.AgentArray{
				dockeragent.AgentArgs{
					Name:        pulumi.String("root"),
					Model:       pulumi.String("anthropic/claude-sonnet-4-5"),
					Instruction: pulumi.String("Run the test suite and fix the failing tests."),
					Toolsets: dockeragent.ToolsetArray{
						dockeragent.ToolsetArgs{Type: pulumi.String("shell")},
						dockeragent.ToolsetArgs{Type: pulumi.String("filesystem")},
					},
					Hooks: &dockeragent.HooksArgs{
						SessionStart: dockeragent.HookDefinitionArray{
							dockeragent.HookDefinitionArgs{Type: pulumi.String("builtin"), Command: pulumi.String("add_environment_info")},
						},
					},
					SimpleCommands: pulumi.StringMap{"test": pulumi.String("Run the tests and summarise the failures")},
				},
			},
			Budget: &dockeragent.BudgetArgs{MaxCost: pulumi.Float64(5), MaxTime: pulumi.String("30m")},
		})
		if err != nil {
			return err
		}
		ctx.Export("agentYaml", fixer.Yaml)
		return nil
	})
}
```

{{% /choosable %}}
{{% choosable language csharp %}}

```csharp
using System.Collections.Generic;
using Pulumi;
using ediri.DockerAgent;
using ediri.DockerAgent.Inputs;

return await Deployment.RunAsync(() =>
{
    var fixer = new AgentConfig("test-fixer", new AgentConfigArgs
    {
        Runtime = new RuntimeDefaultsArgs
        {
            Sandbox = true,
            Safety = "restricted",
            NetworkAllowlist = { "api.github.com", "registry.npmjs.org:443" },
        },
        Agents =
        {
            new AgentArgs
            {
                Name = "root",
                Model = "anthropic/claude-sonnet-4-5",
                Instruction = "Run the test suite and fix the failing tests.",
                Toolsets = { new ToolsetArgs { Type = "shell" }, new ToolsetArgs { Type = "filesystem" } },
                Hooks = new HooksArgs
                {
                    SessionStart = { new HookDefinitionArgs { Type = "builtin", Command = "add_environment_info" } },
                },
                SimpleCommands = { { "test", "Run the tests and summarise the failures" } },
            },
        },
        Budget = new BudgetArgs { MaxCost = 5, MaxTime = "30m" },
    });

    return new Dictionary<string, object?> { ["agentYaml"] = fixer.Yaml };
});
```

{{% /choosable %}}
{{% choosable language yaml %}}

```yaml
name: test-fixer
runtime: yaml

resources:
  test-fixer:
    type: docker-agent:index:AgentConfig
    properties:
      runtime:
        sandbox: true
        safety: restricted
        networkAllowlist:
          - api.github.com
          - registry.npmjs.org:443
      agents:
        - name: root
          model: anthropic/claude-sonnet-4-5
          instruction: Run the test suite and fix the failing tests.
          toolsets:
            - type: shell
            - type: filesystem
          hooks:
            sessionStart:
              - type: builtin
                command: add_environment_info
          simpleCommands:
            test: Run the tests and summarise the failures
      budget:
        maxCost: 5
        maxTime: 30m

outputs:
  agentYaml: ${test-fixer.yaml}
```

{{% /choosable %}}
{{% choosable language hcl %}}

```hcl
terraform {
  required_providers {
    docker-agent = {
      source  = "pulumi/docker-agent"
      version = "0.1.0"
    }
  }
}

resource "docker-agent_agent_config" "test_fixer" {
  runtime = {
    sandbox           = true
    safety            = "restricted"
    network_allowlist = ["api.github.com", "registry.npmjs.org:443"]
  }
  agents {
    name        = "root"
    model       = "anthropic/claude-sonnet-4-5"
    instruction = "Run the test suite and fix the failing tests."
    toolsets {
      type = "shell"
    }
    toolsets {
      type = "filesystem"
    }
    hooks = {
      session_start = [{ type = "builtin", command = "add_environment_info" }]
    }
    simple_commands = {
      test = "Run the tests and summarise the failures"
    }
  }
  budget = {
    max_cost = 5
    max_time = "30m"
  }
}

output "agent_yaml" {
  value = docker-agent_agent_config.test_fixer.yaml
}
```

In HCL, objects such as `runtime`, `hooks` and `budget` are attributes, while `agents` and `toolsets` are repeated nested blocks.

{{% /choosable %}}
{{< /chooser >}}

The `yaml` output of every program above is:

```yaml
version: "16"
agents:
  root:
    model: anthropic/claude-sonnet-4-5
    instruction: Run the test suite and fix the failing tests.
    toolsets:
      - type: shell
      - type: filesystem
    commands:
      test: Run the tests and summarise the failures
    hooks:
      session_start:
        - type: builtin
          command: add_environment_info
runtime:
  sandbox: true
  network_allowlist:
    - api.github.com
    - registry.npmjs.org:443
  safety: restricted
budget:
  max_cost: 5.0
  max_time: 30m
```

YAML shorthands have typed equivalents:

- The bare-string command form `name: instruction` is `simpleCommands`; the object form is `commands`.
- `skills: true` is `skills: ["local"]`, and inline skills go in `inlineSkills`.
- `thinking_budget` is `thinkingBudget` for an effort level, or `thinkingBudgetTokens` for a token count.
- `defer` is `defer: true` for all tools, or `deferTools` for a list of tools.

Every object also has an `extra` map, merged verbatim into the YAML, for keys added by docker-agent releases newer than the one the provider pins.

## Local configuration files

Two Docker Agent configuration files are not agent definitions, and the provider manages both:

- `UserSettings` writes the [user configuration](https://docs.docker.com/ai/docker-agent/configuration/user-settings/) at `~/.config/cagent/config.yaml`: TUI settings, the default safety mode, global permissions and hooks, keybindings, aliases, global model providers, the default model, the board, the credential helper and the sandbox allowlist. Every key of docker-agent's user configuration is a typed input, and the inputs are validated with docker-agent's own checks during `pulumi preview`.
- `AgentsIgnore` writes a [`.agentsignore`](https://docs.docker.com/ai/docker-agent/configuration/agentsignore/) file. Its patterns use `.gitignore` syntax; the filesystem toolset hides matching paths and refuses to read or write them.

{{< chooser language "typescript,python,go,csharp,yaml,hcl" >}}
{{% choosable language typescript %}}

```typescript
import * as dockeragent from "@ediri/pulumi-docker-agent";

// Docker Agent's user configuration, validated with docker-agent's own checks.
const settings = new dockeragent.UserSettings("user-settings", {
    path: "~/.config/cagent/config.yaml",
    settings: {
        theme: "auto",
        safety: "balanced",
        layout: { sidebarPosition: "left" },
        permissions: { deny: ["shell:cmd=sudo*"], allow: ["read_*"] },
        keybindings: [{ action: "commands", keys: ["f2", "ctrl+k"] }],
    },
    aliases: {
        reviewer: { path: "docker.io/myorg/reviewer:latest", safety: "restricted" },
    },
});

// Hide secrets from the agent's filesystem tools in a project.
const ignore = new dockeragent.AgentsIgnore("my-service", {
    directory: "~/src/my-service",
    patterns: [".env", ".env.*", "*.pem", "*.key", "!public.key", "build/"],
});

export const configPath = settings.filePath;
export const agentsIgnore = ignore.content;
```

{{% /choosable %}}
{{% choosable language python %}}

```python
import pulumi
import ediri_docker_agent as dockeragent

# Docker Agent's user configuration, validated with docker-agent's own checks.
settings = dockeragent.UserSettings(
    "user-settings",
    path="~/.config/cagent/config.yaml",
    settings={
        "theme": "auto",
        "safety": "balanced",
        "layout": {"sidebar_position": "left"},
        "permissions": {"deny": ["shell:cmd=sudo*"], "allow": ["read_*"]},
        "keybindings": [{"action": "commands", "keys": ["f2", "ctrl+k"]}],
    },
    aliases={
        "reviewer": {"path": "docker.io/myorg/reviewer:latest", "safety": "restricted"},
    },
)

# Hide secrets from the agent's filesystem tools in a project.
ignore = dockeragent.AgentsIgnore(
    "my-service",
    directory="~/src/my-service",
    patterns=[".env", ".env.*", "*.pem", "*.key", "!public.key", "build/"],
)

pulumi.export("configPath", settings.file_path)
pulumi.export("agentsIgnore", ignore.content)
```

{{% /choosable %}}
{{% choosable language go %}}

```go
package main

import (
	"github.com/dirien/pulumi-docker-agent/sdk/go/dockeragent"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		// Docker Agent's user configuration, validated with docker-agent's own checks.
		settings, err := dockeragent.NewUserSettings(ctx, "user-settings", &dockeragent.UserSettingsArgs{
			Path: pulumi.String("~/.config/cagent/config.yaml"),
			Settings: &dockeragent.UserGlobalSettingsArgs{
				Theme:  pulumi.String("auto"),
				Safety: pulumi.String("balanced"),
				Layout: &dockeragent.UserLayoutSettingsArgs{
					SidebarPosition: pulumi.String("left"),
				},
				Permissions: &dockeragent.UserPermissionsArgs{
					Deny:  pulumi.StringArray{pulumi.String("shell:cmd=sudo*")},
					Allow: pulumi.StringArray{pulumi.String("read_*")},
				},
				Keybindings: dockeragent.UserKeybindingArray{
					dockeragent.UserKeybindingArgs{
						Action: pulumi.String("commands"),
						Keys:   pulumi.StringArray{pulumi.String("f2"), pulumi.String("ctrl+k")},
					},
				},
			},
			Aliases: dockeragent.UserAliasMap{
				"reviewer": dockeragent.UserAliasArgs{
					Path:   pulumi.String("docker.io/myorg/reviewer:latest"),
					Safety: pulumi.String("restricted"),
				},
			},
		})
		if err != nil {
			return err
		}

		// Hide secrets from the agent's filesystem tools in a project.
		ignore, err := dockeragent.NewAgentsIgnore(ctx, "my-service", &dockeragent.AgentsIgnoreArgs{
			Directory: pulumi.String("~/src/my-service"),
			Patterns: pulumi.StringArray{
				pulumi.String(".env"), pulumi.String(".env.*"), pulumi.String("*.pem"),
				pulumi.String("*.key"), pulumi.String("!public.key"), pulumi.String("build/"),
			},
		})
		if err != nil {
			return err
		}

		ctx.Export("configPath", settings.FilePath)
		ctx.Export("agentsIgnore", ignore.Content)
		return nil
	})
}
```

{{% /choosable %}}
{{% choosable language csharp %}}

```csharp
using System.Collections.Generic;
using Pulumi;
using ediri.DockerAgent;
using ediri.DockerAgent.Inputs;

return await Deployment.RunAsync(() =>
{
    // Docker Agent's user configuration, validated with docker-agent's own checks.
    var settings = new UserSettings("user-settings", new UserSettingsArgs
    {
        Path = "~/.config/cagent/config.yaml",
        Settings = new UserGlobalSettingsArgs
        {
            Theme = "auto",
            Safety = "balanced",
            Layout = new UserLayoutSettingsArgs { SidebarPosition = "left" },
            Permissions = new UserPermissionsArgs
            {
                Deny = { "shell:cmd=sudo*" },
                Allow = { "read_*" },
            },
            Keybindings =
            {
                new UserKeybindingArgs { Action = "commands", Keys = { "f2", "ctrl+k" } },
            },
        },
        Aliases =
        {
            ["reviewer"] = new UserAliasArgs
            {
                Path = "docker.io/myorg/reviewer:latest",
                Safety = "restricted",
            },
        },
    });

    // Hide secrets from the agent's filesystem tools in a project.
    var ignore = new AgentsIgnore("my-service", new AgentsIgnoreArgs
    {
        Directory = "~/src/my-service",
        Patterns = { ".env", ".env.*", "*.pem", "*.key", "!public.key", "build/" },
    });

    return new Dictionary<string, object?>
    {
        ["configPath"] = settings.FilePath,
        ["agentsIgnore"] = ignore.Content,
    };
});
```

{{% /choosable %}}
{{% choosable language yaml %}}

```yaml
name: agent-local-config
runtime: yaml

resources:
  # Docker Agent's user configuration, validated with docker-agent's own checks.
  settings:
    type: docker-agent:index:UserSettings
    properties:
      path: ~/.config/cagent/config.yaml
      settings:
        theme: auto
        safety: balanced
        layout:
          sidebarPosition: left
        permissions:
          deny: ["shell:cmd=sudo*"]
          allow: ["read_*"]
        keybindings:
          - action: commands
            keys: [f2, ctrl+k]
      aliases:
        reviewer:
          path: docker.io/myorg/reviewer:latest
          safety: restricted

  # Hide secrets from the agent's filesystem tools in a project.
  ignore:
    type: docker-agent:index:AgentsIgnore
    properties:
      directory: ~/src/my-service
      patterns: [".env", ".env.*", "*.pem", "*.key", "!public.key", "build/"]

outputs:
  configPath: ${settings.filePath}
  agentsIgnore: ${ignore.content}
```

{{% /choosable %}}
{{% choosable language hcl %}}

```hcl
terraform {
  required_providers {
    docker-agent = {
      source  = "pulumi/docker-agent"
      version = "0.1.0"
    }
  }
}

# Docker Agent's user configuration, validated with docker-agent's own checks.
resource "docker-agent_user_settings" "settings" {
  path = "~/.config/cagent/config.yaml"

  settings = {
    theme  = "auto"
    safety = "balanced"
    layout = {
      sidebar_position = "left"
    }
    permissions = {
      deny  = ["shell:cmd=sudo*"]
      allow = ["read_*"]
    }
    keybindings = [{
      action = "commands"
      keys   = ["f2", "ctrl+k"]
    }]
  }

  aliases = {
    reviewer = {
      path   = "docker.io/myorg/reviewer:latest"
      safety = "restricted"
    }
  }
}

# Hide secrets from the agent's filesystem tools in a project.
resource "docker-agent_agents_ignore" "my_service" {
  directory = "~/src/my-service"
  patterns  = [".env", ".env.*", "*.pem", "*.key", "!public.key", "build/"]
}

output "configPath" {
  value = docker-agent_user_settings.settings.file_path
}

output "agentsIgnore" {
  value = docker-agent_agents_ignore.my_service.content
}
```

In HCL, `UserSettings` and `AgentsIgnore` are the `docker-agent_user_settings` and `docker-agent_agents_ignore` resources. `settings`, `aliases` and the other object and map inputs are attributes, with snake_case keys at every level.

{{% /choosable %}}
{{< /chooser >}}

The provider writes the files on the machine that runs `pulumi up`: `~` is that user's home directory, and relative paths are relative to the Pulumi project directory. Writes are atomic, `config.yaml` gets mode `0600`, and previews never write. A file that already exists is never replaced silently: if its content differs, the preview fails until you copy what you want to keep into the inputs or set `overwrite: true`. The existing file is moved to `<file>.pulumi-backup` and moved back by `pulumi destroy`; a file the provider created is deleted instead. Symbolic links, for example from a dotfile manager, are kept: the provider writes the file they point to. Leave `path` (or `directory`) unset to only render the content, for example to bake `yaml` into a container image or a Docker Sandboxes kit. `pulumi refresh` reports changes made outside of Pulumi, including those docker-agent makes itself (`docker agent alias add`, the `/settings` dialog), and the next `pulumi up` writes the declared content again. Set the [`retainOnDelete`](https://www.pulumi.com/docs/iac/concepts/options/retainondelete/) resource option to keep the files on `pulumi destroy`.

## Run an agent during `pulumi up`

`AgentRun` runs a prompt against an agent configuration and exposes the answer as outputs, so other resources can use what the agent produced: a generated text, a review or a structured decision. The configuration is `yaml` (usually the `yaml` output of an `AgentConfig`) or an artifact `reference`. The agent runs inside the provider with the [docker-agent Go SDK](https://docs.docker.com/ai/docker-agent/guides/go-sdk/), the same runtime as `docker agent run --exec`, headless and unattended. The outputs are `output` (the final answer), `structuredOutput` (the answer as JSON, when the agent declares `structuredOutput`), `agentName`, `usage` (tokens and estimated cost) and `toolCalls` (the tools that ran).

{{< chooser language "typescript,python,go,csharp,yaml,hcl" >}}
{{% choosable language typescript %}}

```typescript
import * as pulumi from "@pulumi/pulumi";
import * as dockeragent from "@ediri/pulumi-docker-agent";

const config = new pulumi.Config();

// The agent: its answer must match a JSON schema.
const reviewer = new dockeragent.AgentConfig("reviewer", {
    agents: [{
        name: "root",
        model: "openai/gpt-5-mini",
        instruction: "Rate the risk of the infrastructure change and explain it in one sentence.",
        structuredOutput: {
            name: "change_review",
            schema: {
                type: "object",
                properties: {
                    risk: { type: "string", enum: ["low", "medium", "high"] },
                    summary: { type: "string" },
                },
                required: ["risk", "summary"],
                additionalProperties: false,
            },
        },
    }],
});

// Runs the agent during `pulumi up` (never during a preview) and keeps its answer in the state.
const review = new dockeragent.AgentRun("review", {
    yaml: reviewer.yaml,
    prompt: "Upgrade the production PostgreSQL cluster from version 15 to 17.",
    env: { OPENAI_API_KEY: config.requireSecret("openaiApiKey") },
});

export const risk = review.structuredOutput.apply(o => o.risk);
export const summary = review.structuredOutput.apply(o => o.summary);
```

{{% /choosable %}}
{{% choosable language python %}}

```python
import pulumi
import ediri_docker_agent as dockeragent

config = pulumi.Config()

# The agent: its answer must match a JSON schema.
reviewer = dockeragent.AgentConfig("reviewer", agents=[{
    "name": "root",
    "model": "openai/gpt-5-mini",
    "instruction": "Rate the risk of the infrastructure change and explain it in one sentence.",
    "structured_output": {
        "name": "change_review",
        "schema": {
            "type": "object",
            "properties": {
                "risk": {"type": "string", "enum": ["low", "medium", "high"]},
                "summary": {"type": "string"},
            },
            "required": ["risk", "summary"],
            "additionalProperties": False,
        },
    },
}])

# Runs the agent during `pulumi up` (never during a preview) and keeps its answer in the state.
review = dockeragent.AgentRun("review",
    yaml=reviewer.yaml,
    prompt="Upgrade the production PostgreSQL cluster from version 15 to 17.",
    env={"OPENAI_API_KEY": config.require_secret("openaiApiKey")})

pulumi.export("risk", review.structured_output.apply(lambda o: o["risk"]))
pulumi.export("summary", review.structured_output.apply(lambda o: o["summary"]))
pulumi.export("yaml", reviewer.yaml)
```

{{% /choosable %}}
{{% choosable language go %}}

```go
package main

import (
	"github.com/dirien/pulumi-docker-agent/sdk/go/dockeragent"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		cfg := config.New(ctx, "")

		// The agent: its answer must match a JSON schema.
		reviewer, err := dockeragent.NewAgentConfig(ctx, "reviewer", &dockeragent.AgentConfigArgs{
			Agents: dockeragent.AgentArray{
				dockeragent.AgentArgs{
					Name:        pulumi.String("root"),
					Model:       pulumi.String("openai/gpt-5-mini"),
					Instruction: pulumi.String("Rate the risk of the infrastructure change and explain it in one sentence."),
					StructuredOutput: &dockeragent.StructuredOutputArgs{
						Name: pulumi.String("change_review"),
						Schema: pulumi.ToMap(map[string]any{
							"type": "object",
							"properties": map[string]any{
								"risk":    map[string]any{"type": "string", "enum": []string{"low", "medium", "high"}},
								"summary": map[string]any{"type": "string"},
							},
							"required":             []string{"risk", "summary"},
							"additionalProperties": false,
						}),
					},
				},
			},
		})
		if err != nil {
			return err
		}

		// Runs the agent during `pulumi up` (never during a preview) and keeps its answer in the state.
		review, err := dockeragent.NewAgentRun(ctx, "review", &dockeragent.AgentRunArgs{
			Yaml:   reviewer.Yaml,
			Prompt: pulumi.String("Upgrade the production PostgreSQL cluster from version 15 to 17."),
			Env: pulumi.StringMap{
				"OPENAI_API_KEY": cfg.RequireSecret("openaiApiKey"),
			},
		})
		if err != nil {
			return err
		}

		field := func(name string) pulumi.Output {
			return review.StructuredOutput.ApplyT(func(o any) any { return o.(map[string]any)[name] })
		}
		ctx.Export("risk", field("risk"))
		ctx.Export("summary", field("summary"))
		return nil
	})
}
```

{{% /choosable %}}
{{% choosable language csharp %}}

```csharp
using System.Collections.Generic;
using Pulumi;
using ediri.DockerAgent;
using ediri.DockerAgent.Inputs;

return await Deployment.RunAsync(() =>
{
    var config = new Pulumi.Config();

    // The agent: its answer must match a JSON schema.
    var reviewer = new AgentConfig("reviewer", new AgentConfigArgs
    {
        Agents =
        {
            new AgentArgs
            {
                Name = "root",
                Model = "openai/gpt-5-mini",
                Instruction = "Rate the risk of the infrastructure change and explain it in one sentence.",
                StructuredOutput = new StructuredOutputArgs
                {
                    Name = "change_review",
                    Schema =
                    {
                        ["type"] = "object",
                        ["properties"] = new Dictionary<string, object>
                        {
                            ["risk"] = new Dictionary<string, object>
                            {
                                ["type"] = "string",
                                ["enum"] = new[] { "low", "medium", "high" },
                            },
                            ["summary"] = new Dictionary<string, object> { ["type"] = "string" },
                        },
                        ["required"] = new[] { "risk", "summary" },
                        ["additionalProperties"] = false,
                    },
                },
            },
        },
    });

    // Runs the agent during `pulumi up` (never during a preview) and keeps its answer in the state.
    var review = new AgentRun("review", new AgentRunArgs
    {
        Yaml = reviewer.Yaml,
        Prompt = "Upgrade the production PostgreSQL cluster from version 15 to 17.",
        Env =
        {
            ["OPENAI_API_KEY"] = config.RequireSecret("openaiApiKey"),
        },
    });

    return new Dictionary<string, object?>
    {
        ["risk"] = review.StructuredOutput.Apply(o => ((IDictionary<string, object>)o!)["risk"]),
        ["summary"] = review.StructuredOutput.Apply(o => ((IDictionary<string, object>)o!)["summary"]),
    };
});
```

{{% /choosable %}}
{{% choosable language yaml %}}

```yaml
name: change-review
runtime: yaml
config:
  openaiApiKey:
    type: string
    secret: true

resources:
  # The agent: its answer must match a JSON schema.
  reviewer:
    type: docker-agent:index:AgentConfig
    properties:
      agents:
        - name: root
          model: openai/gpt-5-mini
          instruction: Rate the risk of the infrastructure change and explain it in one sentence.
          structuredOutput:
            name: change_review
            schema:
              type: object
              properties:
                risk: {type: string, enum: [low, medium, high]}
                summary: {type: string}
              required: [risk, summary]
              additionalProperties: false

  # Runs the agent during `pulumi up` (never during a preview) and keeps its answer in the state.
  review:
    type: docker-agent:index:AgentRun
    properties:
      yaml: ${reviewer.yaml}
      prompt: Upgrade the production PostgreSQL cluster from version 15 to 17.
      env:
        OPENAI_API_KEY: ${openaiApiKey}

outputs:
  risk: ${review.structuredOutput.risk}
  summary: ${review.structuredOutput.summary}
```

{{% /choosable %}}
{{% choosable language hcl %}}

```hcl
terraform {
  required_providers {
    docker-agent = {
      source  = "pulumi/docker-agent"
      version = "0.1.0"
    }
  }
}

# Set it with `pulumi config set --secret openai_api_key <key>`.
variable "openai_api_key" {
  type      = string
  sensitive = true
}

# The agent: its answer must match a JSON schema.
resource "docker-agent_agent_config" "reviewer" {
  agents {
    name        = "root"
    model       = "openai/gpt-5-mini"
    instruction = "Rate the risk of the infrastructure change and explain it in one sentence."
    structured_output = {
      name = "change_review"
      schema = {
        type = "object"
        properties = {
          risk    = { type = "string", enum = ["low", "medium", "high"] }
          summary = { type = "string" }
        }
        required             = ["risk", "summary"]
        additionalProperties = false
      }
    }
  }
}

# Runs the agent during `pulumi up` (never during a preview) and keeps its answer in the state.
resource "docker-agent_agent_run" "review" {
  yaml   = docker-agent_agent_config.reviewer.yaml
  prompt = "Upgrade the production PostgreSQL cluster from version 15 to 17."
  env = {
    OPENAI_API_KEY = var.openai_api_key
  }
}

output "risk" {
  value = docker-agent_agent_run.review.structured_output.risk
}

output "summary" {
  value = docker-agent_agent_run.review.structured_output.summary
}
```

In HCL, `AgentRun` is the `docker-agent_agent_run` resource, and its properties are snake_case (`structured_output`, `tool_calls`, `inherit_environment`). `env` and an agent's `structured_output` are attributes; `agents` are repeated nested blocks. HCL variables are Pulumi config values: set the key with `pulumi config set --secret openai_api_key <key>`.

{{% /choosable %}}
{{< /chooser >}}

- **When it runs.** Never during `pulumi preview`: the outputs are unknown until `pulumi up`. After that, `pulumi up` without changes and `pulumi refresh` keep the stored answer. A change of an input that affects the run (`yaml`, `reference`, `agent`, `prompt`, `env`, `inheritEnvironment`, `workingDir`, `toolApproval`, `allowHookApprovals` or `triggers`) runs the agent again as an update, and so does such an input that is unknown during the preview; `triggers` takes arbitrary values for asking again without other changes. A change of `timeoutSeconds` or `outputSecret` alone only updates the state. Rotating the key in `env` also runs the agent again; add `env` to the [`ignoreChanges`](https://www.pulumi.com/docs/iac/concepts/options/ignorechanges/) resource option to avoid that. `pulumi destroy` does nothing, and importing an `AgentRun` is not supported. A `reference` is pulled when the agent runs, so pin it by digest (or use `triggers`) to control when a moved tag is picked up.
- **Credentials and secrets.** The model's API key comes from `env`, which is stored as a secret, and, unless `inheritEnvironment` is `false`, from the environment of the Pulumi CLI. Errors, warnings and logs never contain the values of `env`, of secret inputs, of credential-named variables of the environment (names containing `API_KEY`, `TOKEN`, `SECRET`, `PASSWORD`, `CREDENTIAL`, `ACCESS_KEY`, ...) or of the variables the configuration names as `token_key`. A secret `env` does not make the answer secret by itself, but `output` and `structuredOutput` are stored as secrets when the answer contains one of those values (for example when an instruction passes a key with `${env.NAME}` and the model repeats it), when `prompt` or `yaml` is secret, and with `outputSecret: true`.
- **Isolation.** Each run gets private docker-agent data, config and cache directories that are deleted afterwards. `~/.cagent` is never touched, and the user settings, aliases, global providers and credential sources in `~/.config/cagent` do not apply, so a run does not depend on the docker-agent CLI setup of the machine. The agent works in a fresh temporary directory unless `workingDir` is set. The model catalog (aliases, context windows, pricing for `usage.cost`) is the one bundled with the provider.
- **Limits.** `timeoutSeconds` (default 300) bounds a run, and the run fails if the agent ends without a final answer (for example after `max_iterations`). Runs within one provider process execute one at a time.

### Where tools run

**Tools run on the machine that runs `pulumi up`**: your workstation, the CI runner or the Pulumi Deployments worker, with the permissions of that process.

- With the default `toolApproval: deny`, a tool call only runs when docker-agent's own rules approve it without asking for confirmation:
  - tools annotated read-only, such as `think`, `todo`, `memory` reads, the filesystem toolset's read and search tools, `api` tools, and MCP tools whose server declares them read-only;
  - calls that an `allow` rule of the configuration's `permissions` matches (an `allow: ["*"]` rule therefore approves everything);
  - calls that the configuration's own `safety` mode (`strict`, `balanced` or `restricted`) approves (`autonomous` is ignored);
  - delegation to sub-agents (`transfer_task`, `handoff`).

  Approvals from the configuration's hooks are ignored: a `pre_tool_use` hook that answers `permission_decision: allow`, or a `permission_request` hook, cannot approve a call that the rules above would not run. Set `allowHookApprovals: true` to accept them as docker-agent does. Hook denials always apply. Every other call (shell commands, `script` tools, file writes, `fetch`, other MCP tools) is rejected. The model is told that the tool needs a confirmation the run cannot give, and the provider logs a warning that names the tool.
- `toolApproval: allow` runs every tool call, like `docker agent run --yolo`. `deny` rules in the configuration's `permissions` still apply.
- In both modes, these run without any approval because they are part of the configuration: command hooks (at session start, around tool calls, ...), MCP and LSP servers started with `command`, `docker:` MCP servers (through the Docker MCP Gateway), and filesystem `post_edit` commands after an approved write. Read-only file tools can read any file the provider can read unless the toolset sets `allow_list`. Coding-harness agents (`harness`) are rejected because they run an external coding agent that executes its own tools.

**The environment these processes see.** With the default `inheritEnvironment: true`, every process the configuration starts inherits the environment of the Pulumi CLI, for example `PULUMI_ACCESS_TOKEN` or cloud credentials in CI. With `inheritEnvironment: false`, the processes AgentRun starts get only the system variables (`PATH`, `HOME`, `USER`, `LOGNAME`, `SHELL`, `LANG`, `LANGUAGE`, `LC_ALL`, `LC_CTYPE`, `LC_MESSAGES`, `TERM`, `TMPDIR`, `TEMP`, `TMP`, `TZ`, and on Windows `SYSTEMROOT`, `SYSTEMDRIVE`, `COMSPEC`, `PATHEXT`, `WINDIR`, `USERPROFILE`, `APPDATA`, `LOCALAPPDATA`, `PROGRAMDATA` and `PROGRAMFILES`) plus what the configuration passes to them explicitly:

| Process | Environment with `inheritEnvironment: false` |
|---|---|
| Command hooks | system variables and `env` |
| `shell`, `script` and `background_jobs` tools | system variables and the toolset's own `env` (other variables are set to an empty value) |
| MCP servers started with `command` | system variables and the toolset's own `env` |
| LSP servers started with `command` | system variables and the toolset's own `env` (other variables are set to an empty value) |
| `docker:` MCP servers (Docker MCP Gateway), filesystem `post_edit` commands, the `open_url` browser launcher, docker-agent's on-demand tool installation | the whole environment of the Pulumi CLI: docker-agent does not let AgentRun change it |

A toolset's `env` can pass a value of `env` explicitly, for example `env: {GITHUB_TOKEN: "${env.GITHUB_TOKEN}"}` on an MCP toolset.

Review a configuration before you run it, especially one pulled by `reference`. For agents with tools, prefer `inheritEnvironment: false` with credentials in `env`, and a dedicated CI identity without access to production credentials.

## Configuration

- `registryAuth` (Optional) — A list of registry credentials used to push, pull and delete agent artifacts. Each entry has an `address` (Required, the registry host such as `ghcr.io`, `docker.io` or `localhost:5000`), a `username` and a `password` (Secret; a password or access token). Registries without a matching entry fall back to your Docker credentials: `$DOCKER_CONFIG/config.json` (default `~/.docker/config.json`), including `credsStore` and `credHelpers` set up by `docker login`.
- `forceHttps` (Optional) — Refuse plain-HTTP connections to registries other than `localhost` and loopback addresses. Without it, the provider (like the `docker agent` CLI) falls back to plain HTTP when HTTPS fails for `localhost`, `*.localhost`, loopback and private (RFC 1918) addresses. Defaults to `false`. May also be set with the `DOCKER_AGENT_FORCE_HTTPS` environment variable.
- `skipValidation` (Optional) — Skip validating agent configurations and user settings against the docker-agent library bundled with the provider. Use it when a configuration uses fields that are newer than the provider's pinned docker-agent version. Defaults to `false`. May also be set with the `DOCKER_AGENT_SKIP_VALIDATION` environment variable.

The provider needs no model API keys to render, validate and publish configurations. Model credentials such as `OPENAI_API_KEY` or `ANTHROPIC_API_KEY` are needed where the agent runs: by `docker agent run` and `docker agent serve`, or by `AgentRun`, which takes them from its `env` input or the environment of the Pulumi CLI. See the [Installation & Configuration](https://www.pulumi.com/registry/packages/docker-agent/installation-configuration/) page for details.

## Resources and functions

- `AgentConfig` — A typed, validated Docker Agent configuration: every key of the configuration, including sandbox mode, hooks, commands, skills, MCP and RAG definitions and budgets, is a typed input. It renders deterministic YAML (`yaml` output) and reports `sha256`, `defaultAgent`, `agentNames` and `requiredEnvVars`. It has no external side effects.
- `AgentArtifact` — Pushes an agent configuration to an OCI registry, equivalent to `docker agent share push`. It outputs the manifest `digest`, a `pinnedReference` (`registry/repo@sha256:...`) and the final `manifestAnnotations`. By default the artifact is retained in the registry when the resource is deleted (`deleteBehavior: retain`); with `deleteBehavior: delete` it is deleted unless another tag still points to the same manifest.
- `getAgent` — Reads a shared agent from a registry by tag or digest and returns its YAML, digest, `pinnedReference`, annotations, metadata, agent names and default agent, without writing anything to disk.
- `validateAgentConfig` — Validates arbitrary agent YAML (for example a file you keep next to your program) and returns `valid`, `errors`, `agentNames`, `defaultAgent` and `requiredEnvVars` instead of failing.
- `UserSettings` — Docker Agent's user configuration file (`~/.config/cagent/config.yaml`) with typed settings, aliases, global providers, the default model, the board, the credential helper and the sandbox allowlist. It renders the file exactly as docker-agent writes it (`yaml` output) and, when `path` is set, writes it and detects drift on refresh.
- `AgentsIgnore` — A `.agentsignore` file (`.gitignore` syntax) that hides paths from the agent's filesystem tools. It renders `content` and, when `directory` is set, writes `<directory>/.agentsignore`.
- `AgentRun` — Runs a prompt against an agent configuration (`yaml` or `reference`) during `pulumi up`, in the provider, and outputs the final answer (`output`), the answer as JSON (`structuredOutput`) when the agent declares structured output, `agentName`, `usage` and `toolCalls`. It never runs during previews, runs again when an input that affects the run changes, and rejects tool calls that need approval (including approvals from hooks) unless `toolApproval` is `allow`.
