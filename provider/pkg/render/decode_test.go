// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package render

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPath(t *testing.T) {
	t.Parallel()
	assert.Equal(t, Path("agents[0].toolsets[1].type"), Path("").Field("agents").Index(0).Field("toolsets").Index(1).Field("type"))
	assert.Equal(t, Path(`models["a.b"].maxTokens`), Path("models").Key("a.b").Field("maxTokens"))
}

// TestDecodeYAMLComments checks where DecodeYAML puts the comments of a document: by the path
// of the input they describe, with the comments of folded shorthands on the folded value.
func TestDecodeYAMLComments(t *testing.T) {
	t.Parallel()
	dec, err := DecodeYAML([]byte(`# The document.

# The models.
models:
  fast:
    provider: openai # line
    model: gpt-5-mini
agents:
  root:
    model: fast
    instruction:
      # first part
      - Be brief.
      - Be kind. # second part
    toolsets:
      # A toolset.
      - type: shell
    commands:
      - fix: Fix it. # a command
    foo: bar # not typed
# The end.
`))
	require.NoError(t, err)
	assert.Equal(t, []string{"# The document."}, dec.Head)
	assert.Equal(t, []string{"# The models."}, dec.Comments["models"].Head)
	assert.Equal(t, []string{"# line"}, dec.Comments[`models["fast"].provider`].Line)
	assert.Equal(t, []string{"# first part"}, dec.Comments["agents[0].instruction"].Head)
	assert.Equal(t, []string{"# second part"}, dec.Comments["agents[0].instruction"].Line)
	assert.Equal(t, []string{"# A toolset."}, dec.Comments["agents[0].toolsets[0]"].Head)
	assert.Equal(t, []string{"# a command"}, dec.Comments[`agents[0].simpleCommands["fix"]`].Line)
	assert.Equal(t, []string{"# not typed"}, dec.Comments[`agents[0].extra["foo"]`].Line)
	assert.Equal(t, []Path{`agents[0].extra["foo"]`}, dec.Untyped)
	// yaml.v3 gives a comment after the last nested key to the enclosing top-level key.
	assert.Equal(t, []string{"# The end."}, dec.Comments["agents"].Foot)

	assert.Equal(t, []string{"models", "agents"}, dec.Order[""])
	assert.Equal(t, []string{"provider", "model"}, dec.Order[`models["fast"]`])
	assert.Equal(t, []string{"model", "instruction", "toolsets", "simpleCommands", "commands", "extra"}, dec.Order["agents[0]"])
	assert.Equal(t, "Be brief.\n\nBe kind.", *dec.Document.Agents[0].Instruction)
}

// TestDecodeYAMLAnchors checks that anchors, aliases and merge keys are recorded by path.
func TestDecodeYAMLAnchors(t *testing.T) {
	t.Parallel()
	dec, err := DecodeYAML([]byte(`agents:
  root:
    model: m
    instruction: &rules Follow the rules.
  a: &base
    model: m
    instruction: *rules # reused
  b:
    <<: *base
    description: B
`))
	require.NoError(t, err)
	assert.Equal(t, []Anchor{{Name: "rules", Path: "agents[0].instruction"}, {Name: "base", Path: "agents[1]"}}, dec.Anchors)
	assert.Equal(t, map[Path]string{
		"agents[0].instruction": "rules", "agents[1].instruction": "rules", "agents[1]": "base",
		"agents[2].instruction": "rules", // merged from base
	}, dec.Aliases)
	assert.Equal(t, map[Path][]string{"agents[2]": {"base"}}, dec.Merges)
	// Merged keys are not part of the key order; the values are expanded.
	assert.Equal(t, []string{"description"}, dec.Order["agents[2]"])
	assert.Equal(t, "Follow the rules.", *dec.Document.Agents[2].Instruction)
	assert.Equal(t, []string{"# reused"}, dec.Comments["agents[1].instruction"].Line)
}

func TestUnreadKey(t *testing.T) {
	t.Parallel()
	reason, ok := UnreadKey(`rag["kb"].strategies[1].extra["batch_size"]`)
	assert.True(t, ok)
	assert.Contains(t, reason, "embedding_batch_size")
	_, ok = UnreadKey(`rag["kb"].strategies[1].extra["other"]`)
	assert.False(t, ok)
	_, ok = UnreadKey(`agents[0].extra["batch_size"]`)
	assert.False(t, ok)
	assert.Equal(t, []string{"batch_size"}, UnreadKeyNames())
}
