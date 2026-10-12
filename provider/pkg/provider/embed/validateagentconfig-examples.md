{{% examples %}}
## Example Usage
{{% example %}}
### Validate an agent file

```typescript
import * as pulumi from "@pulumi/pulumi";
import * as docker_agent from "@ediri/pulumi-docker-agent";
import * as fs from "fs";

// An agent file kept as raw YAML next to the program.
const agentFile = fs.readFileSync("./agent.yaml", "utf8");

// Never fails on an invalid document: check `valid` and `errors`.
const result = docker_agent.validateAgentConfigOutput({
    yaml: agentFile,
});
export const valid = result.valid;
export const errors = result.errors;
export const requiredEnvVars = result.requiredEnvVars;
```
```python
import pulumi
import ediri_docker_agent as docker_agent

# An agent file kept as raw YAML next to the program.
agent_file = (lambda path: open(path).read())("./agent.yaml")

# Never fails on an invalid document: check `valid` and `errors`.
result = docker_agent.validate_agent_config_output(yaml=agent_file)
pulumi.export("valid", result.valid)
pulumi.export("errors", result.errors)
pulumi.export("requiredEnvVars", result.required_env_vars)
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
		agentFile := readFileOrPanic("./agent.yaml")

		// Never fails on an invalid document: check `valid` and `errors`.
		result := dockeragent.ValidateAgentConfigOutput(ctx, dockeragent.ValidateAgentConfigOutputArgs{
			Yaml: pulumi.String(agentFile),
		}, nil)
		ctx.Export("valid", result.Valid())
		ctx.Export("errors", result.Errors())
		ctx.Export("requiredEnvVars", result.RequiredEnvVars())
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
    var agentFile = File.ReadAllText("./agent.yaml");

    // Never fails on an invalid document: check `valid` and `errors`.
    var result = DockerAgent.ValidateAgentConfig.Invoke(new()
    {
        Yaml = agentFile,
    });

    return new Dictionary<string, object?>
    {
        ["valid"] = result.Apply(validateAgentConfigResult => validateAgentConfigResult.Valid),
        ["errors"] = result.Apply(validateAgentConfigResult => validateAgentConfigResult.Errors),
        ["requiredEnvVars"] = result.Apply(validateAgentConfigResult => validateAgentConfigResult.RequiredEnvVars),
    };
});
```
```java
package generated_program;

import com.pulumi.Context;
import com.pulumi.Pulumi;
import com.pulumi.core.Output;
import io.github.dirien.dockeragent.DockeragentFunctions;
import io.github.dirien.dockeragent.inputs.ValidateAgentConfigArgs;
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
        final var agentFile = readFile("./agent.yaml");

        // Never fails on an invalid document: check `valid` and `errors`.
        final var result = DockeragentFunctions.validateAgentConfig(ValidateAgentConfigArgs.builder()
            .yaml(agentFile)
            .build());

        ctx.export("valid", result.applyValue(_result -> _result.valid()));
        ctx.export("errors", result.applyValue(_result -> _result.errors()));
        ctx.export("requiredEnvVars", result.applyValue(_result -> _result.requiredEnvVars()));
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
name: validate-file
runtime: yaml
variables:
  # An agent file kept as raw YAML next to the program.
  agentFile:
    fn::readFile: ./agent.yaml
  # Never fails on an invalid document: check `valid` and `errors`.
  result:
    fn::invoke:
      function: docker-agent:index:validateAgentConfig
      arguments:
        yaml: ${agentFile}
outputs:
  valid: ${result.valid}
  errors: ${result.errors}
  requiredEnvVars: ${result.requiredEnvVars}
```
```hcl
terraform {
  required_providers {
    docker-agent = {
      source = "pulumi/dirien/docker-agent"
    }
  }
}

# Never fails on an invalid document: check `valid` and `errors`.
data "docker-agent_validate_agent_config" "result" {
  yaml = local.agentFile
}

locals {
  # An agent file kept as raw YAML next to the program.
  agentFile = file("./agent.yaml")
}

output "valid" {
  value = data.docker-agent_validate_agent_config.result.valid
}

output "errors" {
  value = data.docker-agent_validate_agent_config.result.errors
}

output "requiredEnvVars" {
  value = data.docker-agent_validate_agent_config.result.required_env_vars
}
```
{{% /example %}}
{{% example %}}
### Validate a shared agent before running it

```typescript
import * as pulumi from "@pulumi/pulumi";
import * as docker_agent from "@ediri/pulumi-docker-agent";

const shared = docker_agent.getAgentOutput({
    reference: "docker.io/my-org/assistant:latest",
});

// Checks the pulled configuration against the docker-agent version of the provider.
const check = docker_agent.validateAgentConfigOutput({
    yaml: shared.yaml,
});
export const valid = check.valid;
export const errors = check.errors;
export const agentNames = check.agentNames;
```
```python
import pulumi
import ediri_docker_agent as docker_agent

shared = docker_agent.get_agent_output(reference="docker.io/my-org/assistant:latest")

# Checks the pulled configuration against the docker-agent version of the provider.
check = docker_agent.validate_agent_config_output(yaml=shared.yaml)
pulumi.export("valid", check.valid)
pulumi.export("errors", check.errors)
pulumi.export("agentNames", check.agent_names)
```
```go
package main

import (
	"github.com/dirien/pulumi-docker-agent/sdk/go/dockeragent"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		shared := dockeragent.GetAgentOutput(ctx, dockeragent.GetAgentOutputArgs{
			Reference: pulumi.String("docker.io/my-org/assistant:latest"),
		}, nil)

		// Checks the pulled configuration against the docker-agent version of the provider.
		check := dockeragent.ValidateAgentConfigOutput(ctx, dockeragent.ValidateAgentConfigOutputArgs{
			Yaml: shared.Yaml(),
		}, nil)
		ctx.Export("valid", check.Valid())
		ctx.Export("errors", check.Errors())
		ctx.Export("agentNames", check.AgentNames())
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
    var shared = DockerAgent.GetAgent.Invoke(new()
    {
        Reference = "docker.io/my-org/assistant:latest",
    });

    // Checks the pulled configuration against the docker-agent version of the provider.
    var check = DockerAgent.ValidateAgentConfig.Invoke(new()
    {
        Yaml = shared.Apply(getAgentResult => getAgentResult.Yaml),
    });

    return new Dictionary<string, object?>
    {
        ["valid"] = check.Apply(validateAgentConfigResult => validateAgentConfigResult.Valid),
        ["errors"] = check.Apply(validateAgentConfigResult => validateAgentConfigResult.Errors),
        ["agentNames"] = check.Apply(validateAgentConfigResult => validateAgentConfigResult.AgentNames),
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
import io.github.dirien.dockeragent.inputs.ValidateAgentConfigArgs;
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
        final var shared = DockeragentFunctions.getAgent(GetAgentArgs.builder()
            .reference("docker.io/my-org/assistant:latest")
            .build());

        // Checks the pulled configuration against the docker-agent version of the provider.
        final var check = DockeragentFunctions.validateAgentConfig(ValidateAgentConfigArgs.builder()
            .yaml(shared.applyValue(_shared -> _shared.yaml()))
            .build());

        ctx.export("valid", check.applyValue(_check -> _check.valid()));
        ctx.export("errors", check.applyValue(_check -> _check.errors()));
        ctx.export("agentNames", check.applyValue(_check -> _check.agentNames()));
    }
}
```
```yaml
name: validate-shared
runtime: yaml
variables:
  shared:
    fn::invoke:
      function: docker-agent:index:getAgent
      arguments:
        reference: docker.io/my-org/assistant:latest
  # Checks the pulled configuration against the docker-agent version of the provider.
  check:
    fn::invoke:
      function: docker-agent:index:validateAgentConfig
      arguments:
        yaml: ${shared.yaml}
outputs:
  valid: ${check.valid}
  errors: ${check.errors}
  agentNames: ${check.agentNames}
```
```hcl
terraform {
  required_providers {
    docker-agent = {
      source = "pulumi/dirien/docker-agent"
    }
  }
}

data "docker-agent_agent" "shared" {
  reference = "docker.io/my-org/assistant:latest"
}

# Checks the pulled configuration against the docker-agent version of the provider.
data "docker-agent_validate_agent_config" "check" {
  yaml = data.docker-agent_agent.shared.yaml
}

output "valid" {
  value = data.docker-agent_validate_agent_config.check.valid
}

output "errors" {
  value = data.docker-agent_validate_agent_config.check.errors
}

output "agentNames" {
  value = data.docker-agent_validate_agent_config.check.agent_names
}
```
{{% /example %}}
{{% /examples %}}
