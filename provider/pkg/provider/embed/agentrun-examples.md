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
{{% example %}}
### Claude Code on a Claude subscription

```typescript
import * as pulumi from "@pulumi/pulumi";
import * as docker_agent from "@ediri/pulumi-docker-agent";

const config = new pulumi.Config();

// A long-lived token from `claude setup-token`, which needs a Claude subscription. Set it with
// `pulumi config set --secret claudeCodeOauthToken <token>`.
const claudeCodeOauthToken = config.requireSecret("claudeCodeOauthToken");

// The agent: Claude Code as a coding harness. It needs no model and no API key.
const coder = new docker_agent.AgentConfig("coder", {agents: [{
    name: "root",
    description: "Maintains the notes of a repository with Claude Code",
    harness: {
        type: docker_agent.HarnessType.ClaudeCode,
        model: "sonnet",
        effort: docker_agent.HarnessEffort.High,
    },
}]});

// Starts the claude CLI during `pulumi up` in the existing directory ./workspace, where Claude Code
// runs its own tools (shell commands, file edits) without approval. Without `env`, it uses the
// `claude` login of the machine that runs `pulumi up`.
const notes = new docker_agent.AgentRun("notes", {
    yaml: coder.yaml,
    allowHarnessAgents: true,
    workingDir: "./workspace",
    prompt: "Write NOTES.md with three bullet points about the files in this directory.",
    updatePrompt: "NOTES.md exists. Bring it up to date and keep its format.",
    deletePrompt: "Delete NOTES.md if it exists, then answer: done.",
    inheritEnvironment: false,
    env: {
        CLAUDE_CODE_OAUTH_TOKEN: claudeCodeOauthToken,
    },
    timeoutSeconds: 900,
}, {
    deleteBeforeReplace: true,
});
export const summary = notes.output;
export const usage = notes.usage;
```
```python
import pulumi
import ediri_docker_agent as docker_agent

config = pulumi.Config()

# A long-lived token from `claude setup-token`, which needs a Claude subscription. Set it with
# `pulumi config set --secret claudeCodeOauthToken <token>`.
claude_code_oauth_token = config.require_secret("claudeCodeOauthToken")

# The agent: Claude Code as a coding harness. It needs no model and no API key.
coder = docker_agent.AgentConfig("coder", agents=[{
    "name": "root",
    "description": "Maintains the notes of a repository with Claude Code",
    "harness": {
        "type": docker_agent.HarnessType.CLAUDE_CODE,
        "model": "sonnet",
        "effort": docker_agent.HarnessEffort.HIGH,
    },
}])

# Starts the claude CLI during `pulumi up` in the existing directory ./workspace, where Claude Code
# runs its own tools (shell commands, file edits) without approval. Without `env`, it uses the
# `claude` login of the machine that runs `pulumi up`.
notes = docker_agent.AgentRun("notes",
    yaml=coder.yaml,
    allow_harness_agents=True,
    working_dir="./workspace",
    prompt="Write NOTES.md with three bullet points about the files in this directory.",
    update_prompt="NOTES.md exists. Bring it up to date and keep its format.",
    delete_prompt="Delete NOTES.md if it exists, then answer: done.",
    inherit_environment=False,
    env={
        "CLAUDE_CODE_OAUTH_TOKEN": claude_code_oauth_token,
    },
    timeout_seconds=900,
    opts = pulumi.ResourceOptions(delete_before_replace=True))
pulumi.export("summary", notes.output)
pulumi.export("usage", notes.usage)
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

		// A long-lived token from `claude setup-token`, which needs a Claude subscription. Set it with
		// `pulumi config set --secret claudeCodeOauthToken <token>`.
		claudeCodeOauthToken := cfg.RequireSecret("claudeCodeOauthToken")

		// The agent: Claude Code as a coding harness. It needs no model and no API key.
		coder, err := dockeragent.NewAgentConfig(ctx, "coder", &dockeragent.AgentConfigArgs{
			Agents: dockeragent.AgentArray{
				&dockeragent.AgentArgs{
					Name:        pulumi.String("root"),
					Description: pulumi.String("Maintains the notes of a repository with Claude Code"),
					Harness: &dockeragent.HarnessArgs{
						Type:   dockeragent.HarnessTypeClaudeCode,
						Model:  pulumi.String("sonnet"),
						Effort: dockeragent.HarnessEffortHigh,
					},
				},
			},
		})
		if err != nil {
			return err
		}

		// Starts the claude CLI during `pulumi up` in the existing directory ./workspace, where Claude Code
		// runs its own tools (shell commands, file edits) without approval. Without `env`, it uses the
		// `claude` login of the machine that runs `pulumi up`.
		notes, err := dockeragent.NewAgentRun(ctx, "notes", &dockeragent.AgentRunArgs{
			Yaml:               coder.Yaml,
			AllowHarnessAgents: pulumi.Bool(true),
			WorkingDir:         pulumi.String("./workspace"),
			Prompt:             pulumi.String("Write NOTES.md with three bullet points about the files in this directory."),
			UpdatePrompt:       pulumi.String("NOTES.md exists. Bring it up to date and keep its format."),
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
		ctx.Export("summary", notes.Output)
		ctx.Export("usage", notes.Usage)
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

    // A long-lived token from `claude setup-token`, which needs a Claude subscription. Set it with
    // `pulumi config set --secret claudeCodeOauthToken <token>`.
    var claudeCodeOauthToken = config.RequireSecret("claudeCodeOauthToken");

    // The agent: Claude Code as a coding harness. It needs no model and no API key.
    var coder = new DockerAgent.AgentConfig("coder", new()
    {
        Agents = new[]
        {
            new DockerAgent.Inputs.AgentArgs
            {
                Name = "root",
                Description = "Maintains the notes of a repository with Claude Code",
                Harness = new DockerAgent.Inputs.HarnessArgs
                {
                    Type = DockerAgent.HarnessType.ClaudeCode,
                    Model = "sonnet",
                    Effort = DockerAgent.HarnessEffort.High,
                },
            },
        },
    });

    // Starts the claude CLI during `pulumi up` in the existing directory ./workspace, where Claude Code
    // runs its own tools (shell commands, file edits) without approval. Without `env`, it uses the
    // `claude` login of the machine that runs `pulumi up`.
    var notes = new DockerAgent.AgentRun("notes", new()
    {
        Yaml = coder.Yaml,
        AllowHarnessAgents = true,
        WorkingDir = "./workspace",
        Prompt = "Write NOTES.md with three bullet points about the files in this directory.",
        UpdatePrompt = "NOTES.md exists. Bring it up to date and keep its format.",
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

    return new Dictionary<string, object?>
    {
        ["summary"] = notes.Output,
        ["usage"] = notes.Usage,
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

        // The agent: Claude Code as a coding harness. It needs no model and no API key.
        var coder = new AgentConfig("coder", AgentConfigArgs.builder()
            .agents(AgentArgs.builder()
                .name("root")
                .description("Maintains the notes of a repository with Claude Code")
                .harness(HarnessArgs.builder()
                    .type(HarnessType.ClaudeCode)
                    .model("sonnet")
                    .effort(HarnessEffort.High)
                    .build())
                .build())
            .build());

        // Starts the claude CLI during `pulumi up` in the existing directory ./workspace, where Claude Code
        // runs its own tools (shell commands, file edits) without approval. Without `env`, it uses the
        // `claude` login of the machine that runs `pulumi up`.
        var notes = new AgentRun("notes", AgentRunArgs.builder()
            .yaml(coder.yaml())
            .allowHarnessAgents(true)
            .workingDir("./workspace")
            .prompt("Write NOTES.md with three bullet points about the files in this directory.")
            .updatePrompt("NOTES.md exists. Bring it up to date and keep its format.")
            .deletePrompt("Delete NOTES.md if it exists, then answer: done.")
            .inheritEnvironment(false)
            .env(claudeCodeOauthToken.applyValue(_claudeCodeOauthToken -> Map.of("CLAUDE_CODE_OAUTH_TOKEN", _claudeCodeOauthToken)))
            .timeoutSeconds(900)
            .build(), CustomResourceOptions.builder()
                .deleteBeforeReplace(true)
                .build());

        ctx.export("summary", notes.output());
        ctx.export("usage", notes.usage());
    }
}
```
```yaml
name: claude-code
runtime: yaml
config:
  # A long-lived token from `claude setup-token`, which needs a Claude subscription. Set it with
  # `pulumi config set --secret claudeCodeOauthToken <token>`.
  claudeCodeOauthToken:
    type: string
    secret: true
resources:
  # The agent: Claude Code as a coding harness. It needs no model and no API key.
  coder:
    type: docker-agent:index:AgentConfig
    properties:
      agents:
        - name: root
          description: Maintains the notes of a repository with Claude Code
          harness:
            type: claude-code
            model: sonnet
            effort: high
  # Starts the claude CLI during `pulumi up` in the existing directory ./workspace, where Claude Code
  # runs its own tools (shell commands, file edits) without approval. Without `env`, it uses the
  # `claude` login of the machine that runs `pulumi up`.
  notes:
    type: docker-agent:index:AgentRun
    properties:
      yaml: ${coder.yaml}
      allowHarnessAgents: true
      workingDir: ./workspace
      prompt: Write NOTES.md with three bullet points about the files in this directory.
      # Sent on reruns while `prompt` is unchanged, for example after a change of `triggers`.
      updatePrompt: NOTES.md exists. Bring it up to date and keep its format.
      # Runs when the resource is deleted, for example by `pulumi destroy`. Like every run, it
      # needs a final answer.
      deletePrompt: "Delete NOTES.md if it exists, then answer: done."
      # The CLI gets the system variables and `env`, so an ANTHROPIC_API_KEY of the environment
      # cannot switch it to API billing.
      inheritEnvironment: false
      env:
        CLAUDE_CODE_OAUTH_TOKEN: ${claudeCodeOauthToken}
      timeoutSeconds: 900
    options:
      # A replacement deletes the old resource first, so that its deletePrompt does not undo
      # the new run.
      deleteBeforeReplace: true
outputs:
  # Claude Code's final result.
  summary: ${notes.output}
  usage: ${notes.usage}
```
```hcl
terraform {
  required_providers {
    docker-agent = {
      source = "pulumi/dirien/docker-agent"
    }
  }
}

# The agent: Claude Code as a coding harness. It needs no model and no API key.
resource "docker-agent_agent_config" "coder" {
  agents {
    name        = "root"
    description = "Maintains the notes of a repository with Claude Code"
    harness = {
      type   = "claude-code"
      model  = "sonnet"
      effort = "high"
    }
  }
}

# Starts the claude CLI during `pulumi up` in the existing directory ./workspace, where Claude Code
# runs its own tools (shell commands, file edits) without approval. Without `env`, it uses the
# `claude` login of the machine that runs `pulumi up`.
resource "docker-agent_agent_run" "notes" {
  yaml                 = docker-agent_agent_config.coder.yaml
  allow_harness_agents = true
  working_dir          = "./workspace"
  prompt               = "Write NOTES.md with three bullet points about the files in this directory."
  update_prompt        = "NOTES.md exists. Bring it up to date and keep its format."
  delete_prompt        = "Delete NOTES.md if it exists, then answer: done."
  inherit_environment  = false
  env = {
    "CLAUDE_CODE_OAUTH_TOKEN" = var.claudeCodeOauthToken
  }
  timeout_seconds = 900
}

# A long-lived token from `claude setup-token`, which needs a Claude subscription. Set it with
# `pulumi config set --secret claudeCodeOauthToken <token>`.
variable "claudeCodeOauthToken" {
  type = string
}

output "summary" {
  value = docker-agent_agent_run.notes.output
}

output "usage" {
  value = docker-agent_agent_run.notes.usage
}
```
{{% /example %}}
{{% example %}}
### A ChatGPT subscription model

```typescript
import * as pulumi from "@pulumi/pulumi";
import * as docker_agent from "@ediri/pulumi-docker-agent";

// A docker-agent agent on a ChatGPT plan (Plus, Pro or Business). Sign in once with
// `docker agent setup` (choose ChatGPT) on the machine that runs `pulumi up`; no API key.
const reviewer = new docker_agent.AgentConfig("reviewer", {agents: [{
    name: "root",
    model: "chatgpt/gpt-5.6",
    instruction: "You review Pulumi programs for security issues. Answer in at most five bullet points.",
    toolsets: [{
        type: "filesystem",
    }],
}]});

// Reads the files of ./infra with the read-only filesystem tools: toolApproval: deny (the
// default) rejects writes. The ChatGPT login is not an environment variable, so the run uses
// it also with inheritEnvironment: false.
const review = new docker_agent.AgentRun("review", {
    yaml: reviewer.yaml,
    workingDir: "./infra",
    prompt: "Review the Pulumi program in this directory.",
    inheritEnvironment: false,
});
export const findings = review.output;
export const usage = review.usage;
```
```python
import pulumi
import ediri_docker_agent as docker_agent

# A docker-agent agent on a ChatGPT plan (Plus, Pro or Business). Sign in once with
# `docker agent setup` (choose ChatGPT) on the machine that runs `pulumi up`; no API key.
reviewer = docker_agent.AgentConfig("reviewer", agents=[{
    "name": "root",
    "model": "chatgpt/gpt-5.6",
    "instruction": "You review Pulumi programs for security issues. Answer in at most five bullet points.",
    "toolsets": [{
        "type": "filesystem",
    }],
}])

# Reads the files of ./infra with the read-only filesystem tools: toolApproval: deny (the
# default) rejects writes. The ChatGPT login is not an environment variable, so the run uses
# it also with inheritEnvironment: false.
review = docker_agent.AgentRun("review",
    yaml=reviewer.yaml,
    working_dir="./infra",
    prompt="Review the Pulumi program in this directory.",
    inherit_environment=False)
pulumi.export("findings", review.output)
pulumi.export("usage", review.usage)
```
```go
package main

import (
	"github.com/dirien/pulumi-docker-agent/sdk/go/dockeragent"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		// A docker-agent agent on a ChatGPT plan (Plus, Pro or Business). Sign in once with
		// `docker agent setup` (choose ChatGPT) on the machine that runs `pulumi up`; no API key.
		reviewer, err := dockeragent.NewAgentConfig(ctx, "reviewer", &dockeragent.AgentConfigArgs{
			Agents: dockeragent.AgentArray{
				&dockeragent.AgentArgs{
					Name:        pulumi.String("root"),
					Model:       pulumi.String("chatgpt/gpt-5.6"),
					Instruction: pulumi.String("You review Pulumi programs for security issues. Answer in at most five bullet points."),
					Toolsets: dockeragent.ToolsetArray{
						&dockeragent.ToolsetArgs{
							Type: pulumi.String("filesystem"),
						},
					},
				},
			},
		})
		if err != nil {
			return err
		}

		// Reads the files of ./infra with the read-only filesystem tools: toolApproval: deny (the
		// default) rejects writes. The ChatGPT login is not an environment variable, so the run uses
		// it also with inheritEnvironment: false.
		review, err := dockeragent.NewAgentRun(ctx, "review", &dockeragent.AgentRunArgs{
			Yaml:               reviewer.Yaml,
			WorkingDir:         pulumi.String("./infra"),
			Prompt:             pulumi.String("Review the Pulumi program in this directory."),
			InheritEnvironment: pulumi.Bool(false),
		})
		if err != nil {
			return err
		}
		ctx.Export("findings", review.Output)
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
    // A docker-agent agent on a ChatGPT plan (Plus, Pro or Business). Sign in once with
    // `docker agent setup` (choose ChatGPT) on the machine that runs `pulumi up`; no API key.
    var reviewer = new DockerAgent.AgentConfig("reviewer", new()
    {
        Agents = new[]
        {
            new DockerAgent.Inputs.AgentArgs
            {
                Name = "root",
                Model = "chatgpt/gpt-5.6",
                Instruction = "You review Pulumi programs for security issues. Answer in at most five bullet points.",
                Toolsets = new[]
                {
                    new DockerAgent.Inputs.ToolsetArgs
                    {
                        Type = "filesystem",
                    },
                },
            },
        },
    });

    // Reads the files of ./infra with the read-only filesystem tools: toolApproval: deny (the
    // default) rejects writes. The ChatGPT login is not an environment variable, so the run uses
    // it also with inheritEnvironment: false.
    var review = new DockerAgent.AgentRun("review", new()
    {
        Yaml = reviewer.Yaml,
        WorkingDir = "./infra",
        Prompt = "Review the Pulumi program in this directory.",
        InheritEnvironment = false,
    });

    return new Dictionary<string, object?>
    {
        ["findings"] = review.Output,
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
import io.github.dirien.dockeragent.inputs.ToolsetArgs;
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
        // A docker-agent agent on a ChatGPT plan (Plus, Pro or Business). Sign in once with
        // `docker agent setup` (choose ChatGPT) on the machine that runs `pulumi up`; no API key.
        var reviewer = new AgentConfig("reviewer", AgentConfigArgs.builder()
            .agents(AgentArgs.builder()
                .name("root")
                .model("chatgpt/gpt-5.6")
                .instruction("You review Pulumi programs for security issues. Answer in at most five bullet points.")
                .toolsets(ToolsetArgs.builder()
                    .type("filesystem")
                    .build())
                .build())
            .build());

        // Reads the files of ./infra with the read-only filesystem tools: toolApproval: deny (the
        // default) rejects writes. The ChatGPT login is not an environment variable, so the run uses
        // it also with inheritEnvironment: false.
        var review = new AgentRun("review", AgentRunArgs.builder()
            .yaml(reviewer.yaml())
            .workingDir("./infra")
            .prompt("Review the Pulumi program in this directory.")
            .inheritEnvironment(false)
            .build());

        ctx.export("findings", review.output());
        ctx.export("usage", review.usage());
    }
}
```
```yaml
name: chatgpt-subscription
runtime: yaml
resources:
  # A docker-agent agent on a ChatGPT plan (Plus, Pro or Business). Sign in once with
  # `docker agent setup` (choose ChatGPT) on the machine that runs `pulumi up`; no API key.
  reviewer:
    type: docker-agent:index:AgentConfig
    properties:
      agents:
        - name: root
          model: chatgpt/gpt-5.6
          instruction: You review Pulumi programs for security issues. Answer in at most five bullet points.
          toolsets:
            - type: filesystem
  # Reads the files of ./infra with the read-only filesystem tools: toolApproval: deny (the
  # default) rejects writes. The ChatGPT login is not an environment variable, so the run uses
  # it also with inheritEnvironment: false.
  review:
    type: docker-agent:index:AgentRun
    properties:
      yaml: ${reviewer.yaml}
      workingDir: ./infra
      prompt: Review the Pulumi program in this directory.
      inheritEnvironment: false
outputs:
  findings: ${review.output}
  # The run is billed to the ChatGPT plan, so usage.cost is 0.
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

# A docker-agent agent on a ChatGPT plan (Plus, Pro or Business). Sign in once with
# `docker agent setup` (choose ChatGPT) on the machine that runs `pulumi up`; no API key.
resource "docker-agent_agent_config" "reviewer" {
  agents {
    name        = "root"
    model       = "chatgpt/gpt-5.6"
    instruction = "You review Pulumi programs for security issues. Answer in at most five bullet points."
    toolsets {
      type = "filesystem"
    }
  }
}

# Reads the files of ./infra with the read-only filesystem tools: toolApproval: deny (the
# default) rejects writes. The ChatGPT login is not an environment variable, so the run uses
# it also with inheritEnvironment: false.
resource "docker-agent_agent_run" "review" {
  yaml                = docker-agent_agent_config.reviewer.yaml
  working_dir         = "./infra"
  prompt              = "Review the Pulumi program in this directory."
  inherit_environment = false
}

output "findings" {
  value = docker-agent_agent_run.review.output
}

output "usage" {
  value = docker-agent_agent_run.review.usage
}
```
{{% /example %}}
{{% example %}}
### Codex on a ChatGPT plan

```typescript
import * as pulumi from "@pulumi/pulumi";
import * as docker_agent from "@ediri/pulumi-docker-agent";

const config = new pulumi.Config();

// A Codex home of AgentRun's own, logged in once with a ChatGPT plan:
// `mkdir ~/.codex-agentrun && CODEX_HOME=~/.codex-agentrun codex login`. Set it with
// `pulumi config set codexHome $HOME/.codex-agentrun`.
const codexHome = config.require("codexHome");

// The agent: Codex as a coding harness. Without harness.model, Codex uses the model of its
// config.toml, or its default.
const coder = new docker_agent.AgentConfig("coder", {agents: [{
    name: "root",
    description: "Maintains the notes of a repository with Codex",
    harness: {
        type: docker_agent.HarnessType.Codex,
    },
}]});

// Starts the codex CLI during `pulumi up` in the existing directory ./workspace. With
// toolApproval: deny (the default), Codex runs its commands in its workspace-write sandbox:
// they can write only in workingDir and the temporary directories and reach no network, but
// they can read every file of the user.
const notes = new docker_agent.AgentRun("notes", {
    yaml: coder.yaml,
    allowHarnessAgents: true,
    workingDir: "./workspace",
    prompt: "Write NOTES.md with three bullet points about the files in this directory.",
    updatePrompt: "NOTES.md exists. Bring it up to date and keep its format.",
    inheritEnvironment: false,
    env: {
        CODEX_HOME: codexHome,
    },
    timeoutSeconds: 900,
});
export const summary = notes.output;
export const usage = notes.usage;
```
```python
import pulumi
import ediri_docker_agent as docker_agent

config = pulumi.Config()

# A Codex home of AgentRun's own, logged in once with a ChatGPT plan:
# `mkdir ~/.codex-agentrun && CODEX_HOME=~/.codex-agentrun codex login`. Set it with
# `pulumi config set codexHome $HOME/.codex-agentrun`.
codex_home = config.require("codexHome")

# The agent: Codex as a coding harness. Without harness.model, Codex uses the model of its
# config.toml, or its default.
coder = docker_agent.AgentConfig("coder", agents=[{
    "name": "root",
    "description": "Maintains the notes of a repository with Codex",
    "harness": {
        "type": docker_agent.HarnessType.CODEX,
    },
}])

# Starts the codex CLI during `pulumi up` in the existing directory ./workspace. With
# toolApproval: deny (the default), Codex runs its commands in its workspace-write sandbox:
# they can write only in workingDir and the temporary directories and reach no network, but
# they can read every file of the user.
notes = docker_agent.AgentRun("notes",
    yaml=coder.yaml,
    allow_harness_agents=True,
    working_dir="./workspace",
    prompt="Write NOTES.md with three bullet points about the files in this directory.",
    update_prompt="NOTES.md exists. Bring it up to date and keep its format.",
    inherit_environment=False,
    env={
        "CODEX_HOME": codex_home,
    },
    timeout_seconds=900)
pulumi.export("summary", notes.output)
pulumi.export("usage", notes.usage)
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

		// A Codex home of AgentRun's own, logged in once with a ChatGPT plan:
		// `mkdir ~/.codex-agentrun && CODEX_HOME=~/.codex-agentrun codex login`. Set it with
		// `pulumi config set codexHome $HOME/.codex-agentrun`.
		codexHome := cfg.Require("codexHome")

		// The agent: Codex as a coding harness. Without harness.model, Codex uses the model of its
		// config.toml, or its default.
		coder, err := dockeragent.NewAgentConfig(ctx, "coder", &dockeragent.AgentConfigArgs{
			Agents: dockeragent.AgentArray{
				&dockeragent.AgentArgs{
					Name:        pulumi.String("root"),
					Description: pulumi.String("Maintains the notes of a repository with Codex"),
					Harness: &dockeragent.HarnessArgs{
						Type: dockeragent.HarnessTypeCodex,
					},
				},
			},
		})
		if err != nil {
			return err
		}

		// Starts the codex CLI during `pulumi up` in the existing directory ./workspace. With
		// toolApproval: deny (the default), Codex runs its commands in its workspace-write sandbox:
		// they can write only in workingDir and the temporary directories and reach no network, but
		// they can read every file of the user.
		notes, err := dockeragent.NewAgentRun(ctx, "notes", &dockeragent.AgentRunArgs{
			Yaml:               coder.Yaml,
			AllowHarnessAgents: pulumi.Bool(true),
			WorkingDir:         pulumi.String("./workspace"),
			Prompt:             pulumi.String("Write NOTES.md with three bullet points about the files in this directory."),
			UpdatePrompt:       pulumi.String("NOTES.md exists. Bring it up to date and keep its format."),
			InheritEnvironment: pulumi.Bool(false),
			Env: pulumi.StringMap{
				"CODEX_HOME": pulumi.String(codexHome),
			},
			TimeoutSeconds: pulumi.Int(900),
		})
		if err != nil {
			return err
		}
		ctx.Export("summary", notes.Output)
		ctx.Export("usage", notes.Usage)
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

    // A Codex home of AgentRun's own, logged in once with a ChatGPT plan:
    // `mkdir ~/.codex-agentrun && CODEX_HOME=~/.codex-agentrun codex login`. Set it with
    // `pulumi config set codexHome $HOME/.codex-agentrun`.
    var codexHome = config.Require("codexHome");

    // The agent: Codex as a coding harness. Without harness.model, Codex uses the model of its
    // config.toml, or its default.
    var coder = new DockerAgent.AgentConfig("coder", new()
    {
        Agents = new[]
        {
            new DockerAgent.Inputs.AgentArgs
            {
                Name = "root",
                Description = "Maintains the notes of a repository with Codex",
                Harness = new DockerAgent.Inputs.HarnessArgs
                {
                    Type = DockerAgent.HarnessType.Codex,
                },
            },
        },
    });

    // Starts the codex CLI during `pulumi up` in the existing directory ./workspace. With
    // toolApproval: deny (the default), Codex runs its commands in its workspace-write sandbox:
    // they can write only in workingDir and the temporary directories and reach no network, but
    // they can read every file of the user.
    var notes = new DockerAgent.AgentRun("notes", new()
    {
        Yaml = coder.Yaml,
        AllowHarnessAgents = true,
        WorkingDir = "./workspace",
        Prompt = "Write NOTES.md with three bullet points about the files in this directory.",
        UpdatePrompt = "NOTES.md exists. Bring it up to date and keep its format.",
        InheritEnvironment = false,
        Env =
        {
            { "CODEX_HOME", codexHome },
        },
        TimeoutSeconds = 900,
    });

    return new Dictionary<string, object?>
    {
        ["summary"] = notes.Output,
        ["usage"] = notes.Usage,
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
import io.github.dirien.dockeragent.inputs.HarnessArgs;
import io.github.dirien.dockeragent.AgentRun;
import io.github.dirien.dockeragent.AgentRunArgs;
import io.github.dirien.dockeragent.enums.HarnessType;
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

        // A Codex home of AgentRun's own, logged in once with a ChatGPT plan:
        // `mkdir ~/.codex-agentrun && CODEX_HOME=~/.codex-agentrun codex login`. Set it with
        // `pulumi config set codexHome $HOME/.codex-agentrun`.
        final var codexHome = config.require("codexHome");

        // The agent: Codex as a coding harness. Without harness.model, Codex uses the model of its
        // config.toml, or its default.
        var coder = new AgentConfig("coder", AgentConfigArgs.builder()
            .agents(AgentArgs.builder()
                .name("root")
                .description("Maintains the notes of a repository with Codex")
                .harness(HarnessArgs.builder()
                    .type(HarnessType.Codex)
                    .build())
                .build())
            .build());

        // Starts the codex CLI during `pulumi up` in the existing directory ./workspace. With
        // toolApproval: deny (the default), Codex runs its commands in its workspace-write sandbox:
        // they can write only in workingDir and the temporary directories and reach no network, but
        // they can read every file of the user.
        var notes = new AgentRun("notes", AgentRunArgs.builder()
            .yaml(coder.yaml())
            .allowHarnessAgents(true)
            .workingDir("./workspace")
            .prompt("Write NOTES.md with three bullet points about the files in this directory.")
            .updatePrompt("NOTES.md exists. Bring it up to date and keep its format.")
            .inheritEnvironment(false)
            .env(Map.of("CODEX_HOME", codexHome))
            .timeoutSeconds(900)
            .build());

        ctx.export("summary", notes.output());
        ctx.export("usage", notes.usage());
    }
}
```
```yaml
name: codex
runtime: yaml
config:
  # A Codex home of AgentRun's own, logged in once with a ChatGPT plan:
  # `mkdir ~/.codex-agentrun && CODEX_HOME=~/.codex-agentrun codex login`. Set it with
  # `pulumi config set codexHome $HOME/.codex-agentrun`.
  codexHome:
    type: string
resources:
  # The agent: Codex as a coding harness. Without harness.model, Codex uses the model of its
  # config.toml, or its default.
  coder:
    type: docker-agent:index:AgentConfig
    properties:
      agents:
        - name: root
          description: Maintains the notes of a repository with Codex
          harness:
            type: codex
  # Starts the codex CLI during `pulumi up` in the existing directory ./workspace. With
  # toolApproval: deny (the default), Codex runs its commands in its workspace-write sandbox:
  # they can write only in workingDir and the temporary directories and reach no network, but
  # they can read every file of the user.
  notes:
    type: docker-agent:index:AgentRun
    properties:
      yaml: ${coder.yaml}
      allowHarnessAgents: true
      workingDir: ./workspace
      prompt: Write NOTES.md with three bullet points about the files in this directory.
      # Sent on reruns while `prompt` is unchanged, for example after a change of `triggers`.
      updatePrompt: NOTES.md exists. Bring it up to date and keep its format.
      # The CLI gets the system variables, the Codex and network variables of the environment,
      # and `env`, so a CODEX_API_KEY of the environment cannot switch it to API billing.
      inheritEnvironment: false
      env:
        CODEX_HOME: ${codexHome}
      timeoutSeconds: 900
outputs:
  # Codex's final message.
  summary: ${notes.output}
  # usage.cost is 0 with a ChatGPT login.
  usage: ${notes.usage}
```
```hcl
terraform {
  required_providers {
    docker-agent = {
      source = "pulumi/dirien/docker-agent"
    }
  }
}

# The agent: Codex as a coding harness. Without harness.model, Codex uses the model of its
# config.toml, or its default.
resource "docker-agent_agent_config" "coder" {
  agents {
    name        = "root"
    description = "Maintains the notes of a repository with Codex"
    harness = {
      type = "codex"
    }
  }
}

# Starts the codex CLI during `pulumi up` in the existing directory ./workspace. With
# toolApproval: deny (the default), Codex runs its commands in its workspace-write sandbox:
# they can write only in workingDir and the temporary directories and reach no network, but
# they can read every file of the user.
resource "docker-agent_agent_run" "notes" {
  yaml                 = docker-agent_agent_config.coder.yaml
  allow_harness_agents = true
  working_dir          = "./workspace"
  prompt               = "Write NOTES.md with three bullet points about the files in this directory."
  update_prompt        = "NOTES.md exists. Bring it up to date and keep its format."
  inherit_environment  = false
  env = {
    "CODEX_HOME" = var.codexHome
  }
  timeout_seconds = 900
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

output "usage" {
  value = docker-agent_agent_run.notes.usage
}
```
{{% /example %}}
{{% example %}}
### One agent, several runs

```typescript
import * as pulumi from "@pulumi/pulumi";
import * as docker_agent from "@ediri/pulumi-docker-agent";

const config = new pulumi.Config();

// Set it with `pulumi config set --secret openaiApiKey <key>`.
const openaiApiKey = config.requireSecret("openaiApiKey");

// The agent, authored once.
const writer = new docker_agent.AgentConfig("writer", {agents: [{
    name: "root",
    model: "openai/gpt-5-mini",
    instruction: "You write short, precise texts about infrastructure changes.",
}]});

// Run 1.
const summary = new docker_agent.AgentRun("summary", {
    yaml: writer.yaml,
    prompt: "Explain in two sentences why a team upgrades PostgreSQL from version 15 to 17.",
    env: {
        OPENAI_API_KEY: openaiApiKey,
    },
});

// Run 2: the same agent with another prompt. It uses the answer of run 1 and runs again whenever
// run 1 ran, also when that answer did not change.
const translation = new docker_agent.AgentRun("translation", {
    yaml: writer.yaml,
    prompt: pulumi.interpolate`Translate this text into German: ${summary.output}`,
    triggers: [summary.ranAt],
    env: {
        OPENAI_API_KEY: openaiApiKey,
    },
});
export const summaryText = summary.output;
export const translationText = translation.output;
```
```python
import pulumi
import ediri_docker_agent as docker_agent

config = pulumi.Config()

# Set it with `pulumi config set --secret openaiApiKey <key>`.
openai_api_key = config.require_secret("openaiApiKey")

# The agent, authored once.
writer = docker_agent.AgentConfig("writer", agents=[{
    "name": "root",
    "model": "openai/gpt-5-mini",
    "instruction": "You write short, precise texts about infrastructure changes.",
}])

# Run 1.
summary = docker_agent.AgentRun("summary",
    yaml=writer.yaml,
    prompt="Explain in two sentences why a team upgrades PostgreSQL from version 15 to 17.",
    env={
        "OPENAI_API_KEY": openai_api_key,
    })

# Run 2: the same agent with another prompt. It uses the answer of run 1 and runs again whenever
# run 1 ran, also when that answer did not change.
translation = docker_agent.AgentRun("translation",
    yaml=writer.yaml,
    prompt=summary.output.apply(lambda output: f"Translate this text into German: {output}"),
    triggers=[summary.ran_at],
    env={
        "OPENAI_API_KEY": openai_api_key,
    })
pulumi.export("summaryText", summary.output)
pulumi.export("translationText", translation.output)
```
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

		// Set it with `pulumi config set --secret openaiApiKey <key>`.
		openaiApiKey := cfg.RequireSecret("openaiApiKey")

		// The agent, authored once.
		writer, err := dockeragent.NewAgentConfig(ctx, "writer", &dockeragent.AgentConfigArgs{
			Agents: dockeragent.AgentArray{
				&dockeragent.AgentArgs{
					Name:        pulumi.String("root"),
					Model:       pulumi.String("openai/gpt-5-mini"),
					Instruction: pulumi.String("You write short, precise texts about infrastructure changes."),
				},
			},
		})
		if err != nil {
			return err
		}

		// Run 1.
		summary, err := dockeragent.NewAgentRun(ctx, "summary", &dockeragent.AgentRunArgs{
			Yaml:   writer.Yaml,
			Prompt: pulumi.String("Explain in two sentences why a team upgrades PostgreSQL from version 15 to 17."),
			Env: pulumi.StringMap{
				"OPENAI_API_KEY": openaiApiKey,
			},
		})
		if err != nil {
			return err
		}

		// Run 2: the same agent with another prompt. It uses the answer of run 1 and runs again whenever
		// run 1 ran, also when that answer did not change.
		translation, err := dockeragent.NewAgentRun(ctx, "translation", &dockeragent.AgentRunArgs{
			Yaml: writer.Yaml,
			Prompt: summary.Output.ApplyT(func(output string) (string, error) {
				return fmt.Sprintf("Translate this text into German: %v", output), nil
			}).(pulumi.StringOutput),
			Triggers: pulumi.Array{
				summary.RanAt,
			},
			Env: pulumi.StringMap{
				"OPENAI_API_KEY": openaiApiKey,
			},
		})
		if err != nil {
			return err
		}
		ctx.Export("summaryText", summary.Output)
		ctx.Export("translationText", translation.Output)
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

    // The agent, authored once.
    var writer = new DockerAgent.AgentConfig("writer", new()
    {
        Agents = new[]
        {
            new DockerAgent.Inputs.AgentArgs
            {
                Name = "root",
                Model = "openai/gpt-5-mini",
                Instruction = "You write short, precise texts about infrastructure changes.",
            },
        },
    });

    // Run 1.
    var summary = new DockerAgent.AgentRun("summary", new()
    {
        Yaml = writer.Yaml,
        Prompt = "Explain in two sentences why a team upgrades PostgreSQL from version 15 to 17.",
        Env =
        {
            { "OPENAI_API_KEY", openaiApiKey },
        },
    });

    // Run 2: the same agent with another prompt. It uses the answer of run 1 and runs again whenever
    // run 1 ran, also when that answer did not change.
    var translation = new DockerAgent.AgentRun("translation", new()
    {
        Yaml = writer.Yaml,
        Prompt = summary.Output.Apply(output => $"Translate this text into German: {output}"),
        Triggers = new[]
        {
            summary.RanAt,
        },
        Env =
        {
            { "OPENAI_API_KEY", openaiApiKey },
        },
    });

    return new Dictionary<string, object?>
    {
        ["summaryText"] = summary.Output,
        ["translationText"] = translation.Output,
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

        // The agent, authored once.
        var writer = new AgentConfig("writer", AgentConfigArgs.builder()
            .agents(AgentArgs.builder()
                .name("root")
                .model("openai/gpt-5-mini")
                .instruction("You write short, precise texts about infrastructure changes.")
                .build())
            .build());

        // Run 1.
        var summary = new AgentRun("summary", AgentRunArgs.builder()
            .yaml(writer.yaml())
            .prompt("Explain in two sentences why a team upgrades PostgreSQL from version 15 to 17.")
            .env(openaiApiKey.applyValue(_openaiApiKey -> Map.of("OPENAI_API_KEY", _openaiApiKey)))
            .build());

        // Run 2: the same agent with another prompt. It uses the answer of run 1 and runs again whenever
        // run 1 ran, also when that answer did not change.
        var translation = new AgentRun("translation", AgentRunArgs.builder()
            .yaml(writer.yaml())
            .prompt(summary.output().applyValue(_output -> String.format("Translate this text into German: %s", _output)))
            .triggers(summary.ranAt())
            .env(openaiApiKey.applyValue(_openaiApiKey -> Map.of("OPENAI_API_KEY", _openaiApiKey)))
            .build());

        ctx.export("summaryText", summary.output());
        ctx.export("translationText", translation.output());
    }
}
```
```yaml
name: several-runs
runtime: yaml
config:
  # Set it with `pulumi config set --secret openaiApiKey <key>`.
  openaiApiKey:
    type: string
    secret: true
resources:
  # The agent, authored once.
  writer:
    type: docker-agent:index:AgentConfig
    properties:
      agents:
        - name: root
          model: openai/gpt-5-mini
          instruction: You write short, precise texts about infrastructure changes.
  # Run 1.
  summary:
    type: docker-agent:index:AgentRun
    properties:
      yaml: ${writer.yaml}
      prompt: Explain in two sentences why a team upgrades PostgreSQL from version 15 to 17.
      env:
        OPENAI_API_KEY: ${openaiApiKey}
  # Run 2: the same agent with another prompt. It uses the answer of run 1 and runs again whenever
  # run 1 ran, also when that answer did not change.
  translation:
    type: docker-agent:index:AgentRun
    properties:
      yaml: ${writer.yaml}
      prompt: "Translate this text into German: ${summary.output}"
      triggers:
        - ${summary.ranAt}
      env:
        OPENAI_API_KEY: ${openaiApiKey}
outputs:
  summaryText: ${summary.output}
  translationText: ${translation.output}
```
```hcl
terraform {
  required_providers {
    docker-agent = {
      source = "pulumi/dirien/docker-agent"
    }
  }
}

# The agent, authored once.
resource "docker-agent_agent_config" "writer" {
  agents {
    name        = "root"
    model       = "openai/gpt-5-mini"
    instruction = "You write short, precise texts about infrastructure changes."
  }
}

# Run 1.
resource "docker-agent_agent_run" "summary" {
  yaml   = docker-agent_agent_config.writer.yaml
  prompt = "Explain in two sentences why a team upgrades PostgreSQL from version 15 to 17."
  env = {
    "OPENAI_API_KEY" = var.openaiApiKey
  }
}

# Run 2: the same agent with another prompt. It uses the answer of run 1 and runs again whenever
# run 1 ran, also when that answer did not change.
resource "docker-agent_agent_run" "translation" {
  yaml     = docker-agent_agent_config.writer.yaml
  prompt   = "Translate this text into German: ${docker-agent_agent_run.summary.output}"
  triggers = [docker-agent_agent_run.summary.ran_at]
  env = {
    "OPENAI_API_KEY" = var.openaiApiKey
  }
}

# Set it with `pulumi config set --secret openaiApiKey <key>`.
variable "openaiApiKey" {
  type = string
}

output "summaryText" {
  value = docker-agent_agent_run.summary.output
}

output "translationText" {
  value = docker-agent_agent_run.translation.output
}
```
{{% /example %}}
{{% /examples %}}
