{{% examples %}}
## Example Usage
{{% example %}}
### Read a shared agent

```typescript
import * as pulumi from "@pulumi/pulumi";
import * as docker_agent from "@ediri/pulumi-docker-agent";

// Reads the artifact like `docker agent share pull`, without writing a file.
const agent = docker_agent.getAgentOutput({
    reference: "docker.io/my-org/assistant:latest",
});
export const agentYaml = agent.yaml;
export const agentNames = agent.agentNames;
export const pinnedReference = agent.pinnedReference;
```
```python
import pulumi
import ediri_docker_agent as docker_agent

# Reads the artifact like `docker agent share pull`, without writing a file.
agent = docker_agent.get_agent_output(reference="docker.io/my-org/assistant:latest")
pulumi.export("agentYaml", agent.yaml)
pulumi.export("agentNames", agent.agent_names)
pulumi.export("pinnedReference", agent.pinned_reference)
```
```go
package main

import (
	"github.com/dirien/pulumi-docker-agent/sdk/go/dockeragent"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		// Reads the artifact like `docker agent share pull`, without writing a file.
		agent := dockeragent.GetAgentOutput(ctx, dockeragent.GetAgentOutputArgs{
			Reference: pulumi.String("docker.io/my-org/assistant:latest"),
		}, nil)
		ctx.Export("agentYaml", agent.Yaml())
		ctx.Export("agentNames", agent.AgentNames())
		ctx.Export("pinnedReference", agent.PinnedReference())
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
    // Reads the artifact like `docker agent share pull`, without writing a file.
    var agent = DockerAgent.GetAgent.Invoke(new()
    {
        Reference = "docker.io/my-org/assistant:latest",
    });

    return new Dictionary<string, object?>
    {
        ["agentYaml"] = agent.Apply(getAgentResult => getAgentResult.Yaml),
        ["agentNames"] = agent.Apply(getAgentResult => getAgentResult.AgentNames),
        ["pinnedReference"] = agent.Apply(getAgentResult => getAgentResult.PinnedReference),
    };
});
```
```java
package generated_program;

import com.pulumi.Context;
import com.pulumi.Pulumi;
import com.pulumi.core.Output;
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
        // Reads the artifact like `docker agent share pull`, without writing a file.
        final var agent = DockeragentFunctions.getAgent(GetAgentArgs.builder()
            .reference("docker.io/my-org/assistant:latest")
            .build());

        ctx.export("agentYaml", agent.applyValue(_agent -> _agent.yaml()));
        ctx.export("agentNames", agent.applyValue(_agent -> _agent.agentNames()));
        ctx.export("pinnedReference", agent.applyValue(_agent -> _agent.pinnedReference()));
    }
}
```
```yaml
name: read-agent
runtime: yaml
variables:
  # Reads the artifact like `docker agent share pull`, without writing a file.
  agent:
    fn::invoke:
      function: docker-agent:index:getAgent
      arguments:
        reference: docker.io/my-org/assistant:latest
outputs:
  agentYaml: ${agent.yaml}
  agentNames: ${agent.agentNames}
  pinnedReference: ${agent.pinnedReference}
```
```hcl
terraform {
  required_providers {
    docker-agent = {
      source = "pulumi/dirien/docker-agent"
    }
  }
}

# Reads the artifact like `docker agent share pull`, without writing a file.
data "docker-agent_agent" "agent" {
  reference = "docker.io/my-org/assistant:latest"
}

output "agentYaml" {
  value = data.docker-agent_agent.agent.yaml
}

output "agentNames" {
  value = data.docker-agent_agent.agent.agent_names
}

output "pinnedReference" {
  value = data.docker-agent_agent.agent.pinned_reference
}
```
{{% /example %}}
{{% example %}}
### Read back an agent pushed by the same program

```typescript
import * as pulumi from "@pulumi/pulumi";
import * as docker_agent from "@ediri/pulumi-docker-agent";

const assistant = new docker_agent.AgentConfig("assistant", {
    metadata: {
        author: "platform-team",
        version: "1.0.0",
    },
    agents: [{
        name: "root",
        model: "openai/gpt-5-mini",
        instruction: "You are a helpful assistant. Answer briefly.",
    }],
});
const artifact = new docker_agent.AgentArtifact("artifact", {
    reference: "localhost:5000/agents/assistant:v1",
    yaml: assistant.yaml,
});

// Reads the pushed artifact by digest once the push is done.
const pulled = docker_agent.getAgentOutput({
    reference: artifact.pinnedReference,
});
export const digest = pulled.digest;
export const defaultAgent = pulled.defaultAgent;
export const annotations = pulled.annotations;
```
```python
import pulumi
import ediri_docker_agent as docker_agent

assistant = docker_agent.AgentConfig("assistant",
    metadata={
        "author": "platform-team",
        "version": "1.0.0",
    },
    agents=[{
        "name": "root",
        "model": "openai/gpt-5-mini",
        "instruction": "You are a helpful assistant. Answer briefly.",
    }])
artifact = docker_agent.AgentArtifact("artifact",
    reference="localhost:5000/agents/assistant:v1",
    yaml=assistant.yaml)

# Reads the pushed artifact by digest once the push is done.
pulled = docker_agent.get_agent_output(reference=artifact.pinned_reference)
pulumi.export("digest", pulled.digest)
pulumi.export("defaultAgent", pulled.default_agent)
pulumi.export("annotations", pulled.annotations)
```
```go
package main

import (
	"github.com/dirien/pulumi-docker-agent/sdk/go/dockeragent"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		assistant, err := dockeragent.NewAgentConfig(ctx, "assistant", &dockeragent.AgentConfigArgs{
			Metadata: &dockeragent.MetadataArgs{
				Author:  pulumi.String("platform-team"),
				Version: pulumi.String("1.0.0"),
			},
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
		artifact, err := dockeragent.NewAgentArtifact(ctx, "artifact", &dockeragent.AgentArtifactArgs{
			Reference: pulumi.String("localhost:5000/agents/assistant:v1"),
			Yaml:      assistant.Yaml,
		})
		if err != nil {
			return err
		}

		// Reads the pushed artifact by digest once the push is done.
		pulled := dockeragent.GetAgentOutput(ctx, dockeragent.GetAgentOutputArgs{
			Reference: artifact.PinnedReference,
		}, nil)
		ctx.Export("digest", pulled.Digest())
		ctx.Export("defaultAgent", pulled.DefaultAgent())
		ctx.Export("annotations", pulled.Annotations())
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
    var assistant = new DockerAgent.AgentConfig("assistant", new()
    {
        Metadata = new DockerAgent.Inputs.MetadataArgs
        {
            Author = "platform-team",
            Version = "1.0.0",
        },
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

    var artifact = new DockerAgent.AgentArtifact("artifact", new()
    {
        Reference = "localhost:5000/agents/assistant:v1",
        Yaml = assistant.Yaml,
    });

    // Reads the pushed artifact by digest once the push is done.
    var pulled = DockerAgent.GetAgent.Invoke(new()
    {
        Reference = artifact.PinnedReference,
    });

    return new Dictionary<string, object?>
    {
        ["digest"] = pulled.Apply(getAgentResult => getAgentResult.Digest),
        ["defaultAgent"] = pulled.Apply(getAgentResult => getAgentResult.DefaultAgent),
        ["annotations"] = pulled.Apply(getAgentResult => getAgentResult.Annotations),
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
        var assistant = new AgentConfig("assistant", AgentConfigArgs.builder()
            .metadata(MetadataArgs.builder()
                .author("platform-team")
                .version("1.0.0")
                .build())
            .agents(AgentArgs.builder()
                .name("root")
                .model("openai/gpt-5-mini")
                .instruction("You are a helpful assistant. Answer briefly.")
                .build())
            .build());

        var artifact = new AgentArtifact("artifact", AgentArtifactArgs.builder()
            .reference("localhost:5000/agents/assistant:v1")
            .yaml(assistant.yaml())
            .build());

        // Reads the pushed artifact by digest once the push is done.
        final var pulled = DockeragentFunctions.getAgent(GetAgentArgs.builder()
            .reference(artifact.pinnedReference())
            .build());

        ctx.export("digest", pulled.applyValue(_pulled -> _pulled.digest()));
        ctx.export("defaultAgent", pulled.applyValue(_pulled -> _pulled.defaultAgent()));
        ctx.export("annotations", pulled.applyValue(_pulled -> _pulled.annotations()));
    }
}
```
```yaml
name: round-trip
runtime: yaml
resources:
  assistant:
    type: docker-agent:index:AgentConfig
    properties:
      metadata:
        author: platform-team
        version: 1.0.0
      agents:
        - name: root
          model: openai/gpt-5-mini
          instruction: You are a helpful assistant. Answer briefly.
  artifact:
    type: docker-agent:index:AgentArtifact
    properties:
      reference: localhost:5000/agents/assistant:v1
      yaml: ${assistant.yaml}
variables:
  # Reads the pushed artifact by digest once the push is done.
  pulled:
    fn::invoke:
      function: docker-agent:index:getAgent
      arguments:
        reference: ${artifact.pinnedReference}
outputs:
  digest: ${pulled.digest}
  defaultAgent: ${pulled.defaultAgent}
  annotations: ${pulled.annotations}
```
```hcl
terraform {
  required_providers {
    docker-agent = {
      source = "pulumi/dirien/docker-agent"
    }
  }
}

# Reads the pushed artifact by digest once the push is done.
data "docker-agent_agent" "pulled" {
  reference = docker-agent_agent_artifact.artifact.pinned_reference
}

resource "docker-agent_agent_config" "assistant" {
  metadata = {
    author  = "platform-team"
    version = "1.0.0"
  }
  agents {
    name        = "root"
    model       = "openai/gpt-5-mini"
    instruction = "You are a helpful assistant. Answer briefly."
  }
}

resource "docker-agent_agent_artifact" "artifact" {
  reference = "localhost:5000/agents/assistant:v1"
  yaml      = docker-agent_agent_config.assistant.yaml
}

output "digest" {
  value = data.docker-agent_agent.pulled.digest
}

output "defaultAgent" {
  value = data.docker-agent_agent.pulled.default_agent
}

output "annotations" {
  value = data.docker-agent_agent.pulled.annotations
}
```
{{% /example %}}
{{% /examples %}}
