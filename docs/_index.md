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

`AgentRun` runs a prompt against an agent configuration and exposes the answer as outputs, so other resources can use what the agent produced: a generated text, a review or a structured decision. The configuration is `yaml` (usually the `yaml` output of an `AgentConfig`) or an artifact `reference`. The agent runs with the [docker-agent Go SDK](https://docs.docker.com/ai/docker-agent/guides/go-sdk/), the same runtime as `docker agent run --exec`, headless and unattended, in a child process of the provider on the machine that runs `pulumi up`, with any model provider that docker-agent supports: OpenAI, Anthropic, Google Gemini and Vertex AI, Amazon Bedrock, Docker Model Runner, Ollama and Ollama Cloud, Mistral, OpenRouter, GitHub Copilot and the other aliases, custom OpenAI-compatible endpoints, and a ChatGPT subscription (see [Models and credentials](#models-and-credentials)). The outputs are `output` (the final answer), `structuredOutput` (the answer as JSON, when the agent declares `structuredOutput`), `agentName`, `usage` (tokens and estimated cost) and `toolCalls` (the tools that ran). An agent with a coding `harness` runs the Claude Code or Codex CLI instead, with the opt-in `allowHarnessAgents` (see [Coding-harness agents (Claude Code and Codex)](#coding-harness-agents-claude-code-and-codex)).

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

- **When it runs.** Never during `pulumi preview`: the outputs are unknown until `pulumi up`. Creating the resource sends `prompt`. After that, `pulumi up` without changes and `pulumi refresh` keep the stored answer. A change of an input that affects the run (`yaml`, `reference`, `agent`, `prompt`, `env`, `inheritEnvironment`, `workingDir`, `toolApproval`, `allowHookApprovals` or `triggers`) runs the agent again as an update, and so does such an input that is unknown during the preview; `triggers` takes arbitrary values for asking again without other changes. Such a rerun sends `prompt` when `prompt` changed, and otherwise `updatePrompt`, which defaults to `prompt`. With `rerun: always`, every `pulumi up` that includes the resource runs the agent again. A change of `updatePrompt`, `deletePrompt`, `allowHarnessAgents`, `timeoutSeconds`, `outputSecret` or `logging` alone only updates the state, and so does a change of `rerun`, unless the new value is `always`. Rotating the key in `env` also runs the agent again; add `env` to the [`ignoreChanges`](https://www.pulumi.com/docs/iac/concepts/resources/options/ignorechanges/) resource option to avoid that, but then a `deletePrompt` keeps using the stored key. Deleting the resource runs `deletePrompt` when it is set, and does nothing otherwise. An `AgentRun` cannot be imported: a run has nothing to read back. A `reference` is pulled when the agent runs, so pin it by digest (or use `triggers`) to control when a moved tag is picked up. [Reruns and delete prompts](#reruns-and-delete-prompts) has the details.
- **Credentials and secrets.** The model's API key comes from `env`, which is stored as a secret, and, unless `inheritEnvironment` is `false`, from the environment of the Pulumi CLI; the run's child process has exactly that environment, so the model SDKs see nothing else (see [Models and credentials](#models-and-credentials)). Errors, warnings and logs never contain the values of `env` (except values that are configuration rather than credentials, such as a region, a boolean, a base URL or `CODEX_HOME`, which the program did not mark secret itself), of secret inputs, of credential-named variables of the environment (names containing `API_KEY`, `TOKEN`, `SECRET`, `PASSWORD`, `CREDENTIAL`, `ACCESS_KEY`, ...) or of the variables the configuration names as `token_key`. A secret `env` does not make the answer secret by itself, but `output` and `structuredOutput` are stored as secrets when the answer contains one of those values (for example when an instruction passes a key with `${env.NAME}` and the model repeats it), when `prompt`, `updatePrompt` or `yaml` is secret, and with `outputSecret: true`.
- **Isolation.** Each run runs in a child process of the provider and gets private docker-agent data, config and cache directories that are deleted afterwards. `~/.cagent` is never touched, and the user settings, aliases, global providers, the `.env` file and the other credential sources in `~/.config/cagent` do not apply, so a run does not depend on the docker-agent CLI setup of the machine. The one exception is the ChatGPT login of `docker agent setup`, which a configuration with a `chatgpt` model uses (see [Subscription models](#subscription-models)). The agent works in a fresh temporary directory unless `workingDir` is set. The model catalog (aliases, context windows, pricing for `usage.cost`) is the one bundled with the provider. The Claude Code and Codex CLIs of coding-harness agents are not isolated this way: they use their user's logins, settings and transcripts in `~/.claude` and `$CODEX_HOME` (default `~/.codex`).
- **Progress.** `logging` controls what `pulumi up` shows while the agent runs. `status` (the default) shows which models the run uses and where their credentials come from (`models: openai/gpt-5-mini (OPENAI_API_KEY from env)`), the run's progress as status messages that the next one replaces (an agent started, a task was transferred or the conversation handed off, each tool call and whether it was approved or rejected, a model that fell back to the next one) and ends with one summary line: duration, tool calls, input and output tokens and the estimated per-token cost, or why there is none, with the plans that paid for the rest ("no per-token cost: billed to the ChatGPT plan"). `tools` also logs every tool call with its arguments, `full` also the agents' messages and the tools' results, and `none` shows nothing. Messages are redacted like errors; when the answer is secret, `tools` and `full` log no arguments, messages or results.
- **Limits.** `timeoutSeconds` (default 300) bounds the agent run itself, from loading the configuration (starting MCP servers, pulling a Docker Model Runner model) to the answer, also the run of a `deletePrompt`, and the run also fails if the agent ends without a final answer (for example after `max_iterations`). The [`customTimeouts`](https://www.pulumi.com/docs/iac/concepts/resources/options/customtimeouts/) resource option (`create`, `update`, `delete`) bounds the whole operation, including pulling a `reference` and waiting for another run: runs within one provider process execute one at a time. The shorter limit wins. When `customTimeouts` runs out first, the run fails with "the operation's time limit (customTimeouts) ran out before the agent finished". When Pulumi stops the provider (Ctrl-C, SIGINT), every run stops within about 2 seconds. When a run ends, `AgentRun` stops the processes it left running, for example a server a `shell` tool started in the background, and warns. It finds them below the run's child process, in the child's session and by a marker in their environment, so only a process that also left that session (`setsid`) and whose environment does not show the marker (`env -i`, or, on macOS, a platform binary such as `/bin/sleep`) can outlive the run.

### Models and credentials

`AgentRun` runs every model provider of docker-agent v1.150.0 with the same code and SDKs as `docker agent run`. What a run needs depends on the model:

| Model | What the run needs |
|---|---|
| A provider with an API key: `openai/...`, `anthropic/...`, `google/...` (Gemini API), `mistral/...`, `openrouter/...`, `groq/...`, `xai/...`, `github-copilot/...` and the other aliases, or a custom provider with `tokenKey` | The key in `env` (`OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `GOOGLE_API_KEY`, `MISTRAL_API_KEY`, the `tokenKey` variable, ...), or in the environment of the Pulumi CLI with `inheritEnvironment: true`. |
| Amazon Bedrock (`amazon-bedrock/...`) | `AWS_BEARER_TOKEN_BEDROCK`, or AWS access keys and `AWS_REGION`, in `env`; otherwise the machine's AWS credentials (shared files, SSO, instance metadata). |
| Google Vertex AI (`google/...` with a project and a location) | `GOOGLE_APPLICATION_CREDENTIALS` (a service account key file) in `env`; otherwise the machine's gcloud login or metadata server. |
| Docker Model Runner (`dmr/...`) | Nothing: the Docker CLI's connection (without a `docker` CLI on the `PATH`, the Model Runner's default endpoints). A missing model is pulled within `timeoutSeconds`. |
| Ollama (`ollama/...`) | Nothing for a local Ollama, also for Ollama Cloud models (`ollama/gpt-oss:120b-cloud`) through a local Ollama that is signed in with `ollama signin`. The Ollama Cloud API directly is a custom provider with `tokenKey: OLLAMA_API_KEY`. |
| A ChatGPT plan (`chatgpt/...`) | The login of `docker agent setup` on the machine (see [Subscription models](#subscription-models)). |
| `model: auto` | One provider's credential; `DOCKER_AGENT_DEFAULT_MODEL` in `env` selects the model. |
| Claude Code and Codex agents (`harness`) | The CLI's own login (see [Coding-harness agents](#coding-harness-agents-claude-code-and-codex)). |

[Runtime credentials for agents](https://www.pulumi.com/registry/packages/docker-agent/installation-configuration/#runtime-credentials-for-agents) lists the variables of every provider and alias, and has sections on each kind of model.

- **Where credentials come from.** From `env` and, unless `inheritEnvironment` is `false`, from the environment of the Pulumi CLI, with `env` first. The run's child process has exactly that environment, so the model SDKs, which read their keys, base URLs and headers from the process environment, see nothing else: with `inheritEnvironment: false` they get `env` and a base environment without credentials (system variables, proxies and CA certificates, the Docker connection, the Claude Code and Codex configuration locations, Go and docker-agent settings), and a key, base URL or header exported in your shell does not reach them. Credentials that are not environment variables still apply: the logins of the `claude` and `codex` CLIs, the ChatGPT login, the AWS shared files and instance metadata, and the gcloud login. Pass the credential in `env` to use it instead. docker-agent's own credential sources (`~/.config/cagent/.env`, user-level `providers`, the credential helper, Docker Desktop secrets, a Docker models gateway) are never used.
- **Checked in the preview.** For a `yaml` configuration known during the preview, a preview that plans a run warns about variables that the run would miss, SDK variables of your shell that no longer reach a model with `inheritEnvironment: false` (since v0.2.0) or that send its requests, key or headers elsewhere with `inheritEnvironment: true`, Bedrock and Vertex AI models that would use the machine's credentials, a missing ChatGPT login and `structuredOutput` on Bedrock, and logs what `model: auto` resolves to. [Preview checks and the credential line](https://www.pulumi.com/registry/packages/docker-agent/installation-configuration/#preview-checks-and-the-credential-line) explains each warning.
- **Reported per run.** At the start of each run, `pulumi up` shows the models and where each credential comes from, for example `models: openai/gpt-5-mini (OPENAI_API_KEY from env); fallback anthropic/claude-haiku-4-5 (ANTHROPIC_API_KEY from the environment of the Pulumi CLI)`. It names variables, never values, and a rejected request repeats it in the error.
- **Cost.** `usage.cost` counts per-token billing, from the bundled model catalog: it is `0` for models without known pricing (custom providers without `cost`, `dmr`, `ollama`) and for usage billed to a subscription, and the summary then says why and names the plan.

### Subscription models

Three kinds of agents run on a subscription instead of an API key, all on the machine that runs `pulumi up`:

| | Claude Code agent | Codex agent | ChatGPT model |
|---|---|---|---|
| Configuration | `harness: {type: claude-code}` | `harness: {type: codex}` | `model: chatgpt/gpt-5.6` |
| Plan | a Claude subscription | a ChatGPT plan that includes Codex | a ChatGPT plan (Plus, Pro, Business) |
| What runs | the `claude` CLI, with its own tools | the `codex` CLI, with its own tools | docker-agent's runtime, with the configuration's toolsets |
| Opt-in | `allowHarnessAgents: true` | `allowHarnessAgents: true` | none |
| Tool approval | none: Claude Code runs every tool; `toolApproval` does not apply | Codex's sandbox: `deny` runs commands in `workspace-write`, `allow` turns it off | `toolApproval`, `permissions` and hooks, as for every agent |
| Login | `claude auth login`, or `CLAUDE_CODE_OAUTH_TOKEN` from `claude setup-token` in `env` | `codex login`, preferably into a `CODEX_HOME` of its own | `docker agent setup` (ChatGPT) |
| In CI | `CLAUDE_CODE_OAUTH_TOKEN` in `env` | a `codex login --device-auth` on the runner, or `CODEX_API_KEY` (bills API usage) | not supported: the login needs a browser |
| Systems | Linux, macOS | Linux, macOS | every system the provider runs on |

`usage.cost` is `0` for such runs, and the summary names the plan, for Claude Code and Codex with what the usage would cost at API prices ("no per-token cost: billed to the Claude subscription (API price about $0.0421)"). An API key overrides the subscription: Claude Code uses an `ANTHROPIC_API_KEY` or `ANTHROPIC_AUTH_TOKEN` of its environment, and Codex a `CODEX_API_KEY`, and both then bill API usage; the preview warns, and the line after the run says which credential the CLI used.

A `chatgpt` model keeps everything `AgentRun` does for docker-agent agents. It uses the login that `docker agent setup` stores in `~/.config/cagent/chatgpt-auth.json`, for configurations that name a `chatgpt` model (or select one with `DOCKER_AGENT_DEFAULT_MODEL` for `model: auto`) and whose environment does not set `CHATGPT_OAUTH_TOKEN`: when the run asks for the token, it copies the login into the run's private config directory under a lock file, lets docker-agent refresh the copy when it is about to expire, and writes a refreshed login back to the file at once, when the file did not change meanwhile; a refreshed login that cannot be written back is kept next to it (`chatgpt-auth.json.agentrun-<time>`) with a warning. That is the only file of `~/.config/cagent` that `AgentRun` reads or writes. `model: auto` alone never uses the login. The "A ChatGPT subscription model" example of the [`AgentRun` API docs](https://www.pulumi.com/registry/packages/docker-agent/api-docs/agentrun/) shows it, and [ChatGPT subscription](https://www.pulumi.com/registry/packages/docker-agent/installation-configuration/#chatgpt-subscription) has the details.

### Where tools run

**Tools run on the machine that runs `pulumi up`**: your workstation, the CI runner or the Pulumi Deployments worker, with the permissions of that process.

- With the default `toolApproval: deny`, a tool call only runs when docker-agent's own rules approve it without asking for confirmation:
  - tools annotated read-only, such as `think`, `todo`, `memory` reads, the filesystem toolset's read and search tools, `api` tools, and MCP tools whose server declares them read-only;
  - calls that an `allow` rule of the configuration's `permissions` matches (an `allow: ["*"]` rule therefore approves everything);
  - calls that the configuration's own `safety` mode (`strict`, `balanced` or `restricted`) approves (`autonomous` is ignored);
  - delegation to sub-agents (`transfer_task`, `handoff`).

  Approvals from the configuration's hooks are ignored: a `pre_tool_use` hook that answers `permission_decision: allow`, or a `permission_request` hook, cannot approve a call that the rules above would not run. Set `allowHookApprovals: true` to accept them as docker-agent does. Hook denials always apply. Every other call (shell commands, `script` tools, file writes, `fetch`, other MCP tools) is rejected. The model is told that the tool needs a confirmation the run cannot give, and the provider logs a warning that names the tool.
- `toolApproval: allow` runs every tool call, like `docker agent run --yolo`. `deny` rules in the configuration's `permissions` still apply.
- In both modes, these run without any approval because they are part of the configuration: command hooks (at session start, around tool calls, ...), MCP and LSP servers started with `command`, `docker:` MCP servers (through the Docker MCP Gateway), and filesystem `post_edit` commands after an approved write. Read-only file tools can read any file the provider can read unless the toolset sets `allow_list`.
- A coding-harness agent (`harness`) runs only with `allowHarnessAgents: true`. It starts an external coding agent CLI, Claude Code or Codex, which runs its own tools without any of the rules above; under `toolApproval: deny`, Codex runs its commands in its `workspace-write` sandbox. See [Coding-harness agents (Claude Code and Codex)](#coding-harness-agents-claude-code-and-codex).

**The environment these processes see.** With the default `inheritEnvironment: true`, every process the configuration starts inherits the environment of the Pulumi CLI, for example `PULUMI_ACCESS_TOKEN` or cloud credentials in CI, but not `env`: a variable that only `env` sets is unset for `shell`, `script` and `background_jobs` tools, and empty for LSP servers, a `shell` toolset with `sudo_askpass` and a `background_jobs` toolset with `recall`. With `inheritEnvironment: false`, the processes AgentRun starts get only the system variables (`PATH`, `HOME`, `USER`, `LOGNAME`, `SHELL`, `LANG`, `LANGUAGE`, `LC_ALL`, `LC_CTYPE`, `LC_MESSAGES`, `TERM`, `TMPDIR`, `TEMP`, `TMP`, `TZ`, and on Windows `SYSTEMROOT`, `SYSTEMDRIVE`, `COMSPEC`, `PATHEXT`, `WINDIR`, `USERPROFILE`, `APPDATA`, `LOCALAPPDATA`, `PROGRAMDATA` and `PROGRAMFILES`) plus what the configuration passes to them explicitly:

| Process | Environment with `inheritEnvironment: false` |
|---|---|
| Command hooks | system variables and `env` |
| `shell`, `script` and `background_jobs` tools | system variables and the toolset's own `env` (other variables are set to an empty value) |
| MCP servers started with `command` | system variables and the toolset's own `env` |
| LSP servers started with `command` | system variables and the toolset's own `env` (other variables are set to an empty value) |
| `docker:` MCP servers (Docker MCP Gateway), filesystem `post_edit` commands, the `open_url` browser launcher, docker-agent's on-demand tool installation and helper commands | the environment of the run: its [base environment](https://www.pulumi.com/registry/packages/docker-agent/installation-configuration/#agentrun-credentials) (proxies, the Docker connection, Go settings, ... but no credentials) and `env`; with `inheritEnvironment: true`, the environment of the Pulumi CLI and `env`. docker-agent does not let AgentRun change it |
| The Claude Code CLI of a coding-harness agent | system variables, `CLAUDE_CONFIG_DIR`, `CLAUDE_SECURESTORAGE_CONFIG_DIR`, `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_STATE_HOME`, `XDG_CACHE_HOME`, `IS_SANDBOX` and `env` |
| The Codex CLI of a coding-harness agent | system variables, `CODEX_HOME`, `CODEX_CA_CERTIFICATE`, the proxy variables, `SSL_CERT_FILE`, `SSL_CERT_DIR`, `DBUS_SESSION_BUS_ADDRESS`, `XDG_RUNTIME_DIR` and `env`, plus what Codex loads itself from `$CODEX_HOME/.env` |

A toolset's `env` can pass a value of `env` explicitly, for example `env: {GITHUB_TOKEN: "${env.GITHUB_TOKEN}"}` on an MCP toolset. With `inheritEnvironment: false`, docker-agent's on-demand tool installation gets no `GITHUB_TOKEN` of the Pulumi CLI's environment for GitHub's API: pass it in `env`.

Review a configuration before you run it, especially one pulled by `reference`. For agents with tools, prefer `inheritEnvironment: false` with credentials in `env`, and a dedicated CI identity without access to production credentials.

### Reruns and delete prompts

`AgentRun` sends one user message per step of its lifecycle:

| Step | Message |
|---|---|
| Create | `prompt` |
| Update that runs the agent again | `prompt` when `prompt` changed, otherwise `updatePrompt` (default: `prompt`) |
| Update of inputs that do not affect the run, `pulumi up` without changes, `pulumi refresh` | none: nothing runs |
| Delete: `pulumi destroy`, removing the resource from the program, the old resource of a replacement | `deletePrompt`, when it is set |

Each run is a new conversation: the agent does not see the messages or answers of earlier runs, so an `updatePrompt` or a `deletePrompt` usually refers to what earlier runs left in a persistent `workingDir`, for example "NOTES.md exists. Bring it up to date." An error hook (the `onError` [resource hook](https://www.pulumi.com/docs/iac/concepts/resources/options/hooks/)) that retries a create, update or delete runs the whole agent again each time.

**Running again on every `pulumi up`.** `rerun: always` runs the agent again on every `pulumi up` that includes the resource, with `updatePrompt` (or `prompt`). The preview shows the resource as an update (`[diff: ~rerun]`, and `rerun: "always" => "always"` with `--diff`) with unknown outputs, so a stack with such an `AgentRun` never previews without changes. `pulumi refresh`, `pulumi refresh --run-program` and the refresh of `pulumi up --refresh` report no drift for it, so `pulumi refresh --expect-no-changes` and drift detection keep working. `--target` and `--exclude` leave the resource out like any other: `pulumi up --exclude <urn>` skips one rerun, for example while a login that the run needs is broken. With `PULUMI_ENABLE_LEGACY_DIFF`, `always` has no effect.

The resources that use the outputs of such an `AgentRun` change too. Every preview shows its outputs as unknown, so every consumer shows an update, or a replacement where the consuming property forces one. After the update, `ranAt` always changes, and `usage` and `output` usually do, so the consumers update, and `AgentRun`s with `rerun: onChange` that use its `output` run again. Prefer stable `structuredOutput` fields, `ignoreChanges` on the consumers, or explicit chaining with `triggers`. Runs of one provider process execute one at a time, so several `always` runs add up in time and cost.

| Goal | Configuration |
|---|---|
| Run again in place on every `pulumi up` | `rerun: always` |
| Start fresh on every `pulumi up`: the delete prompt, then the create prompt | `rerun: always` and the [`replaceOnChanges: ["rerun"]`](https://www.pulumi.com/docs/iac/concepts/resources/options/replaceonchanges/) resource option. Pulumi consults the provider's diff for it, so the old delete prompt runs before the new create when a `deletePrompt` is set. |
| Run again when files change | `triggers: [<hash of the files>]` |
| Run again once a day | `triggers: [<today's date>]`. With an update plan (`pulumi up --plan`), the date can differ between the preview and the update around midnight. |
| Run again whenever another run ran | `triggers: [<other run>.ranAt]` |
| Replace instead of update when `triggers` change, like the `triggers` of a `command.local.Command` | `replaceOnChanges: ["triggers"]` |

**Delete prompts.** `deletePrompt` runs when the resource is deleted: by `pulumi destroy`, by removing it from the program, or as the old resource of a replacement. It runs with the configuration and the inputs stored in the state, including `env`, and with the environment of the deleting `pulumi` command (`inheritEnvironment`). A `reference` is pulled again, so pin it by digest. The answer is not kept, but the run still needs a final answer, like every run, so say what to answer, for example "then answer: done". `timeoutSeconds` and `customTimeouts` (`delete`) bound the run, and the preview of `pulumi destroy` runs nothing. Write the prompt so that it can run twice: a delete that was cancelled or timed out runs again from the start. With `toolApproval: deny`, an agent without a coding harness can only run read-only tools and the calls that `permissions` allow during the cleanup, and a Codex agent runs its commands in its sandbox, without network and with writes only in `workingDir` and the temporary directories, which is rarely enough; the preview warns about both. With `inheritEnvironment: false`, the delete prompt gets nothing of the deleting command's environment, also not the variables the model SDKs read (since v0.2.0): keep its credentials in `env`.

The delete prompt is skipped with a warning, and the delete succeeds, when:

- `DOCKER_AGENT_SKIP_DELETE_PROMPTS` is set to `true` in the environment of the `pulumi` command. It applies to every `AgentRun` that the command deletes. It is an environment variable and not a provider setting, because `pulumi destroy` reads the provider settings from the state;
- `workingDir` is set and no longer exists, or is no longer a directory. A `workingDir` that cannot be read is not skipped: the delete fails and says why;
- another `AgentRun` of the same provider ran in that `workingDir` during the same `pulumi` command, also while the delete prompt waited for it to finish: a replacement, or a rename without `aliases`, creates the new run first, and the old delete prompt would undo its work. The warning does not show a secret `workingDir`.

Nothing runs with the [`retainOnDelete`](https://www.pulumi.com/docs/iac/concepts/resources/options/retainondelete/) resource option, and nothing runs for an `AgentRun` with the [`deletedWith`](https://www.pulumi.com/docs/iac/concepts/resources/options/deletedwith/) resource option when its target is deleted in the same operation. Removing only the `AgentRun` still runs its delete prompt.

**Replacements.** `AgentRun` never asks for a replacement itself: a changed input runs the agent again in place. Replacements come from resource options and commands: `replaceOnChanges`, `replacementTrigger`, `replaceWith`, `deletedWith` (the `AgentRun` is replaced, and runs `prompt` again, whenever its `deletedWith` target is replaced), and `pulumi up --replace`. A change of the provider replaces no resource: a changed setting (`registryAuth`, for example a rotated password, `forceHttps` or `skipValidation`, also only through `DOCKER_AGENT_FORCE_HTTPS` or `DOCKER_AGENT_SKIP_VALIDATION` in the environment of one command), a new provider version (also when the program pins it, as the SDKs do) and a move to another provider instance update the provider in place, and its resources keep running as they are. Before v0.1.2, each of them replaced every resource of the provider. A rename without [`aliases`](https://www.pulumi.com/docs/iac/concepts/resources/options/aliases/) also creates a new resource and deletes the old one. By default, Pulumi creates the new resource first, which runs `prompt`, and deletes the old one afterwards, which runs the old `deletePrompt`. When a `deletePrompt` is stored, the provider asks Pulumi to delete first, but Pulumi honours that only for replacements that come from the provider's diff, that is `replaceOnChanges`. So:

- set the [`deleteBeforeReplace`](https://www.pulumi.com/docs/iac/concepts/resources/options/deletebeforereplace/) resource option on an `AgentRun` whose delete prompt undoes what its prompt creates. A delete-before-replace also deletes first the resources that depend on it and are replaced with it;
- use `aliases` when you rename an `AgentRun`;
- give each `AgentRun` its own `workingDir`. The skip for a `workingDir` that another run used applies within one provider: `AgentRun`s of different provider instances (explicit providers) do not see each other's runs.

**When a delete prompt fails**, the delete fails and the resource stays in the state. For a resource that was removed, renamed or replaced, that fails every later `pulumi up` as well, not only `pulumi destroy`: Pulumi tries the delete again each time, and the program can no longer change the stored inputs. To recover:

1. Fix the cause and run the command again. Typical causes are credentials: a revoked or rotated key or token in the stored `env`, a variable that the environment of the deleting command lacks (with `inheritEnvironment: false` since v0.2.0 also the base URLs, AWS and Google credentials and headers that the model SDKs used to read from it; the error names them), or a login on the machine that deletes that expired or is missing (the `claude` or `codex` login, the ChatGPT login of `docker agent setup`). Others are a CLI (`claude`, `codex`) missing on that machine and a registry of `reference` that cannot be reached. A credential changed in `env` reaches the state only with the next `pulumi up`, which runs the agent again; until then, the delete uses the stored one.
2. Delete without the prompt for one command: `DOCKER_AGENT_SKIP_DELETE_PROMPTS=true pulumi destroy`, or the same with `pulumi up`. Each skipped prompt is logged as a warning, and whatever it would have cleaned up stays.
3. Plan ahead: set `retainOnDelete: true` on an `AgentRun` before you remove it (Pulumi then does not call the provider's delete at all), use `aliases` when you rename one, and `deleteBeforeReplace: true` when its delete prompt undoes what its prompt creates.
4. As a last resort, run `pulumi state delete <urn>`. When the resource also has a pending-delete copy with the same URN, select it in the interactive picker: `AgentRun`s created by v0.1.2 or later have unique IDs (`<name>-<8 hex digits>`) that tell the two apart.

Stacks created with v0.1.1 or v0.1.2 upgrade without changes and without running an agent. `AgentRun`s of v0.1.1 keep their IDs (the resource name), and their `rerun` and `allowHarnessAgents` outputs stay undefined until the next update of the resource. v0.2.0 changes how the next runs behave, mostly for `inheritEnvironment: false`: its [CHANGELOG](https://github.com/dirien/pulumi-docker-agent/blob/main/CHANGELOG.md) opens with upgrade notes, including the stored delete prompts to check before a `pulumi destroy`.

### One agent, several runs

An `AgentConfig` authors an agent once, and any number of `AgentRun`s can run it, each with its own prompt and lifecycle: they take the `yaml` output of the `AgentConfig`, or the `reference` of an `AgentArtifact` that pushed it. An `AgentRun` runs at most once per `pulumi up`, so a program that runs one agent several times declares one `AgentRun` per run:

- **Independent runs:** several `AgentRun`s with the same `yaml` and different prompts.
- **A pipeline:** a run whose `prompt` interpolates the `output` of another run, with `triggers: [<other run>.ranAt]`, so that it runs again whenever the other run ran, also when its answer did not change.

Runs of one provider process execute one at a time, and a run that uses the output of another run starts after it, so their durations add up. Give each run that works on files its own `workingDir`. The "One agent, several runs" example of the [`AgentRun` API docs](https://www.pulumi.com/registry/packages/docker-agent/api-docs/agentrun/) chains two runs, and so does the coding-harness example below.

### Coding-harness agents (Claude Code and Codex)

An agent with a `harness` (`harness: {type: claude-code, model, effort}` or `harness: {type: codex, model}` in `AgentConfig`) has no docker-agent model: docker-agent hands its prompt to an external coding agent CLI. `AgentRun` runs such agents with `allowHarnessAgents: true`, for the types `claude-code` and `codex`, on Linux and macOS. Without the opt-in, the preview fails for a `yaml` configuration, also with the provider's `skipValidation` as long as the provider's docker-agent version can load it, and the run fails for a `reference`. Agents of the types `pi` and `opencode` fail as not supported by `AgentRun` yet (run them with `docker agent run`, or use a `chatgpt/<model>` model for a ChatGPT plan), and so does every harness on other systems than Linux and macOS, such as Windows. With a Claude subscription or a ChatGPT plan, such an agent runs during `pulumi up` without an API key. This example runs a Claude Code agent and a Codex agent of one configuration:

<!-- docsgen begin: coding-harness (generated from docs/yaml/index-examples.yaml by `make docs`: edit the YAML, not this block) -->
{{< chooser language "typescript,python,go,csharp,java,yaml,hcl" >}}
{{% choosable language typescript %}}

```typescript
import * as pulumi from "@pulumi/pulumi";
import * as docker_agent from "@ediri/pulumi-docker-agent";

const config = new pulumi.Config();

// A long-lived token from `claude setup-token`, which needs a Claude subscription. Set it with
// `pulumi config set --secret claudeCodeOauthToken <token>`.
const claudeCodeOauthToken = config.requireSecret("claudeCodeOauthToken");

// A Codex home of AgentRun's own, logged in once with a ChatGPT plan:
// `mkdir ~/.codex-agentrun && CODEX_HOME=~/.codex-agentrun codex login`. Set it with
// `pulumi config set codexHome $HOME/.codex-agentrun`.
const codexHome = config.require("codexHome");

// The agents, authored once: Claude Code and Codex as coding harnesses. They need no model and
// no API key.
const coders = new docker_agent.AgentConfig("coders", {agents: [
    {
        name: "root",
        description: "Writes project notes with Claude Code",
        harness: {
            type: docker_agent.HarnessType.ClaudeCode,
            model: "sonnet",
            effort: docker_agent.HarnessEffort.Low,
        },
    },
    {
        name: "reviewer",
        description: "Reviews project notes with Codex",
        harness: {
            type: docker_agent.HarnessType.Codex,
        },
    },
]});

// Run 1: Claude Code maintains NOTES.md in the existing directory ./workspace. It runs its own
// tools (shell commands, file edits) there without approval.
const notes = new docker_agent.AgentRun("notes", {
    yaml: coders.yaml,
    allowHarnessAgents: true,
    workingDir: "./workspace",
    prompt: "Write NOTES.md with three bullet points about this workspace, then summarise the file in one sentence.",
    updatePrompt: "NOTES.md exists. Bring it up to date and keep its format, then summarise the file in one sentence.",
    deletePrompt: "Delete NOTES.md if it exists, then answer: done.",
    inheritEnvironment: false,
    env: {
        CLAUDE_CODE_OAUTH_TOKEN: claudeCodeOauthToken,
    },
    timeoutSeconds: 900,
}, {
    deleteBeforeReplace: true,
});

// Run 2: the Codex agent of the same configuration, in a temporary directory. With
// toolApproval: deny (the default), Codex runs its commands in its workspace-write sandbox.
// It runs again whenever run 1 ran.
const review = new docker_agent.AgentRun("review", {
    yaml: coders.yaml,
    agent: "reviewer",
    allowHarnessAgents: true,
    prompt: pulumi.interpolate`Review this summary of NOTES.md in two sentences: ${notes.output}`,
    triggers: [notes.ranAt],
    inheritEnvironment: false,
    env: {
        CODEX_HOME: codexHome,
    },
    timeoutSeconds: 600,
});
export const summary = notes.output;
export const reviewText = review.output;
export const usage = notes.usage;
```

{{% /choosable %}}
{{% choosable language python %}}

```python
import pulumi
import ediri_docker_agent as docker_agent

config = pulumi.Config()

# A long-lived token from `claude setup-token`, which needs a Claude subscription. Set it with
# `pulumi config set --secret claudeCodeOauthToken <token>`.
claude_code_oauth_token = config.require_secret("claudeCodeOauthToken")

# A Codex home of AgentRun's own, logged in once with a ChatGPT plan:
# `mkdir ~/.codex-agentrun && CODEX_HOME=~/.codex-agentrun codex login`. Set it with
# `pulumi config set codexHome $HOME/.codex-agentrun`.
codex_home = config.require("codexHome")

# The agents, authored once: Claude Code and Codex as coding harnesses. They need no model and
# no API key.
coders = docker_agent.AgentConfig("coders", agents=[
    {
        "name": "root",
        "description": "Writes project notes with Claude Code",
        "harness": {
            "type": docker_agent.HarnessType.CLAUDE_CODE,
            "model": "sonnet",
            "effort": docker_agent.HarnessEffort.LOW,
        },
    },
    {
        "name": "reviewer",
        "description": "Reviews project notes with Codex",
        "harness": {
            "type": docker_agent.HarnessType.CODEX,
        },
    },
])

# Run 1: Claude Code maintains NOTES.md in the existing directory ./workspace. It runs its own
# tools (shell commands, file edits) there without approval.
notes = docker_agent.AgentRun("notes",
    yaml=coders.yaml,
    allow_harness_agents=True,
    working_dir="./workspace",
    prompt="Write NOTES.md with three bullet points about this workspace, then summarise the file in one sentence.",
    update_prompt="NOTES.md exists. Bring it up to date and keep its format, then summarise the file in one sentence.",
    delete_prompt="Delete NOTES.md if it exists, then answer: done.",
    inherit_environment=False,
    env={
        "CLAUDE_CODE_OAUTH_TOKEN": claude_code_oauth_token,
    },
    timeout_seconds=900,
    opts = pulumi.ResourceOptions(delete_before_replace=True))

# Run 2: the Codex agent of the same configuration, in a temporary directory. With
# toolApproval: deny (the default), Codex runs its commands in its workspace-write sandbox.
# It runs again whenever run 1 ran.
review = docker_agent.AgentRun("review",
    yaml=coders.yaml,
    agent="reviewer",
    allow_harness_agents=True,
    prompt=notes.output.apply(lambda output: f"Review this summary of NOTES.md in two sentences: {output}"),
    triggers=[notes.ran_at],
    inherit_environment=False,
    env={
        "CODEX_HOME": codex_home,
    },
    timeout_seconds=600)
pulumi.export("summary", notes.output)
pulumi.export("reviewText", review.output)
pulumi.export("usage", notes.usage)
```

{{% /choosable %}}
{{% choosable language go %}}

```go
package main

import (
	"fmt"

	"github.com/dirien/pulumi-docker-agent/sdk/go/dockeragent"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		cfg := config.New(ctx, "")

		// A long-lived token from `claude setup-token`, which needs a Claude subscription. Set it with
		// `pulumi config set --secret claudeCodeOauthToken <token>`.
		claudeCodeOauthToken := cfg.RequireSecret("claudeCodeOauthToken")

		// A Codex home of AgentRun's own, logged in once with a ChatGPT plan:
		// `mkdir ~/.codex-agentrun && CODEX_HOME=~/.codex-agentrun codex login`. Set it with
		// `pulumi config set codexHome $HOME/.codex-agentrun`.
		codexHome := cfg.Require("codexHome")

		// The agents, authored once: Claude Code and Codex as coding harnesses. They need no model and
		// no API key.
		coders, err := dockeragent.NewAgentConfig(ctx, "coders", &dockeragent.AgentConfigArgs{
			Agents: dockeragent.AgentArray{
				&dockeragent.AgentArgs{
					Name:        pulumi.String("root"),
					Description: pulumi.String("Writes project notes with Claude Code"),
					Harness: &dockeragent.HarnessArgs{
						Type:   dockeragent.HarnessTypeClaudeCode,
						Model:  pulumi.String("sonnet"),
						Effort: dockeragent.HarnessEffortLow,
					},
				},
				&dockeragent.AgentArgs{
					Name:        pulumi.String("reviewer"),
					Description: pulumi.String("Reviews project notes with Codex"),
					Harness: &dockeragent.HarnessArgs{
						Type: dockeragent.HarnessTypeCodex,
					},
				},
			},
		})
		if err != nil {
			return err
		}

		// Run 1: Claude Code maintains NOTES.md in the existing directory ./workspace. It runs its own
		// tools (shell commands, file edits) there without approval.
		notes, err := dockeragent.NewAgentRun(ctx, "notes", &dockeragent.AgentRunArgs{
			Yaml:               coders.Yaml,
			AllowHarnessAgents: pulumi.Bool(true),
			WorkingDir:         pulumi.String("./workspace"),
			Prompt:             pulumi.String("Write NOTES.md with three bullet points about this workspace, then summarise the file in one sentence."),
			UpdatePrompt:       pulumi.String("NOTES.md exists. Bring it up to date and keep its format, then summarise the file in one sentence."),
			DeletePrompt:       pulumi.String("Delete NOTES.md if it exists, then answer: done."),
			InheritEnvironment: pulumi.Bool(false),
			Env: pulumi.StringMap{
				"CLAUDE_CODE_OAUTH_TOKEN": claudeCodeOauthToken,
			},
			TimeoutSeconds: pulumi.Int(900),
		}, pulumi.DeleteBeforeReplace(true))
		if err != nil {
			return err
		}

		// Run 2: the Codex agent of the same configuration, in a temporary directory. With
		// toolApproval: deny (the default), Codex runs its commands in its workspace-write sandbox.
		// It runs again whenever run 1 ran.
		review, err := dockeragent.NewAgentRun(ctx, "review", &dockeragent.AgentRunArgs{
			Yaml:               coders.Yaml,
			Agent:              pulumi.String("reviewer"),
			AllowHarnessAgents: pulumi.Bool(true),
			Prompt: notes.Output.ApplyT(func(output string) (string, error) {
				return fmt.Sprintf("Review this summary of NOTES.md in two sentences: %v", output), nil
			}).(pulumi.StringOutput),
			Triggers: pulumi.Array{
				notes.RanAt,
			},
			InheritEnvironment: pulumi.Bool(false),
			Env: pulumi.StringMap{
				"CODEX_HOME": pulumi.String(codexHome),
			},
			TimeoutSeconds: pulumi.Int(600),
		})
		if err != nil {
			return err
		}
		ctx.Export("summary", notes.Output)
		ctx.Export("reviewText", review.Output)
		ctx.Export("usage", notes.Usage)
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

    // A long-lived token from `claude setup-token`, which needs a Claude subscription. Set it with
    // `pulumi config set --secret claudeCodeOauthToken <token>`.
    var claudeCodeOauthToken = config.RequireSecret("claudeCodeOauthToken");

    // A Codex home of AgentRun's own, logged in once with a ChatGPT plan:
    // `mkdir ~/.codex-agentrun && CODEX_HOME=~/.codex-agentrun codex login`. Set it with
    // `pulumi config set codexHome $HOME/.codex-agentrun`.
    var codexHome = config.Require("codexHome");

    // The agents, authored once: Claude Code and Codex as coding harnesses. They need no model and
    // no API key.
    var coders = new DockerAgent.AgentConfig("coders", new()
    {
        Agents = new[]
        {
            new DockerAgent.Inputs.AgentArgs
            {
                Name = "root",
                Description = "Writes project notes with Claude Code",
                Harness = new DockerAgent.Inputs.HarnessArgs
                {
                    Type = DockerAgent.HarnessType.ClaudeCode,
                    Model = "sonnet",
                    Effort = DockerAgent.HarnessEffort.Low,
                },
            },
            new DockerAgent.Inputs.AgentArgs
            {
                Name = "reviewer",
                Description = "Reviews project notes with Codex",
                Harness = new DockerAgent.Inputs.HarnessArgs
                {
                    Type = DockerAgent.HarnessType.Codex,
                },
            },
        },
    });

    // Run 1: Claude Code maintains NOTES.md in the existing directory ./workspace. It runs its own
    // tools (shell commands, file edits) there without approval.
    var notes = new DockerAgent.AgentRun("notes", new()
    {
        Yaml = coders.Yaml,
        AllowHarnessAgents = true,
        WorkingDir = "./workspace",
        Prompt = "Write NOTES.md with three bullet points about this workspace, then summarise the file in one sentence.",
        UpdatePrompt = "NOTES.md exists. Bring it up to date and keep its format, then summarise the file in one sentence.",
        DeletePrompt = "Delete NOTES.md if it exists, then answer: done.",
        InheritEnvironment = false,
        Env =
        {
            { "CLAUDE_CODE_OAUTH_TOKEN", claudeCodeOauthToken },
        },
        TimeoutSeconds = 900,
    }, new CustomResourceOptions
    {
        DeleteBeforeReplace = true,
    });

    // Run 2: the Codex agent of the same configuration, in a temporary directory. With
    // toolApproval: deny (the default), Codex runs its commands in its workspace-write sandbox.
    // It runs again whenever run 1 ran.
    var review = new DockerAgent.AgentRun("review", new()
    {
        Yaml = coders.Yaml,
        Agent = "reviewer",
        AllowHarnessAgents = true,
        Prompt = notes.Output.Apply(output => $"Review this summary of NOTES.md in two sentences: {output}"),
        Triggers = new[]
        {
            notes.RanAt,
        },
        InheritEnvironment = false,
        Env =
        {
            { "CODEX_HOME", codexHome },
        },
        TimeoutSeconds = 600,
    });

    return new Dictionary<string, object?>
    {
        ["summary"] = notes.Output,
        ["reviewText"] = review.Output,
        ["usage"] = notes.Usage,
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
import io.github.dirien.dockeragent.inputs.HarnessArgs;
import io.github.dirien.dockeragent.AgentRun;
import io.github.dirien.dockeragent.AgentRunArgs;
import io.github.dirien.dockeragent.enums.HarnessEffort;
import io.github.dirien.dockeragent.enums.HarnessType;
import com.pulumi.resources.CustomResourceOptions;
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

        // A long-lived token from `claude setup-token`, which needs a Claude subscription. Set it with
        // `pulumi config set --secret claudeCodeOauthToken <token>`.
        final var claudeCodeOauthToken = config.requireSecret("claudeCodeOauthToken");

        // A Codex home of AgentRun's own, logged in once with a ChatGPT plan:
        // `mkdir ~/.codex-agentrun && CODEX_HOME=~/.codex-agentrun codex login`. Set it with
        // `pulumi config set codexHome $HOME/.codex-agentrun`.
        final var codexHome = config.require("codexHome");

        // The agents, authored once: Claude Code and Codex as coding harnesses. They need no model and
        // no API key.
        var coders = new AgentConfig("coders", AgentConfigArgs.builder()
            .agents(
                AgentArgs.builder()
                    .name("root")
                    .description("Writes project notes with Claude Code")
                    .harness(HarnessArgs.builder()
                        .type(HarnessType.ClaudeCode)
                        .model("sonnet")
                        .effort(HarnessEffort.Low)
                        .build())
                    .build(),
                AgentArgs.builder()
                    .name("reviewer")
                    .description("Reviews project notes with Codex")
                    .harness(HarnessArgs.builder()
                        .type(HarnessType.Codex)
                        .build())
                    .build())
            .build());

        // Run 1: Claude Code maintains NOTES.md in the existing directory ./workspace. It runs its own
        // tools (shell commands, file edits) there without approval.
        var notes = new AgentRun("notes", AgentRunArgs.builder()
            .yaml(coders.yaml())
            .allowHarnessAgents(true)
            .workingDir("./workspace")
            .prompt("Write NOTES.md with three bullet points about this workspace, then summarise the file in one sentence.")
            .updatePrompt("NOTES.md exists. Bring it up to date and keep its format, then summarise the file in one sentence.")
            .deletePrompt("Delete NOTES.md if it exists, then answer: done.")
            .inheritEnvironment(false)
            .env(claudeCodeOauthToken.applyValue(_claudeCodeOauthToken -> Map.of("CLAUDE_CODE_OAUTH_TOKEN", _claudeCodeOauthToken)))
            .timeoutSeconds(900)
            .build(), CustomResourceOptions.builder()
                .deleteBeforeReplace(true)
                .build());

        // Run 2: the Codex agent of the same configuration, in a temporary directory. With
        // toolApproval: deny (the default), Codex runs its commands in its workspace-write sandbox.
        // It runs again whenever run 1 ran.
        var review = new AgentRun("review", AgentRunArgs.builder()
            .yaml(coders.yaml())
            .agent("reviewer")
            .allowHarnessAgents(true)
            .prompt(notes.output().applyValue(_output -> String.format("Review this summary of NOTES.md in two sentences: %s", _output)))
            .triggers(notes.ranAt())
            .inheritEnvironment(false)
            .env(Map.of("CODEX_HOME", codexHome))
            .timeoutSeconds(600)
            .build());

        ctx.export("summary", notes.output());
        ctx.export("reviewText", review.output());
        ctx.export("usage", notes.usage());
    }
}
```

{{% /choosable %}}
{{% choosable language yaml %}}

```yaml
name: coding-harness
runtime: yaml

config:
  # A long-lived token from `claude setup-token`, which needs a Claude subscription. Set it with
  # `pulumi config set --secret claudeCodeOauthToken <token>`.
  claudeCodeOauthToken:
    type: string
    secret: true
  # A Codex home of AgentRun's own, logged in once with a ChatGPT plan:
  # `mkdir ~/.codex-agentrun && CODEX_HOME=~/.codex-agentrun codex login`. Set it with
  # `pulumi config set codexHome $HOME/.codex-agentrun`.
  codexHome:
    type: string

resources:
  # The agents, authored once: Claude Code and Codex as coding harnesses. They need no model and
  # no API key.
  coders:
    type: docker-agent:index:AgentConfig
    properties:
      agents:
        - name: root
          description: Writes project notes with Claude Code
          harness:
            type: claude-code
            model: sonnet
            effort: low
        - name: reviewer
          description: Reviews project notes with Codex
          harness:
            type: codex

  # Run 1: Claude Code maintains NOTES.md in the existing directory ./workspace. It runs its own
  # tools (shell commands, file edits) there without approval.
  notes:
    type: docker-agent:index:AgentRun
    properties:
      yaml: ${coders.yaml}
      allowHarnessAgents: true
      workingDir: ./workspace
      prompt: Write NOTES.md with three bullet points about this workspace, then summarise the file in one sentence.
      # Sent on reruns while `prompt` is unchanged.
      updatePrompt: NOTES.md exists. Bring it up to date and keep its format, then summarise the file in one sentence.
      # Runs when the resource is deleted, for example by `pulumi destroy`. Like every run, it
      # needs a final answer.
      deletePrompt: "Delete NOTES.md if it exists, then answer: done."
      inheritEnvironment: false
      env:
        CLAUDE_CODE_OAUTH_TOKEN: ${claudeCodeOauthToken}
      timeoutSeconds: 900
    options:
      # A replacement deletes the old resource first, so that its deletePrompt does not undo
      # the new run.
      deleteBeforeReplace: true

  # Run 2: the Codex agent of the same configuration, in a temporary directory. With
  # toolApproval: deny (the default), Codex runs its commands in its workspace-write sandbox.
  # It runs again whenever run 1 ran.
  review:
    type: docker-agent:index:AgentRun
    properties:
      yaml: ${coders.yaml}
      agent: reviewer
      allowHarnessAgents: true
      prompt: "Review this summary of NOTES.md in two sentences: ${notes.output}"
      triggers:
        - ${notes.ranAt}
      inheritEnvironment: false
      env:
        CODEX_HOME: ${codexHome}
      timeoutSeconds: 600

outputs:
  summary: ${notes.output}
  reviewText: ${review.output}
  usage: ${notes.usage}
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

# The agents, authored once: Claude Code and Codex as coding harnesses. They need no model and
# no API key.
resource "docker-agent_agent_config" "coders" {
  agents {
    name        = "root"
    description = "Writes project notes with Claude Code"
    harness = {
      type   = "claude-code"
      model  = "sonnet"
      effort = "low"
    }
  }
  agents {
    name        = "reviewer"
    description = "Reviews project notes with Codex"
    harness = {
      type = "codex"
    }
  }
}

# Run 1: Claude Code maintains NOTES.md in the existing directory ./workspace. It runs its own
# tools (shell commands, file edits) there without approval.
resource "docker-agent_agent_run" "notes" {
  yaml                 = docker-agent_agent_config.coders.yaml
  allow_harness_agents = true
  working_dir          = "./workspace"
  prompt               = "Write NOTES.md with three bullet points about this workspace, then summarise the file in one sentence."
  update_prompt        = "NOTES.md exists. Bring it up to date and keep its format, then summarise the file in one sentence."
  delete_prompt        = "Delete NOTES.md if it exists, then answer: done."
  inherit_environment  = false
  env = {
    "CLAUDE_CODE_OAUTH_TOKEN" = var.claudeCodeOauthToken
  }
  timeout_seconds = 900
}

# Run 2: the Codex agent of the same configuration, in a temporary directory. With
# toolApproval: deny (the default), Codex runs its commands in its workspace-write sandbox.
# It runs again whenever run 1 ran.
resource "docker-agent_agent_run" "review" {
  yaml                 = docker-agent_agent_config.coders.yaml
  agent                = "reviewer"
  allow_harness_agents = true
  prompt               = "Review this summary of NOTES.md in two sentences: ${docker-agent_agent_run.notes.output}"
  triggers             = [docker-agent_agent_run.notes.ran_at]
  inherit_environment  = false
  env = {
    "CODEX_HOME" = var.codexHome
  }
  timeout_seconds = 600
}

# A long-lived token from `claude setup-token`, which needs a Claude subscription. Set it with
# `pulumi config set --secret claudeCodeOauthToken <token>`.
variable "claudeCodeOauthToken" {
  type = string
}

# A Codex home of AgentRun's own, logged in once with a ChatGPT plan:
# `mkdir ~/.codex-agentrun && CODEX_HOME=~/.codex-agentrun codex login`. Set it with
# `pulumi config set codexHome $HOME/.codex-agentrun`.
variable "codexHome" {
  type = string
}

output "summary" {
  value = docker-agent_agent_run.notes.output
}

output "reviewText" {
  value = docker-agent_agent_run.review.output
}

output "usage" {
  value = docker-agent_agent_run.notes.usage
}
```

In HCL, the properties of `docker-agent_agent_run` are snake_case (`allow_harness_agents`, `working_dir`, `update_prompt`, `delete_prompt`, `inherit_environment`), and `agents` are repeated nested blocks.

{{% /choosable %}}
{{< /chooser >}}
<!-- docsgen end -->

The two CLIs differ in what `AgentRun` can control:

| | Claude Code (`claude-code`) | Codex (`codex`) |
|---|---|---|
| Command line | `claude` in print mode with `--dangerously-skip-permissions`, `--model` and `--effort`, the prompt on stdin | `codex exec --json` with the sandbox that `toolApproval` selects and `--model`, the prompt on stdin |
| `toolApproval: deny` (the default) | no effect: Claude Code runs every tool without approval | Codex's `workspace-write` sandbox, without approvals (see below) |
| `toolApproval: allow` | no effect | no sandbox (`--dangerously-bypass-approvals-and-sandbox`) |
| Login | the `claude` login of the machine, or `CLAUDE_CODE_OAUTH_TOKEN` in `env` | the Codex login of `CODEX_HOME` (default `~/.codex`), or `CODEX_API_KEY` or `CODEX_ACCESS_TOKEN` in `env` |
| Settings it applies | `~/.claude` (hooks, MCP servers, `CLAUDE.md`), and the `.claude/` settings and `.mcp.json` of `workingDir` without a trust prompt | `$CODEX_HOME/config.toml` (model, profiles, `model_provider`, MCP servers, hooks, rules), the `.codex/` settings of a trusted project, and `AGENTS.md` files |
| Transcripts | `~/.claude/projects/`, and every directory in `~/.claude.json` | `$CODEX_HOME/sessions/`, shown by `codex resume` |
| `structured_output` | decoded from the final result, not enforced | enforced with `strict: true` (`codex --output-schema`), otherwise decoded |

- **What runs, and where.** `AgentRun` starts `claude` or `codex` on the machine that runs `pulumi up`. It looks the CLI up in the absolute entries of the `PATH` that the CLI gets (see below), and runs it in `workingDir`, or in a fresh temporary directory that is deleted after the run, with the model of `harness` and the prompt on stdin. **The CLI runs its own tools (shell commands, file edits, web requests, MCP tools)**: `allowHookApprovals` and the configuration's `permissions` do not apply to it, and `toolApproval` applies only to Codex's sandbox. Point `workingDir` only at directories you trust, never at the Pulumi project itself, and prefer a disposable machine or container. Both CLIs keep transcripts with every tool output, unredacted, also after a temporary directory is deleted.
- **Codex's sandbox.** With `toolApproval: deny`, Codex runs the model's commands in its `workspace-write` sandbox (Seatbelt on macOS, bubblewrap on Linux) with the approval policy `never`. That limits writes and the network, not reads: the commands can write only in `workingDir` (not its `.git`, `.codex` and `.agents`, so `git commit` fails), the temporary directories and the `writable_roots` of `config.toml`, and reach the network only when `config.toml` enables `network_access`; caches in the home directory are read-only too, so `go build` or `npm install` can fail. But they can read every file the user can, `~/.ssh` and `~/.aws` included, and the model sees what they read. MCP servers, hooks and `notify` programs (also those of a trusted project's `.codex/`) run outside the sandbox, and Codex's hosted web search stays on. A command that would need an approval is declined, and `pulumi up` warns how many were. `deny` is therefore no confidentiality boundary. With `toolApproval: allow`, Codex runs without a sandbox and can do everything the user can: use it only on an externally isolated machine or in a container. In a configuration with an agent with a model and a Codex sub-agent, `allow` approves the parent's tools and turns off Codex's sandbox at the same time.
- **The sandbox check.** Under `deny`, `AgentRun` checks before the first Codex turn that the sandbox starts (`codex --config=sandbox_mode="workspace-write" sandbox -- true`, or bubblewrap when that subcommand is missing), and fails the run before any model call when it does not, with "codex's sandbox cannot start on this machine (...)". A check that fails for another reason, or does not finish within 30 seconds, fails the run with "the check of codex's sandbox (...) failed (...), which does not say that the sandbox cannot start": run the command yourself as the user that runs `pulumi up` (it needs no login and calls no model). Before that, in both modes, `codex --version` must print a version; a codex that does not run fails with "the codex CLI does not run (...)". It usually does not start in Docker containers with the default seccomp profile, on Ubuntu 24.04 and other systems that restrict unprivileged user namespaces (AppArmor, `user.max_user_namespaces=0`), and on macOS inside another sandbox, for example when `pulumi up` runs inside a Claude Code or Codex session. The preview warns about these cases. Enable unprivileged user namespaces or install bubblewrap, run `pulumi up` outside the other sandbox, or, in a container that is isolated from everything else (a disposable CI job), set `toolApproval: allow`.
- **Logging in.** Claude Code uses the `claude` login of the user that runs `pulumi up` (`claude auth login`, checked with `claude auth status`), or a long-lived `claude setup-token` token as the secret `env` value `CLAUDE_CODE_OAUTH_TOKEN`, as the example does: use the token for CI, for SSH sessions (where the macOS keychain is locked) and when runs fail with "OAuth session expired and could not be refreshed". Codex uses the login of its `CODEX_HOME`: log in once with `codex login` (`codex login --device-auth` without a browser), preferably into a directory that only `AgentRun` uses, passed as `CODEX_HOME` in `env` as the example does, so that your interactive Codex and `AgentRun` never refresh the same login at the same time; never copy `auth.json`. A run can fail when another process refreshes the same login at the same time; retry, or use a separate login. When `forced_login_method` of Codex's `config.toml` does not allow the credential that codex would get, or `forced_chatgpt_workspace_id` is set and codex would use a `CODEX_ACCESS_TOKEN` whose workspace `AgentRun` cannot check, codex would delete its stored login, so the preview fails instead; so does workload identity under a `forced_login_method` other than `chatgpt`, with which codex would not start. The errors of these cases end with a hint. [Claude Code credentials](https://www.pulumi.com/registry/packages/docker-agent/installation-configuration/#claude-code-credentials) and [Codex credentials](https://www.pulumi.com/registry/packages/docker-agent/installation-configuration/#codex-credentials) have the details.
- **Billing.** In non-interactive mode, Claude Code uses an `ANTHROPIC_API_KEY` or `ANTHROPIC_AUTH_TOKEN` of its environment before `CLAUDE_CODE_OAUTH_TOKEN` and the login, and Codex uses `CODEX_API_KEY` before its login; both then bill API usage. `OPENAI_API_KEY` does not switch Codex to API billing. When `env` sets `CLAUDE_CODE_OAUTH_TOKEN`, an inherited `ANTHROPIC_API_KEY` or `ANTHROPIC_AUTH_TOKEN` is not passed to Claude Code, and when it sets `CODEX_ACCESS_TOKEN`, an inherited `CODEX_API_KEY` is not passed to Codex; one set in `env` is. `inheritEnvironment: false` keeps the other variables of the Pulumi CLI away from the CLIs, and `env: {ANTHROPIC_API_KEY: ""}` or `env: {CODEX_API_KEY: ""}` blanks an inherited key. The preview warns when a CLI would bill API usage, when (for Claude Code) a variable sends its requests elsewhere (`CLAUDE_CODE_USE_BEDROCK`, `CLAUDE_CODE_USE_VERTEX`, `CLAUDE_CODE_USE_FOUNDRY`, `ANTHROPIC_BASE_URL`), and when (for Codex) a custom `model_provider` of `config.toml` decides the billing (one with `requires_openai_auth = true` keeps Codex's login and billing). After the run, unless `logging` is `none`, `pulumi up` logs the CLI's version, the model and its credential: for Claude Code the reported `apiKeySource`, with what the environment selects instead of an API key (the Claude login, `CLAUDE_CODE_OAUTH_TOKEN`, `ANTHROPIC_AUTH_TOKEN` or a cloud provider), and for Codex the login (with the ChatGPT plan, and a `model_provider` or `openai_base_url` of `config.toml` it goes through), the sandbox, `CODEX_HOME`, the thread and how the usage is billed. It warns when the run billed API usage or a cloud account.
- **Environment.** With `inheritEnvironment: true` (the default), a CLI gets the environment of the Pulumi CLI and `env`, which wins. With `false`, Claude Code gets the system variables, `CLAUDE_CONFIG_DIR`, `CLAUDE_SECURESTORAGE_CONFIG_DIR`, `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, `XDG_STATE_HOME`, `XDG_CACHE_HOME`, `IS_SANDBOX` and `env`, and Codex the system variables, `CODEX_HOME`, `CODEX_CA_CERTIFICATE`, the proxy variables, `SSL_CERT_FILE`, `SSL_CERT_DIR`, `DBUS_SESSION_BUS_ADDRESS`, `XDG_RUNTIME_DIR` and `env`: the configuration locations decide which login a CLI reads, and `HOME` and `USER` are among the system variables. Pass Claude Code's proxy and CA variables (`HTTPS_PROXY`, `HTTP_PROXY`, `NO_PROXY`, `NODE_EXTRA_CA_CERTS`) through `env` then. With `false`, a `CLAUDE_CODE_OAUTH_TOKEN` of the Pulumi CLI's environment does not reach Claude Code either, which then uses the login of the machine: pass the token in `env`. The preview warns about that when `env` does not set it (`env: {CLAUDE_CODE_OAUTH_TOKEN: ""}` keeps the login and silences the warning). Codex loads `$CODEX_HOME/.env` itself, whatever `inheritEnvironment` says, and passes its environment, `env` included, to the commands it runs. In both cases, the markers of a parent Claude Code or Codex session (`CLAUDECODE`, `CLAUDE_CODE_SESSION_ID`, `CODEX_THREAD_ID`, `CODEX_SANDBOX`, ...), the variables such a session sets for its processes and the W3C trace context (`TRACEPARENT`, `TRACESTATE`) are removed unless `env` sets them, so a `pulumi up` inside a Claude Code or Codex session starts an independent session outside its trace, and `PWD` is the working directory. The values of `CLAUDE_CODE_OAUTH_TOKEN`, `ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN`, `CODEX_API_KEY`, `CODEX_ACCESS_TOKEN` and the tokens of Codex's `auth.json` are redacted from errors (including the CLIs' error output), warnings and logs like every credential, and inherited values are never stored in the state.
- **Root.** Claude Code refuses `--dangerously-skip-permissions` when it runs as root, which is common in CI containers. Run `pulumi up` as a non-root user, or, in an isolated container, set `IS_SANDBOX=1` in `env`. The preview warns when the provider runs as root (see below for when the preview checks a configuration).
- **Outputs.** `output` is the CLI's final answer: Claude Code's final result, or Codex's last message (from `--output-last-message`), not the text a CLI streamed while it worked, which is what `docker agent run` prints. When Claude Code reports success with an empty result (its last message ended without text), `output` is the text it wrote during the run instead, and `pulumi up` warns. A run without any text fails, and so does a Claude Code run that stopped for a deferred tool call, which needs an interactive session. `toolCalls` lists the tools that the CLI reports it ran (for example `["Write", "Bash"]` for Claude Code, `["Bash", "apply_patch"]` for Codex), and `logging` shows each of them as "run by the harness". `usage` has their tokens, with cached input counted as input; for Codex it covers its main thread, not the agents that Codex spawns itself (the line after the run says how many). `usage.cost` is `0` when the run used the Claude subscription or a ChatGPT plan, and the summary then names the plan with what the run would cost at API prices ("no per-token cost: billed to the ChatGPT plan (API price about $0.0412)"); otherwise it is the cost Claude Code reports, or for Codex an estimate at the catalog's API prices.
- **What the agent does not get.** docker-agent sends the CLI only the prompt (as a sub-agent, its task), not the agent's `instruction`: put instructions into the prompt, or into a `CLAUDE.md` or `AGENTS.md` in `workingDir`. `structured_output` is not enforced, except for Codex with `strict: true`: `structuredOutput` is the final answer decoded as JSON (a surrounding Markdown code fence is removed; `output` keeps it), and an answer that is not valid JSON fails the run, so ask for JSON in the prompt. With `strict: true`, Codex enforces the schema in OpenAI's strict mode, where every property must be required and `additionalProperties` false. When `harness.model` is not set, docker-agent passes the agent's `model` to the CLI's `--model`, so set `harness.model` (for example `sonnet` for Claude Code, a Codex model name for Codex) when the agent's `model` is a key of `models`, a model of another provider than `anthropic` for Claude Code, or any provider-qualified model for Codex; without either, Codex uses the model of its `config.toml` or its default. The preview warns about these cases. These preview warnings, and those about billing, logins, the sandbox and root, need a `yaml` configuration that is known during the preview and that the provider's docker-agent version can load (also with `skipValidation`); a configuration pulled by `reference` gets the tool-approval and billing warnings when it runs. Each run starts a new session of the CLI.
- **Stopping.** Coding tasks take longer than a model answer, so raise `timeoutSeconds` (900 to 1800 seconds is typical). Both CLIs run each shell command in a session of its own, outside the CLI's process group, and can leave the commands they ran in the background running when they exit. `AgentRun` therefore finds the processes started under the CLI in two ways. While the CLI runs, it reads the process table about every 0.2 seconds and records every process below the CLI; a process stays recorded after its parent exited. And the CLI gets a random value in `DOCKER_AGENT_RUN_COOKIE`, which every process it starts inherits: at each cleanup step, `AgentRun` also finds the processes of your user that started after the CLI and have exactly that value in their environment, such as `cmd >/dev/null 2>&1 &` run by a shell that exited at once. On every end of a run it stops the CLI and those processes, together with the process groups and sessions they created, whatever the CLI does with them. When `timeoutSeconds` runs out, or the operation is cancelled, `AgentRun` first sends Codex SIGINT, which ends its turn cleanly, and 2 seconds later (Claude Code at once) SIGTERM to the CLI's process group and to those processes, and SIGKILL to what is left 10 seconds later, or 1 second after the CLI exited if that comes first. After a normal exit, it stops what is left the same way, after 1 second, and warns how many processes the agent left running outside its own process group; MCP servers that the CLI was still shutting down are stopped without a warning. It signals only the processes recorded below the CLI, the process groups and sessions those created, and the processes of your user that started after the CLI and carry the run's exact value; never a process whose PID was reused, nor the provider itself or its other processes (such as the MCP servers of other agents). A process is missed only when it left the CLI's process group and session, its parent exited before a read of the process table saw it, and its environment does not show the value: it was started with a cleared or replaced environment (`env -i`), or, on macOS, the system does not show its environment to other processes (platform binaries such as `/bin/sleep`, and some signed programs, under System Integrity Protection), so that it is stopped only when a read of the process table saw it below the CLI. Codex starts its MCP servers with a minimal environment without the value, so only the process table finds them. When the process table cannot be read, only the CLI's process group and what was recorded until then are stopped. After the CLI exited, its output is read for at most 2 more seconds, so a process it left behind cannot hold up the run. When Pulumi stops the provider (SIGINT, and SIGKILL 5 seconds later), no new CLI starts, the running ones and their processes are stopped within about 2 seconds, and the provider exits once the operation that ran them ended, as it does without them. When the provider dies before that (SIGKILL, a crash), nothing stops the processes the CLI started: on Linux the run's child process and the CLI get SIGTERM from the kernel and the rest gets no signal; on macOS, the CLI and everything it started keep running.
- **Sub-agents.** A `claude-code` or `codex` agent can be the sub-agent of an agent with a model: the parent delegates with `transfer_task` and gets the CLI's final answer back, and the parent's own tool calls still follow `toolApproval`.

The [policy pack](https://github.com/dirien/pulumi-docker-agent/tree/main/policy) reports every `AgentRun` with `allowHarnessAgents: true` that is not in the `allowedResources` of its `agentrun-tool-approval-deny` policy.

### Not supported

These are out of scope for `AgentRun` in v0.2.0:

- **A Docker models gateway.** `DOCKER_AGENT_MODELS_GATEWAY`, `CAGENT_MODELS_GATEWAY` and `bypass_models_gateway` have no effect: models call their providers directly, and the preview says so.
- **Parallel runs.** The runs of one provider process execute one at a time.
- **User-level docker-agent configuration.** The user settings, aliases, global `providers`, the default model and the credential helper of `~/.config/cagent/config.yaml` (which `UserSettings` writes), the API keys in `~/.config/cagent/.env`, `/run/secrets`, Docker Desktop secrets and `op://` references. Declare providers in the configuration and pass keys in `env`. The ChatGPT login of `docker agent setup` is the only exception.
- **The ChatGPT login in CI or Pulumi Deployments.** It needs a browser sign-in on the machine; `CHATGPT_OAUTH_TOKEN` takes a short-lived access token, which is not refreshed.
- **Vertex AI with an access token**, for example from Pulumi ESC's `gcp-login` through OpenID Connect: the Google SDK reads application default credentials, such as a service account key file in `GOOGLE_APPLICATION_CREDENTIALS`.
- **Coding harnesses on Windows**, and the `pi` and `opencode` harnesses on every system.
- **`OLLAMA_HOST`.** docker-agent does not read it; set the model's `baseUrl`.

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

The environment variable `DOCKER_AGENT_SKIP_DELETE_PROMPTS=true` is not a setting: it skips the `deletePrompt` of every `AgentRun` that one `pulumi` command deletes (see [Reruns and delete prompts](#reruns-and-delete-prompts)).

The provider needs no model API keys to render, validate and publish configurations. Model credentials such as `OPENAI_API_KEY` or `ANTHROPIC_API_KEY` are needed where the agent runs: by `docker agent run` and `docker agent serve`, or by `AgentRun`, which takes them from its `env` input or the environment of the Pulumi CLI. A `chatgpt` model of `AgentRun` uses the ChatGPT login of `docker agent setup` on the machine, a Claude Code agent the `claude` login or a `CLAUDE_CODE_OAUTH_TOKEN` in `env`, and a Codex agent the Codex login of its `CODEX_HOME` or a `CODEX_API_KEY` in `env`, instead. See the [Installation & Configuration](https://www.pulumi.com/registry/packages/docker-agent/installation-configuration/) page for details.

## Pulumi ESC

A [Pulumi ESC](https://www.pulumi.com/docs/esc/) environment can supply every credential in one place. Its `pulumiConfig` sets `docker-agent:registryAuth` (with `fn::secret` values) for pushing and pulling artifacts. Its `environmentVariables` export model API keys such as `ANTHROPIC_API_KEY` or `OPENAI_API_KEY`, which `pulumi up` passes to the provider and `AgentRun` uses with its default `inheritEnvironment: true`. `fn::open::aws-login` creates short-lived AWS credentials for `amazon-bedrock` models through OpenID Connect, which `AgentRun` uses from the environment, or from `env` with `inheritEnvironment: false`. The same environment runs a published agent:

```bash
pulumi env run -i <org>/<project>/<environment> -- docker agent run <pinnedReference>
```

[Using Pulumi ESC](https://www.pulumi.com/registry/packages/docker-agent/installation-configuration/#using-pulumi-esc) has an environment definition, the stack settings that import it, and when to pass a key through `env` instead.

## Policy as code

The repository contains a [policy pack](https://github.com/dirien/pulumi-docker-agent/tree/main/policy) for these resources, written for [Pulumi Policies](https://www.pulumi.com/docs/discovery-governance/concepts/policy-as-code/). By default it requires `AgentRun` to keep `toolApproval: deny` without `allowHookApprovals` and without `allowHarnessAgents` (and can remediate all three), rejects permission rules that approve every tool or every call of a tool such as `shell` (in `AgentConfig` and `UserSettings`), the `autonomous` safety mode (and `yolo` in `UserSettings`) and literal credentials in the `env` and `headers` of `AgentConfig`, and warns about `AgentRun` references that are not pinned by digest and about local tools without `runtime.sandbox`. Allowlists for model providers, MCP servers and artifact registries can be enabled in the policy configuration. Run it with any program:

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
- `AgentRun` — Runs a prompt against an agent configuration (`yaml` or `reference`) during `pulumi up`, in a child process of the provider, with any model provider docker-agent supports, and outputs the final answer (`output`), the answer as JSON (`structuredOutput`) when the agent declares structured output, `agentName`, `usage` and `toolCalls`. It never runs during previews, runs again when an input that affects the run changes (with `updatePrompt`, and on every `pulumi up` with `rerun: always`), runs `deletePrompt` when it is deleted, and rejects tool calls that need approval (including approvals from hooks) unless `toolApproval` is `allow`. With `allowHarnessAgents`, it runs Claude Code and Codex agents (`harness: {type: claude-code}` and `{type: codex}`) on Linux and macOS, which run their own tools without approval (Codex under `toolApproval: deny` in its sandbox).
