{{% examples %}}
## Example Usage
{{% example %}}
### Structured output from an agent

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
                risk:
                  type: string
                  enum:
                    - low
                    - medium
                    - high
                summary:
                  type: string
              required:
                - risk
                - summary
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
  # For example {"risk": "high", "summary": "..."}.
  verdict: ${review.structuredOutput}
  usage: ${review.usage}
```
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
{{% /example %}}
{{% example %}}
### Run a shared agent and log its tool calls

```typescript
import * as pulumi from "@pulumi/pulumi";
import * as docker_agent from "@ediri/pulumi-docker-agent";

const config = new pulumi.Config();

// Set it with `pulumi config set --secret anthropicApiKey <key>`.
const anthropicApiKey = config.requireSecret("anthropicApiKey");

// The commit range to summarise, for example `v1.2.0..HEAD`.
const commits = config.require("commits");

// Pulls the agent from a registry, pinned by digest, and runs it.
const notes = new docker_agent.AgentRun("notes", {
    reference: "docker.io/my-org/release-notes@sha256:2c26b46b68ffc68ff99b453c1d30413413422d706483bfa0f98a5e886266e7ae",
    prompt: `Write the release notes for ${commits}.`,
    inheritEnvironment: false,
    env: {
        ANTHROPIC_API_KEY: anthropicApiKey,
    },
    toolApproval: docker_agent.ToolApproval.Deny,
    logging: docker_agent.Logging.Tools,
    timeoutSeconds: 600,
});
export const releaseNotes = notes.output;
export const toolCalls = notes.toolCalls;
```
```python
import pulumi
import ediri_docker_agent as docker_agent

config = pulumi.Config()

# Set it with `pulumi config set --secret anthropicApiKey <key>`.
anthropic_api_key = config.require_secret("anthropicApiKey")

# The commit range to summarise, for example `v1.2.0..HEAD`.
commits = config.require("commits")

# Pulls the agent from a registry, pinned by digest, and runs it.
notes = docker_agent.AgentRun("notes",
    reference="docker.io/my-org/release-notes@sha256:2c26b46b68ffc68ff99b453c1d30413413422d706483bfa0f98a5e886266e7ae",
    prompt=f"Write the release notes for {commits}.",
    inherit_environment=False,
    env={
        "ANTHROPIC_API_KEY": anthropic_api_key,
    },
    tool_approval=docker_agent.ToolApproval.DENY,
    logging=docker_agent.Logging.TOOLS,
    timeout_seconds=600)
pulumi.export("releaseNotes", notes.output)
pulumi.export("toolCalls", notes.tool_calls)
```
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

		// Set it with `pulumi config set --secret anthropicApiKey <key>`.
		anthropicApiKey := cfg.RequireSecret("anthropicApiKey")

		// The commit range to summarise, for example `v1.2.0..HEAD`.
		commits := cfg.Require("commits")

		// Pulls the agent from a registry, pinned by digest, and runs it.
		notes, err := dockeragent.NewAgentRun(ctx, "notes", &dockeragent.AgentRunArgs{
			Reference:          pulumi.String("docker.io/my-org/release-notes@sha256:2c26b46b68ffc68ff99b453c1d30413413422d706483bfa0f98a5e886266e7ae"),
			Prompt:             pulumi.Sprintf("Write the release notes for %v.", commits),
			InheritEnvironment: pulumi.Bool(false),
			Env: pulumi.StringMap{
				"ANTHROPIC_API_KEY": anthropicApiKey,
			},
			ToolApproval:   dockeragent.ToolApprovalDeny,
			Logging:        dockeragent.LoggingTools,
			TimeoutSeconds: pulumi.Int(600),
		})
		if err != nil {
			return err
		}
		ctx.Export("releaseNotes", notes.Output)
		ctx.Export("toolCalls", notes.ToolCalls)
		return nil
	})
}
```
```csharp
using System.Collections.Generic;
using System.Linq;
using Pulumi;
using DockerAgent = ediri.DockerAgent;

return await Deployment.RunAsync(() =>
{
    var config = new Config();

    // Set it with `pulumi config set --secret anthropicApiKey <key>`.
    var anthropicApiKey = config.RequireSecret("anthropicApiKey");

    // The commit range to summarise, for example `v1.2.0..HEAD`.
    var commits = config.Require("commits");

    // Pulls the agent from a registry, pinned by digest, and runs it.
    var notes = new DockerAgent.AgentRun("notes", new()
    {
        Reference = "docker.io/my-org/release-notes@sha256:2c26b46b68ffc68ff99b453c1d30413413422d706483bfa0f98a5e886266e7ae",
        Prompt = $"Write the release notes for {commits}.",
        InheritEnvironment = false,
        Env =
        {
            { "ANTHROPIC_API_KEY", anthropicApiKey },
        },
        ToolApproval = DockerAgent.ToolApproval.Deny,
        Logging = DockerAgent.Logging.Tools,
        TimeoutSeconds = 600,
    });

    return new Dictionary<string, object?>
    {
        ["releaseNotes"] = notes.Output,
        ["toolCalls"] = notes.ToolCalls,
    };
});
```
```java
package generated_program;

import com.pulumi.Context;
import com.pulumi.Pulumi;
import com.pulumi.core.Output;
import io.github.dirien.dockeragent.AgentRun;
import io.github.dirien.dockeragent.AgentRunArgs;
import io.github.dirien.dockeragent.enums.Logging;
import io.github.dirien.dockeragent.enums.ToolApproval;
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

        // Set it with `pulumi config set --secret anthropicApiKey <key>`.
        final var anthropicApiKey = config.requireSecret("anthropicApiKey");

        // The commit range to summarise, for example `v1.2.0..HEAD`.
        final var commits = config.require("commits");

        // Pulls the agent from a registry, pinned by digest, and runs it.
        var notes = new AgentRun("notes", AgentRunArgs.builder()
            .reference("docker.io/my-org/release-notes@sha256:2c26b46b68ffc68ff99b453c1d30413413422d706483bfa0f98a5e886266e7ae")
            .prompt(String.format("Write the release notes for %s.", commits))
            .inheritEnvironment(false)
            .env(anthropicApiKey.applyValue(_anthropicApiKey -> Map.of("ANTHROPIC_API_KEY", _anthropicApiKey)))
            .toolApproval(ToolApproval.Deny)
            .logging(Logging.Tools)
            .timeoutSeconds(600)
            .build());

        ctx.export("releaseNotes", notes.output());
        ctx.export("toolCalls", notes.toolCalls());
    }
}
```
```yaml
name: release-notes
runtime: yaml
config:
  # Set it with `pulumi config set --secret anthropicApiKey <key>`.
  anthropicApiKey:
    type: string
    secret: true
  # The commit range to summarise, for example `v1.2.0..HEAD`.
  commits:
    type: string
resources:
  # Pulls the agent from a registry, pinned by digest, and runs it.
  notes:
    type: docker-agent:index:AgentRun
    properties:
      reference: docker.io/my-org/release-notes@sha256:2c26b46b68ffc68ff99b453c1d30413413422d706483bfa0f98a5e886266e7ae
      prompt: Write the release notes for ${commits}.
      inheritEnvironment: false
      env:
        ANTHROPIC_API_KEY: ${anthropicApiKey}
      toolApproval: deny
      logging: tools
      # Cancels the run after 10 minutes.
      timeoutSeconds: 600
outputs:
  releaseNotes: ${notes.output}
  toolCalls: ${notes.toolCalls}
```
```hcl
terraform {
  required_providers {
    docker-agent = {
      source = "pulumi/dirien/docker-agent"
    }
  }
}

# Pulls the agent from a registry, pinned by digest, and runs it.
resource "docker-agent_agent_run" "notes" {
  reference           = "docker.io/my-org/release-notes@sha256:2c26b46b68ffc68ff99b453c1d30413413422d706483bfa0f98a5e886266e7ae"
  prompt              = "Write the release notes for ${var.commits}."
  inherit_environment = false
  env = {
    "ANTHROPIC_API_KEY" = var.anthropicApiKey
  }
  tool_approval   = "deny"
  logging         = "tools"
  timeout_seconds = 600
}

# Set it with `pulumi config set --secret anthropicApiKey <key>`.
variable "anthropicApiKey" {
  type = string
}

# The commit range to summarise, for example `v1.2.0..HEAD`.
variable "commits" {
  type = string
}

output "releaseNotes" {
  value = docker-agent_agent_run.notes.output
}

output "toolCalls" {
  value = docker-agent_agent_run.notes.tool_calls
}
```
{{% /example %}}
{{% /examples %}}
