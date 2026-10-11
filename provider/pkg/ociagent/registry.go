// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package ociagent

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"github.com/google/go-containerregistry/pkg/v1/types"
)

// maxLayerSize bounds how much of a layer Pull reads. Agent configurations are small.
const maxLayerSize = 32 << 20

// Options configure registry access.
type Options struct {
	// Keychain resolves credentials. Nil means Keychain(Credentials): the explicit
	// credentials first, then authn.DefaultKeychain.
	Keychain authn.Keychain
	// Credentials are explicitly configured credentials (the provider's registryAuth).
	// Pull sends them from the first request to a matching registry instead of trying
	// anonymous access first.
	Credentials []Credential
	// Insecure allows plain-HTTP (and self-signed HTTPS) registries. go-containerregistry
	// already falls back to plain HTTP for localhost, *.localhost, loopback and private
	// (RFC 1918) addresses when HTTPS fails.
	Insecure bool
	// ForceHTTPS refuses plain-HTTP connections to every registry except localhost and
	// loopback addresses, including the private-address fallback described above. It
	// has no effect when Insecure is set.
	ForceHTTPS bool
	// UserAgent is sent with every request.
	UserAgent string
	// Transport overrides the HTTP transport (tests).
	Transport http.RoundTripper
}

func (o Options) keychain() authn.Keychain {
	if o.Keychain != nil {
		return o.Keychain
	}
	return Keychain(o.Credentials)
}

// hasCredentials reports whether explicit credentials exist for registry.
func (o Options) hasCredentials(registry string) bool {
	host := NormalizeRegistry(registry)
	for _, c := range o.Credentials {
		if NormalizeRegistry(c.Address) == host {
			return true
		}
	}
	return false
}

func (o Options) nameOpts() []name.Option {
	opts := []name.Option{name.WithDefaultTag("latest")}
	if o.Insecure {
		opts = append(opts, name.Insecure)
	}
	return opts
}

func (o Options) remoteOpts(ctx context.Context, auth remote.Option) []remote.Option {
	opts := []remote.Option{remote.WithContext(ctx), auth}
	if o.UserAgent != "" {
		opts = append(opts, remote.WithUserAgent(o.UserAgent))
	}
	return append(opts, remote.WithTransport(o.transport()))
}

// transport returns the HTTP transport for registry calls.
func (o Options) transport() http.RoundTripper {
	t := o.Transport
	if t == nil {
		t = remote.DefaultTransport
		if base, ok := t.(*http.Transport); ok && o.Insecure {
			c := base.Clone()
			if c.TLSClientConfig == nil {
				c.TLSClientConfig = &tls.Config{} //nolint:gosec // see below
			}
			c.TLSClientConfig.InsecureSkipVerify = true //nolint:gosec // explicitly requested with `insecure`
			t = c
		}
	}
	if o.ForceHTTPS && !o.Insecure {
		t = &httpsOnly{base: t}
	}
	return t
}

// httpsOnly refuses plain-HTTP requests to anything but localhost and loopback addresses.
type httpsOnly struct{ base http.RoundTripper }

func (t *httpsOnly) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme == "http" && !isLoopbackHost(req.URL.Hostname()) {
		return nil, fmt.Errorf("refusing a plain-HTTP connection to %s because forceHttps is enabled; "+
			"set `insecure: true` to allow plain HTTP for this registry", req.URL.Host)
	}
	return t.base.RoundTrip(req)
}

func isLoopbackHost(host string) bool {
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// ParseTag parses a push reference. A missing tag defaults to `latest`; digest references
// are rejected because a push needs a tag.
func ParseTag(ref string, insecure bool) (name.Tag, error) {
	if strings.Contains(ref, "@") {
		return name.Tag{}, fmt.Errorf("reference %q must be a tag (registry/repository:tag), not a digest", ref)
	}
	return name.NewTag(ref, Options{Insecure: insecure}.nameOpts()...)
}

// ParseReference parses a tag or digest reference. A missing tag defaults to `latest`.
func ParseReference(ref string, insecure bool) (name.Reference, error) {
	return name.ParseReference(ref, Options{Insecure: insecure}.nameOpts()...)
}

// PushResult describes a pushed artifact.
type PushResult struct {
	// Digest is the manifest digest (sha256:...).
	Digest string
	// PinnedReference is registry/repository@sha256:...
	PinnedReference string
}

// Push uploads the artifact and tags it as ref.
func Push(ctx context.Context, ref string, a *Artifact, o Options) (PushResult, error) {
	tag, err := ParseTag(ref, o.Insecure)
	if err != nil {
		return PushResult{}, err
	}
	img, err := a.image()
	if err != nil {
		return PushResult{}, err
	}
	if err := remote.Write(tag, img, o.remoteOpts(ctx, remote.WithAuthFromKeychain(o.keychain()))...); err != nil {
		return PushResult{}, fmt.Errorf("pushing %s: %w", tag, err)
	}
	return PushResult{
		Digest:          a.Digest.String(),
		PinnedReference: tag.Context().Digest(a.Digest.String()).String(),
	}, nil
}

// Head returns the digest ref currently resolves to. found is false if the registry
// reports that the manifest or repository does not exist.
func Head(ctx context.Context, ref string, o Options) (digest string, found bool, err error) {
	r, err := ParseReference(ref, o.Insecure)
	if err != nil {
		return "", false, err
	}
	desc, err := remote.Head(r, o.remoteOpts(ctx, remote.WithAuthFromKeychain(o.keychain()))...)
	if err != nil {
		if IsNotFound(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("resolving %s: %w", r, err)
	}
	return desc.Digest.String(), true, nil
}

// Pulled is an artifact read from a registry.
type Pulled struct {
	YAML        []byte
	Digest      string
	Annotations map[string]string
	Manifest    []byte
	// Reference is the fully qualified reference that was read.
	Reference string
}

// ErrNotAgentArtifact is returned by Pull for manifests that were not created by
// `docker agent share push` (or this provider).
var ErrNotAgentArtifact = errors.New("not a docker-agent artifact")

type manifestDoc struct {
	MediaType    types.MediaType   `json:"mediaType"`
	ArtifactType string            `json:"artifactType"`
	Config       v1.Descriptor     `json:"config"`
	Layers       []v1.Descriptor   `json:"layers"`
	Manifests    []v1.Descriptor   `json:"manifests"`
	Annotations  map[string]string `json:"annotations"`
}

// Pull reads the artifact at ref (tag or digest). When o.Credentials has an entry for the
// registry, they are used from the first request. Otherwise, like `docker agent share
// pull`, it first tries anonymously and retries with credentials on 401, 403, 404 and 429
// (some registries hide private repositories behind a 404). It rejects manifests without
// the docker-agent version annotation.
func Pull(ctx context.Context, ref string, o Options) (*Pulled, error) {
	r, err := ParseReference(ref, o.Insecure)
	if err != nil {
		return nil, err
	}

	if o.hasCredentials(r.Context().RegistryStr()) {
		// Credentials were configured explicitly for this registry: use them right away.
		return pull(r, o.remoteOpts(ctx, remote.WithAuthFromKeychain(o.keychain())))
	}
	var pulled *Pulled
	err = withAuthRetry(o, func(auth remote.Option) error {
		var err error
		pulled, err = pull(r, o.remoteOpts(ctx, auth))
		return err
	})
	if err != nil {
		return nil, err
	}
	return pulled, nil
}

func pull(r name.Reference, opts []remote.Option) (*Pulled, error) {
	desc, err := remote.Get(r, opts...)
	if err != nil {
		return nil, fmt.Errorf("fetching manifest %s: %w", r, err)
	}
	var m manifestDoc
	if err := json.Unmarshal(desc.Manifest, &m); err != nil {
		return nil, fmt.Errorf("decoding manifest %s: %w", r, err)
	}
	if desc.MediaType.IsIndex() || len(m.Manifests) > 0 {
		return nil, fmt.Errorf("%s is an image index, %w", r, ErrNotAgentArtifact)
	}
	if m.Annotations[AnnotationVersion] == "" && m.Annotations[AnnotationLegacyVersion] == "" {
		return nil, fmt.Errorf("%s wasn't created by `docker agent share push` (missing %s annotation): %w",
			r, AnnotationVersion, ErrNotAgentArtifact)
	}
	if len(m.Layers) == 0 {
		return nil, fmt.Errorf("%s has no layers: %w", r, ErrNotAgentArtifact)
	}

	layer, err := remote.Layer(r.Context().Digest(m.Layers[0].Digest.String()), opts...)
	if err != nil {
		return nil, fmt.Errorf("fetching layer %s: %w", m.Layers[0].Digest, err)
	}
	rc, err := layer.Compressed()
	if err != nil {
		return nil, fmt.Errorf("fetching layer %s: %w", m.Layers[0].Digest, err)
	}
	defer rc.Close()
	raw, err := io.ReadAll(io.LimitReader(rc, maxLayerSize+1))
	if err != nil {
		return nil, fmt.Errorf("reading layer %s: %w", m.Layers[0].Digest, err)
	}
	if len(raw) > maxLayerSize {
		return nil, fmt.Errorf("layer %s is larger than %d bytes", m.Layers[0].Digest, maxLayerSize)
	}
	doc, err := DecodeLayer(raw)
	if err != nil {
		return nil, fmt.Errorf("decoding layer %s: %w", m.Layers[0].Digest, err)
	}
	return &Pulled{
		YAML:        doc,
		Digest:      desc.Digest.String(),
		Annotations: m.Annotations,
		Manifest:    desc.Manifest,
		Reference:   r.Name(),
	}, nil
}

// DecodeLayer returns the agent YAML stored in a layer blob. docker-agent stores the
// gzip of the raw YAML; plain YAML and a gzipped single-file tar are accepted as well.
func DecodeLayer(raw []byte) ([]byte, error) {
	data := raw
	if len(raw) >= 2 && raw[0] == 0x1f && raw[1] == 0x8b {
		zr, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return nil, err
		}
		data, err = io.ReadAll(io.LimitReader(zr, maxLayerSize+1))
		if err != nil {
			return nil, err
		}
		if len(data) > maxLayerSize {
			return nil, fmt.Errorf("uncompressed layer is larger than %d bytes", maxLayerSize)
		}
	}
	if isTar(data) {
		tr := tar.NewReader(bytes.NewReader(data))
		for {
			h, err := tr.Next()
			if err != nil {
				return nil, fmt.Errorf("layer is a tar archive without a regular file: %w", err)
			}
			if h.Typeflag == tar.TypeReg {
				return io.ReadAll(io.LimitReader(tr, maxLayerSize))
			}
		}
	}
	return data, nil
}

func isTar(b []byte) bool {
	return len(b) >= 262 && string(b[257:262]) == "ustar"
}

// DeleteResult describes the outcome of Delete.
type DeleteResult struct {
	// Deleted is true if the registry removed the manifest.
	Deleted bool
	// Warning is set when the registry does not support deletion (HTTP 405 or
	// UNSUPPORTED) or the manifest was already gone.
	Warning string
}

// Delete removes the manifest digest from the repository of ref.
//
// Deleting a manifest by digest removes every tag that points to it, so Delete first lists
// the repository's tags and leaves the manifest in place (with a warning) when another tag
// resolves to the same digest, or when the tags cannot be listed. Registries that do not
// support deletion (HTTP 405 / UNSUPPORTED, the default for registry:2) also yield a
// warning, not an error.
func Delete(ctx context.Context, ref, digest string, o Options) (DeleteResult, error) {
	r, err := ParseReference(ref, o.Insecure)
	if err != nil {
		return DeleteResult{}, err
	}
	opts := o.remoteOpts(ctx, remote.WithAuthFromKeychain(o.keychain()))
	repo := r.Context()
	target := repo.Digest(digest)

	ownTag := ""
	if t, ok := r.(name.Tag); ok {
		ownTag = t.TagStr()
	}
	shared, err := tagsWithDigest(repo, digest, ownTag, opts)
	if err != nil {
		return DeleteResult{Warning: fmt.Sprintf("could not check whether other tags of %s point to %s, so the "+
			"artifact was left in place: %v", repo, digest, err)}, nil
	}
	if len(shared) > 0 {
		return DeleteResult{Warning: fmt.Sprintf("%s is also tagged as %s; the artifact was left in place because "+
			"deleting the manifest would remove those tags as well", target, joinTags(repo, shared))}, nil
	}

	err = remote.Delete(target, opts...)
	if err == nil {
		return DeleteResult{Deleted: true}, nil
	}
	if IsUnsupported(err) {
		return DeleteResult{Warning: fmt.Sprintf("registry %s does not support deleting manifests; %s was left "+
			"in place: %v", repo.RegistryStr(), target, err)}, nil
	}
	if IsNotFound(err) {
		return DeleteResult{Warning: fmt.Sprintf("%s was already deleted", target)}, nil
	}
	return DeleteResult{}, fmt.Errorf("deleting %s: %w", target, err)
}

// tagsWithDigest returns the tags of repo, other than skip, that resolve to digest.
func tagsWithDigest(repo name.Repository, digest, skip string, opts []remote.Option) ([]string, error) {
	tags, err := remote.List(repo, opts...)
	if err != nil {
		if IsNotFound(err) {
			return nil, nil // the repository is gone, so no tag can point to the manifest
		}
		return nil, fmt.Errorf("listing tags: %w", err)
	}

	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		shared   []string
		firstErr error
		sem      = make(chan struct{}, 8)
	)
	for _, tag := range tags {
		if tag == skip {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(tag string) {
			defer wg.Done()
			defer func() { <-sem }()
			desc, err := remote.Head(repo.Tag(tag), opts...)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err != nil && IsNotFound(err):
			case err != nil:
				if firstErr == nil {
					firstErr = fmt.Errorf("resolving tag %s: %w", tag, err)
				}
			case desc.Digest.String() == digest:
				shared = append(shared, tag)
			}
		}(tag)
	}
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	sort.Strings(shared)
	return shared, nil
}

func joinTags(repo name.Repository, tags []string) string {
	out := make([]string, len(tags))
	for i, t := range tags {
		out[i] = repo.Tag(t).String()
	}
	return strings.Join(out, ", ")
}

// IsNotFound reports whether err is a registry "not found" error.
func IsNotFound(err error) bool {
	var terr *transport.Error
	if !errors.As(err, &terr) {
		return false
	}
	if terr.StatusCode == http.StatusNotFound {
		return true
	}
	for _, d := range terr.Errors {
		switch d.Code {
		case transport.ManifestUnknownErrorCode, transport.NameUnknownErrorCode, transport.BlobUnknownErrorCode:
			return true
		}
	}
	return false
}

// IsUnsupported reports whether err means the registry does not support the operation.
func IsUnsupported(err error) bool {
	var terr *transport.Error
	if !errors.As(err, &terr) {
		return false
	}
	if terr.StatusCode == http.StatusMethodNotAllowed {
		return true
	}
	for _, d := range terr.Errors {
		if d.Code == transport.UnsupportedErrorCode {
			return true
		}
	}
	return false
}

func withAuthRetry(o Options, fn func(auth remote.Option) error) error {
	err := fn(remote.WithAuth(authn.Anonymous))
	if err == nil {
		return nil
	}
	var terr *transport.Error
	if !errors.As(err, &terr) {
		return err
	}
	switch terr.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests, http.StatusNotFound:
		return fn(remote.WithAuthFromKeychain(o.keychain()))
	}
	return err
}
