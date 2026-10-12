{{% examples %}}
## Example Usage
{{% example %}}
### Write the user configuration

```typescript
import * as pulumi from "@pulumi/pulumi";
import * as docker_agent from "@ediri/pulumi-docker-agent";

// Docker Agent's user configuration, validated with docker-agent's own checks.
const settings = new docker_agent.UserSettings("settings", {
    path: "~/.config/cagent/config.yaml",
    settings: {
        theme: "auto",
        themeDark: "dracula",
        safety: docker_agent.SafetyMode.Balanced,
        sound: true,
        layout: {
            sidebarPosition: docker_agent.SidebarPosition.Left,
            hideUsage: true,
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
export const configPath = settings.filePath;
export const configSha256 = settings.sha256;
```
```python
import pulumi
import ediri_docker_agent as docker_agent

# Docker Agent's user configuration, validated with docker-agent's own checks.
settings = docker_agent.UserSettings("settings",
    path="~/.config/cagent/config.yaml",
    settings={
        "theme": "auto",
        "theme_dark": "dracula",
        "safety": docker_agent.SafetyMode.BALANCED,
        "sound": True,
        "layout": {
            "sidebar_position": docker_agent.SidebarPosition.LEFT,
            "hide_usage": True,
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
pulumi.export("configPath", settings.file_path)
pulumi.export("configSha256", settings.sha256)
```
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
				Theme:     pulumi.String("auto"),
				ThemeDark: pulumi.String("dracula"),
				Safety:    dockeragent.SafetyModeBalanced,
				Sound:     pulumi.Bool(true),
				Layout: &dockeragent.UserLayoutSettingsArgs{
					SidebarPosition: dockeragent.SidebarPositionLeft,
					HideUsage:       pulumi.Bool(true),
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
		ctx.Export("configPath", settings.FilePath)
		ctx.Export("configSha256", settings.Sha256)
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
    // Docker Agent's user configuration, validated with docker-agent's own checks.
    var settings = new DockerAgent.UserSettings("settings", new()
    {
        Path = "~/.config/cagent/config.yaml",
        Settings = new DockerAgent.Inputs.UserGlobalSettingsArgs
        {
            Theme = "auto",
            ThemeDark = "dracula",
            Safety = DockerAgent.SafetyMode.Balanced,
            Sound = true,
            Layout = new DockerAgent.Inputs.UserLayoutSettingsArgs
            {
                SidebarPosition = DockerAgent.SidebarPosition.Left,
                HideUsage = true,
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

    return new Dictionary<string, object?>
    {
        ["configPath"] = settings.FilePath,
        ["configSha256"] = settings.Sha256,
    };
});
```
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
                .themeDark("dracula")
                .safety(SafetyMode.Balanced)
                .sound(true)
                .layout(UserLayoutSettingsArgs.builder()
                    .sidebarPosition(SidebarPosition.Left)
                    .hideUsage(true)
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

        ctx.export("configPath", settings.filePath());
        ctx.export("configSha256", settings.sha256());
    }
}
```
```yaml
name: user-settings
runtime: yaml
resources:
  # Docker Agent's user configuration, validated with docker-agent's own checks.
  settings:
    type: docker-agent:index:UserSettings
    properties:
      path: ~/.config/cagent/config.yaml
      settings:
        theme: auto
        themeDark: dracula
        safety: balanced
        sound: true
        layout:
          sidebarPosition: left
          hideUsage: true
        permissions:
          deny:
            - shell:cmd=sudo*
          allow:
            - read_*
        keybindings:
          - action: commands
            keys:
              - f2
              - ctrl+k
      aliases:
        reviewer:
          path: docker.io/myorg/reviewer:latest
          safety: restricted
outputs:
  configPath: ${settings.filePath}
  configSha256: ${settings.sha256}
```
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
    theme      = "auto"
    theme_dark = "dracula"
    safety     = "balanced"
    sound      = true
    layout = {
      sidebar_position = "left"
      hide_usage       = true
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

output "configPath" {
  value = docker-agent_user_settings.settings.file_path
}

output "configSha256" {
  value = docker-agent_user_settings.settings.sha256
}
```
{{% /example %}}
{{% example %}}
### Render only, with a global provider and hooks

```typescript
import * as pulumi from "@pulumi/pulumi";
import * as docker_agent from "@ediri/pulumi-docker-agent";

// No `path`: nothing is written. Use the YAML in a Dockerfile, a sandbox kit or a ConfigMap.
const rendered = new docker_agent.UserSettings("rendered", {
    providers: {
        gateway: {
            baseUrl: "https://llm.example.com/v1",
            tokenKey: "GATEWAY_API_KEY",
        },
    },
    defaultModel: {
        provider: "gateway",
        model: "qwen3-coder",
    },
    sandboxAllowlist: ["registry.npmjs.org:443"],
    settings: {
        hooks: {
            sessionStart: [{
                type: docker_agent.HookType.Command,
                command: "~/.config/cagent/hooks/session-start.sh",
            }],
            preToolUse: [{
                matcher: "shell",
                hooks: [{
                    type: docker_agent.HookType.Builtin,
                    command: "redact_secrets",
                }],
            }],
        },
    },
});
export const userConfigYaml = rendered.yaml;
```
```python
import pulumi
import ediri_docker_agent as docker_agent

# No `path`: nothing is written. Use the YAML in a Dockerfile, a sandbox kit or a ConfigMap.
rendered = docker_agent.UserSettings("rendered",
    providers={
        "gateway": {
            "base_url": "https://llm.example.com/v1",
            "token_key": "GATEWAY_API_KEY",
        },
    },
    default_model={
        "provider": "gateway",
        "model": "qwen3-coder",
    },
    sandbox_allowlist=["registry.npmjs.org:443"],
    settings={
        "hooks": {
            "session_start": [{
                "type": docker_agent.HookType.COMMAND,
                "command": "~/.config/cagent/hooks/session-start.sh",
            }],
            "pre_tool_use": [{
                "matcher": "shell",
                "hooks": [{
                    "type": docker_agent.HookType.BUILTIN,
                    "command": "redact_secrets",
                }],
            }],
        },
    })
pulumi.export("userConfigYaml", rendered.yaml)
```
```go
package main

import (
	"github.com/dirien/pulumi-docker-agent/sdk/go/dockeragent"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		// No `path`: nothing is written. Use the YAML in a Dockerfile, a sandbox kit or a ConfigMap.
		rendered, err := dockeragent.NewUserSettings(ctx, "rendered", &dockeragent.UserSettingsArgs{
			Providers: dockeragent.UserProviderMap{
				"gateway": &dockeragent.UserProviderArgs{
					BaseUrl:  pulumi.String("https://llm.example.com/v1"),
					TokenKey: pulumi.String("GATEWAY_API_KEY"),
				},
			},
			DefaultModel: &dockeragent.UserModelArgs{
				Provider: pulumi.String("gateway"),
				Model:    pulumi.String("qwen3-coder"),
			},
			SandboxAllowlist: pulumi.StringArray{
				pulumi.String("registry.npmjs.org:443"),
			},
			Settings: &dockeragent.UserGlobalSettingsArgs{
				Hooks: &dockeragent.UserHooksArgs{
					SessionStart: dockeragent.UserHookArray{
						&dockeragent.UserHookArgs{
							Type:    dockeragent.HookTypeCommand,
							Command: pulumi.String("~/.config/cagent/hooks/session-start.sh"),
						},
					},
					PreToolUse: dockeragent.UserHookMatcherArray{
						&dockeragent.UserHookMatcherArgs{
							Matcher: pulumi.String("shell"),
							Hooks: dockeragent.UserHookArray{
								&dockeragent.UserHookArgs{
									Type:    dockeragent.HookTypeBuiltin,
									Command: pulumi.String("redact_secrets"),
								},
							},
						},
					},
				},
			},
		})
		if err != nil {
			return err
		}
		ctx.Export("userConfigYaml", rendered.Yaml)
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
    // No `path`: nothing is written. Use the YAML in a Dockerfile, a sandbox kit or a ConfigMap.
    var rendered = new DockerAgent.UserSettings("rendered", new()
    {
        Providers =
        {
            { "gateway", new DockerAgent.Inputs.UserProviderArgs
            {
                BaseUrl = "https://llm.example.com/v1",
                TokenKey = "GATEWAY_API_KEY",
            } },
        },
        DefaultModel = new DockerAgent.Inputs.UserModelArgs
        {
            Provider = "gateway",
            Model = "qwen3-coder",
        },
        SandboxAllowlist = new[]
        {
            "registry.npmjs.org:443",
        },
        Settings = new DockerAgent.Inputs.UserGlobalSettingsArgs
        {
            Hooks = new DockerAgent.Inputs.UserHooksArgs
            {
                SessionStart = new[]
                {
                    new DockerAgent.Inputs.UserHookArgs
                    {
                        Type = DockerAgent.HookType.Command,
                        Command = "~/.config/cagent/hooks/session-start.sh",
                    },
                },
                PreToolUse = new[]
                {
                    new DockerAgent.Inputs.UserHookMatcherArgs
                    {
                        Matcher = "shell",
                        Hooks = new[]
                        {
                            new DockerAgent.Inputs.UserHookArgs
                            {
                                Type = DockerAgent.HookType.Builtin,
                                Command = "redact_secrets",
                            },
                        },
                    },
                },
            },
        },
    });

    return new Dictionary<string, object?>
    {
        ["userConfigYaml"] = rendered.Yaml,
    };
});
```
```java
package generated_program;

import com.pulumi.Context;
import com.pulumi.Pulumi;
import com.pulumi.core.Output;
import io.github.dirien.dockeragent.UserSettings;
import io.github.dirien.dockeragent.UserSettingsArgs;
import io.github.dirien.dockeragent.inputs.UserProviderArgs;
import io.github.dirien.dockeragent.inputs.UserModelArgs;
import io.github.dirien.dockeragent.inputs.UserGlobalSettingsArgs;
import io.github.dirien.dockeragent.inputs.UserHooksArgs;
import io.github.dirien.dockeragent.inputs.UserHookArgs;
import io.github.dirien.dockeragent.inputs.UserHookMatcherArgs;
import io.github.dirien.dockeragent.enums.HookType;
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
        // No `path`: nothing is written. Use the YAML in a Dockerfile, a sandbox kit or a ConfigMap.
        var rendered = new UserSettings("rendered", UserSettingsArgs.builder()
            .providers(Map.of("gateway", UserProviderArgs.builder()
                .baseUrl("https://llm.example.com/v1")
                .tokenKey("GATEWAY_API_KEY")
                .build()))
            .defaultModel(UserModelArgs.builder()
                .provider("gateway")
                .model("qwen3-coder")
                .build())
            .sandboxAllowlist("registry.npmjs.org:443")
            .settings(UserGlobalSettingsArgs.builder()
                .hooks(UserHooksArgs.builder()
                    .sessionStart(UserHookArgs.builder()
                        .type(HookType.Command)
                        .command("~/.config/cagent/hooks/session-start.sh")
                        .build())
                    .preToolUse(UserHookMatcherArgs.builder()
                        .matcher("shell")
                        .hooks(UserHookArgs.builder()
                            .type(HookType.Builtin)
                            .command("redact_secrets")
                            .build())
                        .build())
                    .build())
                .build())
            .build());

        ctx.export("userConfigYaml", rendered.yaml());
    }
}
```
```yaml
name: image-settings
runtime: yaml
resources:
  # No `path`: nothing is written. Use the YAML in a Dockerfile, a sandbox kit or a ConfigMap.
  rendered:
    type: docker-agent:index:UserSettings
    properties:
      providers:
        gateway:
          baseUrl: https://llm.example.com/v1
          tokenKey: GATEWAY_API_KEY
      defaultModel:
        provider: gateway
        model: qwen3-coder
      sandboxAllowlist:
        - registry.npmjs.org:443
      settings:
        hooks:
          sessionStart:
            - type: command
              command: ~/.config/cagent/hooks/session-start.sh
          preToolUse:
            - matcher: shell
              hooks:
                - type: builtin
                  command: redact_secrets
outputs:
  userConfigYaml: ${rendered.yaml}
```
```hcl
terraform {
  required_providers {
    docker-agent = {
      source = "pulumi/dirien/docker-agent"
    }
  }
}

# No `path`: nothing is written. Use the YAML in a Dockerfile, a sandbox kit or a ConfigMap.
resource "docker-agent_user_settings" "rendered" {
  _ {
    providers = {
      "gateway" = {
        base_url  = "https://llm.example.com/v1"
        token_key = "GATEWAY_API_KEY"
      }
    }
  }
  default_model = {
    provider = "gateway"
    model    = "qwen3-coder"
  }
  sandbox_allowlist = ["registry.npmjs.org:443"]
  settings = {
    hooks = {
      session_start = [{
        type    = "command"
        command = "~/.config/cagent/hooks/session-start.sh"
      }]
      pre_tool_use = [{
        matcher = "shell"
        hooks = [{
          type    = "builtin"
          command = "redact_secrets"
        }]
      }]
    }
  }
}

output "userConfigYaml" {
  value = docker-agent_user_settings.rendered.yaml
}
```
{{% /example %}}
{{% /examples %}}
