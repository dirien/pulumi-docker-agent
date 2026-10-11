// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"

	"github.com/pulumi/pulumi-go-provider/infer"

	"github.com/dirien/pulumi-docker-agent/provider/pkg/validate"
)

// ValidateAgentConfig validates an arbitrary docker-agent YAML document.
type ValidateAgentConfig struct{}

const validateAgentConfigDoc = `Validates a Docker Agent YAML document with the docker-agent library the provider
is built with (` + DockerAgentVersion + `), the same checks ` + "`docker agent debug config`" + ` runs plus a check
for unknown toolset types and model providers.

Use it for agent files you keep as raw YAML. The function never fails on an invalid document; it returns
` + "`valid: false`" + ` and the reasons in ` + "`errors`" + `.

{{% examples %}}
## Example Usage
{{% example %}}
### Validate an agent file

` + "```typescript" + `
import * as fs from "fs";
import * as dockeragent from "@ediri/pulumi-docker-agent";

const result = dockeragent.validateAgentConfigOutput({ yaml: fs.readFileSync("agent.yaml", "utf8") });

export const valid = result.valid;
export const errors = result.errors;
` + "```" + `
` + "```python" + `
import pulumi
import ediri_docker_agent as dockeragent

result = dockeragent.validate_agent_config_output(yaml=open("agent.yaml").read())
pulumi.export("valid", result.valid)
` + "```" + `
{{% /example %}}
{{% /examples %}}`

// Annotate describes the function.
func (v *ValidateAgentConfig) Annotate(a infer.Annotator) {
	a.SetToken("index", "validateAgentConfig")
	a.Describe(&v, validateAgentConfigDoc)
}

// ValidateAgentConfigArgs are the inputs of validateAgentConfig.
type ValidateAgentConfigArgs struct {
	YAML string `pulumi:"yaml"`
}

// Annotate describes the inputs.
func (a *ValidateAgentConfigArgs) Annotate(an infer.Annotator) {
	an.Describe(&a.YAML, "The docker-agent YAML document to validate.")
}

// ValidateAgentConfigResult is the result of validateAgentConfig.
type ValidateAgentConfigResult struct {
	Valid           bool     `pulumi:"valid"`
	Errors          []string `pulumi:"errors"`
	AgentNames      []string `pulumi:"agentNames"`
	DefaultAgent    string   `pulumi:"defaultAgent"`
	RequiredEnvVars []string `pulumi:"requiredEnvVars"`
}

// Annotate describes the result.
func (r *ValidateAgentConfigResult) Annotate(an infer.Annotator) {
	an.Describe(&r.Valid, "Whether the document is a valid docker-agent configuration.")
	an.Describe(&r.Errors, "Why the document is invalid. Empty when `valid` is true.")
	an.Describe(&r.AgentNames, "The agent names, in order (also reported for invalid documents when they "+
		"can be parsed).")
	an.Describe(&r.DefaultAgent, "The agent docker-agent starts by default: `root` if an agent has that name, "+
		"else the first agent.")
	an.Describe(&r.RequiredEnvVars, "Best effort list of the environment variables the configured models "+
		"need at run time. Only computed for valid documents.")
}

// Invoke validates the document.
func (*ValidateAgentConfig) Invoke(
	ctx context.Context, req infer.FunctionRequest[ValidateAgentConfigArgs],
) (infer.FunctionResponse[ValidateAgentConfigResult], error) {
	res := validate.Validate(ctx, "agent.yaml", []byte(req.Input.YAML))
	logWarnings(ctx, res.Warnings, nil)
	out := ValidateAgentConfigResult{
		Valid:           res.Valid,
		Errors:          res.Errors,
		AgentNames:      res.AgentNames,
		DefaultAgent:    res.DefaultAgent,
		RequiredEnvVars: res.RequiredEnvVars,
	}
	if out.Errors == nil {
		out.Errors = []string{}
	}
	if out.AgentNames == nil {
		out.AgentNames = []string{}
	}
	if out.RequiredEnvVars == nil {
		out.RequiredEnvVars = []string{}
	}
	return infer.FunctionResponse[ValidateAgentConfigResult]{Output: out}, nil
}
