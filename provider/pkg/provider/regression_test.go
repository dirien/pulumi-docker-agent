// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"net/http"
	"sync/atomic"
	"testing"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dirien/pulumi-docker-agent/provider/pkg/ociagent"
	"github.com/dirien/pulumi-docker-agent/provider/pkg/render"
)

const secretToken = "Bearer SUPERSECRET-TOKEN-123"

// withSecretHeader returns AgentConfig inputs with a secret Authorization header and an
// unknown field (a typo) in the same toolset.
func withSecretHeader() property.Map {
	inputs := pm(map[string]any{"agents": []any{map[string]any{
		"name":  "root",
		"model": "openai/gpt-5-mini",
		"toolsets": []any{map[string]any{
			"type":    "fetch",
			"headers": map[string]any{"Authorization": secretToken},
			"extra":   map[string]any{"tolls": []any{"x"}},
		}},
	}}})
	agents := inputs.Get("agents").AsArray().AsSlice()
	ts := agents[0].AsMap().Get("toolsets").AsArray().AsSlice()
	headers := ts[0].AsMap().Get("headers").AsMap()
	headers = headers.Set("Authorization", headers.Get("Authorization").WithSecret(true))
	ts[0] = property.New(ts[0].AsMap().Set("headers", property.New(headers)))
	agents[0] = property.New(agents[0].AsMap().Set("toolsets", property.New(ts)))
	return inputs.Set("agents", property.New(agents))
}

// Review finding 1: secrets must never appear in CheckFailures or errors.
func TestCheckFailuresNeverEchoSecrets(t *testing.T) {
	t.Parallel()
	s := server(t)

	resp, err := s.Check(p.CheckRequest{Urn: urn(agentConfigToken, "c"), Inputs: withSecretHeader()})
	require.NoError(t, err)
	text := failureText(resp.Failures)
	require.NotEmpty(t, resp.Failures)
	assert.Contains(t, text, `unknown field "tolls"`)
	assert.NotContains(t, text, "SUPERSECRET")

	// A whole secret YAML document for AgentArtifact.
	doc := "version: \"16\"\nagents:\n  root:\n    model: openai/gpt-5-mini\n    toolsets:\n      - type: fetch\n" +
		"        headers:\n          Authorization: " + secretToken + "\n        tolls: [x]\n"
	in := pm(map[string]any{"reference": "localhost:5000/a/b:v1"}).Set("yaml", property.New(doc).WithSecret(true))
	resp, err = s.Check(p.CheckRequest{Urn: urn(agentArtifactToken, "a"), Inputs: in})
	require.NoError(t, err)
	text = failureText(resp.Failures)
	require.NotEmpty(t, resp.Failures)
	assert.Contains(t, text, `unknown field "tolls"`)
	assert.NotContains(t, text, "SUPERSECRET")

	// Even when the YAML is not marked secret, no YAML content is echoed.
	in = pm(map[string]any{"reference": "localhost:5000/a/b:v1", "yaml": doc})
	resp, err = s.Check(p.CheckRequest{Urn: urn(agentArtifactToken, "a"), Inputs: in})
	require.NoError(t, err)
	assert.NotContains(t, failureText(resp.Failures), "SUPERSECRET")

	// Create bypasses Check in this test; its error must not contain the secret either.
	inputs := withSecretHeader()
	_, err = s.Create(p.CreateRequest{Urn: urn(agentConfigToken, "c"), Properties: inputs})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "SUPERSECRET")

	inv, err := s.Invoke(p.InvokeRequest{Token: "docker-agent:index:validateAgentConfig", Args: pm(map[string]any{"yaml": doc})})
	require.NoError(t, err)
	assert.NotContains(t, inv.Return.Get("errors").AsArray().Get(0).AsString(), "SUPERSECRET")
}

// Review finding 2 (AgentConfig): previews show the real new outputs, including when a
// previously absent top-level input is added.
func TestAgentConfigPreviewAddsTopLevelInputs(t *testing.T) {
	t.Parallel()
	s := server(t)
	u := urn(agentConfigToken, "cfg")
	base := map[string]any{"agents": []any{map[string]any{"name": "root", "model": "openai/gpt-5-mini", "instruction": "Hi."}}}
	first := check(t, s, agentConfigToken, "cfg", base)
	require.Empty(t, first.Failures, failureText(first.Failures))

	// The create preview is exact too.
	preview, err := s.Create(p.CreateRequest{Urn: u, Properties: first.Inputs, DryRun: true})
	require.NoError(t, err)
	created, err := s.Create(p.CreateRequest{Urn: u, Properties: first.Inputs})
	require.NoError(t, err)
	assert.Equal(t, created.Properties.Get("yaml"), preview.Properties.Get("yaml"))
	assert.Equal(t, created.Properties.Get("sha256"), preview.Properties.Get("sha256"))

	added := map[string]any{
		"agents":      base["agents"],
		"permissions": map[string]any{"deny": []any{"shell"}},
		"metadata":    map[string]any{"author": "someone"},
		"models":      map[string]any{"fast": map[string]any{"provider": "anthropic", "model": "claude-haiku-4-5"}},
	}
	second := check(t, s, agentConfigToken, "cfg", added)
	require.Empty(t, second.Failures, failureText(second.Failures))
	diff, err := s.Diff(p.DiffRequest{ID: created.ID, Urn: u, State: created.Properties, Inputs: second.Inputs, OldInputs: first.Inputs})
	require.NoError(t, err)
	require.True(t, diff.HasChanges)

	up, err := s.Update(p.UpdateRequest{ID: created.ID, Urn: u, State: created.Properties, Inputs: second.Inputs,
		OldInputs: first.Inputs, DryRun: true})
	require.NoError(t, err)
	y := up.Properties.Get("yaml")
	require.True(t, y.IsString(), "preview yaml: %v", y)
	assert.Contains(t, y.AsString(), "deny:")
	assert.Contains(t, y.AsString(), "author: someone")
	assert.Equal(t, render.SHA256(y.AsString()), up.Properties.Get("sha256").AsString())

	applied, err := s.Update(p.UpdateRequest{ID: created.ID, Urn: u, State: created.Properties, Inputs: second.Inputs, OldInputs: first.Inputs})
	require.NoError(t, err)
	assert.Equal(t, applied.Properties.Get("yaml"), y, "preview equals the real update")

	// An unknown value inside a newly added input makes the derived outputs unknown, while
	// the agent names stay known.
	unknownAuthor := second.Inputs.Set("metadata", property.New(property.NewMap(map[string]property.Value{
		"author": property.New(property.Computed),
	})))
	up, err = s.Update(p.UpdateRequest{ID: created.ID, Urn: u, State: created.Properties, Inputs: unknownAuthor,
		OldInputs: first.Inputs, DryRun: true})
	require.NoError(t, err)
	assert.True(t, up.Properties.Get("yaml").IsComputed())
	assert.True(t, up.Properties.Get("sha256").IsComputed())
	assert.True(t, up.Properties.Get("requiredEnvVars").IsComputed())
	assert.Equal(t, "root", up.Properties.Get("defaultAgent").AsString())

	// Unknown agent names make agentNames unknown as well.
	agents := second.Inputs.Get("agents").AsArray().AsSlice()
	agents[0] = property.New(agents[0].AsMap().Set("name", property.New(property.Computed)))
	unknownName := second.Inputs.Set("agents", property.New(agents))
	cr, err := s.Create(p.CreateRequest{Urn: urn(agentConfigToken, "new"), Properties: unknownName, DryRun: true})
	require.NoError(t, err)
	assert.True(t, cr.Properties.Get("yaml").IsComputed())
	assert.True(t, cr.Properties.Get("agentNames").IsComputed())
}

// Review finding 2 (AgentArtifact): adding annotations for the first time must not preview
// the old digest.
func TestAgentArtifactPreviewAddsAnnotations(t *testing.T) {
	t.Parallel()
	s := server(t)
	var requests atomic.Int32
	host := newRegistry(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requests.Add(1)
			next.ServeHTTP(w, r)
		})
	})
	ref := host + "/agents/ann:v1"
	u := urn(agentArtifactToken, "ann")
	first := check(t, s, agentArtifactToken, "ann", artifactInputs(ref, artifactYAML))
	created, err := s.Create(p.CreateRequest{Urn: u, Properties: first.Inputs})
	require.NoError(t, err)

	in := artifactInputs(ref, artifactYAML)
	in["annotations"] = map[string]any{"org.opencontainers.image.source": "https://example.com"}
	second := check(t, s, agentArtifactToken, "ann", in)
	before := requests.Load()
	up, err := s.Update(p.UpdateRequest{ID: created.ID, Urn: u, State: created.Properties, Inputs: second.Inputs,
		OldInputs: first.Inputs, DryRun: true})
	require.NoError(t, err)
	assert.Equal(t, before, requests.Load(), "a preview must not talk to the registry")
	for _, k := range []string{"digest", "pinnedReference", "created", "manifestAnnotations"} {
		assert.True(t, up.Properties.Get(k).IsComputed(), "%s must be unknown in the preview", k)
	}
	assert.Equal(t, created.Properties.Get("contentSha256"), up.Properties.Get("contentSha256"), "the YAML did not change")

	applied, err := s.Update(p.UpdateRequest{ID: created.ID, Urn: u, State: created.Properties, Inputs: second.Inputs, OldInputs: first.Inputs})
	require.NoError(t, err)
	assert.NotEqual(t, created.Properties.Get("digest"), applied.Properties.Get("digest"))

	// A change that does not push (deleteBehavior) keeps the digest known in the preview.
	in["deleteBehavior"] = "delete"
	third := check(t, s, agentArtifactToken, "ann", in)
	up, err = s.Update(p.UpdateRequest{ID: created.ID, Urn: u, State: applied.Properties, Inputs: third.Inputs,
		OldInputs: second.Inputs, DryRun: true})
	require.NoError(t, err)
	assert.Equal(t, applied.Properties.Get("digest"), up.Properties.Get("digest"))
	assert.Equal(t, applied.Properties.Get("created"), up.Properties.Get("created"))
}

// Review finding 4: a non-agent manifest at the tag is drift, not a refresh failure.
func TestAgentArtifactRefreshWithForeignManifest(t *testing.T) {
	t.Parallel()
	s := server(t)
	host := newRegistry(t, nil)
	ref := host + "/agents/foreign:v1"
	u := urn(agentArtifactToken, "f")
	first := check(t, s, agentArtifactToken, "f", artifactInputs(ref, artifactYAML))
	created, err := s.Create(p.CreateRequest{Urn: u, Properties: first.Inputs})
	require.NoError(t, err)

	// Someone pushes something that is not a docker-agent artifact to the tag.
	foreign, err := ociagent.Build([]byte("just: data\n"), map[string]string{"org.opencontainers.image.title": "image"})
	require.NoError(t, err)
	_, err = ociagent.Push(t.Context(), ref, foreign, ociagent.Options{})
	require.NoError(t, err)

	read, err := s.Read(p.ReadRequest{ID: created.ID, Urn: u, Properties: created.Properties, Inputs: first.Inputs})
	require.NoError(t, err, "refresh must not fail")
	assert.Equal(t, created.ID, read.ID)
	assert.Equal(t, foreign.Digest.String(), read.Properties.Get("digest").AsString())
	assert.Equal(t, "", read.Properties.Get("yaml").AsString())

	// Refreshing again is stable.
	again, err := s.Read(p.ReadRequest{ID: created.ID, Urn: u, Properties: read.Properties, Inputs: read.Inputs})
	require.NoError(t, err)
	assert.Equal(t, read.Properties.Get("digest"), again.Properties.Get("digest"))

	// The next up re-pushes the configured agent.
	same := check(t, s, agentArtifactToken, "f", artifactInputs(ref, artifactYAML))
	diff, err := s.Diff(p.DiffRequest{ID: created.ID, Urn: u, State: read.Properties, Inputs: same.Inputs, OldInputs: read.Inputs})
	require.NoError(t, err)
	require.True(t, diff.HasChanges)
	up, err := s.Update(p.UpdateRequest{ID: created.ID, Urn: u, State: read.Properties, Inputs: same.Inputs, OldInputs: read.Inputs})
	require.NoError(t, err)
	pulled, err := ociagent.Pull(t.Context(), ref, ociagent.Options{})
	require.NoError(t, err)
	assert.Equal(t, artifactYAML, string(pulled.YAML))
	assert.Equal(t, up.Properties.Get("digest").AsString(), pulled.Digest)
}

// Review finding 8: name-derived outputs are only secret when their sources are.
func TestAgentConfigOutputSecretness(t *testing.T) {
	t.Parallel()
	s := server(t)
	create := func(inputs property.Map) property.Map {
		t.Helper()
		c, err := s.Check(p.CheckRequest{Urn: urn(agentConfigToken, "s"), Inputs: inputs})
		require.NoError(t, err)
		require.Empty(t, c.Failures, failureText(c.Failures))
		cr, err := s.Create(p.CreateRequest{Urn: urn(agentConfigToken, "s"), Properties: c.Inputs})
		require.NoError(t, err)
		return cr.Properties
	}

	// A secret instruction: only yaml and sha256 are secret.
	inputs := pm(agentConfigInputs("x"))
	agents := inputs.Get("agents").AsArray().AsSlice()
	agents[1] = property.New(agents[1].AsMap().Set("instruction", property.New("secret prompt").WithSecret(true)))
	out := create(inputs.Set("agents", property.New(agents)))
	assert.True(t, out.Get("yaml").Secret())
	assert.True(t, out.Get("sha256").Secret())
	for _, k := range []string{"agentNames", "defaultAgent", "requiredEnvVars", "dockerAgentVersion"} {
		assert.False(t, out.Get(k).Secret(), k)
	}
	assert.Equal(t, "root", out.Get("defaultAgent").AsString())

	// A secret agent name makes agentNames and defaultAgent secret.
	inputs = pm(agentConfigInputs("x"))
	agents = inputs.Get("agents").AsArray().AsSlice()
	agents[1] = property.New(agents[1].AsMap().Set("name", property.New("root").WithSecret(true)))
	out = create(inputs.Set("agents", property.New(agents)))
	assert.True(t, out.Get("agentNames").Secret())
	assert.True(t, out.Get("defaultAgent").Secret())
	assert.False(t, out.Get("requiredEnvVars").Secret())

	// A secret tokenKey makes requiredEnvVars secret.
	inputs = pm(agentConfigInputs("x"))
	models := inputs.Get("models").AsMap()
	fast := models.Get("fast").AsMap().Set("tokenKey", property.New("MY_KEY").WithSecret(true))
	out = create(inputs.Set("models", property.New(models.Set("fast", property.New(fast)))))
	assert.True(t, out.Get("requiredEnvVars").Secret())
	assert.False(t, out.Get("agentNames").Secret())
}

// Review finding 5: forceHttps is decoded from the provider configuration and turned into
// registry options. (The HTTPS-only transport itself is tested in pkg/ociagent.)
func TestForceHTTPSConfig(t *testing.T) {
	t.Parallel()
	s := server(t)
	require.NoError(t, s.Configure(p.ConfigureRequest{Args: pm(map[string]any{
		"forceHttps":   true,
		"registryAuth": []any{map[string]any{"address": "ghcr.io", "username": "u", "password": "p"}},
	})}))
	// Loopback registries keep working with forceHttps.
	host := newRegistry(t, nil)
	c := check(t, s, agentArtifactToken, "tls", artifactInputs(host+"/agents/tls:v1", artifactYAML))
	_, err := s.Create(p.CreateRequest{Urn: urn(agentArtifactToken, "tls"), Properties: c.Inputs})
	require.NoError(t, err)

	o := optionsFor(Config{ForceHTTPS: ptr(true), RegistryAuth: []RegistryAuth{{Address: "ghcr.io", Username: ptr("u"), Password: ptr("p")}}}, nil)
	assert.True(t, o.ForceHTTPS)
	assert.False(t, o.Insecure)
	assert.Equal(t, []ociagent.Credential{{Address: "ghcr.io", Username: "u", Password: "p"}}, o.Credentials)
	o = optionsFor(Config{}, ptr(true))
	assert.False(t, o.ForceHTTPS)
	assert.True(t, o.Insecure)
}
