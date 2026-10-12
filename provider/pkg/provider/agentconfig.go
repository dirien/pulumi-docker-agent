// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"

	"github.com/dirien/pulumi-docker-agent/provider/pkg/render"
	"github.com/dirien/pulumi-docker-agent/provider/pkg/validate"
)

// AgentConfig is a typed, validated docker-agent configuration. It has no external side
// effects: it renders the YAML document and validates it with the docker-agent library.
type AgentConfig struct{}

const agentConfigDoc = `A typed, validated [Docker Agent](https://docs.docker.com/ai/docker-agent/) configuration.

` + "`AgentConfig`" + ` renders its inputs into a deterministic docker-agent YAML document (output ` + "`yaml`" + `)
and validates it with the docker-agent library during preview, so typos, unknown fields, references to
undefined models or agents and unknown toolset types fail before anything is deployed. The resource is
purely local: it creates nothing outside of the Pulumi state. Feed ` + "`yaml`" + ` into an ` + "`AgentArtifact`" + ` to
share the agent through an OCI registry, or write it to a file for ` + "`docker agent run`" + `.

Every key of the docker-agent configuration (version ` + render.DefaultConfigVersion + `) is a typed input: agents, models,
providers, toolsets, MCP and RAG definitions, commands, skills, hooks, permissions, sandbox mode (` + "`runtime`" + `),
budgets, evaluators, structured output and flavors. Inputs are camelCase and rendered as the snake_case keys
of the docker-agent YAML. Agents are rendered in list order. The agent named ` + "`root`" + `, or else the first agent,
is the default agent. Each object also has an ` + "`extra`" + ` map, merged verbatim into the YAML, for keys added by
docker-agent releases newer than the one this provider pins. ` + "`instructionFile`" + ` needs ` + "`skipValidation`" + `
because validation happens in memory; read the file in your program and pass its contents as ` + "`instruction`" + `.

Closed value sets, such as the ` + "`safety`" + ` modes or a hook's ` + "`type`" + `, are enums. A value that this
provider version does not list fails on its property during preview unless ` + "`skipValidation`" + ` is set; in
C# and Java, whose enums cannot hold other values, set such a value through the object's ` + "`extra`" + ` map
(for example ` + "`runtime.extra`" + `), which replaces the typed key.

The preview reports every independent problem at once, each attached to the input that caused it, for example
` + "`property agents[0].subAgents ... has a problem: invalid docker-agent configuration: agent 'root' " +
	"references non-existent sub-agent 'missing'`" + `.
docker-agent stops at the first problem; the provider removes the faulty value and validates again to find the
rest. A problem with a whole definition (a model, provider or toolset) is reported alone. The provider also warns
about ` + "`headers`" + ` and ` + "`env`" + ` values that look like literal credentials: they end up in ` + "`yaml`" + `
and in every artifact made from it, so use a ` + "`${env.NAME}`" + ` placeholder that docker-agent resolves at run
time instead.`

var agentConfigImportDoc = `An existing docker-agent configuration can be imported, and ` + "`pulumi import`" + ` generates its typed
inputs in your program's language. The ID is ` + "`file://<path>`" + `, a path ending in ` + "`.yaml`" + `, ` + "`.yml`" + ` or
` + "`.hcl`" + ` (relative to the project directory), or ` + "`oci://<registry>/<repository>:<tag>`" + ` (or
` + "`@sha256:<digest>`" + `) for a shared agent, read with the provider's registry credentials. Only configurations
that pass validation are imported; set the provider's ` + "`skipValidation`" + ` to import one that needs a newer
docker-agent (the resource then gets ` + "`skipValidation: true`" + `). Keys without a typed input go to
` + "`extra`" + `, ` + "`instruction_file`" + ` contents become ` + "`instruction`" + `, and comments, key order and YAML
anchors are not kept. The imported resource's ID is its name; it keeps no link to the file or artifact.` +
	importCommand("docker-agent:index:AgentConfig", "assistant", "./agent.yaml") +
	importCommand("docker-agent:index:AgentConfig", "shared", "oci://ghcr.io/my-org/agents/assistant:v1")

// Annotate describes the resource.
func (r *AgentConfig) Annotate(a infer.Annotator) {
	a.Describe(&r, describe(agentConfigDoc, agentConfigExamples, agentConfigImportDoc))
}

// AgentConfigArgs are the inputs of AgentConfig: every key of the docker-agent configuration
// as a typed input (render.Document, generated from docker-agent's agent-schema.json), plus
// skipValidation.
type AgentConfigArgs struct {
	render.Document
	SkipValidation *bool `pulumi:"skipValidation,optional"`
}

// Annotate describes the inputs that are not part of the docker-agent configuration.
func (a *AgentConfigArgs) Annotate(an infer.Annotator) {
	an.Describe(&a.Version, "The docker-agent configuration schema version. Defaults to `\""+render.DefaultConfigVersion+
		"\"`, the latest version understood by docker-agent "+DockerAgentVersion+".")
	an.Describe(&a.Extra, "Additional top-level YAML sections that are not modelled by this provider version, "+
		"merged verbatim into the document (snake_case keys). Keys of typed inputs (`agents`, `models`, `runtime`, ...) "+
		"are rejected: set the typed input instead.")
	an.Describe(&a.SkipValidation, "Skip validating the rendered YAML with the docker-agent library, and accept "+
		"enum values (such as a `safety` mode) that this provider version does not list. Use it for "+
		"configurations that rely on docker-agent features newer than "+DockerAgentVersion+".")
}

// AgentConfigState is the state of AgentConfig.
type AgentConfigState struct {
	AgentConfigArgs
	YAML               string   `pulumi:"yaml"`
	SHA256             string   `pulumi:"sha256"`
	DefaultAgent       string   `pulumi:"defaultAgent"`
	AgentNames         []string `pulumi:"agentNames"`
	RequiredEnvVars    []string `pulumi:"requiredEnvVars"`
	DockerAgentVersion string   `pulumi:"dockerAgentVersion"`
}

// Annotate describes the outputs.
func (s *AgentConfigState) Annotate(an infer.Annotator) {
	an.Describe(&s.YAML, "The rendered docker-agent YAML document. Identical inputs always render the same "+
		"bytes: agents keep their order and all map keys are sorted.")
	an.Describe(&s.SHA256, "The hex-encoded SHA-256 of `yaml`.")
	an.Describe(&s.DefaultAgent, "The agent docker-agent starts by default: `root` if an agent has that name, "+
		"else the first agent.")
	an.Describe(&s.AgentNames, "The agent names, in order.")
	an.Describe(&s.RequiredEnvVars, "Best effort list of the environment variables the configured models "+
		"need at run time, for example `OPENAI_API_KEY`, `ANTHROPIC_API_KEY` or the `tokenKey` values.")
	an.Describe(&s.DockerAgentVersion, "The docker-agent library version the configuration was validated "+
		"against.")
}

// Check validates the inputs and, when they are all known, the rendered YAML.
func (*AgentConfig) Check(ctx context.Context, req infer.CheckRequest) (infer.CheckResponse[AgentConfigArgs], error) {
	args, failures, err := checkInputs(ctx, req.NewInputs, func(a AgentConfigArgs) bool {
		return skipValidation(ctx, a.SkipValidation)
	})
	// Enum failures leave the inputs complete, so the configuration's other problems are
	// reported in the same preview.
	if err != nil || slices.ContainsFunc(failures, func(f p.CheckFailure) bool { return !isEnumFailure(f) }) {
		return infer.CheckResponse[AgentConfigArgs]{Inputs: args, Failures: failures}, err
	}
	warnLiteralCredentials(ctx, args.Document, req.NewInputs)
	if req.NewInputs.HasComputed() {
		// Validate once every value is known (during `up`, or in the next preview).
		return infer.CheckResponse[AgentConfigArgs]{Inputs: args, Failures: failures}, nil
	}

	yamlDoc, err := render.Render(args.document())
	if err != nil {
		property, _ := render.InputPath(err, args.Document)
		failures = append(failures, p.CheckFailure{Property: string(property), Reason: err.Error()})
		return infer.CheckResponse[AgentConfigArgs]{Inputs: args, Failures: failures}, nil
	}
	if !skipValidation(ctx, args.SkipValidation) {
		res := validate.Validate(ctx, req.Name+".yaml", []byte(yamlDoc))
		secrets := secretStrings(req.NewInputs)
		failures = append(failures, configFailures(yamlDoc, res, secrets, failures)...)
		logWarnings(ctx, res.Warnings, secrets)
	}
	return infer.CheckResponse[AgentConfigArgs]{Inputs: args, Failures: failures}, nil
}

// Create renders the configuration.
func (*AgentConfig) Create(
	ctx context.Context, req infer.CreateRequest[AgentConfigArgs],
) (infer.CreateResponse[AgentConfigState], error) {
	if req.DryRun {
		// The resource is pure, so the preview shows the real outputs. Outputs that depend
		// on unknown inputs are marked unknown by withPreviewOutputs.
		return infer.CreateResponse[AgentConfigState]{ID: req.Name, Output: dryRunState(ctx, req.Name, req.Inputs)}, nil
	}
	state, err := buildAgentConfigState(ctx, req.Name, req.Inputs)
	if err != nil {
		return infer.CreateResponse[AgentConfigState]{}, err
	}
	return infer.CreateResponse[AgentConfigState]{ID: req.Name, Output: state}, nil
}

// Update re-renders the configuration.
func (*AgentConfig) Update(
	ctx context.Context, req infer.UpdateRequest[AgentConfigArgs, AgentConfigState],
) (infer.UpdateResponse[AgentConfigState], error) {
	if req.DryRun {
		// Compute the real new outputs: pulumi-go-provider v1.7.0 does not mark outputs
		// unknown when a previously absent input (for example `metadata`) is added, so
		// returning the old state would preview stale values.
		return infer.UpdateResponse[AgentConfigState]{Output: dryRunState(ctx, req.ID, req.Inputs)}, nil
	}
	state, err := buildAgentConfigState(ctx, req.ID, req.Inputs)
	if err != nil {
		return infer.UpdateResponse[AgentConfigState]{}, err
	}
	return infer.UpdateResponse[AgentConfigState]{Output: state}, nil
}

// Diff compares the new inputs with the previous ones. A custom diff is needed because
// pulumi-go-provider ignores an input called `version` in its default diff.
func (*AgentConfig) Diff(
	_ context.Context, req infer.DiffRequest[AgentConfigArgs, AgentConfigState],
) (infer.DiffResponse, error) {
	diff := map[string]p.PropertyDiff{}
	olds, news := req.State.AgentConfigArgs, req.Inputs
	// `version` is diffed by withConfigVersionDiff: pulumi-go-provider removes it from the
	// inputs before calling Diff.
	news.Version = olds.Version
	ov, nv := reflect.ValueOf(olds), reflect.ValueOf(news)
	for _, f := range reflect.VisibleFields(ov.Type()) {
		name, _, _ := strings.Cut(f.Tag.Get("pulumi"), ",")
		if f.Anonymous || name == "" || name == "version" {
			continue
		}
		if !equalValues(ov.FieldByIndex(f.Index).Interface(), nv.FieldByIndex(f.Index).Interface()) {
			diff[name] = p.PropertyDiff{Kind: p.Update, InputDiff: true}
		}
	}
	if len(diff) == 0 {
		// The rendering itself may have changed between provider versions.
		if y, err := render.Render(news.document()); err == nil && y != req.State.YAML {
			diff["yaml"] = p.PropertyDiff{Kind: p.Update}
		}
	}
	return infer.DiffResponse{HasChanges: len(diff) > 0, DetailedDiff: diff}, nil
}

// WireDependencies declares how secretness flows. The outputs are always known because
// Create and Update compute them during previews too; withPreviewOutputs marks them
// unknown when an input is unknown.
//
// `yaml` and `sha256` are secret when any input is secret. `agentNames`, `defaultAgent`
// and `requiredEnvVars` only contain agent names and environment variable names, so they
// stay public; withPreviewOutputs makes them secret only when an agent name or a
// `tokenKey` value is itself secret.
func (*AgentConfig) WireDependencies(f infer.FieldSelector, args *AgentConfigArgs, state *AgentConfigState) {
	secret := f.InputField(args).Secret()
	for _, out := range []any{&state.YAML, &state.SHA256} {
		f.OutputField(out).AlwaysKnown()
		f.OutputField(out).DependsOn(secret)
	}
	for _, out := range []any{&state.AgentNames, &state.DefaultAgent, &state.RequiredEnvVars} {
		f.OutputField(out).AlwaysKnown()
		f.OutputField(out).NeverSecret()
	}
	f.OutputField(&state.DockerAgentVersion).AlwaysKnown()
}

func (a AgentConfigArgs) document() render.Document { return a.Document }

// dryRunState computes the outputs for a preview. Unknown inputs decode to zero values, so
// errors are ignored here; withPreviewOutputs replaces such outputs with unknowns.
func dryRunState(ctx context.Context, name string, args AgentConfigArgs) AgentConfigState {
	state, err := computeAgentConfigState(ctx, name, args, false)
	if err != nil {
		return AgentConfigState{AgentConfigArgs: args, DockerAgentVersion: DockerAgentVersion}
	}
	return state
}

func buildAgentConfigState(ctx context.Context, name string, args AgentConfigArgs) (AgentConfigState, error) {
	return computeAgentConfigState(ctx, name, args, !skipValidation(ctx, args.SkipValidation))
}

func computeAgentConfigState(ctx context.Context, name string, args AgentConfigArgs, strict bool) (AgentConfigState, error) {
	state := AgentConfigState{AgentConfigArgs: args, DockerAgentVersion: DockerAgentVersion}
	yamlDoc, err := render.Render(args.document())
	if err != nil {
		return state, fmt.Errorf("rendering docker-agent configuration: %w", err)
	}
	state.YAML = yamlDoc
	state.SHA256 = render.SHA256(yamlDoc)

	names := make([]string, 0, len(args.Agents))
	for _, a := range args.Agents {
		names = append(names, a.Name)
	}
	state.AgentNames = names
	state.DefaultAgent = render.DefaultAgent(names)

	res := validate.Validate(ctx, name+".yaml", []byte(yamlDoc))
	if !res.Valid && strict {
		return state, fmt.Errorf("invalid docker-agent configuration: %w", res.Error())
	}
	state.RequiredEnvVars = res.RequiredEnvVars
	if state.RequiredEnvVars == nil {
		state.RequiredEnvVars = []string{}
	}
	return state, nil
}

// equalValues compares two decoded input values, treating nil and empty collections as
// equal.
func equalValues(a, b any) bool {
	if isEmpty(a) && isEmpty(b) {
		return true
	}
	return reflect.DeepEqual(a, b)
}

func isEmpty(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Map, reflect.Slice:
		return rv.Len() == 0
	case reflect.Pointer, reflect.Interface:
		return rv.IsNil()
	}
	return false
}
