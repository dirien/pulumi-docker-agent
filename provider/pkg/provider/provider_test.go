// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/blang/semver"
	"github.com/google/go-containerregistry/pkg/registry"
	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/integration"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/common/tokens"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dirien/pulumi-docker-agent/provider/pkg/ociagent"
)

const (
	agentConfigToken   = "docker-agent:index:AgentConfig"
	agentArtifactToken = "docker-agent:index:AgentArtifact"
)

func server(t *testing.T) integration.Server {
	t.Helper()
	s, err := integration.NewServer(t.Context(), Name, semver.MustParse("0.1.0"), integration.WithProvider(Provider()))
	require.NoError(t, err)
	return s
}

func urn(typ, name string) resource.URN {
	return resource.NewURN("test", "proj", "", tokens.Type(typ), name)
}

// pm converts a plain Go map into a property.Map.
func pm(m map[string]any) property.Map {
	return resource.FromResourcePropertyValue(resource.NewObjectProperty(resource.NewPropertyMapFromMap(m))).AsMap()
}

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

func agentConfigInputs(instruction string) map[string]any {
	return map[string]any{
		"metadata": map[string]any{"author": "pulumi", "tags": []any{"demo"}},
		"agents": []any{
			map[string]any{
				"name":        "helper",
				"model":       "fast",
				"instruction": "Help.",
			},
			map[string]any{
				"name":        "root",
				"model":       "anthropic/claude-sonnet-4-5",
				"instruction": instruction,
				"subAgents":   []any{"helper"},
				"toolsets":    []any{map[string]any{"type": "think"}, map[string]any{"type": "mcp", "ref": "docker:duckduckgo"}},
			},
		},
		"models": map[string]any{
			"fast": map[string]any{"provider": "openai", "model": "gpt-5-mini", "temperature": 0.2},
		},
	}
}

func check(t *testing.T, s integration.Server, typ, name string, inputs map[string]any) p.CheckResponse {
	t.Helper()
	resp, err := s.Check(p.CheckRequest{Urn: urn(typ, name), Inputs: pm(inputs)})
	require.NoError(t, err)
	return resp
}

func failureText(fs []p.CheckFailure) string {
	var b strings.Builder
	for _, f := range fs {
		b.WriteString(f.Property + ": " + f.Reason + "\n")
	}
	return b.String()
}

func TestSchema(t *testing.T) {
	t.Parallel()
	resp, err := server(t).GetSchema(p.GetSchemaRequest{})
	require.NoError(t, err)
	var schema map[string]any
	require.NoError(t, json.Unmarshal([]byte(resp.Schema), &schema))
	assert.Equal(t, "docker-agent", schema["name"])
	assert.Equal(t, "Docker Agent", schema["displayName"])
	assert.Equal(t, "github://api.github.com/dirien/pulumi-docker-agent", schema["pluginDownloadURL"])
	resources := schema["resources"].(map[string]any)
	assert.Contains(t, resources, agentConfigToken)
	assert.Contains(t, resources, agentArtifactToken)
	functions := schema["functions"].(map[string]any)
	assert.Contains(t, functions, "docker-agent:index:getAgent")
	assert.Contains(t, functions, "docker-agent:index:validateAgentConfig")
	lang := schema["language"].(map[string]any)
	assert.Equal(t, "@ediri/pulumi-docker-agent", lang["nodejs"].(map[string]any)["packageName"])
	assert.Equal(t, "ediri_docker_agent", lang["python"].(map[string]any)["packageName"])
	assert.Equal(t, "ediri", lang["csharp"].(map[string]any)["rootNamespace"])
	assert.Equal(t, "github.com/dirien/pulumi-docker-agent/sdk/go/dockeragent", lang["go"].(map[string]any)["importBasePath"])
	art := resources[agentArtifactToken].(map[string]any)
	ref := art["inputProperties"].(map[string]any)["reference"].(map[string]any)
	assert.Equal(t, true, ref["replaceOnChanges"])
}

func TestAgentConfigCheckFailures(t *testing.T) {
	t.Parallel()
	s := server(t)
	cases := map[string]struct {
		inputs map[string]any
		want   string
	}{
		"unknown toolset type": {
			inputs: map[string]any{"agents": []any{map[string]any{
				"name": "root", "model": "openai/gpt-5-mini", "toolsets": []any{map[string]any{"type": "nosuch"}},
			}}},
			want: `unknown toolset type "nosuch" at agents.root.toolsets[0]`,
		},
		"undefined model": {
			inputs: map[string]any{"agents": []any{map[string]any{"name": "root", "model": "missing"}}},
			want:   "non-existent model 'missing'",
		},
		"unknown field in extra": {
			inputs: map[string]any{"agents": []any{map[string]any{
				"name": "root", "model": "openai/gpt-5-mini", "extra": map[string]any{"instructions": "typo"},
			}}},
			want: `unknown field "instructions"`,
		},
		"missing sub-agent": {
			inputs: map[string]any{"agents": []any{map[string]any{
				"name": "root", "model": "openai/gpt-5-mini", "subAgents": []any{"ghost"},
			}}},
			want: "non-existent sub-agent 'ghost'",
		},
		"duplicate agent": {
			inputs: map[string]any{"agents": []any{
				map[string]any{"name": "root", "model": "openai/gpt-5-mini"},
				map[string]any{"name": "root", "model": "openai/gpt-5-mini"},
			}},
			want: `duplicate agent name "root"`,
		},
		"no agents": {
			inputs: map[string]any{"agents": []any{}},
			want:   "at least one agent",
		},
		"missing required agents": {
			inputs: map[string]any{},
			want:   "agents",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			resp := check(t, s, agentConfigToken, "cfg", tc.inputs)
			require.NotEmpty(t, resp.Failures)
			assert.Contains(t, failureText(resp.Failures), tc.want)
			assert.NotContains(t, failureText(resp.Failures), "\x1b[")
		})
	}
}

func TestAgentConfigSkipValidation(t *testing.T) {
	t.Parallel()
	s := server(t)
	inputs := map[string]any{
		"skipValidation": true,
		"agents": []any{map[string]any{
			"name": "root", "model": "openai/gpt-5-mini", "toolsets": []any{map[string]any{"type": "from_the_future"}},
		}},
	}
	resp := check(t, s, agentConfigToken, "cfg", inputs)
	require.Empty(t, resp.Failures, failureText(resp.Failures))
	created, err := s.Create(p.CreateRequest{Urn: urn(agentConfigToken, "cfg"), Properties: resp.Inputs})
	require.NoError(t, err)
	assert.Contains(t, created.Properties.Get("yaml").AsString(), "type: from_the_future")
}

func TestAgentConfigCheckDefaultsAndUnknowns(t *testing.T) {
	t.Parallel()
	s := server(t)
	resp := check(t, s, agentConfigToken, "cfg", agentConfigInputs("Be nice."))
	require.Empty(t, resp.Failures, failureText(resp.Failures))
	assert.Equal(t, "16", resp.Inputs.Get("version").AsString())

	// Unknown values skip validation instead of failing.
	inputs := pm(agentConfigInputs("x"))
	agents := inputs.Get("agents").AsArray().AsSlice()
	root := agents[1].AsMap().Set("instruction", property.New(property.Computed))
	agents[1] = property.New(root)
	inputs = inputs.Set("agents", property.New(agents))
	cr, err := s.Check(p.CheckRequest{Urn: urn(agentConfigToken, "cfg"), Inputs: inputs})
	require.NoError(t, err)
	assert.Empty(t, cr.Failures)
}

func TestAgentConfigLifecycle(t *testing.T) {
	t.Parallel()
	s := server(t)
	integration.LifeCycleTest{
		Resource: agentConfigToken,
		Create: integration.Operation{
			Inputs: pm(agentConfigInputs("You are helpful.")),
			Hook: func(_, out property.Map) {
				y := out.Get("yaml").AsString()
				assert.True(t, strings.HasPrefix(y, "version: \"16\"\nmetadata:\n"), y)
				assert.Less(t, strings.Index(y, "  helper:"), strings.Index(y, "  root:"), "agent order is preserved")
				assert.Contains(t, y, "sub_agents:")
				assert.Contains(t, y, "temperature: 0.2")
				assert.Len(t, out.Get("sha256").AsString(), 64)
				assert.Equal(t, "root", out.Get("defaultAgent").AsString())
				assert.Equal(t, DockerAgentVersion, out.Get("dockerAgentVersion").AsString())
				var env []string
				for _, v := range out.Get("requiredEnvVars").AsArray().All {
					env = append(env, v.AsString())
				}
				assert.Equal(t, []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY"}, env)
				names := out.Get("agentNames").AsArray()
				assert.Equal(t, "helper", names.Get(0).AsString())
				assert.Equal(t, "root", names.Get(1).AsString())
			},
		},
		Updates: []integration.Operation{{
			Inputs: pm(agentConfigInputs("You are very helpful.")),
			Hook: func(_, out property.Map) {
				assert.Contains(t, out.Get("yaml").AsString(), "instruction: You are very helpful.")
			},
		}},
	}.Run(t, s)
}

func TestAgentConfigDiff(t *testing.T) {
	t.Parallel()
	s := server(t)
	u := urn(agentConfigToken, "cfg")
	first := check(t, s, agentConfigToken, "cfg", agentConfigInputs("Hi."))
	require.Empty(t, first.Failures)
	created, err := s.Create(p.CreateRequest{Urn: u, Properties: first.Inputs})
	require.NoError(t, err)

	// Same inputs: no changes.
	again := check(t, s, agentConfigToken, "cfg", agentConfigInputs("Hi."))
	diff, err := s.Diff(p.DiffRequest{ID: created.ID, Urn: u, State: created.Properties, Inputs: again.Inputs, OldInputs: first.Inputs})
	require.NoError(t, err)
	assert.False(t, diff.HasChanges, "%+v", diff.DetailedDiff)

	// A different instruction is an in-place update of `agents`.
	changed := check(t, s, agentConfigToken, "cfg", agentConfigInputs("Hello."))
	diff, err = s.Diff(p.DiffRequest{ID: created.ID, Urn: u, State: created.Properties, Inputs: changed.Inputs, OldInputs: first.Inputs})
	require.NoError(t, err)
	assert.True(t, diff.HasChanges)
	assert.Equal(t, p.Update, diff.DetailedDiff["agents"].Kind)

	// The config version is diffed too (pulumi-go-provider's default diff would ignore it).
	v15 := agentConfigInputs("Hi.")
	v15["version"] = "15"
	versioned := check(t, s, agentConfigToken, "cfg", v15)
	diff, err = s.Diff(p.DiffRequest{ID: created.ID, Urn: u, State: created.Properties, Inputs: versioned.Inputs, OldInputs: first.Inputs})
	require.NoError(t, err)
	assert.True(t, diff.HasChanges)
	assert.Contains(t, diff.DetailedDiff, "version")

	// Refresh keeps the state as is.
	read, err := s.Read(p.ReadRequest{ID: created.ID, Urn: u, Properties: created.Properties, Inputs: first.Inputs})
	require.NoError(t, err)
	assert.Equal(t, created.Properties.Get("yaml"), read.Properties.Get("yaml"))
}

func TestAgentConfigSecretInputsKeepYAMLSecret(t *testing.T) {
	t.Parallel()
	s := server(t)
	inputs := pm(map[string]any{"agents": []any{map[string]any{
		"name":  "root",
		"model": "openai/gpt-5-mini",
		"toolsets": []any{map[string]any{
			"type":    "fetch",
			"headers": map[string]any{"Authorization": "Bearer abc"},
		}},
	}}})
	agents := inputs.Get("agents").AsArray().AsSlice()
	ts := agents[0].AsMap().Get("toolsets").AsArray().AsSlice()
	ts[0] = property.New(ts[0].AsMap().Set("headers", ts[0].AsMap().Get("headers").WithSecret(true)))
	agents[0] = property.New(agents[0].AsMap().Set("toolsets", property.New(ts)))
	inputs = inputs.Set("agents", property.New(agents))

	resp, err := s.Check(p.CheckRequest{Urn: urn(agentConfigToken, "s"), Inputs: inputs})
	require.NoError(t, err)
	require.Empty(t, resp.Failures, failureText(resp.Failures))
	created, err := s.Create(p.CreateRequest{Urn: urn(agentConfigToken, "s"), Properties: resp.Inputs})
	require.NoError(t, err)
	assert.True(t, created.Properties.Get("yaml").Secret(), "yaml must be secret when an input is secret")
}

func artifactInputs(ref, yaml string) map[string]any {
	return map[string]any{"reference": ref, "yaml": yaml}
}

const artifactYAML = `version: "16"
metadata:
  author: pulumi
agents:
  root:
    model: openai/gpt-5-mini
    instruction: You are helpful.
`

func TestAgentArtifactCheckFailures(t *testing.T) {
	t.Parallel()
	s := server(t)
	resp := check(t, s, agentArtifactToken, "a", map[string]any{
		"reference":   "localhost:5000/agents/x@sha256:" + strings.Repeat("a", 64),
		"yaml":        "agents:\n  root:\n    model: nope\n",
		"annotations": map[string]any{"io.docker.agent.version": "v0"},
	})
	text := failureText(resp.Failures)
	assert.Contains(t, text, "reference: ")
	assert.Contains(t, text, "must be a tag")
	assert.Contains(t, text, "annotations: ")
	assert.Contains(t, text, "non-existent model 'nope'")

	ok := check(t, s, agentArtifactToken, "a", artifactInputs("localhost:5000/agents/x", artifactYAML))
	assert.Empty(t, ok.Failures, failureText(ok.Failures))
	assert.Equal(t, "retain", ok.Inputs.Get("deleteBehavior").AsString())
}

func TestAgentArtifactLifecycle(t *testing.T) {
	t.Parallel()
	s := server(t)
	host := newRegistry(t, nil)
	ref := host + "/agents/demo:v1"
	u := urn(agentArtifactToken, "art")

	// Preview and create.
	first := check(t, s, agentArtifactToken, "art", artifactInputs(ref, artifactYAML))
	require.Empty(t, first.Failures, failureText(first.Failures))
	preview, err := s.Create(p.CreateRequest{Urn: u, Properties: first.Inputs, DryRun: true})
	require.NoError(t, err)
	assert.True(t, preview.Properties.Get("digest").IsComputed())

	created, err := s.Create(p.CreateRequest{Urn: u, Properties: first.Inputs})
	require.NoError(t, err)
	assert.Equal(t, ref, created.ID)
	state := created.Properties
	digest := state.Get("digest").AsString()
	assert.True(t, strings.HasPrefix(digest, "sha256:"))
	assert.Equal(t, host+"/agents/demo@"+digest, state.Get("pinnedReference").AsString())
	assert.Equal(t, ociagent.FormatCreated(mustParseTime(t, state.Get("created").AsString())), state.Get("created").AsString())
	ann := state.Get("manifestAnnotations").AsMap()
	assert.Equal(t, DockerAgentVersion, ann.Get("io.docker.agent.version").AsString())
	assert.Equal(t, "pulumi", ann.Get("org.opencontainers.image.authors").AsString())

	pulled, err := ociagent.Pull(t.Context(), ref, ociagent.Options{})
	require.NoError(t, err)
	assert.Equal(t, artifactYAML, string(pulled.YAML))
	assert.Equal(t, digest, pulled.Digest)

	// Same inputs: no diff, so a second `pulumi up` does nothing.
	same := check(t, s, agentArtifactToken, "art", artifactInputs(ref, artifactYAML))
	diff, err := s.Diff(p.DiffRequest{ID: created.ID, Urn: u, State: state, Inputs: same.Inputs, OldInputs: first.Inputs})
	require.NoError(t, err)
	assert.False(t, diff.HasChanges, "%+v", diff.DetailedDiff)

	// Refresh: the tag still points at our digest, nothing changes.
	read, err := s.Read(p.ReadRequest{ID: created.ID, Urn: u, Properties: state, Inputs: first.Inputs})
	require.NoError(t, err)
	assert.Equal(t, created.ID, read.ID)
	assert.Equal(t, digest, read.Properties.Get("digest").AsString())
	assert.Equal(t, state.Get("created"), read.Properties.Get("created"))

	// deleteBehavior only: an update that does not push.
	withDelete := artifactInputs(ref, artifactYAML)
	withDelete["deleteBehavior"] = "delete"
	del := check(t, s, agentArtifactToken, "art", withDelete)
	diff, err = s.Diff(p.DiffRequest{ID: created.ID, Urn: u, State: state, Inputs: del.Inputs, OldInputs: first.Inputs})
	require.NoError(t, err)
	assert.True(t, diff.HasChanges)
	assert.Equal(t, p.Update, diff.DetailedDiff["deleteBehavior"].Kind)
	upd, err := s.Update(p.UpdateRequest{ID: created.ID, Urn: u, State: state, Inputs: del.Inputs, OldInputs: first.Inputs})
	require.NoError(t, err)
	assert.Equal(t, digest, upd.Properties.Get("digest").AsString(), "no re-push")
	assert.Equal(t, "delete", upd.Properties.Get("deleteBehavior").AsString())
	state = upd.Properties

	// New YAML: update pushes again to the same tag.
	newYAML := strings.Replace(artifactYAML, "You are helpful.", "You are very helpful.", 1)
	changedInputs := artifactInputs(ref, newYAML)
	changedInputs["deleteBehavior"] = "delete"
	changed := check(t, s, agentArtifactToken, "art", changedInputs)
	diff, err = s.Diff(p.DiffRequest{ID: created.ID, Urn: u, State: state, Inputs: changed.Inputs, OldInputs: del.Inputs})
	require.NoError(t, err)
	assert.Equal(t, p.Update, diff.DetailedDiff["yaml"].Kind)
	upd, err = s.Update(p.UpdateRequest{ID: created.ID, Urn: u, State: state, Inputs: changed.Inputs, OldInputs: del.Inputs})
	require.NoError(t, err)
	newDigest := upd.Properties.Get("digest").AsString()
	assert.NotEqual(t, digest, newDigest)
	state = upd.Properties
	pulled, err = ociagent.Pull(t.Context(), ref, ociagent.Options{})
	require.NoError(t, err)
	assert.Equal(t, newYAML, string(pulled.YAML))

	// A new reference replaces the resource.
	moved := artifactInputs(host+"/agents/demo:v2", newYAML)
	movedCheck := check(t, s, agentArtifactToken, "art", moved)
	diff, err = s.Diff(p.DiffRequest{ID: created.ID, Urn: u, State: state, Inputs: movedCheck.Inputs, OldInputs: changed.Inputs})
	require.NoError(t, err)
	assert.Equal(t, p.UpdateReplace, diff.DetailedDiff["reference"].Kind)

	// Drift: someone pushes different content to the tag. Refresh picks it up, so the
	// next diff re-pushes the configured YAML.
	other, err := ociagent.Build([]byte(artifactYAML), ociagent.Annotations([]byte(artifactYAML), "2020-01-01T00:00:00Z", nil))
	require.NoError(t, err)
	_, err = ociagent.Push(t.Context(), ref, other, ociagent.Options{})
	require.NoError(t, err)
	read, err = s.Read(p.ReadRequest{ID: created.ID, Urn: u, Properties: state, Inputs: changed.Inputs})
	require.NoError(t, err)
	assert.Equal(t, other.Digest.String(), read.Properties.Get("digest").AsString())
	assert.Equal(t, artifactYAML, read.Properties.Get("yaml").AsString())
	assert.Equal(t, "2020-01-01T00:00:00Z", read.Properties.Get("created").AsString())
	diff, err = s.Diff(p.DiffRequest{ID: created.ID, Urn: u, State: read.Properties, Inputs: changed.Inputs, OldInputs: read.Inputs})
	require.NoError(t, err)
	assert.True(t, diff.HasChanges)
	assert.Contains(t, diff.DetailedDiff, "yaml")
	upd, err = s.Update(p.UpdateRequest{ID: created.ID, Urn: u, State: read.Properties, Inputs: changed.Inputs, OldInputs: read.Inputs})
	require.NoError(t, err)
	state = upd.Properties

	// Delete with deleteBehavior=delete removes the manifest.
	require.NoError(t, s.Delete(p.DeleteRequest{ID: created.ID, Urn: u, Properties: state}))
	_, found, err := ociagent.Head(t.Context(), state.Get("pinnedReference").AsString(), ociagent.Options{})
	require.NoError(t, err)
	assert.False(t, found)

	// Refresh of a missing tag reports the resource as gone. (The in-process registry keeps
	// tags when a manifest is deleted by digest, so use a tag that never existed.)
	missing := state.Set("reference", property.New(host+"/agents/missing:v1"))
	gone, err := s.Read(p.ReadRequest{ID: created.ID, Urn: u, Properties: missing, Inputs: changed.Inputs})
	require.NoError(t, err)
	assert.Empty(t, gone.ID)
}

func TestAgentArtifactRetainAndImport(t *testing.T) {
	t.Parallel()
	s := server(t)
	host := newRegistry(t, nil)
	ref := host + "/agents/keep:v1"
	u := urn(agentArtifactToken, "keep")

	in := artifactInputs(ref, artifactYAML)
	in["annotations"] = map[string]any{"org.opencontainers.image.source": "https://example.com/repo"}
	c := check(t, s, agentArtifactToken, "keep", in)
	require.Empty(t, c.Failures, failureText(c.Failures))
	created, err := s.Create(p.CreateRequest{Urn: u, Properties: c.Inputs})
	require.NoError(t, err)

	// Default deleteBehavior=retain leaves the artifact in place.
	require.NoError(t, s.Delete(p.DeleteRequest{ID: created.ID, Urn: u, Properties: created.Properties}))
	_, found, err := ociagent.Head(t.Context(), ref, ociagent.Options{})
	require.NoError(t, err)
	assert.True(t, found)

	// Import by reference.
	imported, err := s.Read(p.ReadRequest{ID: ref, Urn: u})
	require.NoError(t, err)
	assert.Equal(t, ref, imported.ID)
	assert.Equal(t, artifactYAML, imported.Inputs.Get("yaml").AsString())
	assert.Equal(t, ref, imported.Inputs.Get("reference").AsString())
	assert.Equal(t, "https://example.com/repo",
		imported.Inputs.Get("annotations").AsMap().Get("org.opencontainers.image.source").AsString())
	assert.Equal(t, created.Properties.Get("digest"), imported.Properties.Get("digest"))
}

func TestAgentArtifactRegistryAuthConfig(t *testing.T) {
	t.Parallel()
	s := server(t)
	host := newRegistry(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if u, pw, ok := r.BasicAuth(); !ok || u != "bob" || pw != "hunter2" {
				w.Header().Set("WWW-Authenticate", `Basic realm="test"`)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	require.NoError(t, s.Configure(p.ConfigureRequest{Args: pm(map[string]any{
		"registryAuth": []any{map[string]any{"address": host, "username": "bob", "password": "hunter2"}},
	})}))

	ref := host + "/private/agent:v1"
	c := check(t, s, agentArtifactToken, "priv", artifactInputs(ref, artifactYAML))
	require.Empty(t, c.Failures, failureText(c.Failures))
	created, err := s.Create(p.CreateRequest{Urn: urn(agentArtifactToken, "priv"), Properties: c.Inputs})
	require.NoError(t, err)

	inv, err := s.Invoke(p.InvokeRequest{Token: "docker-agent:index:getAgent", Args: pm(map[string]any{"reference": ref})})
	require.NoError(t, err)
	require.Empty(t, inv.Failures)
	assert.Equal(t, created.Properties.Get("digest"), inv.Return.Get("digest"))
}

func TestGetAgent(t *testing.T) {
	t.Parallel()
	s := server(t)
	host := newRegistry(t, nil)
	ref := host + "/agents/team:v1"
	doc := []byte(`version: "16"
metadata:
  author: pulumi
  description: A team
  tags: [a, b]
agents:
  zed:
    model: openai/gpt-5-mini
  root:
    model: openai/gpt-5-mini
`)
	a, err := ociagent.Build(doc, ociagent.Annotations(doc, "2026-01-01T00:00:00Z", map[string]string{"org.opencontainers.image.revision": "9.9.9"}))
	require.NoError(t, err)
	res, err := ociagent.Push(t.Context(), ref, a, ociagent.Options{})
	require.NoError(t, err)

	for _, r := range []string{ref, res.PinnedReference} {
		inv, err := s.Invoke(p.InvokeRequest{Token: "docker-agent:index:getAgent", Args: pm(map[string]any{"reference": r})})
		require.NoError(t, err, r)
		require.Empty(t, inv.Failures)
		out := inv.Return
		assert.Equal(t, string(doc), out.Get("yaml").AsString())
		assert.Equal(t, res.Digest, out.Get("digest").AsString())
		assert.Equal(t, res.PinnedReference, out.Get("pinnedReference").AsString())
		assert.Equal(t, "root", out.Get("defaultAgent").AsString())
		assert.Equal(t, "zed", out.Get("agentNames").AsArray().Get(0).AsString())
		md := out.Get("metadata").AsMap()
		assert.Equal(t, "pulumi", md.Get("author").AsString())
		assert.Equal(t, "A team", md.Get("description").AsString())
		assert.Equal(t, "9.9.9", md.Get("version").AsString(), "filled from the revision annotation")
		assert.Equal(t, 2, md.Get("tags").AsArray().Len())
		assert.Equal(t, DockerAgentVersion, out.Get("annotations").AsMap().Get("io.docker.agent.version").AsString())
	}

	_, err = s.Invoke(p.InvokeRequest{Token: "docker-agent:index:getAgent", Args: pm(map[string]any{"reference": host + "/agents/none:v1"})})
	require.Error(t, err)
}

func TestValidateAgentConfigFunction(t *testing.T) {
	t.Parallel()
	s := server(t)
	inv, err := s.Invoke(p.InvokeRequest{Token: "docker-agent:index:validateAgentConfig", Args: pm(map[string]any{"yaml": artifactYAML})})
	require.NoError(t, err)
	assert.True(t, inv.Return.Get("valid").AsBool())
	assert.Equal(t, 0, inv.Return.Get("errors").AsArray().Len())
	assert.Equal(t, "root", inv.Return.Get("defaultAgent").AsString())
	assert.Equal(t, "OPENAI_API_KEY", inv.Return.Get("requiredEnvVars").AsArray().Get(0).AsString())

	inv, err = s.Invoke(p.InvokeRequest{Token: "docker-agent:index:validateAgentConfig", Args: pm(map[string]any{"yaml": "agents: {root: {model: x}}"})})
	require.NoError(t, err, "invalid YAML must not fail the invoke")
	assert.False(t, inv.Return.Get("valid").AsBool())
	assert.Contains(t, inv.Return.Get("errors").AsArray().Get(0).AsString(), "non-existent model 'x'")

	inv, err = s.Invoke(p.InvokeRequest{Token: "docker-agent:index:validateAgentConfig", Args: pm(map[string]any{"yaml": "::: not yaml"})})
	require.NoError(t, err)
	assert.False(t, inv.Return.Get("valid").AsBool())
}

func mustParseTime(t *testing.T, s string) time.Time {
	t.Helper()
	tm, err := time.Parse(time.RFC3339, s)
	require.NoError(t, err)
	return tm
}
