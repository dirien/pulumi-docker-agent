{{% examples %}}
## Example Usage
{{% example %}}
### Push an agent to a registry

```typescript
import * as pulumi from "@pulumi/pulumi";
import * as docker_agent from "@ediri/pulumi-docker-agent";

// The agent configuration, rendered and validated.
const assistant = new docker_agent.AgentConfig("assistant", {agents: [{
    name: "root",
    model: "openai/gpt-5-mini",
    instruction: "You are a helpful assistant. Answer briefly.",
}]});

// Pushes the configuration like `docker agent share push`.
const artifact = new docker_agent.AgentArtifact("artifact", {
    reference: "ghcr.io/my-org/agents/assistant:v1",
    yaml: assistant.yaml,
});
export const pinnedReference = artifact.pinnedReference;
export const digest = artifact.digest;
```
```python
import pulumi
import ediri_docker_agent as docker_agent

# The agent configuration, rendered and validated.
assistant = docker_agent.AgentConfig("assistant", agents=[{
    "name": "root",
    "model": "openai/gpt-5-mini",
    "instruction": "You are a helpful assistant. Answer briefly.",
}])

# Pushes the configuration like `docker agent share push`.
artifact = docker_agent.AgentArtifact("artifact",
    reference="ghcr.io/my-org/agents/assistant:v1",
    yaml=assistant.yaml)
pulumi.export("pinnedReference", artifact.pinned_reference)
pulumi.export("digest", artifact.digest)
```
```go
package main

import (
	"github.com/dirien/pulumi-docker-agent/sdk/go/dockeragent"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		// The agent configuration, rendered and validated.
		assistant, err := dockeragent.NewAgentConfig(ctx, "assistant", &dockeragent.AgentConfigArgs{
			Agents: dockeragent.AgentArray{
				&dockeragent.AgentArgs{
					Name:        pulumi.String("root"),
					Model:       pulumi.String("openai/gpt-5-mini"),
					Instruction: pulumi.String("You are a helpful assistant. Answer briefly."),
				},
			},
		})
		if err != nil {
			return err
		}

		// Pushes the configuration like `docker agent share push`.
		artifact, err := dockeragent.NewAgentArtifact(ctx, "artifact", &dockeragent.AgentArtifactArgs{
			Reference: pulumi.String("ghcr.io/my-org/agents/assistant:v1"),
			Yaml:      assistant.Yaml,
		})
		if err != nil {
			return err
		}
		ctx.Export("pinnedReference", artifact.PinnedReference)
		ctx.Export("digest", artifact.Digest)
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
    // The agent configuration, rendered and validated.
    var assistant = new DockerAgent.AgentConfig("assistant", new()
    {
        Agents = new[]
        {
            new DockerAgent.Inputs.AgentArgs
            {
                Name = "root",
                Model = "openai/gpt-5-mini",
                Instruction = "You are a helpful assistant. Answer briefly.",
            },
        },
    });

    // Pushes the configuration like `docker agent share push`.
    var artifact = new DockerAgent.AgentArtifact("artifact", new()
    {
        Reference = "ghcr.io/my-org/agents/assistant:v1",
        Yaml = assistant.Yaml,
    });

    return new Dictionary<string, object?>
    {
        ["pinnedReference"] = artifact.PinnedReference,
        ["digest"] = artifact.Digest,
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
import io.github.dirien.dockeragent.AgentArtifact;
import io.github.dirien.dockeragent.AgentArtifactArgs;
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
        // The agent configuration, rendered and validated.
        var assistant = new AgentConfig("assistant", AgentConfigArgs.builder()
            .agents(AgentArgs.builder()
                .name("root")
                .model("openai/gpt-5-mini")
                .instruction("You are a helpful assistant. Answer briefly.")
                .build())
            .build());

        // Pushes the configuration like `docker agent share push`.
        var artifact = new AgentArtifact("artifact", AgentArtifactArgs.builder()
            .reference("ghcr.io/my-org/agents/assistant:v1")
            .yaml(assistant.yaml())
            .build());

        ctx.export("pinnedReference", artifact.pinnedReference());
        ctx.export("digest", artifact.digest());
    }
}
```
```yaml
name: publish
runtime: yaml
resources:
  # The agent configuration, rendered and validated.
  assistant:
    type: docker-agent:index:AgentConfig
    properties:
      agents:
        - name: root
          model: openai/gpt-5-mini
          instruction: You are a helpful assistant. Answer briefly.
  # Pushes the configuration like `docker agent share push`.
  artifact:
    type: docker-agent:index:AgentArtifact
    properties:
      reference: ghcr.io/my-org/agents/assistant:v1
      yaml: ${assistant.yaml}
outputs:
  # Run it with `docker agent run <pinnedReference>`.
  pinnedReference: ${artifact.pinnedReference}
  digest: ${artifact.digest}
```
```hcl
terraform {
  required_providers {
    docker-agent = {
      source = "pulumi/dirien/docker-agent"
    }
  }
}

# The agent configuration, rendered and validated.
resource "docker-agent_agent_config" "assistant" {
  agents {
    name        = "root"
    model       = "openai/gpt-5-mini"
    instruction = "You are a helpful assistant. Answer briefly."
  }
}

# Pushes the configuration like `docker agent share push`.
resource "docker-agent_agent_artifact" "artifact" {
  reference = "ghcr.io/my-org/agents/assistant:v1"
  yaml      = docker-agent_agent_config.assistant.yaml
}

output "pinnedReference" {
  value = docker-agent_agent_artifact.artifact.pinned_reference
}

output "digest" {
  value = docker-agent_agent_artifact.artifact.digest
}
```
{{% /example %}}
{{% example %}}
### Push an existing agent file

```typescript
import * as pulumi from "@pulumi/pulumi";
import * as docker_agent from "@ediri/pulumi-docker-agent";
import * as fs from "fs";

// An agent file kept as raw YAML next to the program.
const agentFile = fs.readFileSync("./pirate.yaml", "utf8");

// Deleted from the registry when the resource is destroyed.
const pirate = new docker_agent.AgentArtifact("pirate", {
    reference: "localhost:5000/agents/pirate:latest",
    yaml: agentFile,
    annotations: {
        "org.opencontainers.image.source": "https://github.com/my-org/agents",
    },
    deleteBehavior: docker_agent.DeleteBehavior.Delete,
});
export const pinnedReference = pirate.pinnedReference;
```
```python
import pulumi
import ediri_docker_agent as docker_agent

# An agent file kept as raw YAML next to the program.
agent_file = (lambda path: open(path).read())("./pirate.yaml")

# Deleted from the registry when the resource is destroyed.
pirate = docker_agent.AgentArtifact("pirate",
    reference="localhost:5000/agents/pirate:latest",
    yaml=agent_file,
    annotations={
        "org.opencontainers.image.source": "https://github.com/my-org/agents",
    },
    delete_behavior=docker_agent.DeleteBehavior.DELETE)
pulumi.export("pinnedReference", pirate.pinned_reference)
```
```go
package main

import (
	"os"

	"github.com/dirien/pulumi-docker-agent/sdk/go/dockeragent"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func readFileOrPanic(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		panic(err.Error())
	}
	return string(data)
}

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		// An agent file kept as raw YAML next to the program.
		agentFile := readFileOrPanic("./pirate.yaml")

		// Deleted from the registry when the resource is destroyed.
		pirate, err := dockeragent.NewAgentArtifact(ctx, "pirate", &dockeragent.AgentArtifactArgs{
			Reference: pulumi.String("localhost:5000/agents/pirate:latest"),
			Yaml:      pulumi.String(agentFile),
			Annotations: pulumi.StringMap{
				"org.opencontainers.image.source": pulumi.String("https://github.com/my-org/agents"),
			},
			DeleteBehavior: dockeragent.DeleteBehaviorDelete,
		})
		if err != nil {
			return err
		}
		ctx.Export("pinnedReference", pirate.PinnedReference)
		return nil
	})
}
```
```csharp
using System.Collections.Generic;
using System.IO;
using System.Linq;
using Pulumi;
using DockerAgent = ediri.DockerAgent;

return await Deployment.RunAsync(() =>
{
    // An agent file kept as raw YAML next to the program.
    var agentFile = File.ReadAllText("./pirate.yaml");

    // Deleted from the registry when the resource is destroyed.
    var pirate = new DockerAgent.AgentArtifact("pirate", new()
    {
        Reference = "localhost:5000/agents/pirate:latest",
        Yaml = agentFile,
        Annotations =
        {
            { "org.opencontainers.image.source", "https://github.com/my-org/agents" },
        },
        DeleteBehavior = DockerAgent.DeleteBehavior.Delete,
    });

    return new Dictionary<string, object?>
    {
        ["pinnedReference"] = pirate.PinnedReference,
    };
});
```
```java
package generated_program;

import com.pulumi.Context;
import com.pulumi.Pulumi;
import com.pulumi.core.Output;
import io.github.dirien.dockeragent.AgentArtifact;
import io.github.dirien.dockeragent.AgentArtifactArgs;
import io.github.dirien.dockeragent.enums.DeleteBehavior;
import java.io.IOException;
import java.io.UncheckedIOException;
import java.nio.file.Files;
import java.nio.file.Paths;
import java.util.ArrayList;
import java.util.Arrays;
import java.util.Map;
import java.io.File;

public class App {
    public static void main(String[] args) {
        Pulumi.run(App::stack);
    }

    public static void stack(Context ctx) {
        // An agent file kept as raw YAML next to the program.
        final var agentFile = readFile("./pirate.yaml");

        // Deleted from the registry when the resource is destroyed.
        var pirate = new AgentArtifact("pirate", AgentArtifactArgs.builder()
            .reference("localhost:5000/agents/pirate:latest")
            .yaml(agentFile)
            .annotations(Map.of("org.opencontainers.image.source", "https://github.com/my-org/agents"))
            .deleteBehavior(DeleteBehavior.Delete)
            .build());

        ctx.export("pinnedReference", pirate.pinnedReference());
    }

    private static String readFile(String path) {
        try {
            return Files.readString(Paths.get(path));
        } catch (IOException e) {
            throw new UncheckedIOException(e);
        }
    }
}
```
```yaml
name: pirate
runtime: yaml
variables:
  # An agent file kept as raw YAML next to the program.
  agentFile:
    fn::readFile: ./pirate.yaml
resources:
  # Deleted from the registry when the resource is destroyed.
  pirate:
    type: docker-agent:index:AgentArtifact
    properties:
      reference: localhost:5000/agents/pirate:latest
      yaml: ${agentFile}
      annotations:
        org.opencontainers.image.source: https://github.com/my-org/agents
      deleteBehavior: delete
outputs:
  pinnedReference: ${pirate.pinnedReference}
```
```hcl
terraform {
  required_providers {
    docker-agent = {
      source = "pulumi/dirien/docker-agent"
    }
  }
}

# Deleted from the registry when the resource is destroyed.
resource "docker-agent_agent_artifact" "pirate" {
  reference = "localhost:5000/agents/pirate:latest"
  yaml      = local.agentFile
  annotations = {
    "org.opencontainers.image.source" = "https://github.com/my-org/agents"
  }
  delete_behavior = "delete"
}

locals {
  # An agent file kept as raw YAML next to the program.
  agentFile = file("./pirate.yaml")
}

output "pinnedReference" {
  value = docker-agent_agent_artifact.pirate.pinned_reference
}
```
{{% /example %}}
{{% /examples %}}
