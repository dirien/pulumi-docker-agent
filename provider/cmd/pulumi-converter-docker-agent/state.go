// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/hashicorp/hcl/v2"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource/plugin"

	"github.com/dirien/pulumi-docker-agent/provider/pkg/pcl"
)

// The annotations `docker agent share push` (and the provider) write on an agent artifact.
// AgentArtifact imports only manifests that carry one of them.
var versionAnnotations = []string{"io.docker.agent.version", "io.docker.cagent.version"}

// registryOptions are the options of every registry request: credentials from the Docker
// credential store (`docker login`, credential helpers).
func registryOptions(ctx context.Context) []remote.Option {
	return []remote.Option{remote.WithContext(ctx), remote.WithAuthFromKeychain(authn.DefaultKeychain)}
}

// importArtifacts lists the tags of a repository and returns an AgentArtifact import for each
// tag that holds a docker-agent artifact. Other tags are reported as warnings.
func importArtifacts(
	ctx context.Context, repository string, insecure bool, version string, opts ...remote.Option,
) ([]plugin.ResourceImport, hcl.Diagnostics, error) {
	repository = strings.TrimSpace(repository)
	var nameOpts []name.Option
	if insecure {
		nameOpts = append(nameOpts, name.Insecure)
	}
	repo, err := name.NewRepository(repository, nameOpts...)
	if err != nil {
		return nil, nil, fmt.Errorf("%q is not a repository (<registry>/<repository>): %w", repository, err)
	}
	tags, err := remote.List(repo, opts...)
	if err != nil {
		return nil, nil, fmt.Errorf("listing the tags of %s: %w", repository, err)
	}
	slices.Sort(tags)

	var resources []plugin.ResourceImport
	var diags hcl.Diagnostics
	base := repository[strings.LastIndex(repository, "/")+1:]
	taken := map[string]bool{}
	for _, tag := range tags {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		ok, err := isAgentArtifact(repo.Tag(tag), opts...)
		if err != nil {
			return nil, nil, err
		}
		if !ok {
			diags = append(diags, &hcl.Diagnostic{
				Severity: hcl.DiagWarning,
				Summary:  "Skipped " + repository + ":" + tag,
				Detail:   "The tag does not hold a docker-agent artifact.",
			})
			continue
		}
		resName := pcl.Identifier(base + "-" + tag)
		for i := 2; taken[resName]; i++ {
			resName = fmt.Sprintf("%s%d", pcl.Identifier(base+"-"+tag), i)
		}
		taken[resName] = true
		resources = append(resources, plugin.ResourceImport{
			Type:              pcl.ArtifactType,
			Name:              resName,
			ID:                repository + ":" + tag,
			Version:           version,
			PluginDownloadURL: pcl.PluginDownloadURL,
		})
	}
	if len(resources) == 0 {
		diags = append(diags, &hcl.Diagnostic{
			Severity: hcl.DiagError,
			Summary:  "No agent artifacts in " + repository,
			Detail:   fmt.Sprintf("None of the %d tags of %s holds a docker-agent artifact.", len(tags), repository),
		})
	}
	return resources, diags, nil
}

// isAgentArtifact reports whether the manifest a tag points to is a docker-agent artifact.
func isAgentArtifact(ref name.Reference, opts ...remote.Option) (bool, error) {
	desc, err := remote.Get(ref, opts...)
	if err != nil {
		return false, fmt.Errorf("fetching the manifest of %s: %w", ref, err)
	}
	if desc.MediaType.IsIndex() {
		return false, nil
	}
	var m struct {
		Layers      []json.RawMessage `json:"layers"`
		Manifests   []json.RawMessage `json:"manifests"`
		Annotations map[string]string `json:"annotations"`
	}
	if err := json.Unmarshal(desc.Manifest, &m); err != nil {
		return false, nil // not a manifest of an artifact
	}
	if len(m.Manifests) > 0 || len(m.Layers) == 0 {
		return false, nil
	}
	return slices.ContainsFunc(versionAnnotations, func(a string) bool { return m.Annotations[a] != "" }), nil
}
