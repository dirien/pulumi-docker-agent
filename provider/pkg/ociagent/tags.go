// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package ociagent

import (
	"context"

	"github.com/google/go-containerregistry/pkg/v1/remote"
)

// TagsOf returns the tags of ref's repository that point to digest, sorted. It is used to
// import an artifact by digest: the provider manages tags, so it needs the tag that holds the
// digest.
func TagsOf(ctx context.Context, ref, digest string, o Options) ([]string, error) {
	r, err := ParseReference(ref, o.Insecure)
	if err != nil {
		return nil, err
	}
	return tagsWithDigest(r.Context(), digest, "", o.remoteOpts(ctx, remote.WithAuthFromKeychain(o.keychain())))
}
