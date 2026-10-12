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

Add `io.github.dirien:docker-agent` from Maven Central to your build. The Java package is `io.github.dirien.dockeragent`.

```groovy
// build.gradle
dependencies {
    implementation "io.github.dirien:docker-agent:0.1.0"
}
```

```xml
<!-- pom.xml -->
<dependency>
    <groupId>io.github.dirien</groupId>
    <artifactId>docker-agent</artifactId>
    <version>0.1.0</version>
</dependency>
```

The Java SDK is published starting with the first release that has Maven Central publishing enabled. If `io.github.dirien:docker-agent` is not on [Maven Central](https://central.sonatype.com/artifact/io.github.dirien/docker-agent) yet, install the plugin and generate the SDK locally from your project directory instead:

```bash
pulumi plugin install resource docker-agent v0.1.0 --server github://api.github.com/dirien/pulumi-docker-agent
pulumi package add docker-agent@0.1.0
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
pulumi plugin install resource docker-agent v0.1.0 --server github://api.github.com/dirien/pulumi-docker-agent
pulumi install
```

{{% /choosable %}}
{{< /chooser >}}

The SDKs download the `docker-agent` provider plugin automatically from the [GitHub releases](https://github.com/dirien/pulumi-docker-agent/releases) on first use. An HCL program names the package only by its `source`, so install the plugin once as shown, or commit the package descriptor described in [Installation & Configuration](https://www.pulumi.com/registry/packages/docker-agent/installation-configuration/#hcl) so that `pulumi install` alone fetches it.

## Example Usage

This program defines a two-agent configuration: a `root` agent that delegates web research to a `researcher` sub-agent, which uses the DuckDuckGo MCP server from the Docker MCP Catalog. It then pushes the configuration to a registry as an OCI artifact, and reads it back with `getAgent`.

The example pushes to a local registry. Start one with `docker run -d -p 5000:5000 registry:2`, or change the reference to a registry you are logged in to, such as `ghcr.io/<user>/research-assistant:v1` or `docker.io/<user>/research-assistant:v1`.

<!-- docsgen begin: research-assistant (generated from docs/yaml/index-examples.yaml by `make docs`: edit the YAML, not this block) -->
{{< chooser language "typescript,python,go,csharp,java,yaml,hcl" >}}
{{% choosable language typescript %}}

```typescript
import * as pulumi from "@pulumi/pulumi";
import * as docker_agent from "@ediri/pulumi-docker-agent";

// A typed docker-agent configuration, validated during `pulumi preview`.
const assistant = new docker_agent.AgentConfig("assistant", {
    models: {
        fast: {
            provider: "openai",
            model: "gpt-5-mini",
            maxTokens: 16000,
        },
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
            toolsets: [{
                type: "mcp",
                ref: "docker:duckduckgo",
            }],
        },
    ],
    metadata: {
        author: "Jane Doe",
        license: "Apache-2.0",
        version: "1.0.0",
        tags: [
            "research",
            "demo",
        ],
    },
});

// Share the configuration as an OCI artifact, like `docker agent share push`.
const artifact = new docker_agent.AgentArtifact("artifact", {
    reference: "localhost:5000/agents/research-assistant:v1",
    yaml: assistant.yaml,
});

// Read the shared agent back from the registry, like `docker agent share pull`.
const pulled = docker_agent.getAgentOutput({
    reference: artifact.pinnedReference,
});
export const digest = artifact.digest;
export const pinnedReference = artifact.pinnedReference;
export const requiredEnvVars = assistant.requiredEnvVars;
export const pulledAgents = pulled.agentNames;
```

{{% /choosable %}}
{{% choosable language python %}}

```python
import pulumi
import ediri_docker_agent as docker_agent

# A typed docker-agent configuration, validated during `pulumi preview`.
assistant = docker_agent.AgentConfig("assistant",
    models={
        "fast": {
            "provider": "openai",
            "model": "gpt-5-mini",
            "max_tokens": 16000,
        },
    },
    agents=[
        {
            "name": "root",
            "model": "fast",
            "description": "Answers questions and delegates web research",
            "instruction": "You are a helpful assistant. Delegate questions that need current information to the researcher agent, then summarise its findings.",
            "sub_agents": ["researcher"],
        },
        {
            "name": "researcher",
            "model": "fast",
            "description": "Searches the web with DuckDuckGo",
            "instruction": "Search the web and answer with short summaries that cite their sources.",
            "toolsets": [{
                "type": "mcp",
                "ref": "docker:duckduckgo",
            }],
        },
    ],
    metadata={
        "author": "Jane Doe",
        "license": "Apache-2.0",
        "version": "1.0.0",
        "tags": [
            "research",
            "demo",
        ],
    })

# Share the configuration as an OCI artifact, like `docker agent share push`.
artifact = docker_agent.AgentArtifact("artifact",
    reference="localhost:5000/agents/research-assistant:v1",
    yaml=assistant.yaml)

# Read the shared agent back from the registry, like `docker agent share pull`.
pulled = docker_agent.get_agent_output(reference=artifact.pinned_reference)
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
		assistant, err := dockeragent.NewAgentConfig(ctx, "assistant", &dockeragent.AgentConfigArgs{
			Models: dockeragent.ModelDefMap{
				"fast": &dockeragent.ModelDefArgs{
					Provider:  pulumi.String("openai"),
					Model:     pulumi.String("gpt-5-mini"),
					MaxTokens: pulumi.Int(16000),
				},
			},
			Agents: dockeragent.AgentArray{
				&dockeragent.AgentArgs{
					Name:        pulumi.String("root"),
					Model:       pulumi.String("fast"),
					Description: pulumi.String("Answers questions and delegates web research"),
					Instruction: pulumi.String("You are a helpful assistant. Delegate questions that need current information to the researcher agent, then summarise its findings."),
					SubAgents: pulumi.StringArray{
						pulumi.String("researcher"),
					},
				},
				&dockeragent.AgentArgs{
					Name:        pulumi.String("researcher"),
					Model:       pulumi.String("fast"),
					Description: pulumi.String("Searches the web with DuckDuckGo"),
					Instruction: pulumi.String("Search the web and answer with short summaries that cite their sources."),
					Toolsets: dockeragent.ToolsetArray{
						&dockeragent.ToolsetArgs{
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
				Tags: pulumi.StringArray{
					pulumi.String("research"),
					pulumi.String("demo"),
				},
			},
		})
		if err != nil {
			return err
		}

		// Share the configuration as an OCI artifact, like `docker agent share push`.
		artifact, err := dockeragent.NewAgentArtifact(ctx, "artifact", &dockeragent.AgentArtifactArgs{
			Reference: pulumi.String("localhost:5000/agents/research-assistant:v1"),
			Yaml:      assistant.Yaml,
		})
		if err != nil {
			return err
		}

		// Read the shared agent back from the registry, like `docker agent share pull`.
		pulled := dockeragent.GetAgentOutput(ctx, dockeragent.GetAgentOutputArgs{
			Reference: artifact.PinnedReference,
		}, nil)
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
using System.Linq;
using Pulumi;
using DockerAgent = ediri.DockerAgent;

return await Deployment.RunAsync(() =>
{
    // A typed docker-agent configuration, validated during `pulumi preview`.
    var assistant = new DockerAgent.AgentConfig("assistant", new()
    {
        Models =
        {
            { "fast", new DockerAgent.Inputs.ModelDefArgs
            {
                Provider = "openai",
                Model = "gpt-5-mini",
                MaxTokens = 16000,
            } },
        },
        Agents = new[]
        {
            new DockerAgent.Inputs.AgentArgs
            {
                Name = "root",
                Model = "fast",
                Description = "Answers questions and delegates web research",
                Instruction = "You are a helpful assistant. Delegate questions that need current information to the researcher agent, then summarise its findings.",
                SubAgents = new[]
                {
                    "researcher",
                },
            },
            new DockerAgent.Inputs.AgentArgs
            {
                Name = "researcher",
                Model = "fast",
                Description = "Searches the web with DuckDuckGo",
                Instruction = "Search the web and answer with short summaries that cite their sources.",
                Toolsets = new[]
                {
                    new DockerAgent.Inputs.ToolsetArgs
                    {
                        Type = "mcp",
                        Ref = "docker:duckduckgo",
                    },
                },
            },
        },
        Metadata = new DockerAgent.Inputs.MetadataArgs
        {
            Author = "Jane Doe",
            License = "Apache-2.0",
            Version = "1.0.0",
            Tags = new[]
            {
                "research",
                "demo",
            },
        },
    });

    // Share the configuration as an OCI artifact, like `docker agent share push`.
    var artifact = new DockerAgent.AgentArtifact("artifact", new()
    {
        Reference = "localhost:5000/agents/research-assistant:v1",
        Yaml = assistant.Yaml,
    });

    // Read the shared agent back from the registry, like `docker agent share pull`.
    var pulled = DockerAgent.GetAgent.Invoke(new()
    {
        Reference = artifact.PinnedReference,
    });

    return new Dictionary<string, object?>
    {
        ["digest"] = artifact.Digest,
        ["pinnedReference"] = artifact.PinnedReference,
        ["requiredEnvVars"] = assistant.RequiredEnvVars,
        ["pulledAgents"] = pulled.Apply(getAgentResult => getAgentResult.AgentNames),
    };
});
```

{{% /choosable %}}
{{% choosable language java %}}

```java
package generated_program;

import com.pulumi.Context;
import com.pulumi.Pulumi;
import com.pulumi.core.Output;
import io.github.dirien.dockeragent.AgentConfig;
import io.github.dirien.dockeragent.AgentConfigArgs;
import io.github.dirien.dockeragent.inputs.ModelDefArgs;
import io.github.dirien.dockeragent.inputs.AgentArgs;
import io.github.dirien.dockeragent.inputs.ToolsetArgs;
import io.github.dirien.dockeragent.inputs.MetadataArgs;
import io.github.dirien.dockeragent.AgentArtifact;
import io.github.dirien.dockeragent.AgentArtifactArgs;
import io.github.dirien.dockeragent.DockeragentFunctions;
import io.github.dirien.dockeragent.inputs.GetAgentArgs;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Map;
import java.io.File;
import java.nio.file.Files;
import java.nio.file.Paths;

public class App {
    public static void main(String[] args) {
        Pulumi.run(App::stack);
    }

    public static void stack(Context ctx) {
        // A typed docker-agent configuration, validated during `pulumi preview`.
        var assistant = new AgentConfig("assistant", AgentConfigArgs.builder()
            .models(Map.of("fast", ModelDefArgs.builder()
                .provider("openai")
                .model("gpt-5-mini")
                .maxTokens(16000)
                .build()))
            .agents(
                AgentArgs.builder()
                    .name("root")
                    .model("fast")
                    .description("Answers questions and delegates web research")
                    .instruction("You are a helpful assistant. Delegate questions that need current information to the researcher agent, then summarise its findings.")
                    .subAgents("researcher")
                    .build(),
                AgentArgs.builder()
                    .name("researcher")
                    .model("fast")
                    .description("Searches the web with DuckDuckGo")
                    .instruction("Search the web and answer with short summaries that cite their sources.")
                    .toolsets(ToolsetArgs.builder()
                        .type("mcp")
                        .ref("docker:duckduckgo")
                        .build())
                    .build())
            .metadata(MetadataArgs.builder()
                .author("Jane Doe")
                .license("Apache-2.0")
                .version("1.0.0")
                .tags(
                    "research",
                    "demo")
                .build())
            .build());

        // Share the configuration as an OCI artifact, like `docker agent share push`.
        var artifact = new AgentArtifact("artifact", AgentArtifactArgs.builder()
            .reference("localhost:5000/agents/research-assistant:v1")
            .yaml(assistant.yaml())
            .build());

        // Read the shared agent back from the registry, like `docker agent share pull`.
        final var pulled = DockeragentFunctions.getAgent(GetAgentArgs.builder()
            .reference(artifact.pinnedReference())
            .build());

        ctx.export("digest", artifact.digest());
        ctx.export("pinnedReference", artifact.pinnedReference());
        ctx.export("requiredEnvVars", assistant.requiredEnvVars());
        ctx.export("pulledAgents", pulled.applyValue(_pulled -> _pulled.agentNames()));
    }
}
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
      source = "pulumi/dirien/docker-agent"
    }
  }
}

# Read the shared agent back from the registry, like `docker agent share pull`.
data "docker-agent_agent" "pulled" {
  reference = docker-agent_agent_artifact.artifact.pinned_reference
}

# A typed docker-agent configuration, validated during `pulumi preview`.
resource "docker-agent_agent_config" "assistant" {
  models = {
    "fast" = {
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
<!-- docsgen end -->

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

<!-- docsgen begin: test-fixer (generated from docs/yaml/index-examples.yaml by `make docs`: edit the YAML, not this block) -->
{{< chooser language "typescript,python,go,csharp,java,yaml,hcl" >}}
{{% choosable language typescript %}}

```typescript
import * as pulumi from "@pulumi/pulumi";
import * as docker_agent from "@ediri/pulumi-docker-agent";

const fixer = new docker_agent.AgentConfig("fixer", {
    runtime: {
        sandbox: true,
        safety: docker_agent.SafetyMode.Restricted,
        networkAllowlist: [
            "api.github.com",
            "registry.npmjs.org:443",
        ],
    },
    agents: [{
        name: "root",
        model: "anthropic/claude-sonnet-4-5",
        instruction: "Run the test suite and fix the failing tests.",
        toolsets: [
            {
                type: "shell",
            },
            {
                type: "filesystem",
            },
        ],
        hooks: {
            sessionStart: [{
                type: docker_agent.HookType.Builtin,
                command: "add_environment_info",
            }],
        },
        simpleCommands: {
            test: "Run the tests and summarise the failures",
        },
    }],
    budget: {
        maxCost: 5,
        maxTime: "30m",
    },
});
export const agentYaml = fixer.yaml;
```

{{% /choosable %}}
{{% choosable language python %}}

```python
import pulumi
import ediri_docker_agent as docker_agent

fixer = docker_agent.AgentConfig("fixer",
    runtime={
        "sandbox": True,
        "safety": docker_agent.SafetyMode.RESTRICTED,
        "network_allowlist": [
            "api.github.com",
            "registry.npmjs.org:443",
        ],
    },
    agents=[{
        "name": "root",
        "model": "anthropic/claude-sonnet-4-5",
        "instruction": "Run the test suite and fix the failing tests.",
        "toolsets": [
            {
                "type": "shell",
            },
            {
                "type": "filesystem",
            },
        ],
        "hooks": {
            "session_start": [{
                "type": docker_agent.HookType.BUILTIN,
                "command": "add_environment_info",
            }],
        },
        "simple_commands": {
            "test": "Run the tests and summarise the failures",
        },
    }],
    budget={
        "max_cost": float(5),
        "max_time": "30m",
    })
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
		fixer, err := dockeragent.NewAgentConfig(ctx, "fixer", &dockeragent.AgentConfigArgs{
			Runtime: &dockeragent.RuntimeDefaultsArgs{
				Sandbox: pulumi.Bool(true),
				Safety:  dockeragent.SafetyModeRestricted,
				NetworkAllowlist: pulumi.StringArray{
					pulumi.String("api.github.com"),
					pulumi.String("registry.npmjs.org:443"),
				},
			},
			Agents: dockeragent.AgentArray{
				&dockeragent.AgentArgs{
					Name:        pulumi.String("root"),
					Model:       pulumi.String("anthropic/claude-sonnet-4-5"),
					Instruction: pulumi.String("Run the test suite and fix the failing tests."),
					Toolsets: dockeragent.ToolsetArray{
						&dockeragent.ToolsetArgs{
							Type: pulumi.String("shell"),
						},
						&dockeragent.ToolsetArgs{
							Type: pulumi.String("filesystem"),
						},
					},
					Hooks: &dockeragent.HooksArgs{
						SessionStart: dockeragent.HookDefinitionArray{
							&dockeragent.HookDefinitionArgs{
								Type:    dockeragent.HookTypeBuiltin,
								Command: pulumi.String("add_environment_info"),
							},
						},
					},
					SimpleCommands: pulumi.StringMap{
						"test": pulumi.String("Run the tests and summarise the failures"),
					},
				},
			},
			Budget: &dockeragent.BudgetArgs{
				MaxCost: pulumi.Float64(5),
				MaxTime: pulumi.String("30m"),
			},
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
using System.Linq;
using Pulumi;
using DockerAgent = ediri.DockerAgent;

return await Deployment.RunAsync(() =>
{
    var fixer = new DockerAgent.AgentConfig("fixer", new()
    {
        Runtime = new DockerAgent.Inputs.RuntimeDefaultsArgs
        {
            Sandbox = true,
            Safety = DockerAgent.SafetyMode.Restricted,
            NetworkAllowlist = new[]
            {
                "api.github.com",
                "registry.npmjs.org:443",
            },
        },
        Agents = new[]
        {
            new DockerAgent.Inputs.AgentArgs
            {
                Name = "root",
                Model = "anthropic/claude-sonnet-4-5",
                Instruction = "Run the test suite and fix the failing tests.",
                Toolsets = new[]
                {
                    new DockerAgent.Inputs.ToolsetArgs
                    {
                        Type = "shell",
                    },
                    new DockerAgent.Inputs.ToolsetArgs
                    {
                        Type = "filesystem",
                    },
                },
                Hooks = new DockerAgent.Inputs.HooksArgs
                {
                    SessionStart = new[]
                    {
                        new DockerAgent.Inputs.HookDefinitionArgs
                        {
                            Type = DockerAgent.HookType.Builtin,
                            Command = "add_environment_info",
                        },
                    },
                },
                SimpleCommands =
                {
                    { "test", "Run the tests and summarise the failures" },
                },
            },
        },
        Budget = new DockerAgent.Inputs.BudgetArgs
        {
            MaxCost = 5.0,
            MaxTime = "30m",
        },
    });

    return new Dictionary<string, object?>
    {
        ["agentYaml"] = fixer.Yaml,
    };
});
```

{{% /choosable %}}
{{% choosable language java %}}

```java
package generated_program;

import com.pulumi.Context;
import com.pulumi.Pulumi;
import com.pulumi.core.Output;
import io.github.dirien.dockeragent.AgentConfig;
import io.github.dirien.dockeragent.AgentConfigArgs;
import io.github.dirien.dockeragent.inputs.RuntimeDefaultsArgs;
import io.github.dirien.dockeragent.inputs.AgentArgs;
import io.github.dirien.dockeragent.inputs.ToolsetArgs;
import io.github.dirien.dockeragent.inputs.HooksArgs;
import io.github.dirien.dockeragent.inputs.HookDefinitionArgs;
import io.github.dirien.dockeragent.inputs.BudgetArgs;
import io.github.dirien.dockeragent.enums.HookType;
import io.github.dirien.dockeragent.enums.SafetyMode;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Map;
import java.io.File;
import java.nio.file.Files;
import java.nio.file.Paths;

public class App {
    public static void main(String[] args) {
        Pulumi.run(App::stack);
    }

    public static void stack(Context ctx) {
        var fixer = new AgentConfig("fixer", AgentConfigArgs.builder()
            .runtime(RuntimeDefaultsArgs.builder()
                .sandbox(true)
                .safety(SafetyMode.Restricted)
                .networkAllowlist(
                    "api.github.com",
                    "registry.npmjs.org:443")
                .build())
            .agents(AgentArgs.builder()
                .name("root")
                .model("anthropic/claude-sonnet-4-5")
                .instruction("Run the test suite and fix the failing tests.")
                .toolsets(
                    ToolsetArgs.builder()
                        .type("shell")
                        .build(),
                    ToolsetArgs.builder()
                        .type("filesystem")
                        .build())
                .hooks(HooksArgs.builder()
                    .sessionStart(HookDefinitionArgs.builder()
                        .type(HookType.Builtin)
                        .command("add_environment_info")
                        .build())
                    .build())
                .simpleCommands(Map.of("test", "Run the tests and summarise the failures"))
                .build())
            .budget(BudgetArgs.builder()
                .maxCost(5.0)
                .maxTime("30m")
                .build())
            .build());

        ctx.export("agentYaml", fixer.yaml());
    }
}
```

{{% /choosable %}}
{{% choosable language yaml %}}

```yaml
name: test-fixer
runtime: yaml

resources:
  fixer:
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
  agentYaml: ${fixer.yaml}
```

{{% /choosable %}}
{{% choosable language hcl %}}

```hcl
terraform {
  required_providers {
    docker-agent = {
      source = "pulumi/dirien/docker-agent"
    }
  }
}

resource "docker-agent_agent_config" "fixer" {
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
      session_start = [{
        type    = "builtin"
        command = "add_environment_info"
      }]
    }
    simple_commands = {
      "test" = "Run the tests and summarise the failures"
    }
  }
  budget = {
    max_cost = 5
    max_time = "30m"
  }
}

output "agentYaml" {
  value = docker-agent_agent_config.fixer.yaml
}
```

In HCL, objects such as `runtime`, `hooks` and `budget` are attributes, while `agents` and `toolsets` are repeated nested blocks.

{{% /choosable %}}
{{< /chooser >}}
<!-- docsgen end -->

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

<!-- docsgen begin: agent-local-config (generated from docs/yaml/index-examples.yaml by `make docs`: edit the YAML, not this block) -->
{{< chooser language "typescript,python,go,csharp,java,yaml,hcl" >}}
{{% choosable language typescript %}}

```typescript
import * as pulumi from "@pulumi/pulumi";
import * as docker_agent from "@ediri/pulumi-docker-agent";

// Docker Agent's user configuration, validated with docker-agent's own checks.
const settings = new docker_agent.UserSettings("settings", {
    path: "~/.config/cagent/config.yaml",
    settings: {
        theme: "auto",
        safety: docker_agent.SafetyMode.Balanced,
        layout: {
            sidebarPosition: docker_agent.SidebarPosition.Left,
        },
        permissions: {
            deny: ["shell:cmd=sudo*"],
            allow: ["read_*"],
        },
        keybindings: [{
            action: "commands",
            keys: [
                "f2",
                "ctrl+k",
            ],
        }],
    },
    aliases: {
        reviewer: {
            path: "docker.io/myorg/reviewer:latest",
            safety: docker_agent.SafetyMode.Restricted,
        },
    },
});

// Hide secrets from the agent's filesystem tools in a project.
const ignore = new docker_agent.AgentsIgnore("ignore", {
    directory: "~/src/my-service",
    patterns: [
        ".env",
        ".env.*",
        "*.pem",
        "*.key",
        "!public.key",
        "build/",
    ],
});
export const configPath = settings.filePath;
export const agentsIgnore = ignore.content;
```

{{% /choosable %}}
{{% choosable language python %}}

```python
import pulumi
import ediri_docker_agent as docker_agent

# Docker Agent's user configuration, validated with docker-agent's own checks.
settings = docker_agent.UserSettings("settings",
    path="~/.config/cagent/config.yaml",
    settings={
        "theme": "auto",
        "safety": docker_agent.SafetyMode.BALANCED,
        "layout": {
            "sidebar_position": docker_agent.SidebarPosition.LEFT,
        },
        "permissions": {
            "deny": ["shell:cmd=sudo*"],
            "allow": ["read_*"],
        },
        "keybindings": [{
            "action": "commands",
            "keys": [
                "f2",
                "ctrl+k",
            ],
        }],
    },
    aliases={
        "reviewer": {
            "path": "docker.io/myorg/reviewer:latest",
            "safety": docker_agent.SafetyMode.RESTRICTED,
        },
    })

# Hide secrets from the agent's filesystem tools in a project.
ignore = docker_agent.AgentsIgnore("ignore",
    directory="~/src/my-service",
    patterns=[
        ".env",
        ".env.*",
        "*.pem",
        "*.key",
        "!public.key",
        "build/",
    ])
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
		settings, err := dockeragent.NewUserSettings(ctx, "settings", &dockeragent.UserSettingsArgs{
			Path: pulumi.String("~/.config/cagent/config.yaml"),
			Settings: &dockeragent.UserGlobalSettingsArgs{
				Theme:  pulumi.String("auto"),
				Safety: dockeragent.SafetyModeBalanced,
				Layout: &dockeragent.UserLayoutSettingsArgs{
					SidebarPosition: dockeragent.SidebarPositionLeft,
				},
				Permissions: &dockeragent.UserPermissionsArgs{
					Deny: pulumi.StringArray{
						pulumi.String("shell:cmd=sudo*"),
					},
					Allow: pulumi.StringArray{
						pulumi.String("read_*"),
					},
				},
				Keybindings: dockeragent.UserKeybindingArray{
					&dockeragent.UserKeybindingArgs{
						Action: pulumi.String("commands"),
						Keys: pulumi.StringArray{
							pulumi.String("f2"),
							pulumi.String("ctrl+k"),
						},
					},
				},
			},
			Aliases: dockeragent.UserAliasMap{
				"reviewer": &dockeragent.UserAliasArgs{
					Path:   pulumi.String("docker.io/myorg/reviewer:latest"),
					Safety: dockeragent.SafetyModeRestricted,
				},
			},
		})
		if err != nil {
			return err
		}

		// Hide secrets from the agent's filesystem tools in a project.
		ignore, err := dockeragent.NewAgentsIgnore(ctx, "ignore", &dockeragent.AgentsIgnoreArgs{
			Directory: pulumi.String("~/src/my-service"),
			Patterns: pulumi.StringArray{
				pulumi.String(".env"),
				pulumi.String(".env.*"),
				pulumi.String("*.pem"),
				pulumi.String("*.key"),
				pulumi.String("!public.key"),
				pulumi.String("build/"),
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
using System.Linq;
using Pulumi;
using DockerAgent = ediri.DockerAgent;

return await Deployment.RunAsync(() =>
{
    // Docker Agent's user configuration, validated with docker-agent's own checks.
    var settings = new DockerAgent.UserSettings("settings", new()
    {
        Path = "~/.config/cagent/config.yaml",
        Settings = new DockerAgent.Inputs.UserGlobalSettingsArgs
        {
            Theme = "auto",
            Safety = DockerAgent.SafetyMode.Balanced,
            Layout = new DockerAgent.Inputs.UserLayoutSettingsArgs
            {
                SidebarPosition = DockerAgent.SidebarPosition.Left,
            },
            Permissions = new DockerAgent.Inputs.UserPermissionsArgs
            {
                Deny = new[]
                {
                    "shell:cmd=sudo*",
                },
                Allow = new[]
                {
                    "read_*",
                },
            },
            Keybindings = new[]
            {
                new DockerAgent.Inputs.UserKeybindingArgs
                {
                    Action = "commands",
                    Keys = new[]
                    {
                        "f2",
                        "ctrl+k",
                    },
                },
            },
        },
        Aliases =
        {
            { "reviewer", new DockerAgent.Inputs.UserAliasArgs
            {
                Path = "docker.io/myorg/reviewer:latest",
                Safety = DockerAgent.SafetyMode.Restricted,
            } },
        },
    });

    // Hide secrets from the agent's filesystem tools in a project.
    var ignore = new DockerAgent.AgentsIgnore("ignore", new()
    {
        Directory = "~/src/my-service",
        Patterns = new[]
        {
            ".env",
            ".env.*",
            "*.pem",
            "*.key",
            "!public.key",
            "build/",
        },
    });

    return new Dictionary<string, object?>
    {
        ["configPath"] = settings.FilePath,
        ["agentsIgnore"] = ignore.Content,
    };
});
```

{{% /choosable %}}
{{% choosable language java %}}

```java
package generated_program;

import com.pulumi.Context;
import com.pulumi.Pulumi;
import com.pulumi.core.Output;
import io.github.dirien.dockeragent.UserSettings;
import io.github.dirien.dockeragent.UserSettingsArgs;
import io.github.dirien.dockeragent.inputs.UserGlobalSettingsArgs;
import io.github.dirien.dockeragent.inputs.UserLayoutSettingsArgs;
import io.github.dirien.dockeragent.inputs.UserPermissionsArgs;
import io.github.dirien.dockeragent.inputs.UserKeybindingArgs;
import io.github.dirien.dockeragent.inputs.UserAliasArgs;
import io.github.dirien.dockeragent.AgentsIgnore;
import io.github.dirien.dockeragent.AgentsIgnoreArgs;
import io.github.dirien.dockeragent.enums.SafetyMode;
import io.github.dirien.dockeragent.enums.SidebarPosition;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Map;
import java.io.File;
import java.nio.file.Files;
import java.nio.file.Paths;

public class App {
    public static void main(String[] args) {
        Pulumi.run(App::stack);
    }

    public static void stack(Context ctx) {
        // Docker Agent's user configuration, validated with docker-agent's own checks.
        var settings = new UserSettings("settings", UserSettingsArgs.builder()
            .path("~/.config/cagent/config.yaml")
            .settings(UserGlobalSettingsArgs.builder()
                .theme("auto")
                .safety(SafetyMode.Balanced)
                .layout(UserLayoutSettingsArgs.builder()
                    .sidebarPosition(SidebarPosition.Left)
                    .build())
                .permissions(UserPermissionsArgs.builder()
                    .deny("shell:cmd=sudo*")
                    .allow("read_*")
                    .build())
                .keybindings(UserKeybindingArgs.builder()
                    .action("commands")
                    .keys(
                        "f2",
                        "ctrl+k")
                    .build())
                .build())
            .aliases(Map.of("reviewer", UserAliasArgs.builder()
                .path("docker.io/myorg/reviewer:latest")
                .safety(SafetyMode.Restricted)
                .build()))
            .build());

        // Hide secrets from the agent's filesystem tools in a project.
        var ignore = new AgentsIgnore("ignore", AgentsIgnoreArgs.builder()
            .directory("~/src/my-service")
            .patterns(
                ".env",
                ".env.*",
                "*.pem",
                "*.key",
                "!public.key",
                "build/")
            .build());

        ctx.export("configPath", settings.filePath());
        ctx.export("agentsIgnore", ignore.content());
    }
}
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
      source = "pulumi/dirien/docker-agent"
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
    "reviewer" = {
      path   = "docker.io/myorg/reviewer:latest"
      safety = "restricted"
    }
  }
}

# Hide secrets from the agent's filesystem tools in a project.
resource "docker-agent_agents_ignore" "ignore" {
  directory = "~/src/my-service"
  patterns  = [".env", ".env.*", "*.pem", "*.key", "!public.key", "build/"]
}

output "configPath" {
  value = docker-agent_user_settings.settings.file_path
}

output "agentsIgnore" {
  value = docker-agent_agents_ignore.ignore.content
}
```

In HCL, `UserSettings` and `AgentsIgnore` are the `docker-agent_user_settings` and `docker-agent_agents_ignore` resources. `settings`, `aliases` and the other object and map inputs are attributes, with snake_case keys at every level.

{{% /choosable %}}
{{< /chooser >}}
<!-- docsgen end -->

The provider writes the files on the machine that runs `pulumi up`: `~` is that user's home directory, and relative paths are relative to the Pulumi project directory. Writes are atomic, `config.yaml` gets mode `0600`, and previews never write. A file that already exists is never replaced silently: if its content differs, the preview fails until you copy what you want to keep into the inputs or set `overwrite: true`. The existing file is moved to `<file>.pulumi-backup` and moved back by `pulumi destroy`; a file the provider created is deleted instead, and a file adopted with `pulumi import` is left in place. Symbolic links, for example from a dotfile manager, are kept: the provider writes the file they point to. Leave `path` (or `directory`) unset to only render the content, for example to bake `yaml` into a container image or a Docker Sandboxes kit. `pulumi refresh` reports changes made outside of Pulumi, including those docker-agent makes itself (`docker agent alias add`, the `/settings` dialog), and the next `pulumi up` writes the declared content again. Set the [`retainOnDelete`](https://www.pulumi.com/docs/iac/concepts/resources/options/retainondelete/) resource option to keep the files on `pulumi destroy`.

## Run an agent during `pulumi up`

`AgentRun` runs a prompt against an agent configuration and exposes the answer as outputs, so other resources can use what the agent produced: a generated text, a review or a structured decision. The configuration is `yaml` (usually the `yaml` output of an `AgentConfig`) or an artifact `reference`. The agent runs inside the provider with the [docker-agent Go SDK](https://docs.docker.com/ai/docker-agent/guides/go-sdk/), the same runtime as `docker agent run --exec`, headless and unattended. The outputs are `output` (the final answer), `structuredOutput` (the answer as JSON, when the agent declares `structuredOutput`), `agentName`, `usage` (tokens and estimated cost) and `toolCalls` (the tools that ran).

<!-- docsgen begin: change-review (generated from docs/yaml/index-examples.yaml by `make docs`: edit the YAML, not this block) -->
{{< chooser language "typescript,python,go,csharp,java,yaml,hcl" >}}
{{% choosable language typescript %}}

```typescript
import * as pulumi from "@pulumi/pulumi";
import * as docker_agent from "@ediri/pulumi-docker-agent";

const config = new pulumi.Config();

// Set it with `pulumi config set --secret openaiApiKey <key>`.
const openaiApiKey = config.requireSecret("openaiApiKey");

// The agent: its answer must match a JSON schema.
const reviewer = new docker_agent.AgentConfig("reviewer", {agents: [{
    name: "root",
    model: "openai/gpt-5-mini",
    instruction: "Rate the risk of the infrastructure change and explain it in one sentence.",
    structuredOutput: {
        name: "change_review",
        schema: {
            type: "object",
            properties: {
                risk: {
                    type: "string",
                    "enum": [
                        "low",
                        "medium",
                        "high",
                    ],
                },
                summary: {
                    type: "string",
                },
            },
            required: [
                "risk",
                "summary",
            ],
            additionalProperties: false,
        },
    },
}]});

// Runs the agent during `pulumi up` (never during a preview) and keeps its answer in the state.
const review = new docker_agent.AgentRun("review", {
    yaml: reviewer.yaml,
    prompt: "Upgrade the production PostgreSQL cluster from version 15 to 17.",
    env: {
        OPENAI_API_KEY: openaiApiKey,
    },
});
export const verdict = review.structuredOutput;
export const usage = review.usage;
```

{{% /choosable %}}
{{% choosable language python %}}

```python
import pulumi
import ediri_docker_agent as docker_agent

config = pulumi.Config()

# Set it with `pulumi config set --secret openaiApiKey <key>`.
openai_api_key = config.require_secret("openaiApiKey")

# The agent: its answer must match a JSON schema.
reviewer = docker_agent.AgentConfig("reviewer", agents=[{
    "name": "root",
    "model": "openai/gpt-5-mini",
    "instruction": "Rate the risk of the infrastructure change and explain it in one sentence.",
    "structured_output": {
        "name": "change_review",
        "schema": {
            "type": "object",
            "properties": {
                "risk": {
                    "type": "string",
                    "enum": [
                        "low",
                        "medium",
                        "high",
                    ],
                },
                "summary": {
                    "type": "string",
                },
            },
            "required": [
                "risk",
                "summary",
            ],
            "additionalProperties": False,
        },
    },
}])

# Runs the agent during `pulumi up` (never during a preview) and keeps its answer in the state.
review = docker_agent.AgentRun("review",
    yaml=reviewer.yaml,
    prompt="Upgrade the production PostgreSQL cluster from version 15 to 17.",
    env={
        "OPENAI_API_KEY": openai_api_key,
    })
pulumi.export("verdict", review.structured_output)
pulumi.export("usage", review.usage)
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

		// Set it with `pulumi config set --secret openaiApiKey <key>`.
		openaiApiKey := cfg.RequireSecret("openaiApiKey")

		// The agent: its answer must match a JSON schema.
		reviewer, err := dockeragent.NewAgentConfig(ctx, "reviewer", &dockeragent.AgentConfigArgs{
			Agents: dockeragent.AgentArray{
				&dockeragent.AgentArgs{
					Name:        pulumi.String("root"),
					Model:       pulumi.String("openai/gpt-5-mini"),
					Instruction: pulumi.String("Rate the risk of the infrastructure change and explain it in one sentence."),
					StructuredOutput: dockeragent.StructuredOutputArgs{
						Name: pulumi.String("change_review"),
						Schema: pulumi.Map{
							"type": pulumi.Any("object"),
							"properties": pulumi.Any(map[string]interface{}{
								"risk": map[string]interface{}{
									"type": "string",
									"enum": []string{
										"low",
										"medium",
										"high",
									},
								},
								"summary": map[string]string{
									"type": "string",
								},
							}),
							"required": pulumi.Any([]interface{}{
								"risk",
								"summary",
							}),
							"additionalProperties": pulumi.Any(false),
						},
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
				"OPENAI_API_KEY": openaiApiKey,
			},
		})
		if err != nil {
			return err
		}
		ctx.Export("verdict", review.StructuredOutput)
		ctx.Export("usage", review.Usage)
		return nil
	})
}
```

{{% /choosable %}}
{{% choosable language csharp %}}

```csharp
using System.Collections.Generic;
using System.Linq;
using Pulumi;
using DockerAgent = ediri.DockerAgent;

return await Deployment.RunAsync(() =>
{
    var config = new Config();

    // Set it with `pulumi config set --secret openaiApiKey <key>`.
    var openaiApiKey = config.RequireSecret("openaiApiKey");

    // The agent: its answer must match a JSON schema.
    var reviewer = new DockerAgent.AgentConfig("reviewer", new()
    {
        Agents = new[]
        {
            new DockerAgent.Inputs.AgentArgs
            {
                Name = "root",
                Model = "openai/gpt-5-mini",
                Instruction = "Rate the risk of the infrastructure change and explain it in one sentence.",
                StructuredOutput = new DockerAgent.Inputs.StructuredOutputArgs
                {
                    Name = "change_review",
                    Schema =
                    {
                        { "type", "object" },
                        { "properties", new Dictionary<string, object?>
                        {
                            ["risk"] = new Dictionary<string, object?>
                            {
                                ["type"] = "string",
                                ["enum"] = new[]
                                {
                                    "low",
                                    "medium",
                                    "high",
                                },
                            },
                            ["summary"] = new Dictionary<string, object?>
                            {
                                ["type"] = "string",
                            },
                        } },
                        { "required", new[]
                        {
                            "risk",
                            "summary",
                        } },
                        { "additionalProperties", false },
                    },
                },
            },
        },
    });

    // Runs the agent during `pulumi up` (never during a preview) and keeps its answer in the state.
    var review = new DockerAgent.AgentRun("review", new()
    {
        Yaml = reviewer.Yaml,
        Prompt = "Upgrade the production PostgreSQL cluster from version 15 to 17.",
        Env =
        {
            { "OPENAI_API_KEY", openaiApiKey },
        },
    });

    return new Dictionary<string, object?>
    {
        ["verdict"] = review.StructuredOutput,
        ["usage"] = review.Usage,
    };
});
```

{{% /choosable %}}
{{% choosable language java %}}

```java
package generated_program;

import com.pulumi.Context;
import com.pulumi.Pulumi;
import com.pulumi.core.Output;
import io.github.dirien.dockeragent.AgentConfig;
import io.github.dirien.dockeragent.AgentConfigArgs;
import io.github.dirien.dockeragent.inputs.AgentArgs;
import io.github.dirien.dockeragent.inputs.StructuredOutputArgs;
import io.github.dirien.dockeragent.AgentRun;
import io.github.dirien.dockeragent.AgentRunArgs;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Map;
import java.io.File;
import java.nio.file.Files;
import java.nio.file.Paths;

public class App {
    public static void main(String[] args) {
        Pulumi.run(App::stack);
    }

    public static void stack(Context ctx) {
        final var config = ctx.config();

        // Set it with `pulumi config set --secret openaiApiKey <key>`.
        final var openaiApiKey = config.requireSecret("openaiApiKey");

        // The agent: its answer must match a JSON schema.
        var reviewer = new AgentConfig("reviewer", AgentConfigArgs.builder()
            .agents(AgentArgs.builder()
                .name("root")
                .model("openai/gpt-5-mini")
                .instruction("Rate the risk of the infrastructure change and explain it in one sentence.")
                .structuredOutput(StructuredOutputArgs.builder()
                    .name("change_review")
                    .schema(Map.ofEntries(
                        Map.entry("type", "object"),
                        Map.entry("properties", Map.ofEntries(
                            Map.entry("risk", Map.ofEntries(
                                Map.entry("type", "string"),
                                Map.entry("enum", Arrays.asList(
                                    "low",
                                    "medium",
                                    "high"))
                            )),
                            Map.entry("summary", Map.of("type", "string"))
                        )),
                        Map.entry("required", Arrays.asList(
                            "risk",
                            "summary")),
                        Map.entry("additionalProperties", false)
                    ))
                    .build())
                .build())
            .build());

        // Runs the agent during `pulumi up` (never during a preview) and keeps its answer in the state.
        var review = new AgentRun("review", AgentRunArgs.builder()
            .yaml(reviewer.yaml())
            .prompt("Upgrade the production PostgreSQL cluster from version 15 to 17.")
            .env(openaiApiKey.applyValue(_openaiApiKey -> Map.of("OPENAI_API_KEY", _openaiApiKey)))
            .build());

        ctx.export("verdict", review.structuredOutput());
        ctx.export("usage", review.usage());
    }
}
```

{{% /choosable %}}
{{% choosable language yaml %}}

```yaml
name: change-review
runtime: yaml

config:
  # Set it with `pulumi config set --secret openaiApiKey <key>`.
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
  # The answer as an object, for example {"risk": "high", "summary": "..."}.
  verdict: ${review.structuredOutput}
  usage: ${review.usage}
```

{{% /choosable %}}
{{% choosable language hcl %}}

```hcl
terraform {
  required_providers {
    docker-agent = {
      source = "pulumi/dirien/docker-agent"
    }
  }
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
        "type" = "object"
        "properties" = {
          "risk" = {
            "type" = "string"
            "enum" = ["low", "medium", "high"]
          }
          "summary" = {
            "type" = "string"
          }
        }
        "required"             = ["risk", "summary"]
        "additionalProperties" = false
      }
    }
  }
}

# Runs the agent during `pulumi up` (never during a preview) and keeps its answer in the state.
resource "docker-agent_agent_run" "review" {
  yaml   = docker-agent_agent_config.reviewer.yaml
  prompt = "Upgrade the production PostgreSQL cluster from version 15 to 17."
  env = {
    "OPENAI_API_KEY" = var.openaiApiKey
  }
}

# Set it with `pulumi config set --secret openaiApiKey <key>`.
variable "openaiApiKey" {
  type = string
}

output "verdict" {
  value = docker-agent_agent_run.review.structured_output
}

output "usage" {
  value = docker-agent_agent_run.review.usage
}
```

In HCL, `AgentRun` is the `docker-agent_agent_run` resource, and its properties are snake_case (`structured_output`, `tool_calls`, `inherit_environment`). `env` and an agent's `structured_output` are attributes; `agents` are repeated nested blocks. HCL variables are Pulumi config values.

{{% /choosable %}}
{{< /chooser >}}
<!-- docsgen end -->

- **When it runs.** Never during `pulumi preview`: the outputs are unknown until `pulumi up`. After that, `pulumi up` without changes and `pulumi refresh` keep the stored answer. A change of an input that affects the run (`yaml`, `reference`, `agent`, `prompt`, `env`, `inheritEnvironment`, `workingDir`, `toolApproval`, `allowHookApprovals` or `triggers`) runs the agent again as an update, and so does such an input that is unknown during the preview; `triggers` takes arbitrary values for asking again without other changes. A change of `timeoutSeconds`, `outputSecret` or `logging` alone only updates the state. Rotating the key in `env` also runs the agent again; add `env` to the [`ignoreChanges`](https://www.pulumi.com/docs/iac/concepts/resources/options/ignorechanges/) resource option to avoid that. `pulumi destroy` does nothing, and an `AgentRun` cannot be imported: a run has nothing to read back. A `reference` is pulled when the agent runs, so pin it by digest (or use `triggers`) to control when a moved tag is picked up.
- **Credentials and secrets.** The model's API key comes from `env`, which is stored as a secret, and, unless `inheritEnvironment` is `false`, from the environment of the Pulumi CLI. Errors, warnings and logs never contain the values of `env`, of secret inputs, of credential-named variables of the environment (names containing `API_KEY`, `TOKEN`, `SECRET`, `PASSWORD`, `CREDENTIAL`, `ACCESS_KEY`, ...) or of the variables the configuration names as `token_key`. A secret `env` does not make the answer secret by itself, but `output` and `structuredOutput` are stored as secrets when the answer contains one of those values (for example when an instruction passes a key with `${env.NAME}` and the model repeats it), when `prompt` or `yaml` is secret, and with `outputSecret: true`.
- **Isolation.** Each run gets private docker-agent data, config and cache directories that are deleted afterwards. `~/.cagent` is never touched, and the user settings, aliases, global providers and credential sources in `~/.config/cagent` do not apply, so a run does not depend on the docker-agent CLI setup of the machine. The agent works in a fresh temporary directory unless `workingDir` is set. The model catalog (aliases, context windows, pricing for `usage.cost`) is the one bundled with the provider.
- **Progress.** `logging` controls what `pulumi up` shows while the agent runs. `status` (the default) shows the run's progress as status messages that the next one replaces (an agent started, a task was transferred or the conversation handed off, each tool call and whether it was approved or rejected, a model that fell back to the next one) and ends with one summary line: duration, tool calls, input and output tokens and the estimated cost. `tools` also logs every tool call with its arguments, `full` also the agents' messages and the tools' results, and `none` shows nothing. Messages are redacted like errors; when the answer is secret, `tools` and `full` log no arguments, messages or results.
- **Limits.** `timeoutSeconds` (default 300) bounds the agent run itself, and the run also fails if the agent ends without a final answer (for example after `max_iterations`). The [`customTimeouts`](https://www.pulumi.com/docs/iac/concepts/resources/options/customtimeouts/) resource option (`create`, `update`) bounds the whole operation, including pulling a `reference` and waiting for another run: runs within one provider process execute one at a time. The shorter limit wins.

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

## Import and convert existing agents

`pulumi import` turns an agent file, a shared agent, Docker Agent's user configuration or a `.agentsignore` into a resource with typed inputs in the language of your program:

```bash
pulumi import docker-agent:index:AgentConfig assistant ./agent.yaml
pulumi import docker-agent:index:AgentArtifact published ghcr.io/my-org/agents/assistant:v1
```

`pulumi convert --from docker-agent` turns a whole directory of agent files (YAML and HCL) into a new project, with one `AgentConfig` per file and the comments of the files. It can also add an `AgentArtifact` and an `AgentRun` for each configuration. The converter is a separate plugin; install it once:

```bash
pulumi plugin install converter docker-agent v0.1.0 --server github://api.github.com/dirien/pulumi-docker-agent
pulumi convert --from docker-agent --language typescript --out ../my-agents
```

[Importing existing agents and files](https://www.pulumi.com/registry/packages/docker-agent/installation-configuration/#importing-existing-agents-and-files) and [Converting existing agent files](https://www.pulumi.com/registry/packages/docker-agent/installation-configuration/#converting-existing-agent-files) have the details.

## Configuration

- `registryAuth` (Optional) — A list of registry credentials used to push, pull and delete agent artifacts. Each entry has an `address` (Required, the registry host such as `ghcr.io`, `docker.io` or `localhost:5000`), a `username` and a `password` (Secret; a password or access token). Registries without a matching entry fall back to your Docker credentials: `$DOCKER_CONFIG/config.json` (default `~/.docker/config.json`), including `credsStore` and `credHelpers` set up by `docker login`.
- `forceHttps` (Optional) — Refuse plain-HTTP connections to registries other than `localhost` and loopback addresses. Without it, the provider (like the `docker agent` CLI) falls back to plain HTTP when HTTPS fails for `localhost`, `*.localhost`, loopback and private (RFC 1918) addresses. Defaults to `false`. May also be set with the `DOCKER_AGENT_FORCE_HTTPS` environment variable.
- `skipValidation` (Optional) — Skip validating agent configurations and user settings against the docker-agent library bundled with the provider. Use it when a configuration uses fields that are newer than the provider's pinned docker-agent version. Defaults to `false`. May also be set with the `DOCKER_AGENT_SKIP_VALIDATION` environment variable.

The provider needs no model API keys to render, validate and publish configurations. Model credentials such as `OPENAI_API_KEY` or `ANTHROPIC_API_KEY` are needed where the agent runs: by `docker agent run` and `docker agent serve`, or by `AgentRun`, which takes them from its `env` input or the environment of the Pulumi CLI. See the [Installation & Configuration](https://www.pulumi.com/registry/packages/docker-agent/installation-configuration/) page for details.

## Pulumi ESC

A [Pulumi ESC](https://www.pulumi.com/docs/esc/) environment can supply every credential in one place. Its `pulumiConfig` sets `docker-agent:registryAuth` (with `fn::secret` values) for pushing and pulling artifacts. Its `environmentVariables` export model API keys such as `ANTHROPIC_API_KEY` or `OPENAI_API_KEY`, which `pulumi up` passes to the provider and `AgentRun` uses with its default `inheritEnvironment: true`. `fn::open::aws-login` creates short-lived AWS credentials for `amazon-bedrock` models through OpenID Connect. The same environment runs a published agent:

```bash
pulumi env run -i <org>/<project>/<environment> -- docker agent run <pinnedReference>
```

[Using Pulumi ESC](https://www.pulumi.com/registry/packages/docker-agent/installation-configuration/#using-pulumi-esc) has an environment definition, the stack settings that import it, and when to pass a key through `env` instead.

## Policy as code

The repository contains a [policy pack](https://github.com/dirien/pulumi-docker-agent/tree/main/policy) for these resources, written for [Pulumi Policies](https://www.pulumi.com/docs/discovery-governance/concepts/policy-as-code/). By default it requires `AgentRun` to keep `toolApproval: deny` without `allowHookApprovals` (and can remediate both), rejects permission rules that approve every tool or every call of a tool such as `shell` (in `AgentConfig` and `UserSettings`), the `autonomous` safety mode (and `yolo` in `UserSettings`) and literal credentials in the `env` and `headers` of `AgentConfig`, and warns about `AgentRun` references that are not pinned by digest and about local tools without `runtime.sandbox`. Allowlists for model providers, MCP servers and artifact registries can be enabled in the policy configuration. Run it with any program:

```bash
pulumi preview --policy-pack <path-to-repository>/policy
```

## Resources and functions

- `AgentConfig` — A typed, validated Docker Agent configuration: every key of the configuration, including sandbox mode, hooks, commands, skills, MCP and RAG definitions and budgets, is a typed input. It renders deterministic YAML (`yaml` output) and reports `sha256`, `defaultAgent`, `agentNames` and `requiredEnvVars`. It has no external side effects.
- `AgentArtifact` — Pushes an agent configuration to an OCI registry, equivalent to `docker agent share push`. It outputs the manifest `digest`, a `pinnedReference` (`registry/repo@sha256:...`) and the final `manifestAnnotations`. By default the artifact is retained in the registry when the resource is deleted (`deleteBehavior: retain`); with `deleteBehavior: delete` it is deleted unless another tag still points to the same manifest.
- `getAgent` — Reads a shared agent from a registry by tag or digest and returns its YAML, digest, `pinnedReference`, annotations, metadata, agent names and default agent, without writing anything to disk.
- `validateAgentConfig` — Validates arbitrary agent YAML (for example a file you keep next to your program) and returns `valid`, `errors`, `agentNames`, `defaultAgent` and `requiredEnvVars` instead of failing.
- `UserSettings` — Docker Agent's user configuration file (`~/.config/cagent/config.yaml`) with typed settings, aliases, global providers, the default model, the board, the credential helper and the sandbox allowlist. It renders the file exactly as docker-agent writes it (`yaml` output) and, when `path` is set, writes it and detects drift on refresh.
- `AgentsIgnore` — A `.agentsignore` file (`.gitignore` syntax) that hides paths from the agent's filesystem tools. It renders `content` and, when `directory` is set, writes `<directory>/.agentsignore`.
- `AgentRun` — Runs a prompt against an agent configuration (`yaml` or `reference`) during `pulumi up`, in the provider, and outputs the final answer (`output`), the answer as JSON (`structuredOutput`) when the agent declares structured output, `agentName`, `usage` and `toolCalls`. It never runs during previews, runs again when an input that affects the run changes, and rejects tool calls that need approval (including approvals from hooks) unless `toolApproval` is `allow`.
