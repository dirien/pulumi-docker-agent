// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"testing"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/integration"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dirien/pulumi-docker-agent/provider/pkg/ociagent"
)

// pushArtifact pushes doc to ref like `docker agent share push` and returns the digest.
func pushArtifact(t *testing.T, ref, doc string) string {
	t.Helper()
	art, err := ociagent.Build([]byte(doc), ociagent.Annotations([]byte(doc), "2026-01-01T00:00:00Z", nil))
	require.NoError(t, err)
	res, err := ociagent.Push(t.Context(), ref, art, ociagent.Options{})
	require.NoError(t, err)
	return res.Digest
}

// importArtifact imports an AgentArtifact like the engine and checks that the program
// generated from the imported inputs previews no changes.
func importArtifact(t *testing.T, s integration.Server, id string) p.ReadResponse {
	t.Helper()
	u := urn(agentArtifactToken, "imported")
	read, err := s.Read(p.ReadRequest{ID: id, Urn: u})
	require.NoError(t, err)
	checked, err := s.Check(p.CheckRequest{Urn: u, State: read.Inputs, Inputs: read.Inputs})
	require.NoError(t, err)
	require.Empty(t, checked.Failures, failureText(checked.Failures))
	assert.True(t, property.New(read.Inputs).Equals(property.New(checked.Inputs)),
		"Check changed the imported inputs:\nread:    %v\nchecked: %v", read.Inputs, checked.Inputs)
	diff, err := s.Diff(p.DiffRequest{ID: read.ID, Urn: u, State: read.Properties, OldInputs: read.Inputs, Inputs: checked.Inputs})
	require.NoError(t, err)
	assert.False(t, diff.HasChanges, "the first preview after the import has changes: %v", diff.DetailedDiff)
	return read
}

// TestAgentArtifactImportReferences imports an artifact pushed by `docker agent share push`
// by tag, by tag and digest, and by digest alone.
func TestAgentArtifactImportReferences(t *testing.T) {
	t.Parallel()
	s := server(t)
	host := newRegistry(t, nil)
	repo := host + "/agents/shared"
	digest := pushArtifact(t, repo+":v1", artifactYAML)

	for _, id := range []string{repo + ":v1", repo + ":v1@" + digest, repo + "@" + digest} {
		read := importArtifact(t, s, id)
		assert.Equal(t, repo+":v1", read.ID, id)
		assert.Equal(t, repo+":v1", read.Inputs.Get("reference").AsString(), id)
		assert.Equal(t, artifactYAML, read.Inputs.Get("yaml").AsString())
		assert.Equal(t, "retain", read.Inputs.Get("deleteBehavior").AsString())
		assert.Equal(t, digest, read.Properties.Get("digest").AsString())
		assert.Equal(t, repo+"@"+digest, read.Properties.Get("pinnedReference").AsString())
	}

	// A tag without the digest it is pinned to, a digest that two tags share, and a digest no
	// tag points to cannot be imported.
	pushArtifact(t, repo+":latest", artifactYAML)
	other := pushArtifact(t, repo+":v2", artifactYAML+"description: other\n")
	moved := pushArtifact(t, repo+":v3", artifactYAML+"description: moved\n")
	pushArtifact(t, repo+":v3", artifactYAML+"description: now\n")
	for id, want := range map[string]string{
		repo + ":v1@" + other: "points to " + digest + ", not " + other,
		repo + "@" + digest:   "several tags of " + repo + " point to " + digest + " (latest, v1)",
		repo + "@" + moved:    "no tag of " + repo + " points to " + moved,
		repo + ":v9":          "does not exist",
	} {
		_, err := s.Read(p.ReadRequest{ID: id, Urn: urn(agentArtifactToken, "imported")})
		if want == "does not exist" {
			// A missing tag reads as a missing resource; the engine reports it as such.
			require.NoError(t, err)
			continue
		}
		assert.ErrorContains(t, err, want, id)
	}
}
