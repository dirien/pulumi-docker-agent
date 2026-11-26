{{% examples %}}
## Example Usage
{{% example %}}
### Reuse the default model of the user configuration

```typescript
import * as pulumi from "@pulumi/pulumi";
import * as docker_agent from "@ediri/pulumi-docker-agent";

// Reads ~/.config/cagent/config.yaml without managing it.
const current = docker_agent.getUserSettingsOutput({});
const assistant = new docker_agent.AgentConfig("assistant", {
    models: {
        main: {
            provider: current.apply(current => current.defaultModel?.provider),
            model: current.apply(current => current.defaultModel?.model),
        },
    },
    agents: [{
        name: "root",
        model: "main",
        instruction: "You are a helpful assistant. Answer briefly.",
    }],
});
export const configExists = current.exists;
export const aliases = current.aliases;
```
```python
import pulumi
import ediri_docker_agent as docker_agent

# Reads ~/.config/cagent/config.yaml without managing it.
current = docker_agent.get_user_settings_output()
assistant = docker_agent.AgentConfig("assistant",
    models={
        "main": {
            "provider": current.default_model.provider,
            "model": current.default_model.model,
        },
    },
    agents=[{
        "name": "root",
        "model": "main",
        "instruction": "You are a helpful assistant. Answer briefly.",
    }])
pulumi.export("configExists", current.exists)
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
		// Reads ~/.config/cagent/config.yaml without managing it.
		current := dockeragent.GetUserSettingsOutput(ctx, dockeragent.GetUserSettingsOutputArgs{}, nil)
		_, err := dockeragent.NewAgentConfig(ctx, "assistant", &dockeragent.AgentConfigArgs{
			Models: dockeragent.ModelDefMap{
				"main": &dockeragent.ModelDefArgs{
					Provider: current.ApplyT(func(current dockeragent.GetUserSettingsResult) (*string, error) {
						return current.DefaultModel.Provider, nil
					}).(pulumi.StringPtrOutput),
					Model: current.ApplyT(func(current dockeragent.GetUserSettingsResult) (*string, error) {
						return current.DefaultModel.Model, nil
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
		ctx.Export("configExists", current.Exists())
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
    // Reads ~/.config/cagent/config.yaml without managing it.
    var current = DockerAgent.GetUserSettings.Invoke();

    var assistant = new DockerAgent.AgentConfig("assistant", new()
    {
        Models =
        {
            { "main", new DockerAgent.Inputs.ModelDefArgs
            {
                Provider = current.Apply(getUserSettingsResult => getUserSettingsResult.DefaultModel?.Provider),
                Model = current.Apply(getUserSettingsResult => getUserSettingsResult.DefaultModel?.Model),
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
        ["configExists"] = current.Apply(getUserSettingsResult => getUserSettingsResult.Exists),
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
        // Reads ~/.config/cagent/config.yaml without managing it.
        final var current = DockeragentFunctions.getUserSettings(GetUserSettingsArgs.builder()
            .build());

        var assistant = new AgentConfig("assistant", AgentConfigArgs.builder()
            .models(Map.of("main", ModelDefArgs.builder()
                .provider(current.applyValue(_current -> _current.defaultModel().provider()))
                .model(current.applyValue(_current -> _current.defaultModel().model()))
                .build()))
            .agents(AgentArgs.builder()
                .name("root")
                .model("main")
                .instruction("You are a helpful assistant. Answer briefly.")
                .build())
            .build());

        ctx.export("configExists", current.applyValue(_current -> _current.exists()));
        ctx.export("aliases", current.applyValue(_current -> _current.aliases()));
    }
}
```
```yaml
name: reuse-default-model
runtime: yaml
variables:
  # Reads ~/.config/cagent/config.yaml without managing it.
  current:
    fn::invoke:
      function: docker-agent:index:getUserSettings
      arguments: {}
resources:
  assistant:
    type: docker-agent:index:AgentConfig
    properties:
      models:
        main:
          provider: ${current.defaultModel.provider}
          model: ${current.defaultModel.model}
      agents:
        - name: root
          model: main
          instruction: You are a helpful assistant. Answer briefly.
outputs:
  configExists: ${current.exists}
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

# Reads ~/.config/cagent/config.yaml without managing it.
data "docker-agent_user_settings" "current" {
}

resource "docker-agent_agent_config" "assistant" {
  models = {
    "main" = {
      provider = data.docker-agent_user_settings.current.default_model.provider
      model    = data.docker-agent_user_settings.current.default_model.model
    }
  }
  agents {
    name        = "root"
    model       = "main"
    instruction = "You are a helpful assistant. Answer briefly."
  }
}

output "configExists" {
  value = data.docker-agent_user_settings.current.exists
}

output "aliases" {
  value = data.docker-agent_user_settings.current.aliases
}
```
{{% /example %}}
{{% example %}}
### Read a configuration file in the project

```typescript
import * as pulumi from "@pulumi/pulumi";
import * as docker_agent from "@ediri/pulumi-docker-agent";

// Relative paths are relative to the Pulumi project directory.
const team = docker_agent.getUserSettingsOutput({
    path: "./team-config.yaml",
});
export const filePath = team.filePath;
export const sha256 = team.sha256;
export const theme = team.apply(team => team.settings?.theme);
```
```python
import pulumi
import ediri_docker_agent as docker_agent

# Relative paths are relative to the Pulumi project directory.
team = docker_agent.get_user_settings_output(path="./team-config.yaml")
pulumi.export("filePath", team.file_path)
pulumi.export("sha256", team.sha256)
pulumi.export("theme", team.settings.theme)
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
		team := dockeragent.GetUserSettingsOutput(ctx, dockeragent.GetUserSettingsOutputArgs{
			Path: pulumi.String("./team-config.yaml"),
		}, nil)
		ctx.Export("filePath", team.FilePath())
		ctx.Export("sha256", team.Sha256())
		ctx.Export("theme", team.ApplyT(func(team dockeragent.GetUserSettingsResult) (*string, error) {
			return team.Settings.Theme, nil
		}).(pulumi.StringPtrOutput))
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

    return new Dictionary<string, object?>
    {
        ["filePath"] = team.Apply(getUserSettingsResult => getUserSettingsResult.FilePath),
        ["sha256"] = team.Apply(getUserSettingsResult => getUserSettingsResult.Sha256),
        ["theme"] = team.Apply(getUserSettingsResult => getUserSettingsResult.Settings?.Theme),
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
        // Relative paths are relative to the Pulumi project directory.
        final var team = DockeragentFunctions.getUserSettings(GetUserSettingsArgs.builder()
            .path("./team-config.yaml")
            .build());

        ctx.export("filePath", team.applyValue(_team -> _team.filePath()));
        ctx.export("sha256", team.applyValue(_team -> _team.sha256()));
        ctx.export("theme", team.applyValue(_team -> _team.settings().theme()));
    }
}
```
```yaml
name: read-project-settings
runtime: yaml
variables:
  # Relative paths are relative to the Pulumi project directory.
  team:
    fn::invoke:
      function: docker-agent:index:getUserSettings
      arguments:
        path: ./team-config.yaml
outputs:
  filePath: ${team.filePath}
  sha256: ${team.sha256}
  theme: ${team.settings.theme}
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

output "filePath" {
  value = data.docker-agent_user_settings.team.file_path
}

output "sha256" {
  value = data.docker-agent_user_settings.team.sha256
}

output "theme" {
  value = data.docker-agent_user_settings.team.settings.theme
}
```
{{% /example %}}
{{% /examples %}}
