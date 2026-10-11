// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
	"github.com/pulumi/pulumi/sdk/v3/go/property"

	"github.com/dirien/pulumi-docker-agent/provider/pkg/agentrun"
	"github.com/dirien/pulumi-docker-agent/provider/pkg/ociagent"
	"github.com/dirien/pulumi-docker-agent/provider/pkg/validate"
)

// AgentRun runs a prompt against an agent configuration during `pulumi up`, in process,
// with the docker-agent Go SDK.
type AgentRun struct{}

const agentRunDoc = `Runs a prompt against a [Docker Agent](https://docs.docker.com/ai/docker-agent/) configuration during
` + "`pulumi up`" + ` and exposes the agent's answer as outputs, for example to generate content, a review or a
structured decision that other resources consume.

The agent runs inside the provider process with the docker-agent Go SDK (the same runtime as
` + "`docker agent run --exec`" + `), headless and unattended. It never runs during ` + "`pulumi preview`" + `: the
outputs are unknown until the update. A change of ` + "`yaml`" + `, ` + "`reference`" + `, ` + "`agent`" + `,
` + "`prompt`" + `, ` + "`env`" + `, ` + "`inheritEnvironment`" + `, ` + "`workingDir`" + `, ` + "`toolApproval`" + `,
` + "`allowHookApprovals`" + ` or ` + "`triggers`" + ` (including a value that is unknown during the preview) runs the
agent again; a change of ` + "`timeoutSeconds`" + ` or ` + "`outputSecret`" + ` alone only updates the state.
` + "`pulumi up`" + ` without changes and ` + "`pulumi refresh`" + ` reuse the stored answer, deleting the resource
does nothing, and importing is not supported.

**Tools run on the machine executing ` + "`pulumi up`" + `.** With the default ` + "`toolApproval: deny`" + `, a tool
call only runs when docker-agent would run it without asking: tools annotated read-only (for example
` + "`think`" + `, ` + "`todo`" + `, ` + "`memory`" + ` reads, the filesystem toolset's read and search tools,
` + "`api`" + ` tools and MCP tools whose server declares them read-only), calls an ` + "`allow`" + ` rule of the
configuration's ` + "`permissions`" + ` matches, calls the configuration's own ` + "`safety`" + ` mode
(` + "`strict`" + `, ` + "`balanced`" + ` or ` + "`restricted`" + `) approves, and agent delegation
(` + "`transfer_task`" + `, ` + "`handoff`" + `). Approvals from the configuration's hooks (` + "`pre_tool_use`" + `
or ` + "`permission_request`" + `) are ignored unless ` + "`allowHookApprovals`" + ` is set. Every other call (shell
commands, ` + "`script`" + ` tools, file writes, ` + "`fetch`" + `, MCP tools without a read-only annotation, ...) is
rejected, and the model is told why. ` + "`toolApproval: allow`" + ` runs every tool call, like
` + "`docker agent run --yolo`" + `.

In both modes, these run without approval because they are part of the configuration: command hooks, MCP and LSP
servers started with ` + "`command`" + `, ` + "`docker:`" + ` MCP servers (through the Docker MCP Gateway), and
filesystem ` + "`post_edit`" + ` commands after an approved write. Read-only tools can read any file the provider can
read unless the toolset restricts them (` + "`allow_list`" + `). Coding-harness agents are rejected. Review
configurations pulled by ` + "`reference`" + ` before running them.

Credentials and ` + "`${env.NAME}`" + ` references resolve from ` + "`env`" + ` and, unless
` + "`inheritEnvironment`" + ` is false, the environment of the Pulumi CLI. With
` + "`inheritEnvironment: false`" + `, command hooks, ` + "`shell`" + `, ` + "`script`" + ` and
` + "`background_jobs`" + ` tools and MCP and LSP servers started with ` + "`command`" + ` also lose that environment:
they see the system variables (` + "`PATH`" + `, ` + "`HOME`" + `, ` + "`USER`" + `, ` + "`SHELL`" + `,
` + "`LANG`" + `, ` + "`TMPDIR`" + `, ` + "`TZ`" + `, ...) plus their own ` + "`env`" + ` (hooks also get
` + "`env`" + `). ` + "`docker:`" + ` MCP servers, filesystem ` + "`post_edit`" + ` commands and the
` + "`open_url`" + ` launcher always inherit it. Errors, warnings and logs are redacted: the values of
` + "`env`" + `, secret inputs, credential-named variables of the environment (` + "`*_API_KEY`" + `,
` + "`*_TOKEN`" + `, ` + "`*SECRET*`" + `, ` + "`*PASSWORD*`" + `, ...) and the variables the configuration names as
` + "`token_key`" + `. When the answer contains one of these values, ` + "`output`" + ` and
` + "`structuredOutput`" + ` are stored as secrets.

Each run is isolated from the docker-agent CLI on the same machine: docker-agent's data, config and cache directories
point to a private temporary directory that is deleted afterwards, so ` + "`~/.cagent`" + ` and the user settings,
aliases, global providers and credentials in ` + "`~/.config/cagent`" + ` are neither read nor written. The agent
works in a fresh temporary directory unless ` + "`workingDir`" + ` is set. Runs of one provider process execute one at
a time.

{{% examples %}}
## Example Usage
{{% example %}}
### Structured output from an agent

` + "```typescript" + `
import * as pulumi from "@pulumi/pulumi";
import * as dockeragent from "@ediri/pulumi-docker-agent";

const config = new pulumi.Config();

// The agent: its answer must match a JSON schema.
const reviewer = new dockeragent.AgentConfig("reviewer", {
    agents: [{
        name: "root",
        model: "openai/gpt-5-mini",
        instruction: "Rate the risk of the infrastructure change and explain it in one sentence.",
        structuredOutput: {
            name: "change_review",
            schema: {
                type: "object",
                properties: {
                    risk: { type: "string", enum: ["low", "medium", "high"] },
                    summary: { type: "string" },
                },
                required: ["risk", "summary"],
                additionalProperties: false,
            },
        },
    }],
});

// Runs the agent during ` + "`" + `pulumi up` + "`" + ` (never during a preview) and keeps its answer in the state.
const review = new dockeragent.AgentRun("review", {
    yaml: reviewer.yaml,
    prompt: "Upgrade the production PostgreSQL cluster from version 15 to 17.",
    env: { OPENAI_API_KEY: config.requireSecret("openaiApiKey") },
});

export const risk = review.structuredOutput.apply(o => o.risk);
export const summary = review.structuredOutput.apply(o => o.summary);
` + "```" + `
` + "```python" + `
import pulumi
import ediri_docker_agent as dockeragent

config = pulumi.Config()

# The agent: its answer must match a JSON schema.
reviewer = dockeragent.AgentConfig("reviewer", agents=[{
    "name": "root",
    "model": "openai/gpt-5-mini",
    "instruction": "Rate the risk of the infrastructure change and explain it in one sentence.",
    "structured_output": {
        "name": "change_review",
        "schema": {
            "type": "object",
            "properties": {
                "risk": {"type": "string", "enum": ["low", "medium", "high"]},
                "summary": {"type": "string"},
            },
            "required": ["risk", "summary"],
            "additionalProperties": False,
        },
    },
}])

# Runs the agent during ` + "`" + `pulumi up` + "`" + ` (never during a preview) and keeps its answer in the state.
review = dockeragent.AgentRun("review",
    yaml=reviewer.yaml,
    prompt="Upgrade the production PostgreSQL cluster from version 15 to 17.",
    env={"OPENAI_API_KEY": config.require_secret("openaiApiKey")})

pulumi.export("risk", review.structured_output.apply(lambda o: o["risk"]))
pulumi.export("summary", review.structured_output.apply(lambda o: o["summary"]))
` + "```" + `
` + "```yaml" + `
name: change-review
runtime: yaml
config:
  openaiApiKey:
    type: string
    secret: true

resources:
  # The agent: its answer must match a JSON schema.
  reviewer:
    type: docker-agent:index:AgentConfig
    properties:
      agents:
        - name: root
          model: openai/gpt-5-mini
          instruction: Rate the risk of the infrastructure change and explain it in one sentence.
          structuredOutput:
            name: change_review
            schema:
              type: object
              properties:
                risk: {type: string, enum: [low, medium, high]}
                summary: {type: string}
              required: [risk, summary]
              additionalProperties: false

  # Runs the agent during ` + "`" + `pulumi up` + "`" + ` (never during a preview) and keeps its answer in the state.
  review:
    type: docker-agent:index:AgentRun
    properties:
      yaml: ${reviewer.yaml}
      prompt: Upgrade the production PostgreSQL cluster from version 15 to 17.
      env:
        OPENAI_API_KEY: ${openaiApiKey}

outputs:
  risk: ${review.structuredOutput.risk}
  summary: ${review.structuredOutput.summary}
` + "```" + `
{{% /example %}}
{{% /examples %}}`

// Annotate describes the resource.
func (r *AgentRun) Annotate(a infer.Annotator) {
	a.Describe(&r, agentRunDoc)
}

// ToolApproval decides what happens to tool calls that docker-agent would ask a user to
// confirm.
type ToolApproval string

const (
	// ToolApprovalDeny rejects tool calls that need approval.
	ToolApprovalDeny ToolApproval = "deny"
	// ToolApprovalAllow approves every tool call.
	ToolApprovalAllow ToolApproval = "allow"
)

// Values lists the enum values.
func (ToolApproval) Values() []infer.EnumValue[ToolApproval] {
	return []infer.EnumValue[ToolApproval]{
		{Name: "Deny", Value: ToolApprovalDeny, Description: "Run only the tool calls docker-agent approves without " +
			"asking (read-only tools, `permissions` allow rules, the configuration's own safety mode); reject the rest."},
		{Name: "Allow", Value: ToolApprovalAllow, Description: "Approve every tool call, like `docker agent run --yolo`. " +
			"Tools run on the machine executing `pulumi up`."},
	}
}

// AgentRunArgs are the inputs of AgentRun.
type AgentRunArgs struct {
	YAML               *string           `pulumi:"yaml,optional"`
	Reference          *string           `pulumi:"reference,optional"`
	Agent              *string           `pulumi:"agent,optional"`
	Prompt             string            `pulumi:"prompt"`
	Env                map[string]string `pulumi:"env,optional" provider:"secret"`
	InheritEnvironment *bool             `pulumi:"inheritEnvironment,optional"`
	WorkingDir         *string           `pulumi:"workingDir,optional"`
	ToolApproval       *ToolApproval     `pulumi:"toolApproval,optional"`
	TimeoutSeconds     *int              `pulumi:"timeoutSeconds,optional"`
	Triggers           []any             `pulumi:"triggers,optional"`
	OutputSecret       *bool             `pulumi:"outputSecret,optional"`
	AllowHookApprovals *bool             `pulumi:"allowHookApprovals,optional"`
}

// Annotate describes the inputs.
func (a *AgentRunArgs) Annotate(an infer.Annotator) {
	an.Describe(&a.YAML, "The docker-agent configuration to run, for example `AgentConfig.yaml` or the "+
		"contents of an agent file. Exactly one of `yaml` and `reference` is required.")
	an.Describe(&a.Reference, "An agent shared through an OCI registry (`registry/repository:tag` or "+
		"`@sha256:...`), pulled like `getAgent` with the provider's `registryAuth`. Pin it by digest, or use "+
		"`triggers`, to control when a moved tag reruns the agent: only input changes do.")
	an.Describe(&a.Agent, "The agent to run. Defaults to `root`, or else the first agent of the configuration.")
	an.Describe(&a.Prompt, "The user message sent to the agent.")
	an.Describe(&a.Env, "Environment variables for model credentials (for example `OPENAI_API_KEY` or the "+
		"`tokenKey` of a custom provider) and `${env.NAME}` references in the configuration. They take precedence "+
		"over the inherited environment, are stored as secrets, are redacted from errors and logs, and also reach "+
		"command hooks. Other processes get them only through their toolset's own `env` (`${env.NAME}`).")
	an.Describe(&a.InheritEnvironment, "Also resolve credentials and `${env.NAME}` references from the "+
		"environment of the Pulumi CLI, and let the processes the configuration starts inherit that environment. "+
		"With `false`, command hooks, `shell`, `script` and `background_jobs` tools, and MCP and LSP servers "+
		"started with `command` only see the system variables (`PATH`, `HOME`, `USER`, `SHELL`, `LANG`, `TMPDIR`, "+
		"`TZ`, ...) plus their own `env` (hooks also get `env`); `docker:` MCP servers, filesystem `post_edit` "+
		"commands and the `open_url` launcher still inherit everything. docker-agent's other credential sources "+
		"(its `.env` file, credential helper, Docker Desktop) are never used.")
	an.SetDefault(&a.InheritEnvironment, true)
	an.Describe(&a.WorkingDir, "The agent's working directory, used by the shell, filesystem and other "+
		"local tools. Defaults to a fresh temporary directory that is deleted after the run. Relative paths are "+
		"resolved against the directory of the Pulumi program.")
	an.Describe(&a.ToolApproval, "What happens to tool calls that docker-agent would ask a user to confirm. "+
		"`deny` rejects them and tells the model; `allow` approves every tool call. Tools run on the machine "+
		"executing `pulumi up`.")
	an.SetDefault(&a.ToolApproval, ToolApprovalDeny)
	an.Describe(&a.TimeoutSeconds, "How long the run may take, in seconds, before it fails. Changing it updates the "+
		"state without running the agent again.")
	an.SetDefault(&a.TimeoutSeconds, int(agentrun.DefaultTimeout/time.Second))
	an.Describe(&a.Triggers, "Arbitrary values; any change runs the agent again, like a change of any other "+
		"input.")
	an.Describe(&a.OutputSecret, "Mark `output` and `structuredOutput` as secret. They are also secret when "+
		"`prompt` or `yaml` is secret, and when the answer contains the value of a secret (an `env` value, a secret "+
		"input, or a credential of the environment such as an API key the agent was given through `${env.NAME}`). "+
		"A secret `env` alone does not make them secret. Changing it updates the state without running the agent "+
		"again.")
	an.SetDefault(&a.OutputSecret, false)
	an.Describe(&a.AllowHookApprovals, "Let the configuration's hooks approve tool calls under `toolApproval: deny`. "+
		"docker-agent runs a tool call that a `pre_tool_use` hook (`permission_decision: allow`) or a "+
		"`permission_request` hook approves; by default AgentRun ignores such approvals and rejects the call, so "+
		"`deny` only runs what docker-agent approves by its rules. Hook denials always apply. It has no effect with "+
		"`toolApproval: allow`.")
	an.SetDefault(&a.AllowHookApprovals, false)
}

// AgentRunUsage is the token usage and cost of a run.
type AgentRunUsage struct {
	InputTokens  int     `pulumi:"inputTokens"`
	OutputTokens int     `pulumi:"outputTokens"`
	Cost         float64 `pulumi:"cost"`
}

// Annotate describes AgentRunUsage.
func (u *AgentRunUsage) Annotate(a infer.Annotator) {
	a.Describe(&u, "Token usage and cost of an agent run, including sub-agents.")
	a.Describe(&u.InputTokens, "Prompt tokens, including cached input tokens.")
	a.Describe(&u.OutputTokens, "Completion tokens.")
	a.Describe(&u.Cost, "Estimated cost in US dollars, from the model pricing of the models.dev catalog bundled "+
		"with docker-agent "+DockerAgentVersion+". 0 for models without known pricing, such as custom providers.")
}

// AgentRunState is the state of AgentRun.
type AgentRunState struct {
	AgentRunArgs
	Output           string        `pulumi:"output"`
	StructuredOutput any           `pulumi:"structuredOutput,optional"`
	AgentName        string        `pulumi:"agentName"`
	Usage            AgentRunUsage `pulumi:"usage"`
	ToolCalls        []string      `pulumi:"toolCalls"`
	RanAt            string        `pulumi:"ranAt"`
}

// Annotate describes the outputs.
func (s *AgentRunState) Annotate(an infer.Annotator) {
	an.Describe(&s.Output, "The agent's final answer.")
	an.Describe(&s.StructuredOutput, "The final answer decoded as JSON, when the answering agent declares "+
		"`structured_output`.")
	an.Describe(&s.AgentName, "The agent that gave the final answer: the selected agent, or the agent it "+
		"handed the conversation off to.")
	an.Describe(&s.Usage, "Token usage and estimated cost of the run.")
	an.Describe(&s.ToolCalls, "The names of the tools that ran, in order, including those of sub-agents the "+
		"agent transferred tasks to. Rejected tool calls are reported as warnings instead.")
	an.Describe(&s.RanAt, "When the run finished (RFC 3339).")
}

// agentRunOutputs are the outputs computed by a run.
var agentRunOutputs = []string{"output", "structuredOutput", "agentName", "usage", "toolCalls", "ranAt"}

// Check validates the inputs and, when it is known, the configuration.
func (*AgentRun) Check(ctx context.Context, req infer.CheckRequest) (infer.CheckResponse[AgentRunArgs], error) {
	args, failures, err := infer.DefaultCheck[AgentRunArgs](ctx, req.NewInputs)
	if err != nil {
		return infer.CheckResponse[AgentRunArgs]{Inputs: args, Failures: failures}, err
	}
	in := req.NewInputs
	hasYAML, hasRef := isSet(in.Get("yaml")), isSet(in.Get("reference"))
	switch {
	case hasYAML && hasRef:
		failures = append(failures, p.CheckFailure{Property: "reference", Reason: "set either `yaml` or `reference`, not both"})
	case !hasYAML && !hasRef:
		failures = append(failures, p.CheckFailure{Property: "yaml", Reason: "one of `yaml` and `reference` is required"})
	}
	if v := in.Get("prompt"); v.IsString() && strings.TrimSpace(v.AsString()) == "" {
		failures = append(failures, p.CheckFailure{Property: "prompt", Reason: "`prompt` must not be empty"})
	}
	if args.ToolApproval != nil && *args.ToolApproval != ToolApprovalDeny && *args.ToolApproval != ToolApprovalAllow {
		failures = append(failures, p.CheckFailure{Property: "toolApproval",
			Reason: fmt.Sprintf("invalid value %q: must be %q or %q", *args.ToolApproval, ToolApprovalDeny, ToolApprovalAllow)})
	}
	if v := in.Get("timeoutSeconds"); v.IsNumber() && v.AsNumber() <= 0 {
		failures = append(failures, p.CheckFailure{Property: "timeoutSeconds", Reason: "`timeoutSeconds` must be positive"})
	}

	if y := in.Get("yaml"); hasYAML && !hasRef && y.IsString() {
		secrets := secretStrings(in)
		doc := []byte(y.AsString())
		if !skipValidation(ctx, nil) {
			res := validate.Validate(ctx, req.Name+".yaml", doc)
			for _, e := range res.Errors {
				failures = append(failures, p.CheckFailure{Property: "yaml",
					Reason: "invalid docker-agent configuration: " + validate.Redact(e, secrets)})
			}
			if res.Valid {
				for _, e := range agentrun.CheckConfig(ctx, req.Name+".yaml", doc) {
					failures = append(failures, p.CheckFailure{Property: "yaml", Reason: validate.Redact(e, secrets)})
				}
			}
		}
		if a := in.Get("agent"); a.IsString() && a.AsString() != "" {
			if names := validate.AgentNames(doc); names != nil && !slices.Contains(names, a.AsString()) {
				failures = append(failures, p.CheckFailure{Property: "agent",
					Reason: fmt.Sprintf("agent %q is not defined in the configuration (agents: %s)",
						a.AsString(), strings.Join(names, ", "))})
			}
		}
	}
	return infer.CheckResponse[AgentRunArgs]{Inputs: args, Failures: failures}, nil
}

// isSet reports whether a raw input is present: a non-empty string or an unknown value.
func isSet(v property.Value) bool {
	if v.IsComputed() {
		return true
	}
	return v.IsString() && v.AsString() != ""
}

// Create runs the agent. Previews never run it.
func (*AgentRun) Create(ctx context.Context, req infer.CreateRequest[AgentRunArgs]) (infer.CreateResponse[AgentRunState], error) {
	if req.DryRun {
		return infer.CreateResponse[AgentRunState]{ID: req.Name, Output: AgentRunState{AgentRunArgs: req.Inputs}}, nil
	}
	state, err := runAgent(ctx, req.Name, req.Inputs)
	if err != nil {
		return infer.CreateResponse[AgentRunState]{}, err
	}
	return infer.CreateResponse[AgentRunState]{ID: req.Name, Output: state}, nil
}

// Update runs the agent again when an input that affects the run changed. Changes of
// `outputSecret` or `timeoutSeconds` alone only update the state.
func (*AgentRun) Update(
	ctx context.Context, req infer.UpdateRequest[AgentRunArgs, AgentRunState],
) (infer.UpdateResponse[AgentRunState], error) {
	rc := runContextFrom(ctx)
	if !rerunNeeded(req.State.AgentRunArgs, req.Inputs) {
		state := req.State
		state.AgentRunArgs = req.Inputs
		if rc != nil && state.Output != "" {
			rc.answerSecret = agentrun.AnswerContainsSecret(state.Output, agentrun.Request{
				YAML: []byte(runString(req.Inputs.YAML)), Env: req.Inputs.Env,
				InheritEnvironment: inheritEnvironment(req.Inputs), Secrets: rc.secrets,
			})
		}
		return infer.UpdateResponse[AgentRunState]{Output: state}, nil
	}
	if rc != nil {
		rc.rerun = true
	}
	if req.DryRun {
		return infer.UpdateResponse[AgentRunState]{Output: AgentRunState{AgentRunArgs: req.Inputs}}, nil
	}
	state, err := runAgent(ctx, req.ID, req.Inputs)
	if err != nil {
		return infer.UpdateResponse[AgentRunState]{}, err
	}
	return infer.UpdateResponse[AgentRunState]{Output: state}, nil
}

// rerunNeeded reports whether an input that affects the run changed: everything except
// `outputSecret` and `timeoutSeconds`.
func rerunNeeded(olds, news AgentRunArgs) bool {
	return len(runInputChanges(olds, news)) > 0
}

// runInputChanges lists the changed inputs that affect the run.
func runInputChanges(olds, news AgentRunArgs) []string {
	var changed []string
	add := func(name string, before, after any) {
		if !reflect.DeepEqual(before, after) {
			changed = append(changed, name)
		}
	}
	add("yaml", runString(olds.YAML), runString(news.YAML))
	add("reference", runString(olds.Reference), runString(news.Reference))
	add("agent", runString(olds.Agent), runString(news.Agent))
	add("prompt", olds.Prompt, news.Prompt)
	if !maps.Equal(olds.Env, news.Env) {
		changed = append(changed, "env")
	}
	add("inheritEnvironment", inheritEnvironment(olds), inheritEnvironment(news))
	add("workingDir", runString(olds.WorkingDir), runString(news.WorkingDir))
	add("toolApproval", toolApproval(olds), toolApproval(news))
	add("allowHookApprovals", runBool(olds.AllowHookApprovals), runBool(news.AllowHookApprovals))
	if len(olds.Triggers) != 0 || len(news.Triggers) != 0 {
		add("triggers", olds.Triggers, news.Triggers)
	}
	return changed
}

// Diff reports every input change as an update. Inputs that affect the run make the
// update run the agent again (see Update); unknown inputs are handled by agentRunResource.
func (*AgentRun) Diff(
	_ context.Context, req infer.DiffRequest[AgentRunArgs, AgentRunState],
) (infer.DiffResponse, error) {
	olds, news := req.State.AgentRunArgs, req.Inputs
	diff := map[string]p.PropertyDiff{}
	for _, name := range runInputChanges(olds, news) {
		diff[name] = p.PropertyDiff{Kind: p.Update, InputDiff: true}
	}
	if timeoutOf(olds) != timeoutOf(news) {
		diff["timeoutSeconds"] = p.PropertyDiff{Kind: p.Update, InputDiff: true}
	}
	if runBool(olds.OutputSecret) != runBool(news.OutputSecret) {
		diff["outputSecret"] = p.PropertyDiff{Kind: p.Update, InputDiff: true}
	}
	return infer.DiffResponse{HasChanges: len(diff) > 0, DetailedDiff: diff}, nil
}

// Read keeps the stored answer: a run cannot be observed after the fact. There is nothing
// to import.
func (*AgentRun) Read(
	_ context.Context, req infer.ReadRequest[AgentRunArgs, AgentRunState],
) (infer.ReadResponse[AgentRunArgs, AgentRunState], error) {
	if req.State.RanAt == "" {
		return infer.ReadResponse[AgentRunArgs, AgentRunState]{}, errAgentRunImport
	}
	return infer.ReadResponse[AgentRunArgs, AgentRunState](req), nil
}

var errAgentRunImport = errors.New("importing an AgentRun is not supported: a run has nothing to import; " +
	"declare the resource in your program and `pulumi up` runs the agent")

// Delete does nothing: a run leaves nothing behind.
func (*AgentRun) Delete(context.Context, infer.DeleteRequest[AgentRunState]) (infer.DeleteResponse, error) {
	return infer.DeleteResponse{}, nil
}

// WireDependencies declares secretness. `env` never makes an output secret. `output` and
// `structuredOutput` are secret with `outputSecret`, or when `prompt` or `yaml` is secret,
// since the answer can repeat them (agentRunResource also marks them secret when the
// answer contains a secret value). The run's outputs are only unknown when the agent runs
// again, which agentRunResource decides, so they are declared always known here; the
// inputs stored in the state keep their own secretness and computedness.
func (*AgentRun) WireDependencies(f infer.FieldSelector, args *AgentRunArgs, state *AgentRunState) {
	prompt, yaml, agent := f.InputField(&args.Prompt).Secret(), f.InputField(&args.YAML).Secret(), f.InputField(&args.Agent).Secret()
	for _, out := range []any{&state.Output, &state.StructuredOutput} {
		f.OutputField(out).AlwaysKnown()
		if args.OutputSecret != nil && *args.OutputSecret {
			f.OutputField(out).AlwaysSecret()
		} else {
			f.OutputField(out).DependsOn(prompt, yaml)
		}
	}
	f.OutputField(&state.AgentName).AlwaysKnown()
	f.OutputField(&state.AgentName).DependsOn(agent, yaml)
	f.OutputField(&state.ToolCalls).AlwaysKnown()
	f.OutputField(&state.ToolCalls).DependsOn(yaml)
	for _, out := range []any{&state.Usage, &state.RanAt} {
		f.OutputField(out).AlwaysKnown()
		f.OutputField(out).NeverSecret()
	}

	f.OutputField(&state.YAML).DependsOn(f.InputField(&args.YAML))
	f.OutputField(&state.Reference).DependsOn(f.InputField(&args.Reference))
	f.OutputField(&state.Agent).DependsOn(f.InputField(&args.Agent))
	f.OutputField(&state.Prompt).DependsOn(f.InputField(&args.Prompt))
	f.OutputField(&state.Env).AlwaysSecret()
	f.OutputField(&state.Env).DependsOn(f.InputField(&args.Env).Computed())
	f.OutputField(&state.InheritEnvironment).DependsOn(f.InputField(&args.InheritEnvironment))
	f.OutputField(&state.WorkingDir).DependsOn(f.InputField(&args.WorkingDir))
	f.OutputField(&state.ToolApproval).DependsOn(f.InputField(&args.ToolApproval))
	f.OutputField(&state.TimeoutSeconds).DependsOn(f.InputField(&args.TimeoutSeconds))
	f.OutputField(&state.Triggers).DependsOn(f.InputField(&args.Triggers))
	f.OutputField(&state.OutputSecret).DependsOn(f.InputField(&args.OutputSecret))
	f.OutputField(&state.AllowHookApprovals).DependsOn(f.InputField(&args.AllowHookApprovals))
}

func runAgent(ctx context.Context, name string, args AgentRunArgs) (AgentRunState, error) {
	state := AgentRunState{AgentRunArgs: args}
	doc, err := agentDocument(ctx, args)
	if err != nil {
		return state, err
	}
	// Secret inputs (env values, a secret yaml or prompt) never appear in errors,
	// warnings or logs. agentRunResource collects them from the raw inputs.
	rc := runContextFrom(ctx)
	var secrets []string
	if rc != nil {
		secrets = rc.secrets
	}
	// docker-agent's own log records: ERROR records of a successful run (a model that
	// fell back, a toolset that failed to start) become warnings, everything else is
	// debug output. A failed run reports its error instead.
	var logs []runLogRecord
	var logsMu sync.Mutex
	res, err := agentrun.Run(ctx, agentrun.Request{
		Name:               name,
		YAML:               doc,
		Agent:              runString(args.Agent),
		Prompt:             args.Prompt,
		Env:                args.Env,
		InheritEnvironment: inheritEnvironment(args),
		WorkingDir:         runString(args.WorkingDir),
		ToolApproval:       agentrun.ToolApproval(toolApproval(args)),
		AllowHookApprovals: runBool(args.AllowHookApprovals),
		Timeout:            time.Duration(timeoutOf(args)) * time.Second,
		Secrets:            secrets,
		Log: func(level slog.Level, msg string) {
			logsMu.Lock()
			defer logsMu.Unlock()
			logs = append(logs, runLogRecord{level, msg})
		},
	})
	logsMu.Lock()
	defer logsMu.Unlock()
	logger := p.GetLogger(ctx)
	for _, l := range logs {
		if err == nil && l.level >= slog.LevelError {
			logger.Warning("docker-agent: " + l.msg)
		} else {
			logger.Debug("docker-agent: " + l.msg)
		}
	}
	if err != nil {
		return state, err
	}
	for _, w := range res.Warnings {
		logger.Warning("docker-agent: " + w)
	}
	state.Output = res.Output
	if rc != nil {
		rc.answerSecret = res.OutputContainsSecret
	}
	if res.HasStructuredOutput {
		state.StructuredOutput = res.StructuredOutput
	}
	state.AgentName = res.AgentName
	state.Usage = AgentRunUsage{
		InputTokens:  int(res.Usage.InputTokens),
		OutputTokens: int(res.Usage.OutputTokens),
		Cost:         res.Usage.Cost,
	}
	state.ToolCalls = res.ToolCalls
	state.RanAt = time.Now().UTC().Format(time.RFC3339)
	return state, nil
}

// agentDocument returns the configuration to run: `yaml`, or the artifact `reference`
// points to.
func agentDocument(ctx context.Context, args AgentRunArgs) ([]byte, error) {
	if y := runString(args.YAML); y != "" {
		return []byte(y), nil
	}
	ref := runString(args.Reference)
	if ref == "" {
		return nil, fmt.Errorf("one of `yaml` and `reference` is required")
	}
	pulled, err := ociagent.Pull(ctx, ref, registryOptions(ctx, nil))
	if err != nil {
		return nil, fmt.Errorf("pulling %s: %w", ref, err)
	}
	return pulled.YAML, nil
}

type runLogRecord struct {
	level slog.Level
	msg   string
}

func runString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func runBool(b *bool) bool { return b != nil && *b }

func inheritEnvironment(a AgentRunArgs) bool {
	return a.InheritEnvironment == nil || *a.InheritEnvironment
}

func toolApproval(a AgentRunArgs) ToolApproval {
	if a.ToolApproval == nil || *a.ToolApproval == "" {
		return ToolApprovalDeny
	}
	return *a.ToolApproval
}

func timeoutOf(a AgentRunArgs) int {
	if a.TimeoutSeconds == nil || *a.TimeoutSeconds <= 0 {
		return int(agentrun.DefaultTimeout / time.Second)
	}
	return *a.TimeoutSeconds
}

// NewAgentRunResource returns the AgentRun resource for the provider builder. It wraps
// the inferred resource so that previews report every output of a (re)run as unknown:
// pulumi-go-provider only marks an output unknown when an input it depends on changed,
// and misses inputs that were absent before (for example newly added `triggers`).
func NewAgentRunResource() infer.InferredResource {
	return agentRunResource{infer.Resource(&AgentRun{})}
}

type agentRunResource struct {
	infer.InferredResource
}

// runContext passes per-call information between agentRunResource and the typed
// resource: the secret input strings (redacted from errors and logs) in, whether the
// answer contains a secret and whether the agent runs out.
type runContext struct {
	secrets      []string
	answerSecret bool
	rerun        bool
}

type runContextKey struct{}

func runContextFrom(ctx context.Context) *runContext {
	rc, _ := ctx.Value(runContextKey{}).(*runContext)
	return rc
}

func (r agentRunResource) Create(ctx context.Context, req p.CreateRequest) (p.CreateResponse, error) {
	rc := &runContext{secrets: secretStrings(req.Properties)}
	resp, err := r.InferredResource.Create(context.WithValue(ctx, runContextKey{}, rc), req)
	if err != nil {
		return resp, err
	}
	if req.DryRun {
		resp.Properties = agentRunPreview(req.Properties, resp.Properties)
	} else if rc.answerSecret {
		resp.Properties = markSecret(resp.Properties, "output", "structuredOutput")
	}
	return resp, nil
}

func (r agentRunResource) Update(ctx context.Context, req p.UpdateRequest) (p.UpdateResponse, error) {
	rc := &runContext{secrets: secretStrings(req.Inputs)}
	resp, err := r.InferredResource.Update(context.WithValue(ctx, runContextKey{}, rc), req)
	if err != nil {
		return resp, err
	}
	switch {
	case req.DryRun && (rc.rerun || req.Inputs.HasComputed()):
		resp.Properties = agentRunPreview(req.Inputs, resp.Properties)
	case rc.answerSecret:
		resp.Properties = markSecret(resp.Properties, "output", "structuredOutput")
	}
	return resp, nil
}

// Diff reports an update for every input that is unknown during a preview: the typed
// Diff sees unknown values as zero values, which equal an absent old value.
func (r agentRunResource) Diff(ctx context.Context, req p.DiffRequest) (p.DiffResponse, error) {
	resp, err := r.InferredResource.Diff(ctx, req)
	if err != nil {
		return resp, err
	}
	for k, v := range req.Inputs.All {
		if !v.HasComputed() {
			continue
		}
		if resp.DetailedDiff == nil {
			resp.DetailedDiff = map[string]p.PropertyDiff{}
		}
		resp.DetailedDiff[k] = p.PropertyDiff{Kind: p.Update, InputDiff: true}
		resp.HasChanges = true
	}
	return resp, nil
}

// agentRunPreview marks every run output unknown, keeping the secretness the real outputs
// will have.
func agentRunPreview(inputs, out property.Map) property.Map {
	outputSecret := inputs.Get("outputSecret")
	answerSecret := (outputSecret.IsBool() && outputSecret.AsBool()) || outputSecret.Secret() ||
		inputs.Get("prompt").Secret() || inputs.Get("yaml").Secret()
	for _, k := range agentRunOutputs {
		secret := false
		switch k {
		case "output", "structuredOutput":
			secret = answerSecret
		case "agentName":
			secret = inputs.Get("agent").Secret() || inputs.Get("yaml").Secret()
		case "toolCalls":
			secret = inputs.Get("yaml").Secret()
		}
		out = out.Set(k, property.New(property.Computed).WithSecret(secret))
	}
	return out
}
