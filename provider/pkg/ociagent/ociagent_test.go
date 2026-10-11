// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package ociagent

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sampleYAML = `version: "16"
metadata:
  author: pulumi
  license: Apache-2.0
  version: 0.1.0
  tags: [demo, test]
agents:
  root:
    model: openai/gpt-5-mini
    instruction: You are helpful.
`

// newRegistry starts an in-process OCI registry and returns its host:port.
func newRegistry(t *testing.T, wrap func(http.Handler) http.Handler) string {
	t.Helper()
	h := registry.New(registry.Logger(log.New(io.Discard, "", 0)))
	if wrap != nil {
		h = wrap(h)
	}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "http://")
}

func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "cli", name))
	require.NoError(t, err)
	return b
}

// TestBuildMatchesDockerAgentCLI rebuilds two artifacts captured from
// `docker agent share push` (docker-agent v1.150.0) and requires byte-identical
// manifests, configs and layers.
func TestBuildMatchesDockerAgentCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ yaml, manifest string }{
		{"agent.yaml", "agent.manifest.json"},
		{"meta.yaml", "meta.manifest.json"},
	} {
		t.Run(tc.yaml, func(t *testing.T) {
			t.Parallel()
			doc := readTestdata(t, tc.yaml)
			want := readTestdata(t, tc.manifest)
			var cli manifestDoc
			require.NoError(t, json.Unmarshal(want, &cli))
			// The fixtures were captured with one docker-agent release. Only the version
			// annotations depend on it, so substitute the pinned version: upgrading
			// docker-agent (make upgrade_docker_agent) must not break this test.
			captured := cli.Annotations[AnnotationVersion]
			want = bytes.ReplaceAll(want, []byte(strconv.Quote(captured)), []byte(strconv.Quote(DockerAgentVersion)))
			require.NoError(t, json.Unmarshal(want, &cli))

			ann := Annotations(doc, cli.Annotations[AnnotationCreated], nil)
			if tc.yaml == "agent.yaml" {
				// The CLI derives the authors annotation from metadata.author.
				assert.Equal(t, "rea-test", ann[AnnotationAuthors])
			}
			assert.Equal(t, cli.Annotations, ann, "annotations derived from the YAML must match the CLI")

			a, err := Build(doc, ann)
			require.NoError(t, err)
			assert.Equal(t, string(want), string(a.Manifest))
			assert.Equal(t, cli.Layers[0].Digest, a.LayerDigest)
			assert.Equal(t, cli.Config.Digest, a.ConfigDigest)
			assert.Equal(t, a.DiffID.Hex, a.ContentSHA256())
		})
	}
	// The config blob is the one the CLI uploads.
	a, err := Build(readTestdata(t, "agent.yaml"), nil)
	require.NoError(t, err)
	assert.Equal(t, string(readTestdata(t, "agent.config.json")), string(a.Config))
}

func TestBuildIsDeterministic(t *testing.T) {
	t.Parallel()
	ann := Annotations([]byte(sampleYAML), "2026-01-02T03:04:05Z", map[string]string{"x": "y"})
	a1, err := Build([]byte(sampleYAML), ann)
	require.NoError(t, err)
	a2, err := Build([]byte(sampleYAML), ann)
	require.NoError(t, err)
	assert.Equal(t, a1.Layer, a2.Layer)
	assert.Equal(t, a1.LayerDigest, a2.LayerDigest)
	assert.Equal(t, a1.Manifest, a2.Manifest)
	assert.Equal(t, a1.Digest, a2.Digest)

	// A different timestamp changes the manifest digest but not the layer.
	a3, err := Build([]byte(sampleYAML), Annotations([]byte(sampleYAML), "2027-01-01T00:00:00Z", nil))
	require.NoError(t, err)
	assert.Equal(t, a1.LayerDigest, a3.LayerDigest)
	assert.Equal(t, a1.ConfigDigest, a3.ConfigDigest)
	assert.NotEqual(t, a1.Digest, a3.Digest)

	// The layer is the gzip of the raw YAML, with no name and no mtime in the header.
	zr, err := gzip.NewReader(bytes.NewReader(a1.Layer))
	require.NoError(t, err)
	assert.Empty(t, zr.Name)
	assert.True(t, zr.ModTime.IsZero())
	raw, err := io.ReadAll(zr)
	require.NoError(t, err)
	assert.Equal(t, sampleYAML, string(raw))
}

func TestAnnotations(t *testing.T) {
	t.Parallel()
	ann := Annotations([]byte(sampleYAML), "2026-01-02T03:04:05Z", map[string]string{
		AnnotationVersion:                 "v0.0.0", // reserved, ignored
		AnnotationCreated:                 "never",  // reserved, ignored
		AnnotationDescription:             "custom description",
		"org.opencontainers.image.source": "https://github.com/dirien/pulumi-docker-agent",
	})
	assert.Equal(t, map[string]string{
		AnnotationVersion:                 DockerAgentVersion,
		AnnotationLegacyVersion:           DockerAgentVersion,
		AnnotationCreated:                 "2026-01-02T03:04:05Z",
		AnnotationDescription:             "custom description",
		AnnotationAuthors:                 "pulumi",
		AnnotationLicenses:                "Apache-2.0",
		AnnotationRevision:                "0.1.0",
		AnnotationTags:                    "demo,test",
		"org.opencontainers.image.source": "https://github.com/dirien/pulumi-docker-agent",
	}, ann)
	assert.Equal(t, "2026-01-02T03:04:05Z", FormatCreated(time.Date(2026, 1, 2, 4, 4, 5, 999, time.FixedZone("CET", 3600))))
}

func TestPushPullRoundTrip(t *testing.T) {
	t.Parallel()
	host := newRegistry(t, nil)
	ref := host + "/agents/demo:v1"
	ann := Annotations([]byte(sampleYAML), "2026-01-02T03:04:05Z", nil)
	a, err := Build([]byte(sampleYAML), ann)
	require.NoError(t, err)

	res, err := Push(t.Context(), ref, a, Options{})
	require.NoError(t, err)
	assert.Equal(t, a.Digest.String(), res.Digest)
	assert.Equal(t, host+"/agents/demo@"+a.Digest.String(), res.PinnedReference)

	// The registry stores the exact manifest bytes with the docker-agent media types.
	parsed, err := name.ParseReference(ref)
	require.NoError(t, err)
	desc, err := remote.Get(parsed)
	require.NoError(t, err)
	assert.Equal(t, a.Manifest, desc.Manifest)
	assert.Equal(t, res.Digest, desc.Digest.String())
	var m map[string]any
	require.NoError(t, json.Unmarshal(desc.Manifest, &m))
	assert.Equal(t, "application/vnd.oci.image.manifest.v1+json", m["mediaType"])
	assert.Equal(t, "application/vnd.docker.agent.config.v1+json", m["artifactType"])
	assert.Equal(t, "application/vnd.docker.container.image.v1+json", m["config"].(map[string]any)["mediaType"])
	layers := m["layers"].([]any)
	require.Len(t, layers, 1)
	assert.Equal(t, "application/vnd.docker.image.rootfs.diff.tar.gzip", layers[0].(map[string]any)["mediaType"])
	annotations := m["annotations"].(map[string]any)
	assert.Equal(t, DockerAgentVersion, annotations["io.docker.agent.version"])
	assert.Equal(t, DockerAgentVersion, annotations["io.docker.cagent.version"])
	assert.Equal(t, "2026-01-02T03:04:05Z", annotations["org.opencontainers.image.created"])
	assert.Equal(t, "OCI artifact containing agent.yaml", annotations["org.opencontainers.image.description"])

	for _, r := range []string{ref, res.PinnedReference} {
		pulled, err := Pull(t.Context(), r, Options{})
		require.NoError(t, err, r)
		assert.Equal(t, sampleYAML, string(pulled.YAML))
		assert.Equal(t, res.Digest, pulled.Digest)
		assert.Equal(t, ann, pulled.Annotations)
	}

	digest, found, err := Head(t.Context(), ref, Options{})
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, res.Digest, digest)

	// Pushing identical content again gives the same digest (blobs already exist).
	res2, err := Push(t.Context(), ref, a, Options{})
	require.NoError(t, err)
	assert.Equal(t, res.Digest, res2.Digest)
}

func TestHeadNotFound(t *testing.T) {
	t.Parallel()
	host := newRegistry(t, nil)
	_, found, err := Head(t.Context(), host+"/nothing/here:v1", Options{})
	require.NoError(t, err)
	assert.False(t, found)
}

func TestPullRejectsForeignManifest(t *testing.T) {
	t.Parallel()
	host := newRegistry(t, nil)
	ref := host + "/agents/foreign:v1"
	// No version annotation: not created by `docker agent share push`.
	a, err := Build([]byte(sampleYAML), map[string]string{AnnotationCreated: "2026-01-02T03:04:05Z"})
	require.NoError(t, err)
	_, err = Push(t.Context(), ref, a, Options{})
	require.NoError(t, err)

	_, err = Pull(t.Context(), ref, Options{})
	require.ErrorIs(t, err, ErrNotAgentArtifact)
	assert.Contains(t, err.Error(), "wasn't created by `docker agent share push`")

	// The legacy cagent annotation alone is accepted, like upstream.
	b, err := Build([]byte(sampleYAML), map[string]string{AnnotationLegacyVersion: "v1.0.0"})
	require.NoError(t, err)
	_, err = Push(t.Context(), host+"/agents/legacy:v1", b, Options{})
	require.NoError(t, err)
	_, err = Pull(t.Context(), host+"/agents/legacy:v1", Options{})
	require.NoError(t, err)
}

func TestPullMissingTag(t *testing.T) {
	t.Parallel()
	host := newRegistry(t, nil)
	_, err := Pull(t.Context(), host+"/agents/missing:v1", Options{})
	require.Error(t, err)
	assert.True(t, IsNotFound(err), err.Error())
}

func TestDelete(t *testing.T) {
	t.Parallel()
	host := newRegistry(t, nil)
	ref := host + "/agents/del:v1"
	a, err := Build([]byte(sampleYAML), Annotations([]byte(sampleYAML), "2026-01-02T03:04:05Z", nil))
	require.NoError(t, err)
	res, err := Push(t.Context(), ref, a, Options{})
	require.NoError(t, err)

	out, err := Delete(t.Context(), ref, res.Digest, Options{})
	require.NoError(t, err)
	assert.True(t, out.Deleted)
	assert.Empty(t, out.Warning)

	_, err = Pull(t.Context(), res.PinnedReference, Options{})
	require.Error(t, err)

	// Deleting again is not an error.
	out, err = Delete(t.Context(), ref, res.Digest, Options{})
	require.NoError(t, err)
	assert.False(t, out.Deleted)
}

// TestDeleteUnsupported mimics registry:2 without REGISTRY_STORAGE_DELETE_ENABLED, which
// answers 405 UNSUPPORTED.
func TestDeleteUnsupported(t *testing.T) {
	t.Parallel()
	host := newRegistry(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodDelete {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusMethodNotAllowed)
				_, _ = w.Write([]byte(`{"errors":[{"code":"UNSUPPORTED","message":"The operation is unsupported."}]}`))
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	ref := host + "/agents/keep:v1"
	a, err := Build([]byte(sampleYAML), Annotations([]byte(sampleYAML), "2026-01-02T03:04:05Z", nil))
	require.NoError(t, err)
	res, err := Push(t.Context(), ref, a, Options{})
	require.NoError(t, err)

	out, err := Delete(t.Context(), ref, res.Digest, Options{})
	require.NoError(t, err, "405 must be a warning, not an error")
	assert.False(t, out.Deleted)
	assert.Contains(t, out.Warning, "does not support deleting manifests")

	// The artifact is still there.
	_, err = Pull(t.Context(), ref, Options{})
	require.NoError(t, err)
}

func TestDeleteOtherErrorsFail(t *testing.T) {
	t.Parallel()
	host := newRegistry(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodDelete {
				// 400 is not retried by go-containerregistry (unlike 5xx), keeping the test fast.
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"errors":[{"code":"DENIED","message":"no"}]}`))
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	_, err := Delete(t.Context(), host+"/agents/x:v1", "sha256:"+strings.Repeat("a", 64), Options{})
	require.Error(t, err)
}

// TestAuth checks credentials from the static keychain and the anonymous-first pull.
func TestAuth(t *testing.T) {
	t.Parallel()
	var anonymousTries, authedRequests atomic.Int32
	host := newRegistry(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u, p, ok := r.BasicAuth()
			if !ok {
				anonymousTries.Add(1)
				w.Header().Set("WWW-Authenticate", `Basic realm="test"`)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if u != "alice" || p != "s3cret" {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			authedRequests.Add(1)
			next.ServeHTTP(w, r)
		})
	})
	ref := host + "/private/agent:v1"
	a, err := Build([]byte(sampleYAML), Annotations([]byte(sampleYAML), "2026-01-02T03:04:05Z", nil))
	require.NoError(t, err)

	// Without credentials the push fails.
	_, err = Push(t.Context(), ref, a, Options{Keychain: StaticKeychain{}})
	require.Error(t, err)

	opts := Options{Keychain: StaticKeychain{{Address: "http://" + host + "/", Username: "alice", Password: "s3cret"}}}
	_, err = Push(t.Context(), ref, a, opts)
	require.NoError(t, err)

	before := anonymousTries.Load()
	pulled, err := Pull(t.Context(), ref, opts)
	require.NoError(t, err)
	assert.Equal(t, sampleYAML, string(pulled.YAML))
	assert.Greater(t, anonymousTries.Load(), before, "pull must try anonymously first")
	assert.Positive(t, authedRequests.Load())
}

func TestDecodeLayer(t *testing.T) {
	t.Parallel()
	doc := []byte(sampleYAML)

	got, err := DecodeLayer(doc)
	require.NoError(t, err)
	assert.Equal(t, doc, got, "plain YAML")

	gz, err := Gzip(doc)
	require.NoError(t, err)
	got, err = DecodeLayer(gz)
	require.NoError(t, err)
	assert.Equal(t, doc, got, "gzip of YAML")

	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: "agent.yaml", Mode: 0o644, Size: int64(len(doc)), Typeflag: tar.TypeReg}))
	_, err = tw.Write(doc)
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	tgz, err := Gzip(tarBuf.Bytes())
	require.NoError(t, err)
	got, err = DecodeLayer(tgz)
	require.NoError(t, err)
	assert.Equal(t, doc, got, "gzipped tar")
}

func TestParseReferences(t *testing.T) {
	t.Parallel()
	tag, err := ParseTag("localhost:5000/agents/demo", false)
	require.NoError(t, err)
	assert.Equal(t, "localhost:5000/agents/demo:latest", tag.Name())

	_, err = ParseTag("localhost:5000/agents/demo@sha256:"+strings.Repeat("a", 64), false)
	require.ErrorContains(t, err, "must be a tag")

	_, err = ParseTag("UPPER/case:tag", false)
	require.Error(t, err)

	r, err := ParseReference("docker.io/dirien/agent", false)
	require.NoError(t, err)
	assert.Equal(t, "index.docker.io/dirien/agent:latest", r.Name())
}

func TestKeychain(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"ghcr.io":                        "ghcr.io",
		"https://ghcr.io/v2/":            "ghcr.io",
		"docker.io":                      "index.docker.io",
		"https://index.docker.io/v1/":    "index.docker.io",
		"registry-1.docker.io":           "index.docker.io",
		"localhost:5000":                 "localhost:5000",
		"LOCALHOST:5000/some/path":       "localhost:5000",
		" my.registry.example.com:8443 ": "my.registry.example.com:8443",
	} {
		assert.Equal(t, want, NormalizeRegistry(in), in)
	}

	kc := StaticKeychain{{Address: "docker.io", Username: "u", Password: "p"}}
	reg, err := name.NewRegistry("index.docker.io")
	require.NoError(t, err)
	auth, err := kc.Resolve(reg)
	require.NoError(t, err)
	cfg, err := auth.Authorization()
	require.NoError(t, err)
	assert.Equal(t, "u", cfg.Username)

	other, err := name.NewRegistry("ghcr.io")
	require.NoError(t, err)
	auth, err = kc.Resolve(other)
	require.NoError(t, err)
	assert.Equal(t, authn.Anonymous, auth)

	assert.Equal(t, authn.DefaultKeychain, Keychain(nil))
}

func TestParseMetadataIsLenient(t *testing.T) {
	t.Parallel()
	assert.Equal(t, Metadata{}, ParseMetadata([]byte(":::not yaml")))
	md := ParseMetadata([]byte(sampleYAML))
	assert.Equal(t, "pulumi", md.Author)
	assert.Equal(t, []string{"demo", "test"}, md.Tags)
}

// TestDeleteKeepsManifestSharedByAnotherTag is the regression test for deleting by digest
// removing other tags: identical content pushed to two tags has one digest.
func TestDeleteKeepsManifestSharedByAnotherTag(t *testing.T) {
	t.Parallel()
	host := newRegistry(t, nil)
	a, err := Build([]byte(sampleYAML), Annotations([]byte(sampleYAML), "2026-01-02T03:04:05Z", nil))
	require.NoError(t, err)
	v1, err := Push(t.Context(), host+"/agents/shared:v1", a, Options{})
	require.NoError(t, err)
	latest, err := Push(t.Context(), host+"/agents/shared:latest", a, Options{})
	require.NoError(t, err)
	require.Equal(t, v1.Digest, latest.Digest)

	out, err := Delete(t.Context(), host+"/agents/shared:v1", v1.Digest, Options{})
	require.NoError(t, err)
	assert.False(t, out.Deleted)
	assert.Contains(t, out.Warning, host+"/agents/shared:latest")
	for _, ref := range []string{host + "/agents/shared:v1", host + "/agents/shared:latest", v1.PinnedReference} {
		_, err := Pull(t.Context(), ref, Options{})
		require.NoError(t, err, "%s must still exist", ref)
	}

	// Once the other tag points elsewhere, the delete goes ahead.
	other, err := Build([]byte(sampleYAML), Annotations([]byte(sampleYAML), "2030-01-01T00:00:00Z", nil))
	require.NoError(t, err)
	_, err = Push(t.Context(), host+"/agents/shared:latest", other, Options{})
	require.NoError(t, err)
	out, err = Delete(t.Context(), host+"/agents/shared:v1", v1.Digest, Options{})
	require.NoError(t, err)
	assert.True(t, out.Deleted, out.Warning)
	_, err = Pull(t.Context(), v1.PinnedReference, Options{})
	require.Error(t, err)
}

// TestDeleteRetainsWhenTagsCannotBeListed keeps the artifact if the tag list fails.
func TestDeleteRetainsWhenTagsCannotBeListed(t *testing.T) {
	t.Parallel()
	var deletes atomic.Int32
	host := newRegistry(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/tags/list") {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"errors":[{"code":"DENIED","message":"listing is disabled"}]}`))
				return
			}
			if r.Method == http.MethodDelete {
				deletes.Add(1)
			}
			next.ServeHTTP(w, r)
		})
	})
	ref := host + "/agents/nolist:v1"
	a, err := Build([]byte(sampleYAML), Annotations([]byte(sampleYAML), "2026-01-02T03:04:05Z", nil))
	require.NoError(t, err)
	res, err := Push(t.Context(), ref, a, Options{})
	require.NoError(t, err)

	out, err := Delete(t.Context(), ref, res.Digest, Options{})
	require.NoError(t, err)
	assert.False(t, out.Deleted)
	assert.Contains(t, out.Warning, "could not check whether other tags")
	assert.Zero(t, deletes.Load(), "no DELETE request may be sent")
	_, err = Pull(t.Context(), ref, Options{})
	require.NoError(t, err)
}

// basicAuthRegistry requires alice/s3cret and counts unauthenticated manifest requests.
// With hide404, unauthenticated manifest requests get 404 instead of 401.
func basicAuthRegistry(t *testing.T, anonymousManifestRequests *atomic.Int32, hide404 bool) string {
	t.Helper()
	return newRegistry(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u, p, ok := r.BasicAuth()
			if ok && u == "alice" && p == "s3cret" {
				next.ServeHTTP(w, r)
				return
			}
			if strings.Contains(r.URL.Path, "/manifests/") {
				anonymousManifestRequests.Add(1)
				if hide404 {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusNotFound)
					_, _ = w.Write([]byte(`{"errors":[{"code":"NAME_UNKNOWN","message":"repository name not known"}]}`))
					return
				}
			}
			if hide404 && r.URL.Path == "/v2/" {
				// Anonymous pings succeed, like registries that hide private repositories.
				w.WriteHeader(http.StatusOK)
				return
			}
			w.Header().Set("WWW-Authenticate", `Basic realm="test"`)
			w.WriteHeader(http.StatusUnauthorized)
		})
	})
}

func TestPullSendsExplicitCredentialsFirst(t *testing.T) {
	t.Parallel()
	var anon atomic.Int32
	host := basicAuthRegistry(t, &anon, false)
	ref := host + "/private/agent:v1"
	creds := []Credential{{Address: host, Username: "alice", Password: "s3cret"}}
	a, err := Build([]byte(sampleYAML), Annotations([]byte(sampleYAML), "2026-01-02T03:04:05Z", nil))
	require.NoError(t, err)
	_, err = Push(t.Context(), ref, a, Options{Credentials: creds})
	require.NoError(t, err)

	anon.Store(0)
	pulled, err := Pull(t.Context(), ref, Options{Credentials: creds})
	require.NoError(t, err)
	assert.Equal(t, sampleYAML, string(pulled.YAML))
	assert.Zero(t, anon.Load(), "explicit credentials must be sent from the first manifest request")
}

func TestPullRetriesWithCredentialsOn404(t *testing.T) {
	t.Parallel()
	var anon atomic.Int32
	host := basicAuthRegistry(t, &anon, true)
	ref := host + "/private/agent:v1"
	kc := StaticKeychain{{Address: host, Username: "alice", Password: "s3cret"}}
	a, err := Build([]byte(sampleYAML), Annotations([]byte(sampleYAML), "2026-01-02T03:04:05Z", nil))
	require.NoError(t, err)
	_, err = Push(t.Context(), ref, a, Options{Keychain: kc})
	require.NoError(t, err)

	anon.Store(0)
	// The keychain is not "explicit" here, so Pull tries anonymously first, gets a 404 and
	// retries with credentials.
	pulled, err := Pull(t.Context(), ref, Options{Keychain: kc})
	require.NoError(t, err)
	assert.Equal(t, sampleYAML, string(pulled.YAML))
	assert.Positive(t, anon.Load(), "the first attempt is anonymous")
}

func TestForceHTTPS(t *testing.T) {
	t.Parallel()
	base := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	rt := &httpsOnly{base: base}
	for url, allowed := range map[string]bool{
		"http://192.168.1.10:5000/v2/":    false,
		"http://10.0.0.5/v2/":             false,
		"http://registry.example.com/v2/": false,
		"https://192.168.1.10:5000/v2/":   true,
		"http://localhost:5000/v2/":       true,
		"http://registry.localhost:5000/": true,
		"http://127.0.0.1:5000/v2/":       true,
		"http://[::1]:5000/v2/":           true,
	} {
		req, err := http.NewRequest(http.MethodGet, url, nil)
		require.NoError(t, err)
		resp, err := rt.RoundTrip(req)
		if allowed {
			require.NoError(t, err, url)
			_ = resp.Body.Close()
		} else {
			require.ErrorContains(t, err, "forceHttps", url)
		}
	}

	// The option is wired into every registry call; loopback registries keep working.
	host := newRegistry(t, nil)
	a, err := Build([]byte(sampleYAML), Annotations([]byte(sampleYAML), "2026-01-02T03:04:05Z", nil))
	require.NoError(t, err)
	_, err = Push(t.Context(), host+"/agents/tls:v1", a, Options{ForceHTTPS: true})
	require.NoError(t, err)
	_, err = Pull(t.Context(), host+"/agents/tls:v1", Options{ForceHTTPS: true})
	require.NoError(t, err)
	// `insecure` wins over forceHttps; without forceHttps nothing is wrapped.
	_, wrapped := Options{ForceHTTPS: true, Insecure: true}.transport().(*httpsOnly)
	assert.False(t, wrapped)
	_, wrapped = Options{}.transport().(*httpsOnly)
	assert.False(t, wrapped)
	_, wrapped = Options{ForceHTTPS: true}.transport().(*httpsOnly)
	assert.True(t, wrapped)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
