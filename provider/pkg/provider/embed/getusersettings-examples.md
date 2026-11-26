{{% examples %}}
## Example Usage
{{% example %}}
### Read the user configuration

```typescript
import * as pulumi from "@pulumi/pulumi";
import * as docker_agent from "@ediri/pulumi-docker-agent";

// Reads ~/.config/cagent/config.yaml without managing it. A missing file returns exists: false.
const current = docker_agent.getUserSettingsOutput({});
export const configExists = current.exists;
export const filePath = current.filePath;
export const sha256 = current.sha256;
export const aliases = current.aliases;
```
```python
import pulumi
import ediri_docker_agent as docker_agent

# Reads ~/.config/cagent/config.yaml without managing it. A missing file returns exists: false.
current = docker_agent.get_user_settings_output()
pulumi.export("configExists", current.exists)
pulumi.export("filePath", current.file_path)
pulumi.export("sha256", current.sha256)
pulumi.export("aliases", current.aliases)
```
```go
package main

import (
	"github.com/dirien/pulumi-docker-agent/sdk/go/dockeragent"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		// Reads ~/.config/cagent/config.yaml without managing it. A missing file returns exists: false.
		current := dockeragent.LookupUserSettingsOutput(ctx, dockeragent.LookupUserSettingsOutputArgs{}, nil)
		ctx.Export("configExists", current.Exists())
		ctx.Export("filePath", current.FilePath())
		ctx.Export("sha256", current.Sha256())
		ctx.Export("aliases", current.Aliases())
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
    // Reads ~/.config/cagent/config.yaml without managing it. A missing file returns exists: false.
    var current = DockerAgent.GetUserSettings.Invoke();

    return new Dictionary<string, object?>
    {
        ["configExists"] = current.Apply(getUserSettingsResult => getUserSettingsResult.Exists),
        ["filePath"] = current.Apply(getUserSettingsResult => getUserSettingsResult.FilePath),
        ["sha256"] = current.Apply(getUserSettingsResult => getUserSettingsResult.Sha256),
        ["aliases"] = current.Apply(getUserSettingsResult => getUserSettingsResult.Aliases),
    };
});
```
```java
package generated_program;

import com.pulumi.Context;
import com.pulumi.Pulumi;
import com.pulumi.core.Output;
import io.github.dirien.dockeragent.DockeragentFunctions;
import io.github.dirien.dockeragent.inputs.GetUserSettingsArgs;
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
        // Reads ~/.config/cagent/config.yaml without managing it. A missing file returns exists: false.
        final var current = DockeragentFunctions.getUserSettings(GetUserSettingsArgs.builder()
            .build());

        ctx.export("configExists", current.applyValue(_current -> _current.exists()));
        ctx.export("filePath", current.applyValue(_current -> _current.filePath()));
        ctx.export("sha256", current.applyValue(_current -> _current.sha256()));
        ctx.export("aliases", current.applyValue(_current -> _current.aliases()));
    }
}
```
```yaml
name: read-user-settings
runtime: yaml
variables:
  # Reads ~/.config/cagent/config.yaml without managing it. A missing file returns exists: false.
  current:
    fn::invoke:
      function: docker-agent:index:getUserSettings
      arguments: {}
outputs:
  configExists: ${current.exists}
  filePath: ${current.filePath}
  sha256: ${current.sha256}
  aliases: ${current.aliases}
```
```hcl
terraform {
  required_providers {
    docker-agent = {
      source = "pulumi/dirien/docker-agent"
    }
  }
}

# Reads ~/.config/cagent/config.yaml without managing it. A missing file returns exists: false.
data "docker-agent_user_settings" "current" {
}

output "configExists" {
  value = data.docker-agent_user_settings.current.exists
}

output "filePath" {
  value = data.docker-agent_user_settings.current.file_path
}

output "sha256" {
  value = data.docker-agent_user_settings.current.sha256
}

output "aliases" {
  value = data.docker-agent_user_settings.current.aliases
}
```
{{% /example %}}
{{% example %}}
### Reuse the default model of a configuration file

```typescript
import * as pulumi from "@pulumi/pulumi";
import * as docker_agent from "@ediri/pulumi-docker-agent";

// Relative paths are relative to the Pulumi project directory.
const team = docker_agent.getUserSettingsOutput({
    path: "./team-config.yaml",
});
const assistant = new docker_agent.AgentConfig("assistant", {
    models: {
        main: {
            provider: team.apply(team => team.defaultModel?.provider),
            model: team.apply(team => team.defaultModel?.model),
        },
    },
    agents: [{
        name: "root",
        model: "main",
        instruction: "You are a helpful assistant. Answer briefly.",
    }],
});
export const sha256 = team.sha256;
export const theme = team.apply(team => team.settings?.theme);
export const agentYaml = assistant.yaml;
```
```python
import pulumi
import ediri_docker_agent as docker_agent

# Relative paths are relative to the Pulumi project directory.
team = docker_agent.get_user_settings_output(path="./team-config.yaml")
assistant = docker_agent.AgentConfig("assistant",
    models={
        "main": {
            "provider": team.default_model.provider,
            "model": team.default_model.model,
        },
    },
    agents=[{
        "name": "root",
        "model": "main",
        "instruction": "You are a helpful assistant. Answer briefly.",
    }])
pulumi.export("sha256", team.sha256)
pulumi.export("theme", team.settings.theme)
pulumi.export("agentYaml", assistant.yaml)
```
```go
package main

import (
	"github.com/dirien/pulumi-docker-agent/sdk/go/dockeragent"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		// Relative paths are relative to the Pulumi project directory.
		team := dockeragent.LookupUserSettingsOutput(ctx, dockeragent.LookupUserSettingsOutputArgs{
			Path: pulumi.String("./team-config.yaml"),
		}, nil)
		assistant, err := dockeragent.NewAgentConfig(ctx, "assistant", &dockeragent.AgentConfigArgs{
			Models: dockeragent.ModelDefMap{
				"main": &dockeragent.ModelDefArgs{
					Provider: team.ApplyT(func(team dockeragent.LookupUserSettingsResult) (*string, error) {
						return team.DefaultModel.Provider, nil
					}).(pulumi.StringPtrOutput),
					Model: team.ApplyT(func(team dockeragent.LookupUserSettingsResult) (*string, error) {
						return team.DefaultModel.Model, nil
					}).(pulumi.StringPtrOutput),
				},
			},
			Agents: dockeragent.AgentArray{
				&dockeragent.AgentArgs{
					Name:        pulumi.String("root"),
					Model:       pulumi.String("main"),
					Instruction: pulumi.String("You are a helpful assistant. Answer briefly."),
				},
			},
		})
		if err != nil {
			return err
		}
		ctx.Export("sha256", team.Sha256())
		ctx.Export("theme", team.ApplyT(func(team dockeragent.LookupUserSettingsResult) (*string, error) {
			return team.Settings.Theme, nil
		}).(pulumi.StringPtrOutput))
		ctx.Export("agentYaml", assistant.Yaml)
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
    // Relative paths are relative to the Pulumi project directory.
    var team = DockerAgent.GetUserSettings.Invoke(new()
    {
        Path = "./team-config.yaml",
    });

    var assistant = new DockerAgent.AgentConfig("assistant", new()
    {
        Models =
        {
            { "main", new DockerAgent.Inputs.ModelDefArgs
            {
                Provider = team.Apply(getUserSettingsResult => getUserSettingsResult.DefaultModel?.Provider),
                Model = team.Apply(getUserSettingsResult => getUserSettingsResult.DefaultModel?.Model),
            } },
        },
        Agents = new[]
        {
            new DockerAgent.Inputs.AgentArgs
            {
                Name = "root",
                Model = "main",
                Instruction = "You are a helpful assistant. Answer briefly.",
            },
        },
    });

    return new Dictionary<string, object?>
    {
        ["sha256"] = team.Apply(getUserSettingsResult => getUserSettingsResult.Sha256),
        ["theme"] = team.Apply(getUserSettingsResult => getUserSettingsResult.Settings?.Theme),
        ["agentYaml"] = assistant.Yaml,
    };
});
```
```java
package generated_program;

import com.pulumi.Context;
import com.pulumi.Pulumi;
import com.pulumi.core.Output;
import io.github.dirien.dockeragent.DockeragentFunctions;
import io.github.dirien.dockeragent.inputs.GetUserSettingsArgs;
import io.github.dirien.dockeragent.AgentConfig;
import io.github.dirien.dockeragent.AgentConfigArgs;
import io.github.dirien.dockeragent.inputs.ModelDefArgs;
import io.github.dirien.dockeragent.inputs.AgentArgs;
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
        // Relative paths are relative to the Pulumi project directory.
        final var team = DockeragentFunctions.getUserSettings(GetUserSettingsArgs.builder()
            .path("./team-config.yaml")
            .build());

        var assistant = new AgentConfig("assistant", AgentConfigArgs.builder()
            .models(Map.of("main", ModelDefArgs.builder()
                .provider(team.applyValue(_team -> _team.defaultModel().provider()))
                .model(team.applyValue(_team -> _team.defaultModel().model()))
                .build()))
            .agents(AgentArgs.builder()
                .name("root")
                .model("main")
                .instruction("You are a helpful assistant. Answer briefly.")
                .build())
            .build());

        ctx.export("sha256", team.applyValue(_team -> _team.sha256()));
        ctx.export("theme", team.applyValue(_team -> _team.settings().theme()));
        ctx.export("agentYaml", assistant.yaml());
    }
}
```
```yaml
name: reuse-default-model
runtime: yaml
variables:
  # Relative paths are relative to the Pulumi project directory.
  team:
    fn::invoke:
      function: docker-agent:index:getUserSettings
      arguments:
        path: ./team-config.yaml
resources:
  assistant:
    type: docker-agent:index:AgentConfig
    properties:
      models:
        main:
          provider: ${team.defaultModel.provider}
          model: ${team.defaultModel.model}
      agents:
        - name: root
          model: main
          instruction: You are a helpful assistant. Answer briefly.
outputs:
  sha256: ${team.sha256}
  theme: ${team.settings.theme}
  agentYaml: ${assistant.yaml}
```
```hcl
terraform {
  required_providers {
    docker-agent = {
      source = "pulumi/dirien/docker-agent"
    }
  }
}

# Relative paths are relative to the Pulumi project directory.
data "docker-agent_user_settings" "team" {
  path = "./team-config.yaml"
}

resource "docker-agent_agent_config" "assistant" {
  models = {
    "main" = {
      provider = data.docker-agent_user_settings.team.default_model.provider
      model    = data.docker-agent_user_settings.team.default_model.model
    }
  }
  agents {
    name        = "root"
    model       = "main"
    instruction = "You are a helpful assistant. Answer briefly."
  }
}

output "sha256" {
  value = data.docker-agent_user_settings.team.sha256
}

output "theme" {
  value = data.docker-agent_user_settings.team.settings.theme
}

output "agentYaml" {
  value = docker-agent_agent_config.assistant.yaml
}
```
{{% /example %}}
{{% /examples %}}
