{{% examples %}}
## Example Usage
{{% example %}}
### Reuse a project's patterns

```typescript
import * as pulumi from "@pulumi/pulumi";
import * as docker_agent from "@ediri/pulumi-docker-agent";

const projectVar = docker_agent.getAgentsIgnoreOutput({
    directory: ".",
});

// Copies the patterns of ./.agentsignore, which the function reads without managing it.
const service = new docker_agent.AgentsIgnore("service", {
    directory: "./services/api",
    comment: projectVar.comment,
    patterns: projectVar.patterns,
});
export const existed = projectVar.exists;
export const patterns = projectVar.patterns;
```
```python
import pulumi
import ediri_docker_agent as docker_agent

project_var = docker_agent.get_agents_ignore_output(directory=".")

# Copies the patterns of ./.agentsignore, which the function reads without managing it.
service = docker_agent.AgentsIgnore("service",
    directory="./services/api",
    comment=project_var.comment,
    patterns=project_var.patterns)
pulumi.export("existed", project_var.exists)
pulumi.export("patterns", project_var.patterns)
```
```go
package main

import (
	"github.com/dirien/pulumi-docker-agent/sdk/go/dockeragent"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		projectVar := dockeragent.LookupAgentsIgnoreOutput(ctx, dockeragent.LookupAgentsIgnoreOutputArgs{
			Directory: pulumi.String("."),
		}, nil)

		// Copies the patterns of ./.agentsignore, which the function reads without managing it.
		_, err := dockeragent.NewAgentsIgnore(ctx, "service", &dockeragent.AgentsIgnoreArgs{
			Directory: pulumi.String("./services/api"),
			Comment:   projectVar.Comment(),
			Patterns:  projectVar.Patterns(),
		})
		if err != nil {
			return err
		}
		ctx.Export("existed", projectVar.Exists())
		ctx.Export("patterns", projectVar.Patterns())
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
    var projectVar = DockerAgent.GetAgentsIgnore.Invoke(new()
    {
        Directory = ".",
    });

    // Copies the patterns of ./.agentsignore, which the function reads without managing it.
    var service = new DockerAgent.AgentsIgnore("service", new()
    {
        Directory = "./services/api",
        Comment = projectVar.Apply(getAgentsIgnoreResult => getAgentsIgnoreResult.Comment),
        Patterns = projectVar.Apply(getAgentsIgnoreResult => getAgentsIgnoreResult.Patterns),
    });

    return new Dictionary<string, object?>
    {
        ["existed"] = projectVar.Apply(getAgentsIgnoreResult => getAgentsIgnoreResult.Exists),
        ["patterns"] = projectVar.Apply(getAgentsIgnoreResult => getAgentsIgnoreResult.Patterns),
    };
});
```
```java
package generated_program;

import com.pulumi.Context;
import com.pulumi.Pulumi;
import com.pulumi.core.Output;
import io.github.dirien.dockeragent.DockeragentFunctions;
import io.github.dirien.dockeragent.inputs.GetAgentsIgnoreArgs;
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
        final var projectVar = DockeragentFunctions.getAgentsIgnore(GetAgentsIgnoreArgs.builder()
            .directory(".")
            .build());

        // Copies the patterns of ./.agentsignore, which the function reads without managing it.
        var service = new AgentsIgnore("service", AgentsIgnoreArgs.builder()
            .directory("./services/api")
            .comment(projectVar.applyValue(_projectVar -> _projectVar.comment()))
            .patterns(projectVar.applyValue(_projectVar -> _projectVar.patterns()))
            .build());

        ctx.export("existed", projectVar.applyValue(_projectVar -> _projectVar.exists()));
        ctx.export("patterns", projectVar.applyValue(_projectVar -> _projectVar.patterns()));
    }
}
```
```yaml
name: reuse-agents-ignore
runtime: yaml
variables:
  project:
    fn::invoke:
      function: docker-agent:index:getAgentsIgnore
      arguments:
        directory: .
resources:
  # Copies the patterns of ./.agentsignore, which the function reads without managing it.
  service:
    type: docker-agent:index:AgentsIgnore
    properties:
      directory: ./services/api
      comment: ${project.comment}
      patterns: ${project.patterns}
outputs:
  existed: ${project.exists}
  patterns: ${project.patterns}
```
```hcl
terraform {
  required_providers {
    docker-agent = {
      source = "pulumi/dirien/docker-agent"
    }
  }
}

data "docker-agent_agents_ignore" "projectVar" {
  directory = "."
}

# Copies the patterns of ./.agentsignore, which the function reads without managing it.
resource "docker-agent_agents_ignore" "service" {
  directory = "./services/api"
  comment   = data.docker-agent_agents_ignore.projectVar.comment
  patterns  = data.docker-agent_agents_ignore.projectVar.patterns
}

output "existed" {
  value = data.docker-agent_agents_ignore.projectVar.exists
}

output "patterns" {
  value = data.docker-agent_agents_ignore.projectVar.patterns
}
```
{{% /example %}}
{{% example %}}
### Read a file by its path

```typescript
import * as pulumi from "@pulumi/pulumi";
import * as docker_agent from "@ediri/pulumi-docker-agent";

const repo = docker_agent.getAgentsIgnoreOutput({
    directory: "~/src/my-service/.agentsignore",
});

// No `directory`: renders the same patterns for a container image.
const image = new docker_agent.AgentsIgnore("image", {patterns: repo.patterns});
export const sourceFile = repo.filePath;
export const sourceSha256 = repo.sha256;
export const imageFile = image.content;
```
```python
import pulumi
import ediri_docker_agent as docker_agent

repo = docker_agent.get_agents_ignore_output(directory="~/src/my-service/.agentsignore")

# No `directory`: renders the same patterns for a container image.
image = docker_agent.AgentsIgnore("image", patterns=repo.patterns)
pulumi.export("sourceFile", repo.file_path)
pulumi.export("sourceSha256", repo.sha256)
pulumi.export("imageFile", image.content)
```
```go
package main

import (
	"github.com/dirien/pulumi-docker-agent/sdk/go/dockeragent"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		repo := dockeragent.LookupAgentsIgnoreOutput(ctx, dockeragent.LookupAgentsIgnoreOutputArgs{
			Directory: pulumi.String("~/src/my-service/.agentsignore"),
		}, nil)

		// No `directory`: renders the same patterns for a container image.
		image, err := dockeragent.NewAgentsIgnore(ctx, "image", &dockeragent.AgentsIgnoreArgs{
			Patterns: repo.Patterns(),
		})
		if err != nil {
			return err
		}
		ctx.Export("sourceFile", repo.FilePath())
		ctx.Export("sourceSha256", repo.Sha256())
		ctx.Export("imageFile", image.Content)
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
    var repo = DockerAgent.GetAgentsIgnore.Invoke(new()
    {
        Directory = "~/src/my-service/.agentsignore",
    });

    // No `directory`: renders the same patterns for a container image.
    var image = new DockerAgent.AgentsIgnore("image", new()
    {
        Patterns = repo.Apply(getAgentsIgnoreResult => getAgentsIgnoreResult.Patterns),
    });

    return new Dictionary<string, object?>
    {
        ["sourceFile"] = repo.Apply(getAgentsIgnoreResult => getAgentsIgnoreResult.FilePath),
        ["sourceSha256"] = repo.Apply(getAgentsIgnoreResult => getAgentsIgnoreResult.Sha256),
        ["imageFile"] = image.Content,
    };
});
```
```java
package generated_program;

import com.pulumi.Context;
import com.pulumi.Pulumi;
import com.pulumi.core.Output;
import io.github.dirien.dockeragent.DockeragentFunctions;
import io.github.dirien.dockeragent.inputs.GetAgentsIgnoreArgs;
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
        final var repo = DockeragentFunctions.getAgentsIgnore(GetAgentsIgnoreArgs.builder()
            .directory("~/src/my-service/.agentsignore")
            .build());

        // No `directory`: renders the same patterns for a container image.
        var image = new AgentsIgnore("image", AgentsIgnoreArgs.builder()
            .patterns(repo.applyValue(_repo -> _repo.patterns()))
            .build());

        ctx.export("sourceFile", repo.applyValue(_repo -> _repo.filePath()));
        ctx.export("sourceSha256", repo.applyValue(_repo -> _repo.sha256()));
        ctx.export("imageFile", image.content());
    }
}
```
```yaml
name: read-agents-ignore-file
runtime: yaml
variables:
  repo:
    fn::invoke:
      function: docker-agent:index:getAgentsIgnore
      arguments:
        directory: ~/src/my-service/.agentsignore
resources:
  # No `directory`: renders the same patterns for a container image.
  image:
    type: docker-agent:index:AgentsIgnore
    properties:
      patterns: ${repo.patterns}
outputs:
  sourceFile: ${repo.filePath}
  sourceSha256: ${repo.sha256}
  imageFile: ${image.content}
```
```hcl
terraform {
  required_providers {
    docker-agent = {
      source = "pulumi/dirien/docker-agent"
    }
  }
}

data "docker-agent_agents_ignore" "repo" {
  directory = "~/src/my-service/.agentsignore"
}

# No `directory`: renders the same patterns for a container image.
resource "docker-agent_agents_ignore" "image" {
  patterns = data.docker-agent_agents_ignore.repo.patterns
}

output "sourceFile" {
  value = data.docker-agent_agents_ignore.repo.file_path
}

output "sourceSha256" {
  value = data.docker-agent_agents_ignore.repo.sha256
}

output "imageFile" {
  value = docker-agent_agents_ignore.image.content
}
```
{{% /example %}}
{{% /examples %}}
