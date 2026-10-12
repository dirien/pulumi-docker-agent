// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/dirien/pulumi-docker-agent/provider/pkg/ociagent"
)

// artifactImportReference turns an AgentArtifact import ID into the tag the resource manages:
//
//   - `registry/repository:tag` (the tag defaults to `latest`) is used as it is;
//   - `registry/repository:tag@sha256:...` names the tag, which must point to that digest;
//   - `registry/repository@sha256:...` is resolved to the one tag of the repository that
//     points to the digest.
func artifactImportReference(ctx context.Context, id string, opts ociagent.Options) (string, error) {
	id = strings.TrimSpace(id)
	repo, digest, pinned := strings.Cut(id, "@")
	if !pinned {
		return id, nil
	}
	if _, err := ociagent.ParseReference(id, opts.Insecure); err != nil {
		return "", err
	}
	if strings.LastIndex(repo, ":") > strings.LastIndex(repo, "/") {
		current, found, err := ociagent.Head(ctx, repo, opts)
		switch {
		case err != nil:
			return "", err
		case !found:
			return "", fmt.Errorf("%s does not exist", repo)
		case current != digest:
			return "", fmt.Errorf("%s points to %s, not %s; import the tag as %s", repo, current, digest, repo)
		}
		return repo, nil
	}
	tags, err := ociagent.TagsOf(ctx, id, digest, opts)
	if err != nil {
		return "", fmt.Errorf("finding the tag of %s: %w", id, err)
	}
	switch len(tags) {
	case 0:
		return "", fmt.Errorf("no tag of %s points to %s. An AgentArtifact manages a tag: import it as %s:<tag>",
			repo, digest, repo)
	case 1:
		return repo + ":" + tags[0], nil
	}
	return "", fmt.Errorf("several tags of %s point to %s (%s). An AgentArtifact manages one tag: import it as "+
		"%s:<tag>", repo, digest, strings.Join(tags, ", "), repo)
}
