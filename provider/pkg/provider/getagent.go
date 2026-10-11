// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"

	"github.com/pulumi/pulumi-go-provider/infer"

	"github.com/dirien/pulumi-docker-agent/provider/pkg/ociagent"
	"github.com/dirien/pulumi-docker-agent/provider/pkg/render"
	"github.com/dirien/pulumi-docker-agent/provider/pkg/validate"
)

// GetAgent reads an agent artifact from an OCI registry, like `docker agent share pull`
// but without writing anything to disk.
type GetAgent struct{}

const getAgentDoc = `Reads a Docker Agent configuration from an OCI registry, like ` + "`docker agent share pull`" + `,
without writing anything to disk.

The reference can be a tag or a digest. When the provider's ` + "`registryAuth`" + ` has credentials for the registry,
they are used right away. Otherwise the function first tries anonymous access and retries with the Docker
credential store when the registry answers 401, 403, 404 or 429. It accepts artifacts pushed by
` + "`docker agent share push`" + ` and by ` + "`AgentArtifact`" + `, and
rejects manifests without the ` + "`io.docker.agent.version`" + ` annotation.

{{% examples %}}
## Example Usage
{{% example %}}
### Read a shared agent

` + "```typescript" + `
import * as dockeragent from "@ediri/pulumi-docker-agent";

const agent = dockeragent.getAgentOutput({ reference: "docker.io/my-org/assistant:latest" });

export const agentYaml = agent.yaml;
export const agentNames = agent.agentNames;
` + "```" + `
` + "```python" + `
import pulumi
import ediri_docker_agent as dockeragent

agent = dockeragent.get_agent_output(reference="docker.io/my-org/assistant:latest")
pulumi.export("agentYaml", agent.yaml)
` + "```" + `
` + "```yaml" + `
variables:
  agent:
    fn::invoke:
      function: docker-agent:index:getAgent
      arguments:
        reference: docker.io/my-org/assistant:latest
outputs:
  agentYaml: ${agent.yaml}
` + "```" + `
{{% /example %}}
{{% /examples %}}`

// Annotate describes the function.
func (g *GetAgent) Annotate(a infer.Annotator) {
	a.SetToken("index", "getAgent")
	a.Describe(&g, getAgentDoc)
}

// GetAgentArgs are the inputs of getAgent.
type GetAgentArgs struct {
	Reference string `pulumi:"reference"`
	Insecure  *bool  `pulumi:"insecure,optional"`
}

// Annotate describes the inputs.
func (a *GetAgentArgs) Annotate(an infer.Annotator) {
	an.Describe(&a.Reference, "The artifact to read: `registry/repository:tag` (the tag defaults to "+
		"`latest`) or `registry/repository@sha256:...`.")
	an.Describe(&a.Insecure, "Allow a plain-HTTP or self-signed registry. Without it, plain HTTP is only used "+
		"as a fallback when HTTPS fails for `localhost`, `*.localhost`, loopback and private (RFC 1918) "+
		"addresses, the same as the docker CLI.")
}

// GetAgentResult is the result of getAgent.
type GetAgentResult struct {
	YAML            string            `pulumi:"yaml"`
	Digest          string            `pulumi:"digest"`
	PinnedReference string            `pulumi:"pinnedReference"`
	Annotations     map[string]string `pulumi:"annotations"`
	Metadata        render.Metadata   `pulumi:"metadata"`
	AgentNames      []string          `pulumi:"agentNames"`
	DefaultAgent    string            `pulumi:"defaultAgent"`
}

// Annotate describes the result.
func (r *GetAgentResult) Annotate(an infer.Annotator) {
	an.Describe(&r.YAML, "The agent configuration, exactly as stored in the artifact.")
	an.Describe(&r.Digest, "The manifest digest (`sha256:...`).")
	an.Describe(&r.PinnedReference, "The artifact pinned by digest, `registry/repository@sha256:...`.")
	an.Describe(&r.Annotations, "The manifest annotations.")
	an.Describe(&r.Metadata, "The `metadata` block of the configuration. Fields missing from the YAML are "+
		"filled from the matching manifest annotations.")
	an.Describe(&r.AgentNames, "The agent names, in order.")
	an.Describe(&r.DefaultAgent, "The agent docker-agent starts by default: `root` if an agent has that name, "+
		"else the first agent.")
}

// Invoke pulls the artifact.
func (*GetAgent) Invoke(
	ctx context.Context, req infer.FunctionRequest[GetAgentArgs],
) (infer.FunctionResponse[GetAgentResult], error) {
	in := req.Input
	pulled, err := ociagent.Pull(ctx, in.Reference, registryOptions(ctx, in.Insecure))
	if err != nil {
		return infer.FunctionResponse[GetAgentResult]{}, err
	}

	md := ociagent.ParseMetadata(pulled.YAML)
	ann := pulled.Annotations
	if ann == nil {
		ann = map[string]string{}
	}
	meta := render.Metadata{
		Author:      orAnnotation(md.Author, ann, ociagent.AnnotationAuthors),
		License:     orAnnotation(md.License, ann, ociagent.AnnotationLicenses),
		Description: nonEmpty(md.Description),
		Readme:      nonEmpty(md.Readme),
		Version:     orAnnotation(md.Version, ann, ociagent.AnnotationRevision),
		Tags:        md.Tags,
	}
	if len(meta.Tags) == 0 && ann[ociagent.AnnotationTags] != "" {
		meta.Tags = splitTags(ann[ociagent.AnnotationTags])
	}

	names := validate.AgentNames(pulled.YAML)
	if names == nil {
		names = []string{}
	}
	return infer.FunctionResponse[GetAgentResult]{Output: GetAgentResult{
		YAML:            string(pulled.YAML),
		Digest:          pulled.Digest,
		PinnedReference: pinnedReference(in.Reference, pulled.Digest, in.Insecure),
		Annotations:     ann,
		Metadata:        meta,
		AgentNames:      names,
		DefaultAgent:    validate.DefaultAgent(names),
	}}, nil
}

func orAnnotation(v string, ann map[string]string, key string) *string {
	if v != "" {
		return &v
	}
	return nonEmpty(ann[key])
}

func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func splitTags(s string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			if t := s[start:i]; t != "" {
				out = append(out, t)
			}
			start = i + 1
		}
	}
	return out
}
