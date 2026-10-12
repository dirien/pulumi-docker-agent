// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package render

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIndexYAML checks that YAML locations of a rendered document resolve to the inputs that
// produced them.
func TestIndexYAML(t *testing.T) {
	t.Parallel()
	doc := Document{
		Models: map[string]ModelDef{"claude": {Provider: ptr("anthropic"), Model: ptr("c"), MaxTokens: ptr(10)}},
		Agents: []Agent{
			{Name: "helper", Model: ptr("openai/gpt-5")},
			{
				Name: "root", Model: ptr("claude"), SubAgents: []string{"helper"},
				Toolsets: []Toolset{{Type: "think"}, {Type: "fetch"}, {Type: "shell", Ref: ptr("foo")}},
				Extra:    map[string]any{"tolls": []any{"x"}},
			},
		},
	}
	y, err := Render(doc)
	require.NoError(t, err)
	ix, err := IndexYAML([]byte(y))
	require.NoError(t, err)

	for yamlPath, want := range map[string]Path{
		"agents.root.sub_agents":     "agents[1].subAgents",
		"agents.root.toolsets.2.ref": "agents[1].toolsets[2].ref",
		"agents.root.toolsets.1":     "agents[1].toolsets[1]",
		"agents.helper":              "agents[0]",
		"models.claude.max_tokens":   `models["claude"].maxTokens`,
		"agents.root.tolls":          `agents[1].extra["tolls"]`,
		// Deeper than the document: the closest input.
		"agents.root.toolsets.2.ref.nothing": "agents[1].toolsets[2].ref",
		"agents.root.missing":                "agents[1]",
		"version":                            "version",
	} {
		got, ok := ix.Lookup(strings.Split(yamlPath, "."))
		assert.True(t, ok, yamlPath)
		assert.Equal(t, want, got, yamlPath)
	}
	_, ok := ix.Lookup([]string{"nothing"})
	assert.False(t, ok)
	_, ok = (*PathIndex)(nil).Lookup([]string{"agents"})
	assert.False(t, ok)
}

// TestInputPath checks that rendering errors, which name agents by key, map to input paths.
func TestInputPath(t *testing.T) {
	t.Parallel()
	doc := Document{
		Models: map[string]ModelDef{"m": {Provider: ptr("openai"), Model: ptr("gpt-5"),
			ThinkingBudget: ptr("low"), ThinkingBudgetTokens: ptr(10)}},
		Agents: []Agent{{Name: "helper", Model: ptr("m")}, {Name: "root", Model: ptr("m")}},
	}
	_, err := Render(doc)
	require.Error(t, err)
	p, ok := InputPath(err, doc)
	require.True(t, ok)
	assert.Equal(t, Path(`models["m"]`), p, err.Error())

	doc.Models = nil
	doc.Agents[1].Toolsets = []Toolset{{Type: "think"}, {Type: "shell", Defer: ptr(true), DeferTools: []string{"x"}}}
	_, err = Render(doc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `agents["root"].toolsets[1]`)
	p, ok = InputPath(err, doc)
	require.True(t, ok)
	assert.Equal(t, Path("agents[1].toolsets[1]"), p, err.Error())

	_, ok = InputPath(assert.AnError, doc)
	assert.False(t, ok)
}

func TestSplitPath(t *testing.T) {
	t.Parallel()
	segs, ok := splitPath(`agents["a.b\"c"].toolsets[12].x`)
	require.True(t, ok)
	assert.Equal(t, []pathSegment{
		{name: "agents"}, {name: `a.b"c`, isKey: true}, {name: "toolsets"}, {index: 12, isIndex: true}, {name: "x"},
	}, segs)
	for _, bad := range []string{`a[`, `a["x`, `a[x]`, `a..b`} {
		_, ok := splitPath(bad)
		assert.False(t, ok, bad)
	}
}
