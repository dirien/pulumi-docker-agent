// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"

	"github.com/dirien/pulumi-docker-agent/provider/pkg/ociagent"
	"github.com/dirien/pulumi-docker-agent/provider/pkg/render"
	"github.com/dirien/pulumi-docker-agent/provider/pkg/validate"
)

// AgentArtifact pushes an agent configuration to an OCI registry, like
// `docker agent share push`.
type AgentArtifact struct{}

const agentArtifactDoc = `Shares a Docker Agent configuration through an OCI registry, the same way
` + "`docker agent share push`" + ` does.

The artifact is byte-compatible with the docker-agent CLI: an OCI manifest with artifact type
` + "`application/vnd.docker.agent.config.v1+json`" + `, one layer holding the gzipped YAML, and the
` + "`io.docker.agent.version`" + ` annotation the CLI requires. Anyone can run it with
` + "`docker agent run <reference>`" + `, pull it with ` + "`docker agent share pull <reference>`" + `, or reference it as a
sub-agent.

Unlike the CLI, the provider keeps the ` + "`org.opencontainers.image.created`" + ` timestamp in its state and only
pushes when ` + "`yaml`" + ` or ` + "`annotations`" + ` change, so ` + "`pulumi up`" + ` without changes pushes nothing and the
manifest digest stays stable. A refresh detects when the tag was moved by someone else.

Registry credentials come from the provider's ` + "`registryAuth`" + ` setting, falling back to the Docker
credential store (` + "`docker login`" + `). Like the docker CLI, the provider tries HTTPS first and falls back to
plain HTTP for ` + "`localhost`" + `, ` + "`*.localhost`" + `, loopback and private (RFC 1918) addresses; set ` + "`insecure`" + ` for any
other plain-HTTP or self-signed registry, and the provider's ` + "`forceHttps`" + ` option to forbid the private-address
fallback.

With ` + "`deleteBehavior: delete`" + `, destroying the resource deletes the pushed manifest, unless another tag in the
repository points to the same digest (two tags pushed with identical content in the same second) or the tags cannot
be listed; the provider then keeps the artifact and logs a warning.

{{% examples %}}
## Example Usage
{{% example %}}
### Push an agent to a registry

` + "```typescript" + `
import * as dockeragent from "@ediri/pulumi-docker-agent";

const assistant = new dockeragent.AgentConfig("assistant", {
    agents: [{
        name: "root",
        model: "openai/gpt-5-mini",
        instruction: "You are a helpful assistant. Answer briefly.",
    }],
});

const artifact = new dockeragent.AgentArtifact("assistant", {
    reference: "ghcr.io/my-org/agents/assistant:v1",
    yaml: assistant.yaml,
});

// Run it with: docker agent run <pinnedReference>
export const pinnedReference = artifact.pinnedReference;
` + "```" + `
` + "```python" + `
import pulumi
import ediri_docker_agent as dockeragent

assistant = dockeragent.AgentConfig("assistant", agents=[{
    "name": "root",
    "model": "openai/gpt-5-mini",
    "instruction": "You are a helpful assistant. Answer briefly.",
}])

artifact = dockeragent.AgentArtifact("assistant",
    reference="ghcr.io/my-org/agents/assistant:v1",
    yaml=assistant.yaml)

pulumi.export("pinnedReference", artifact.pinned_reference)
` + "```" + `
` + "```yaml" + `
resources:
  assistant:
    type: docker-agent:index:AgentConfig
    properties:
      agents:
        - name: root
          model: openai/gpt-5-mini
          instruction: You are a helpful assistant. Answer briefly.
  artifact:
    type: docker-agent:index:AgentArtifact
    properties:
      reference: ghcr.io/my-org/agents/assistant:v1
      yaml: ${assistant.yaml}
outputs:
  pinnedReference: ${artifact.pinnedReference}
` + "```" + `
{{% /example %}}
{{% example %}}
### Push an existing agent file

` + "```typescript" + `
import * as fs from "fs";
import * as dockeragent from "@ediri/pulumi-docker-agent";

new dockeragent.AgentArtifact("pirate", {
    reference: "localhost:5000/agents/pirate:latest",
    yaml: fs.readFileSync("pirate.yaml", "utf8"),
    annotations: { "org.opencontainers.image.source": "https://github.com/my-org/agents" },
    deleteBehavior: "delete",
});
` + "```" + `
{{% /example %}}
{{% /examples %}}`

// Annotate describes the resource.
func (r *AgentArtifact) Annotate(a infer.Annotator) {
	a.Describe(&r, agentArtifactDoc)
}

// DeleteBehavior controls what happens to a pushed artifact when the resource is deleted.
type DeleteBehavior string

// Delete behaviours.
const (
	DeleteBehaviorRetain DeleteBehavior = "retain"
	DeleteBehaviorDelete DeleteBehavior = "delete"
)

// Values lists the enum values for the schema.
func (DeleteBehavior) Values() []infer.EnumValue[DeleteBehavior] {
	return []infer.EnumValue[DeleteBehavior]{
		{Name: "Retain", Value: DeleteBehaviorRetain, Description: "Leave the artifact in the registry."},
		{Name: "Delete", Value: DeleteBehaviorDelete, Description: "Delete the pushed manifest from the " +
			"registry, unless another tag points to the same digest. Registries that do not support deletion " +
			"(HTTP 405) only produce a warning."},
	}
}

// AgentArtifactArgs are the inputs of AgentArtifact.
type AgentArtifactArgs struct {
	Reference      string            `pulumi:"reference" provider:"replaceOnChanges"`
	YAML           string            `pulumi:"yaml"`
	Annotations    map[string]string `pulumi:"annotations,optional"`
	Insecure       *bool             `pulumi:"insecure,optional"`
	DeleteBehavior *DeleteBehavior   `pulumi:"deleteBehavior,optional"`
	SkipValidation *bool             `pulumi:"skipValidation,optional"`
}

// Annotate describes the inputs.
func (a *AgentArtifactArgs) Annotate(an infer.Annotator) {
	an.Describe(&a.Reference, "The tag to push to, as `registry/repository:tag`, for example "+
		"`ghcr.io/my-org/agents/assistant:v1`. The tag defaults to `latest`. Changing it replaces the resource. "+
		"Registries on `localhost`, `*.localhost`, loopback and private (RFC 1918) addresses may be reached over "+
		"plain HTTP when HTTPS fails (see `insecure` and the provider's `forceHttps`).")
	an.Describe(&a.YAML, "The docker-agent configuration to push, usually the `yaml` output of an "+
		"`AgentConfig` or the contents of an agent file. The bytes are pushed unchanged.")
	an.Describe(&a.Annotations, "Additional manifest annotations. They can override the description and the "+
		"annotations derived from `metadata`, but not `io.docker.agent.version`, `io.docker.cagent.version` or "+
		"`org.opencontainers.image.created`.")
	an.Describe(&a.Insecure, "Allow a plain-HTTP or self-signed registry. Without it, plain HTTP is only used "+
		"as a fallback when HTTPS fails for `localhost`, `*.localhost`, loopback and private (RFC 1918: 10.0.0.0/8, "+
		"172.16.0.0/12, 192.168.0.0/16) addresses, the same as the docker CLI; the provider's `forceHttps` option "+
		"turns that fallback off except for localhost and loopback.")
	an.Describe(&a.DeleteBehavior, "What to do with the pushed manifest when the resource is deleted. "+
		"Defaults to `retain`. With `delete`, the manifest is kept (with a warning) if another tag of the "+
		"repository points to the same digest, if the tags cannot be listed, or if the registry does not "+
		"support deletion.")
	an.SetDefault(&a.DeleteBehavior, DeleteBehaviorRetain)
	an.Describe(&a.SkipValidation, "Skip validating `yaml` with the docker-agent library before pushing.")
}

// AgentArtifactState is the state of AgentArtifact.
type AgentArtifactState struct {
	AgentArtifactArgs
	Digest              string            `pulumi:"digest"`
	PinnedReference     string            `pulumi:"pinnedReference"`
	ContentSHA256       string            `pulumi:"contentSha256"`
	Created             string            `pulumi:"created"`
	ManifestAnnotations map[string]string `pulumi:"manifestAnnotations"`
	DockerAgentVersion  string            `pulumi:"dockerAgentVersion"`
}

// Annotate describes the outputs.
func (s *AgentArtifactState) Annotate(an infer.Annotator) {
	an.Describe(&s.Digest, "The manifest digest (`sha256:...`).")
	an.Describe(&s.PinnedReference, "The artifact pinned by digest, `registry/repository@sha256:...`. Use it "+
		"with `docker agent run` or as a sub-agent reference to get exactly this version.")
	an.Describe(&s.ContentSHA256, "The hex-encoded SHA-256 of `yaml` (the layer's diff ID).")
	an.Describe(&s.Created, "The `org.opencontainers.image.created` timestamp of the pushed manifest. It "+
		"only changes when the artifact is pushed again.")
	an.Describe(&s.ManifestAnnotations, "All annotations of the pushed manifest.")
	an.Describe(&s.DockerAgentVersion, "The docker-agent version written to the `io.docker.agent.version` "+
		"annotation.")
}

// Check validates the reference, the annotations and the YAML.
func (*AgentArtifact) Check(
	ctx context.Context, req infer.CheckRequest,
) (infer.CheckResponse[AgentArtifactArgs], error) {
	args, failures, err := infer.DefaultCheck[AgentArtifactArgs](ctx, req.NewInputs)
	if err != nil || len(failures) > 0 {
		return infer.CheckResponse[AgentArtifactArgs]{Inputs: args, Failures: failures}, err
	}

	if v := req.NewInputs.Get("reference"); !v.IsComputed() && !v.HasComputed() {
		if _, err := ociagent.ParseTag(args.Reference, boolValue(args.Insecure)); err != nil {
			failures = append(failures, p.CheckFailure{Property: "reference", Reason: err.Error()})
		}
	}
	for _, k := range slices.Sorted(maps.Keys(args.Annotations)) {
		if slices.Contains(ociagent.ReservedAnnotations, k) {
			failures = append(failures, p.CheckFailure{
				Property: "annotations",
				Reason:   fmt.Sprintf("annotation %q is set by the provider and cannot be overridden", k),
			})
		}
	}
	yamlValue := req.NewInputs.Get("yaml")
	if !yamlValue.IsComputed() && !yamlValue.HasComputed() && !skipValidation(ctx, args.SkipValidation) {
		res := validate.Validate(ctx, "agent.yaml", []byte(args.YAML))
		secrets := secretStrings(req.NewInputs)
		for _, e := range res.Errors {
			failures = append(failures, p.CheckFailure{
				Reason: "invalid docker-agent configuration in `yaml`: " + validate.Redact(e, secrets),
			})
		}
		logWarnings(ctx, res.Warnings, secrets)
	}
	return infer.CheckResponse[AgentArtifactArgs]{Inputs: args, Failures: failures}, nil
}

// Create pushes the artifact.
func (*AgentArtifact) Create(
	ctx context.Context, req infer.CreateRequest[AgentArtifactArgs],
) (infer.CreateResponse[AgentArtifactState], error) {
	args := req.Inputs
	if req.DryRun {
		state := AgentArtifactState{AgentArtifactArgs: args, DockerAgentVersion: DockerAgentVersion}
		return infer.CreateResponse[AgentArtifactState]{ID: args.Reference, Output: state}, nil
	}
	tag, err := ociagent.ParseTag(args.Reference, boolValue(args.Insecure))
	if err != nil {
		return infer.CreateResponse[AgentArtifactState]{}, err
	}
	state, err := push(ctx, args)
	if err != nil {
		return infer.CreateResponse[AgentArtifactState]{}, err
	}
	return infer.CreateResponse[AgentArtifactState]{ID: tag.Name(), Output: state}, nil
}

// Diff replaces the resource when the reference changes and updates it otherwise.
func (*AgentArtifact) Diff(
	_ context.Context, req infer.DiffRequest[AgentArtifactArgs, AgentArtifactState],
) (infer.DiffResponse, error) {
	olds, news := req.State.AgentArtifactArgs, req.Inputs
	diff := map[string]p.PropertyDiff{}
	if normalizeReference(olds.Reference, olds.Insecure) != normalizeReference(news.Reference, news.Insecure) {
		diff["reference"] = p.PropertyDiff{Kind: p.UpdateReplace, InputDiff: true}
	}
	if olds.YAML != news.YAML {
		diff["yaml"] = p.PropertyDiff{Kind: p.Update, InputDiff: true}
	}
	if !maps.Equal(olds.Annotations, news.Annotations) {
		diff["annotations"] = p.PropertyDiff{Kind: p.Update, InputDiff: true}
	}
	if boolValue(olds.Insecure) != boolValue(news.Insecure) {
		diff["insecure"] = p.PropertyDiff{Kind: p.Update, InputDiff: true}
	}
	if deleteBehavior(olds.DeleteBehavior) != deleteBehavior(news.DeleteBehavior) {
		diff["deleteBehavior"] = p.PropertyDiff{Kind: p.Update, InputDiff: true}
	}
	if boolValue(olds.SkipValidation) != boolValue(news.SkipValidation) {
		diff["skipValidation"] = p.PropertyDiff{Kind: p.Update, InputDiff: true}
	}
	return infer.DiffResponse{HasChanges: len(diff) > 0, DetailedDiff: diff}, nil
}

// Update pushes again when the YAML or the annotations changed. Other changes only update
// the state.
func (*AgentArtifact) Update(
	ctx context.Context, req infer.UpdateRequest[AgentArtifactArgs, AgentArtifactState],
) (infer.UpdateResponse[AgentArtifactState], error) {
	state := req.State
	state.AgentArtifactArgs = req.Inputs
	needsPush := req.State.YAML != req.Inputs.YAML || !maps.Equal(req.State.Annotations, req.Inputs.Annotations) ||
		req.State.Digest == ""
	if req.DryRun || !needsPush {
		return infer.UpdateResponse[AgentArtifactState]{Output: state}, nil
	}
	newState, err := push(ctx, req.Inputs)
	if err != nil {
		return infer.UpdateResponse[AgentArtifactState]{}, err
	}
	return infer.UpdateResponse[AgentArtifactState]{Output: newState}, nil
}

// Read checks which digest the tag points to. When it moved, the state is updated from
// the registry, so the next `pulumi up` pushes the configured YAML again if it differs.
// When the tag is gone, the resource is reported as deleted. Read also implements
// `pulumi import` with the reference as ID.
func (*AgentArtifact) Read(
	ctx context.Context, req infer.ReadRequest[AgentArtifactArgs, AgentArtifactState],
) (infer.ReadResponse[AgentArtifactArgs, AgentArtifactState], error) {
	ref := req.State.Reference
	if ref == "" {
		ref = req.Inputs.Reference
	}
	if ref == "" {
		ref = req.ID
	}
	insecure := req.State.Insecure
	if insecure == nil {
		insecure = req.Inputs.Insecure
	}
	opts := registryOptions(ctx, insecure)

	digest, found, err := ociagent.Head(ctx, ref, opts)
	if err != nil {
		return infer.ReadResponse[AgentArtifactArgs, AgentArtifactState]{}, err
	}
	if !found {
		return infer.ReadResponse[AgentArtifactArgs, AgentArtifactState]{}, nil
	}
	if digest == req.State.Digest {
		return infer.ReadResponse[AgentArtifactArgs, AgentArtifactState](req), nil
	}

	// The tag points to a different manifest (drift), or this is an import.
	imported := req.State.Digest == "" && req.State.YAML == ""
	pulled, err := ociagent.Pull(ctx, ref, opts)
	if err != nil && !imported && errors.Is(err, ociagent.ErrNotAgentArtifact) {
		// Something else (for example a container image) was pushed to the tag. Record the
		// drift with an empty YAML, so the next `pulumi up` pushes the agent again.
		p.GetLogger(ctx).Warningf("%s now points to %s, which is not a docker-agent artifact; "+
			"the next update pushes the configured agent again", ref, digest)
		inputs := req.Inputs
		inputs.YAML = ""
		return infer.ReadResponse[AgentArtifactArgs, AgentArtifactState]{
			ID:     req.ID,
			Inputs: inputs,
			State: AgentArtifactState{
				AgentArtifactArgs:   inputs,
				Digest:              digest,
				PinnedReference:     pinnedReference(ref, digest, insecure),
				ManifestAnnotations: map[string]string{},
				DockerAgentVersion:  DockerAgentVersion,
			},
		}, nil
	}
	if err != nil {
		return infer.ReadResponse[AgentArtifactArgs, AgentArtifactState]{}, err
	}

	inputs := req.Inputs
	if imported {
		inputs = AgentArtifactArgs{
			Reference:      ref,
			Annotations:    userAnnotations(pulled),
			Insecure:       insecure,
			DeleteBehavior: ptr(DeleteBehaviorRetain),
		}
		if len(inputs.Annotations) == 0 {
			inputs.Annotations = nil
		}
	}
	inputs.YAML = string(pulled.YAML)

	state := AgentArtifactState{
		AgentArtifactArgs:   inputs,
		Digest:              pulled.Digest,
		PinnedReference:     pinnedReference(ref, pulled.Digest, insecure),
		ContentSHA256:       render.SHA256(string(pulled.YAML)),
		Created:             pulled.Annotations[ociagent.AnnotationCreated],
		ManifestAnnotations: pulled.Annotations,
		DockerAgentVersion:  DockerAgentVersion,
	}
	if state.ManifestAnnotations == nil {
		state.ManifestAnnotations = map[string]string{}
	}
	id := req.ID
	if id == "" {
		id = ref
	}
	return infer.ReadResponse[AgentArtifactArgs, AgentArtifactState]{ID: id, Inputs: inputs, State: state}, nil
}

// Delete removes the manifest when deleteBehavior is `delete`.
func (*AgentArtifact) Delete(ctx context.Context, req infer.DeleteRequest[AgentArtifactState]) (infer.DeleteResponse, error) {
	if deleteBehavior(req.State.DeleteBehavior) != DeleteBehaviorDelete || req.State.Digest == "" {
		return infer.DeleteResponse{}, nil
	}
	res, err := ociagent.Delete(ctx, req.State.Reference, req.State.Digest, registryOptions(ctx, req.State.Insecure))
	if err != nil {
		return infer.DeleteResponse{}, err
	}
	if res.Warning != "" {
		p.GetLogger(ctx).Warning(res.Warning)
	}
	return infer.DeleteResponse{}, nil
}

// WireDependencies limits which inputs make the outputs unknown during a preview, so
// changing `deleteBehavior` or `insecure` does not suggest a new digest.
func (*AgentArtifact) WireDependencies(f infer.FieldSelector, args *AgentArtifactArgs, state *AgentArtifactState) {
	content := []infer.InputField{
		f.InputField(&args.Reference),
		f.InputField(&args.YAML).Computed(),
		f.InputField(&args.Annotations).Computed(),
	}
	f.OutputField(&state.Digest).DependsOn(content...)
	f.OutputField(&state.PinnedReference).DependsOn(content...)
	f.OutputField(&state.Created).DependsOn(f.InputField(&args.YAML).Computed(), f.InputField(&args.Annotations).Computed())
	f.OutputField(&state.ContentSHA256).DependsOn(f.InputField(&args.YAML).Computed())
	f.OutputField(&state.ManifestAnnotations).DependsOn(f.InputField(&args.YAML).Computed(), f.InputField(&args.Annotations))
	f.OutputField(&state.DockerAgentVersion).AlwaysKnown()
}

func push(ctx context.Context, args AgentArtifactArgs) (AgentArtifactState, error) {
	created := ociagent.FormatCreated(time.Now())
	doc := []byte(args.YAML)
	art, err := ociagent.Build(doc, ociagent.Annotations(doc, created, args.Annotations))
	if err != nil {
		return AgentArtifactState{}, err
	}
	res, err := ociagent.Push(ctx, args.Reference, art, registryOptions(ctx, args.Insecure))
	if err != nil {
		return AgentArtifactState{}, err
	}
	p.GetLogger(ctx).Debugf("pushed %s as %s", args.Reference, res.Digest)
	return AgentArtifactState{
		AgentArtifactArgs:   args,
		Digest:              res.Digest,
		PinnedReference:     res.PinnedReference,
		ContentSHA256:       art.ContentSHA256(),
		Created:             created,
		ManifestAnnotations: art.Annotations,
		DockerAgentVersion:  DockerAgentVersion,
	}, nil
}

// userAnnotations returns the annotations of a pulled artifact that the provider would not
// have written itself.
func userAnnotations(pulled *ociagent.Pulled) map[string]string {
	generated := ociagent.Annotations(pulled.YAML, pulled.Annotations[ociagent.AnnotationCreated], nil)
	out := map[string]string{}
	for k, v := range pulled.Annotations {
		if g, ok := generated[k]; ok && g == v {
			continue
		}
		if slices.Contains(ociagent.ReservedAnnotations, k) {
			continue
		}
		out[k] = v
	}
	return out
}

func normalizeReference(ref string, insecure *bool) string {
	tag, err := ociagent.ParseTag(ref, boolValue(insecure))
	if err != nil {
		return ref
	}
	return tag.Name()
}

func pinnedReference(ref, digest string, insecure *bool) string {
	r, err := ociagent.ParseReference(ref, boolValue(insecure))
	if err != nil {
		return ""
	}
	return r.Context().Digest(digest).String()
}

func deleteBehavior(d *DeleteBehavior) DeleteBehavior {
	if d == nil || *d == "" {
		return DeleteBehaviorRetain
	}
	return *d
}

func boolValue(b *bool) bool { return b != nil && *b }

func ptr[T any](v T) *T { return &v }
