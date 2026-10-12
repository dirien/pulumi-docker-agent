{{% examples %}}
## Example Usage
{{% example %}}
### Hide secrets from the agent

```typescript
import * as pulumi from "@pulumi/pulumi";
import * as docker_agent from "@ediri/pulumi-docker-agent";

// Writes /home/me/src/my-service/.agentsignore.
const ignore = new docker_agent.AgentsIgnore("ignore", {
    directory: "/home/me/src/my-service",
    comment: "Managed by Pulumi. Keep secrets away from docker-agent.",
    patterns: [
        ".env",
        ".env.*",
        "*.pem",
        "*.key",
        "!public.key",
        ".aws/",
        "build/",
    ],
});
export const agentsIgnore = ignore.content;
export const filePath = ignore.filePath;
```
```python
import pulumi
import ediri_docker_agent as docker_agent

# Writes /home/me/src/my-service/.agentsignore.
ignore = docker_agent.AgentsIgnore("ignore",
    directory="/home/me/src/my-service",
    comment="Managed by Pulumi. Keep secrets away from docker-agent.",
    patterns=[
        ".env",
        ".env.*",
        "*.pem",
        "*.key",
        "!public.key",
        ".aws/",
        "build/",
    ])
pulumi.export("agentsIgnore", ignore.content)
pulumi.export("filePath", ignore.file_path)
```
```go
package main

import (
	"github.com/dirien/pulumi-docker-agent/sdk/go/dockeragent"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		// Writes /home/me/src/my-service/.agentsignore.
		ignore, err := dockeragent.NewAgentsIgnore(ctx, "ignore", &dockeragent.AgentsIgnoreArgs{
			Directory: pulumi.String("/home/me/src/my-service"),
			Comment:   pulumi.String("Managed by Pulumi. Keep secrets away from docker-agent."),
			Patterns: pulumi.StringArray{
				pulumi.String(".env"),
				pulumi.String(".env.*"),
				pulumi.String("*.pem"),
				pulumi.String("*.key"),
				pulumi.String("!public.key"),
				pulumi.String(".aws/"),
				pulumi.String("build/"),
			},
		})
		if err != nil {
			return err
		}
		ctx.Export("agentsIgnore", ignore.Content)
		ctx.Export("filePath", ignore.FilePath)
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
    // Writes /home/me/src/my-service/.agentsignore.
    var ignore = new DockerAgent.AgentsIgnore("ignore", new()
    {
        Directory = "/home/me/src/my-service",
        Comment = "Managed by Pulumi. Keep secrets away from docker-agent.",
        Patterns = new[]
        {
            ".env",
            ".env.*",
            "*.pem",
            "*.key",
            "!public.key",
            ".aws/",
            "build/",
        },
    });

    return new Dictionary<string, object?>
    {
        ["agentsIgnore"] = ignore.Content,
        ["filePath"] = ignore.FilePath,
    };
});
```
```java
package generated_program;

import com.pulumi.Context;
import com.pulumi.Pulumi;
import com.pulumi.core.Output;
import io.github.dirien.dockeragent.AgentsIgnore;
import io.github.dirien.dockeragent.AgentsIgnoreArgs;
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
        // Writes /home/me/src/my-service/.agentsignore.
        var ignore = new AgentsIgnore("ignore", AgentsIgnoreArgs.builder()
            .directory("/home/me/src/my-service")
            .comment("Managed by Pulumi. Keep secrets away from docker-agent.")
            .patterns(
                ".env",
                ".env.*",
                "*.pem",
                "*.key",
                "!public.key",
                ".aws/",
                "build/")
            .build());

        ctx.export("agentsIgnore", ignore.content());
        ctx.export("filePath", ignore.filePath());
    }
}
```
```yaml
name: agents-ignore
runtime: yaml
resources:
  # Writes /home/me/src/my-service/.agentsignore.
  ignore:
    type: docker-agent:index:AgentsIgnore
    properties:
      directory: /home/me/src/my-service
      comment: Managed by Pulumi. Keep secrets away from docker-agent.
      patterns:
        - .env
        - .env.*
        - "*.pem"
        - "*.key"
        - "!public.key"
        - .aws/
        - build/
outputs:
  agentsIgnore: ${ignore.content}
  filePath: ${ignore.filePath}
```
```hcl
terraform {
  required_providers {
    docker-agent = {
      source = "pulumi/dirien/docker-agent"
    }
  }
}

# Writes /home/me/src/my-service/.agentsignore.
resource "docker-agent_agents_ignore" "ignore" {
  directory = "/home/me/src/my-service"
  comment   = "Managed by Pulumi. Keep secrets away from docker-agent."
  patterns  = [".env", ".env.*", "*.pem", "*.key", "!public.key", ".aws/", "build/"]
}

output "agentsIgnore" {
  value = docker-agent_agents_ignore.ignore.content
}

output "filePath" {
  value = docker-agent_agents_ignore.ignore.file_path
}
```
{{% /example %}}
{{% example %}}
### Render the file for a container image

```typescript
import * as pulumi from "@pulumi/pulumi";
import * as docker_agent from "@ediri/pulumi-docker-agent";

// No `directory`: nothing is written, the file is only rendered.
const rendered = new docker_agent.AgentsIgnore("rendered", {patterns: [
    ".git/",
    "node_modules/",
    "**/*.pem",
]});
export const agentsIgnore = rendered.content;
```
```python
import pulumi
import ediri_docker_agent as docker_agent

# No `directory`: nothing is written, the file is only rendered.
rendered = docker_agent.AgentsIgnore("rendered", patterns=[
    ".git/",
    "node_modules/",
    "**/*.pem",
])
pulumi.export("agentsIgnore", rendered.content)
```
```go
package main

import (
	"github.com/dirien/pulumi-docker-agent/sdk/go/dockeragent"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		// No `directory`: nothing is written, the file is only rendered.
		rendered, err := dockeragent.NewAgentsIgnore(ctx, "rendered", &dockeragent.AgentsIgnoreArgs{
			Patterns: pulumi.StringArray{
				pulumi.String(".git/"),
				pulumi.String("node_modules/"),
				pulumi.String("**/*.pem"),
			},
		})
		if err != nil {
			return err
		}
		ctx.Export("agentsIgnore", rendered.Content)
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
    // No `directory`: nothing is written, the file is only rendered.
    var rendered = new DockerAgent.AgentsIgnore("rendered", new()
    {
        Patterns = new[]
        {
            ".git/",
            "node_modules/",
            "**/*.pem",
        },
    });

    return new Dictionary<string, object?>
    {
        ["agentsIgnore"] = rendered.Content,
    };
});
```
```java
package generated_program;

import com.pulumi.Context;
import com.pulumi.Pulumi;
import com.pulumi.core.Output;
import io.github.dirien.dockeragent.AgentsIgnore;
import io.github.dirien.dockeragent.AgentsIgnoreArgs;
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
        // No `directory`: nothing is written, the file is only rendered.
        var rendered = new AgentsIgnore("rendered", AgentsIgnoreArgs.builder()
            .patterns(
                ".git/",
                "node_modules/",
                "**/*.pem")
            .build());

        ctx.export("agentsIgnore", rendered.content());
    }
}
```
```yaml
name: image-agents-ignore
runtime: yaml
resources:
  # No `directory`: nothing is written, the file is only rendered.
  rendered:
    type: docker-agent:index:AgentsIgnore
    properties:
      patterns:
        - .git/
        - node_modules/
        - "**/*.pem"
outputs:
  # Copy it into the image, for example with a Dockerfile `COPY`.
  agentsIgnore: ${rendered.content}
```
```hcl
terraform {
  required_providers {
    docker-agent = {
      source = "pulumi/dirien/docker-agent"
    }
  }
}

# No `directory`: nothing is written, the file is only rendered.
resource "docker-agent_agents_ignore" "rendered" {
  patterns = [".git/", "node_modules/", "**/*.pem"]
}

output "agentsIgnore" {
  value = docker-agent_agents_ignore.rendered.content
}
```
{{% /example %}}
{{% /examples %}}
