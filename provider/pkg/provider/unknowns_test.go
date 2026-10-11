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
)

// Regression tests for withUnknownInputs (unknowns.go): inputs that depend on other
// resources' outputs are unknown during previews, and every resource must preview them
// cleanly instead of failing with "Missing required field" or previewing made-up values.

var unknown = property.New(property.Computed)

// previewCreate runs Check and a preview Create on raw inputs, the way the engine does,
// and returns the checked inputs and the previewed outputs.
func previewCreate(t *testing.T, s integration.Server, typ string, inputs property.Map) (checked, out property.Map) {
	t.Helper()
	cr, err := s.Check(p.CheckRequest{Urn: urn(typ, "u"), Inputs: inputs})
	require.NoError(t, err)
	require.Empty(t, cr.Failures, failureText(cr.Failures))
	created, err := s.Create(p.CreateRequest{Urn: urn(typ, "u"), Properties: cr.Inputs, DryRun: true})
	require.NoError(t, err, "preview of %s with unknown inputs", typ)
	return cr.Inputs, created.Properties
}

func TestUnknownInputsSurviveCheckAgentConfig(t *testing.T) {
	t.Parallel()
	s := server(t)

	t.Run("fully unknown agents", func(t *testing.T) {
		t.Parallel()
		inputs := pm(map[string]any{"metadata": map[string]any{"author": "me"}}).Set("agents", unknown)
		checked, out := previewCreate(t, s, agentConfigToken, inputs)
		assert.True(t, checked.Get("agents").IsComputed())
		for _, k := range []string{"yaml", "sha256", "agentNames", "defaultAgent", "requiredEnvVars"} {
			assert.True(t, out.Get(k).IsComputed(), "%s must be unknown", k)
		}
	})

	t.Run("unknown nested toolset field", func(t *testing.T) {
		t.Parallel()
		inputs := pm(map[string]any{"agents": []any{map[string]any{
			"name": "root", "model": "openai/gpt-5-mini",
			"toolsets": []any{map[string]any{"type": "think"}, map[string]any{"type": "mcp"}},
		}}})
		agents := inputs.Get("agents").AsArray().AsSlice()
		toolsets := agents[0].AsMap().Get("toolsets").AsArray().AsSlice()
		toolsets[1] = property.New(toolsets[1].AsMap().Set("type", unknown).Set("ref", unknown))
		agents[0] = property.New(agents[0].AsMap().Set("toolsets", property.New(toolsets)))
		inputs = inputs.Set("agents", property.New(agents))

		checked, out := previewCreate(t, s, agentConfigToken, inputs)
		ts := checked.Get("agents").AsArray().Get(0).AsMap().Get("toolsets").AsArray()
		assert.Equal(t, "think", ts.Get(0).AsMap().Get("type").AsString())
		assert.True(t, ts.Get(1).AsMap().Get("type").IsComputed(), "a toolset type must not turn into \"\"")
		assert.True(t, ts.Get(1).AsMap().Get("ref").IsComputed())
		assert.True(t, out.Get("yaml").IsComputed())
		assert.Equal(t, "root", out.Get("defaultAgent").AsString())
	})

	t.Run("unknown agent name", func(t *testing.T) {
		t.Parallel()
		inputs := pm(map[string]any{"agents": []any{map[string]any{"name": "x", "model": "openai/gpt-5-mini"}}})
		agents := inputs.Get("agents").AsArray().AsSlice()
		agents[0] = property.New(agents[0].AsMap().Set("name", unknown))
		inputs = inputs.Set("agents", property.New(agents))
		checked, out := previewCreate(t, s, agentConfigToken, inputs)
		assert.True(t, checked.Get("agents").AsArray().Get(0).AsMap().Get("name").IsComputed())
		assert.True(t, out.Get("agentNames").IsComputed())
	})
}

func TestUnknownInputsSurviveCheckAgentArtifact(t *testing.T) {
	t.Parallel()
	s := server(t)
	inputs := pm(map[string]any{"reference": "localhost:5000/agents/x:1"}).Set("yaml", unknown)
	checked, out := previewCreate(t, s, agentArtifactToken, inputs)
	assert.True(t, checked.Get("yaml").IsComputed())
	for _, k := range []string{"digest", "pinnedReference", "contentSha256"} {
		assert.True(t, out.Get(k).IsComputed(), "%s must be unknown", k)
	}

	// An unknown reference works too.
	inputs = pm(map[string]any{"yaml": artifactYAML}).Set("reference", unknown)
	checked, _ = previewCreate(t, s, agentArtifactToken, inputs)
	assert.True(t, checked.Get("reference").IsComputed())
}

func TestUnknownInputsSurviveCheckLocalFiles(t *testing.T) {
	t.Parallel()
	s := server(t)

	inputs := property.NewMap(map[string]property.Value{
		"aliases": property.New(map[string]property.Value{"a": property.New(map[string]property.Value{"path": unknown})}),
		"settings": property.New(map[string]property.Value{"keybindings": property.New([]property.Value{
			property.New(map[string]property.Value{"action": unknown, "keys": unknown}),
		})}),
	})
	checked, out := previewCreate(t, s, userSettingsToken, inputs)
	kb := checked.Get("settings").AsMap().Get("keybindings").AsArray().Get(0).AsMap()
	assert.True(t, kb.Get("action").IsComputed())
	assert.True(t, kb.Get("keys").IsComputed())
	assert.True(t, out.Get("yaml").IsComputed())

	checked, out = previewCreate(t, s, agentsIgnoreToken,
		pm(map[string]any{"directory": t.TempDir()}).Set("patterns", unknown))
	assert.True(t, checked.Get("patterns").IsComputed())
	assert.True(t, out.Get("content").IsComputed())
	assert.False(t, out.Get("filePath").IsComputed())
}

func TestRestoreUnknowns(t *testing.T) {
	t.Parallel()
	raw := property.New(map[string]property.Value{
		"list":    property.New([]property.Value{property.New("a"), unknown.WithSecret(true)}),
		"obj":     property.New(map[string]property.Value{"x": unknown}),
		"dropped": property.New(map[string]property.Value{"y": unknown, "z": property.New("z")}),
		"kept":    property.New("v"),
	})
	checked := property.New(map[string]property.Value{
		"list": property.New([]property.Value{property.New("a"), property.New("")}),
		"obj":  property.New(map[string]property.Value{}),
		"kept": property.New("v"),
		"dflt": property.New("command"),
	})
	got := restoreUnknowns(raw, checked).AsMap()
	assert.True(t, got.Get("list").AsArray().Get(1).IsComputed())
	assert.True(t, got.Get("list").AsArray().Get(1).Secret())
	assert.True(t, got.Get("obj").AsMap().Get("x").IsComputed())
	assert.True(t, got.Get("dropped").AsMap().Get("y").IsComputed(), "a dropped value falls back to the request")
	assert.Equal(t, "v", got.Get("kept").AsString())
	assert.Equal(t, "command", got.Get("dflt").AsString(), "defaults applied by Check are kept")

	// Without unknowns, the checked inputs are returned unchanged.
	plain := property.New(map[string]property.Value{"a": property.New("1")})
	assert.True(t, restoreUnknowns(plain, property.New(map[string]property.Value{"a": property.New("2")})).
		AsMap().Get("a").Equals(property.New("2")))
}
