{{% examples %}}
## Example Usage
{{% example %}}
### A single agent with tools

```typescript
import * as pulumi from "@pulumi/pulumi";
import * as docker_agent from "@ediri/pulumi-docker-agent";

// A typed docker-agent configuration, validated during `pulumi preview`.
const assistant = new docker_agent.AgentConfig("assistant", {
    metadata: {
        author: "platform-team",
        license: "Apache-2.0",
    },
    agents: [{
        name: "root",
        model: "openai/gpt-5-mini",
        description: "A helpful assistant",
        instruction: "You are a helpful assistant. Answer briefly.",
        toolsets: [
            {
                type: "think",
            },
            {
                type: "mcp",
                ref: "docker:duckduckgo",
            },
        ],
    }],
});
export const agentYaml = assistant.yaml;
export const requiredEnvVars = assistant.requiredEnvVars;
```
```python
import pulumi
import ediri_docker_agent as docker_agent

# A typed docker-agent configuration, validated during `pulumi preview`.
assistant = docker_agent.AgentConfig("assistant",
    metadata={
        "author": "platform-team",
        "license": "Apache-2.0",
    },
    agents=[{
        "name": "root",
        "model": "openai/gpt-5-mini",
        "description": "A helpful assistant",
        "instruction": "You are a helpful assistant. Answer briefly.",
        "toolsets": [
            {
                "type": "think",
            },
            {
                "type": "mcp",
                "ref": "docker:duckduckgo",
            },
        ],
    }])
pulumi.export("agentYaml", assistant.yaml)
pulumi.export("requiredEnvVars", assistant.required_env_vars)
```
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
			Metadata: &dockeragent.MetadataArgs{
				Author:  pulumi.String("platform-team"),
				License: pulumi.String("Apache-2.0"),
			},
			Agents: dockeragent.AgentArray{
				&dockeragent.AgentArgs{
					Name:        pulumi.String("root"),
					Model:       pulumi.String("openai/gpt-5-mini"),
					Description: pulumi.String("A helpful assistant"),
					Instruction: pulumi.String("You are a helpful assistant. Answer briefly."),
					Toolsets: dockeragent.ToolsetArray{
						&dockeragent.ToolsetArgs{
							Type: pulumi.String("think"),
						},
						&dockeragent.ToolsetArgs{
							Type: pulumi.String("mcp"),
							Ref:  pulumi.String("docker:duckduckgo"),
						},
					},
				},
			},
		})
		if err != nil {
			return err
		}
		ctx.Export("agentYaml", assistant.Yaml)
		ctx.Export("requiredEnvVars", assistant.RequiredEnvVars)
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
    // A typed docker-agent configuration, validated during `pulumi preview`.
    var assistant = new DockerAgent.AgentConfig("assistant", new()
    {
        Metadata = new DockerAgent.Inputs.MetadataArgs
        {
            Author = "platform-team",
            License = "Apache-2.0",
        },
        Agents = new[]
        {
            new DockerAgent.Inputs.AgentArgs
            {
                Name = "root",
                Model = "openai/gpt-5-mini",
                Description = "A helpful assistant",
                Instruction = "You are a helpful assistant. Answer briefly.",
                Toolsets = new[]
                {
                    new DockerAgent.Inputs.ToolsetArgs
                    {
                        Type = "think",
                    },
                    new DockerAgent.Inputs.ToolsetArgs
                    {
                        Type = "mcp",
                        Ref = "docker:duckduckgo",
                    },
                },
            },
        },
    });

    return new Dictionary<string, object?>
    {
        ["agentYaml"] = assistant.Yaml,
        ["requiredEnvVars"] = assistant.RequiredEnvVars,
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
import io.github.dirien.dockeragent.inputs.MetadataArgs;
import io.github.dirien.dockeragent.inputs.AgentArgs;
import io.github.dirien.dockeragent.inputs.ToolsetArgs;
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
            .metadata(MetadataArgs.builder()
                .author("platform-team")
                .license("Apache-2.0")
                .build())
            .agents(AgentArgs.builder()
                .name("root")
                .model("openai/gpt-5-mini")
                .description("A helpful assistant")
                .instruction("You are a helpful assistant. Answer briefly.")
                .toolsets(
                    ToolsetArgs.builder()
                        .type("think")
                        .build(),
                    ToolsetArgs.builder()
                        .type("mcp")
                        .ref("docker:duckduckgo")
                        .build())
                .build())
            .build());

        ctx.export("agentYaml", assistant.yaml());
        ctx.export("requiredEnvVars", assistant.requiredEnvVars());
    }
}
```
```yaml
name: assistant
runtime: yaml
resources:
  # A typed docker-agent configuration, validated during `pulumi preview`.
  assistant:
    type: docker-agent:index:AgentConfig
    properties:
      metadata:
        author: platform-team
        license: Apache-2.0
      agents:
        - name: root
          model: openai/gpt-5-mini
          description: A helpful assistant
          instruction: You are a helpful assistant. Answer briefly.
          toolsets:
            - type: think
            - type: mcp
              ref: docker:duckduckgo
outputs:
  agentYaml: ${assistant.yaml}
  requiredEnvVars: ${assistant.requiredEnvVars}
```
```hcl
terraform {
  required_providers {
    docker-agent = {
      source = "pulumi/dirien/docker-agent"
    }
  }
}

# A typed docker-agent configuration, validated during `pulumi preview`.
resource "docker-agent_agent_config" "assistant" {
  metadata = {
    author  = "platform-team"
    license = "Apache-2.0"
  }
  agents {
    name        = "root"
    model       = "openai/gpt-5-mini"
    description = "A helpful assistant"
    instruction = "You are a helpful assistant. Answer briefly."
    toolsets {
      type = "think"
    }
    toolsets {
      type = "mcp"
      ref  = "docker:duckduckgo"
    }
  }
}

output "agentYaml" {
  value = docker-agent_agent_config.assistant.yaml
}

output "requiredEnvVars" {
  value = docker-agent_agent_config.assistant.required_env_vars
}
```
{{% /example %}}
{{% example %}}
### A team with a named model

```typescript
import * as pulumi from "@pulumi/pulumi";
import * as docker_agent from "@ediri/pulumi-docker-agent";

// `root` delegates research questions to the `researcher` sub-agent.
const team = new docker_agent.AgentConfig("team", {
    models: {
        fast: {
            provider: "anthropic",
            model: "claude-haiku-4-5",
            maxTokens: 8192,
        },
    },
    agents: [
        {
            name: "root",
            model: "fast",
            instruction: "Coordinate the team and delegate research questions.",
            subAgents: ["researcher"],
        },
        {
            name: "researcher",
            model: "openai/gpt-5-mini",
            description: "Searches the web",
            instruction: "Research the question and report sources.",
            toolsets: [{
                type: "fetch",
            }],
            maxConsecutiveToolCalls: 10,
        },
    ],
});
export const agentNames = team.agentNames;
export const defaultAgent = team.defaultAgent;
```
```python
import pulumi
import ediri_docker_agent as docker_agent

# `root` delegates research questions to the `researcher` sub-agent.
team = docker_agent.AgentConfig("team",
    models={
        "fast": {
            "provider": "anthropic",
            "model": "claude-haiku-4-5",
            "max_tokens": 8192,
        },
    },
    agents=[
        {
            "name": "root",
            "model": "fast",
            "instruction": "Coordinate the team and delegate research questions.",
            "sub_agents": ["researcher"],
        },
        {
            "name": "researcher",
            "model": "openai/gpt-5-mini",
            "description": "Searches the web",
            "instruction": "Research the question and report sources.",
            "toolsets": [{
                "type": "fetch",
            }],
            "max_consecutive_tool_calls": 10,
        },
    ])
pulumi.export("agentNames", team.agent_names)
pulumi.export("defaultAgent", team.default_agent)
```
```go
package main

import (
	"github.com/dirien/pulumi-docker-agent/sdk/go/dockeragent"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		// `root` delegates research questions to the `researcher` sub-agent.
		team, err := dockeragent.NewAgentConfig(ctx, "team", &dockeragent.AgentConfigArgs{
			Models: dockeragent.ModelDefMap{
				"fast": &dockeragent.ModelDefArgs{
					Provider:  pulumi.String("anthropic"),
					Model:     pulumi.String("claude-haiku-4-5"),
					MaxTokens: pulumi.Int(8192),
				},
			},
			Agents: dockeragent.AgentArray{
				&dockeragent.AgentArgs{
					Name:        pulumi.String("root"),
					Model:       pulumi.String("fast"),
					Instruction: pulumi.String("Coordinate the team and delegate research questions."),
					SubAgents: pulumi.StringArray{
						pulumi.String("researcher"),
					},
				},
				&dockeragent.AgentArgs{
					Name:        pulumi.String("researcher"),
					Model:       pulumi.String("openai/gpt-5-mini"),
					Description: pulumi.String("Searches the web"),
					Instruction: pulumi.String("Research the question and report sources."),
					Toolsets: dockeragent.ToolsetArray{
						&dockeragent.ToolsetArgs{
							Type: pulumi.String("fetch"),
						},
					},
					MaxConsecutiveToolCalls: pulumi.Int(10),
				},
			},
		})
		if err != nil {
			return err
		}
		ctx.Export("agentNames", team.AgentNames)
		ctx.Export("defaultAgent", team.DefaultAgent)
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
    // `root` delegates research questions to the `researcher` sub-agent.
    var team = new DockerAgent.AgentConfig("team", new()
    {
        Models =
        {
            { "fast", new DockerAgent.Inputs.ModelDefArgs
            {
                Provider = "anthropic",
                Model = "claude-haiku-4-5",
                MaxTokens = 8192,
            } },
        },
        Agents = new[]
        {
            new DockerAgent.Inputs.AgentArgs
            {
                Name = "root",
                Model = "fast",
                Instruction = "Coordinate the team and delegate research questions.",
                SubAgents = new[]
                {
                    "researcher",
                },
            },
            new DockerAgent.Inputs.AgentArgs
            {
                Name = "researcher",
                Model = "openai/gpt-5-mini",
                Description = "Searches the web",
                Instruction = "Research the question and report sources.",
                Toolsets = new[]
                {
                    new DockerAgent.Inputs.ToolsetArgs
                    {
                        Type = "fetch",
                    },
                },
                MaxConsecutiveToolCalls = 10,
            },
        },
    });

    return new Dictionary<string, object?>
    {
        ["agentNames"] = team.AgentNames,
        ["defaultAgent"] = team.DefaultAgent,
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
import io.github.dirien.dockeragent.inputs.ModelDefArgs;
import io.github.dirien.dockeragent.inputs.AgentArgs;
import io.github.dirien.dockeragent.inputs.ToolsetArgs;
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
        // `root` delegates research questions to the `researcher` sub-agent.
        var team = new AgentConfig("team", AgentConfigArgs.builder()
            .models(Map.of("fast", ModelDefArgs.builder()
                .provider("anthropic")
                .model("claude-haiku-4-5")
                .maxTokens(8192)
                .build()))
            .agents(
                AgentArgs.builder()
                    .name("root")
                    .model("fast")
                    .instruction("Coordinate the team and delegate research questions.")
                    .subAgents("researcher")
                    .build(),
                AgentArgs.builder()
                    .name("researcher")
                    .model("openai/gpt-5-mini")
                    .description("Searches the web")
                    .instruction("Research the question and report sources.")
                    .toolsets(ToolsetArgs.builder()
                        .type("fetch")
                        .build())
                    .maxConsecutiveToolCalls(10)
                    .build())
            .build());

        ctx.export("agentNames", team.agentNames());
        ctx.export("defaultAgent", team.defaultAgent());
    }
}
```
```yaml
name: team
runtime: yaml
resources:
  # `root` delegates research questions to the `researcher` sub-agent.
  team:
    type: docker-agent:index:AgentConfig
    properties:
      models:
        fast:
          provider: anthropic
          model: claude-haiku-4-5
          maxTokens: 8192
      agents:
        - name: root
          model: fast
          instruction: Coordinate the team and delegate research questions.
          subAgents:
            - researcher
        - name: researcher
          model: openai/gpt-5-mini
          description: Searches the web
          instruction: Research the question and report sources.
          toolsets:
            - type: fetch
          maxConsecutiveToolCalls: 10
outputs:
  agentNames: ${team.agentNames}
  defaultAgent: ${team.defaultAgent}
```
```hcl
terraform {
  required_providers {
    docker-agent = {
      source = "pulumi/dirien/docker-agent"
    }
  }
}

# `root` delegates research questions to the `researcher` sub-agent.
resource "docker-agent_agent_config" "team" {
  models = {
    "fast" = {
      provider   = "anthropic"
      model      = "claude-haiku-4-5"
      max_tokens = 8192
    }
  }
  agents {
    name        = "root"
    model       = "fast"
    instruction = "Coordinate the team and delegate research questions."
    sub_agents  = ["researcher"]
  }
  agents {
    name        = "researcher"
    model       = "openai/gpt-5-mini"
    description = "Searches the web"
    instruction = "Research the question and report sources."
    toolsets {
      type = "fetch"
    }
    max_consecutive_tool_calls = 10
  }
}

output "agentNames" {
  value = docker-agent_agent_config.team.agent_names
}

output "defaultAgent" {
  value = docker-agent_agent_config.team.default_agent
}
```
{{% /example %}}
{{% example %}}
### Sandbox mode, a safety mode, hooks and a budget

```typescript
import * as pulumi from "@pulumi/pulumi";
import * as docker_agent from "@ediri/pulumi-docker-agent";

// Runs in a sandbox by default, with the fail-closed `restricted` safety mode.
const sandboxed = new docker_agent.AgentConfig("sandboxed", {
    runtime: {
        sandbox: true,
        safety: docker_agent.SafetyMode.Restricted,
        networkAllowlist: [
            "api.github.com",
            "registry.npmjs.org:443",
        ],
    },
    permissions: {
        deny: ["shell:cmd=sudo*"],
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
export const agentYaml = sandboxed.yaml;
```
```python
import pulumi
import ediri_docker_agent as docker_agent

# Runs in a sandbox by default, with the fail-closed `restricted` safety mode.
sandboxed = docker_agent.AgentConfig("sandboxed",
    runtime={
        "sandbox": True,
        "safety": docker_agent.SafetyMode.RESTRICTED,
        "network_allowlist": [
            "api.github.com",
            "registry.npmjs.org:443",
        ],
    },
    permissions={
        "deny": ["shell:cmd=sudo*"],
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
pulumi.export("agentYaml", sandboxed.yaml)
```
```go
package main

import (
	"github.com/dirien/pulumi-docker-agent/sdk/go/dockeragent"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		// Runs in a sandbox by default, with the fail-closed `restricted` safety mode.
		sandboxed, err := dockeragent.NewAgentConfig(ctx, "sandboxed", &dockeragent.AgentConfigArgs{
			Runtime: &dockeragent.RuntimeDefaultsArgs{
				Sandbox: pulumi.Bool(true),
				Safety:  dockeragent.SafetyModeRestricted,
				NetworkAllowlist: pulumi.StringArray{
					pulumi.String("api.github.com"),
					pulumi.String("registry.npmjs.org:443"),
				},
			},
			Permissions: &dockeragent.PermissionsArgs{
				Deny: pulumi.StringArray{
					pulumi.String("shell:cmd=sudo*"),
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
		ctx.Export("agentYaml", sandboxed.Yaml)
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
    // Runs in a sandbox by default, with the fail-closed `restricted` safety mode.
    var sandboxed = new DockerAgent.AgentConfig("sandboxed", new()
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
        Permissions = new DockerAgent.Inputs.PermissionsArgs
        {
            Deny = new[]
            {
                "shell:cmd=sudo*",
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
        ["agentYaml"] = sandboxed.Yaml,
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
import io.github.dirien.dockeragent.inputs.RuntimeDefaultsArgs;
import io.github.dirien.dockeragent.inputs.PermissionsArgs;
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
        // Runs in a sandbox by default, with the fail-closed `restricted` safety mode.
        var sandboxed = new AgentConfig("sandboxed", AgentConfigArgs.builder()
            .runtime(RuntimeDefaultsArgs.builder()
                .sandbox(true)
                .safety(SafetyMode.Restricted)
                .networkAllowlist(
                    "api.github.com",
                    "registry.npmjs.org:443")
                .build())
            .permissions(PermissionsArgs.builder()
                .deny("shell:cmd=sudo*")
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

        ctx.export("agentYaml", sandboxed.yaml());
    }
}
```
```yaml
name: sandboxed
runtime: yaml
resources:
  # Runs in a sandbox by default, with the fail-closed `restricted` safety mode.
  sandboxed:
    type: docker-agent:index:AgentConfig
    properties:
      runtime:
        sandbox: true
        safety: restricted
        networkAllowlist:
          - api.github.com
          - registry.npmjs.org:443
      permissions:
        deny:
          - shell:cmd=sudo*
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
  agentYaml: ${sandboxed.yaml}
```
```hcl
terraform {
  required_providers {
    docker-agent = {
      source = "pulumi/dirien/docker-agent"
    }
  }
}

# Runs in a sandbox by default, with the fail-closed `restricted` safety mode.
resource "docker-agent_agent_config" "sandboxed" {
  runtime = {
    sandbox           = true
    safety            = "restricted"
    network_allowlist = ["api.github.com", "registry.npmjs.org:443"]
  }
  permissions = {
    deny = ["shell:cmd=sudo*"]
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
  value = docker-agent_agent_config.sandboxed.yaml
}
```
{{% /example %}}
{{% /examples %}}
