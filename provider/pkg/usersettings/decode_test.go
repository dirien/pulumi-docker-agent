// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package usersettings

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// level is a test type with a scalar YAML form, like a thinking budget.
type level struct {
	Name   *string `pulumi:"name,optional"   yaml:"name"`
	Number *int    `pulumi:"number,optional" yaml:"number"`
}

func (l level) YAMLValue() (any, error) {
	if l.Name != nil {
		return *l.Name, nil
	}
	return *l.Number, nil
}

func (l *level) SetYAMLValue(v any) (bool, error) {
	if s, ok := v.(string); ok {
		*l = level{Name: &s}
		return true, nil
	}
	n, ok := IntValue(v)
	if ok {
		*l = level{Number: ptr(int(n))}
	}
	return ok, nil
}

type decodedInner struct {
	Name  string         `pulumi:"name"           yaml:"name"`
	Score *float64       `pulumi:"score,optional" yaml:"score"`
	Opts  map[string]any `pulumi:"opts,optional"  yaml:"opts"`
}

type decoded struct {
	Flag   *bool                   `pulumi:"flag,optional"   yaml:"flag"`
	Count  *int                    `pulumi:"count,optional"  yaml:"count"`
	Items  []decodedInner          `pulumi:"items,optional"  yaml:"items"`
	ByName map[string]decodedInner `pulumi:"byName,optional" yaml:"by_name"`
	Levels map[string]level        `pulumi:"levels,optional" yaml:"levels"`
	Env    map[string]string       `pulumi:"env,optional"    yaml:"env"`
	Path   *string                 `pulumi:"path,optional"   yaml:"-"`
	Extra  map[string]any          `pulumi:"extra,optional"  yaml:",inline"`
}

// TestDecodeInvertsEncode decodes the generic document Encode produced back into the typed
// struct.
func TestDecodeInvertsEncode(t *testing.T) {
	t.Parallel()
	in := decoded{
		Flag:   ptr(false),
		Count:  ptr(0),
		Items:  []decodedInner{{Name: "a", Score: ptr(2.0), Opts: map[string]any{"size": int64(32768)}}},
		ByName: map[string]decodedInner{"x": {Name: "x", Score: ptr(0.5)}},
		Levels: map[string]level{"effort": {Name: ptr("high")}, "tokens": {Number: ptr(2048)}},
		Env:    map[string]string{"A": "1"},
		Extra:  map[string]any{"future": int64(3), "nested": map[string]any{"n": "x"}},
	}
	doc, err := Encode(in)
	require.NoError(t, err)
	var out decoded
	require.NoError(t, Decode(doc, &out))
	assert.Equal(t, in, out)
}

func TestDecodeErrors(t *testing.T) {
	t.Parallel()
	for want, doc := range map[string]map[string]any{
		"items[0].unknown has no typed input": {"items": []any{map[string]any{"name": "a", "unknown": 1}}},
		"count: expected an integer":          {"count": 1.5},
		"flag: expected a boolean":            {"flag": "yes"},
		"items: expected a list":              {"items": "a"},
		"by_name.x: expected a mapping":       {"by_name": map[string]any{"x": "a"}},
	} {
		var out decoded
		err := Decode(doc, &out)
		require.Error(t, err, want)
		assert.Contains(t, err.Error(), want)
	}
	assert.ErrorContains(t, Decode(map[string]any{}, decoded{}), "needs a pointer to a struct")
}

// TestCanonical checks that Canonical reads a file the way docker-agent does and returns
// what docker-agent writes for it: comments dropped, shorthands expanded, unknown keys kept
// where docker-agent keeps them, and the version set.
func TestCanonical(t *testing.T) {
	t.Parallel()
	doc, canonical, err := Canonical([]byte(`# comment
version: v0
default_model: openai/gpt-5-mini
settings:
  hooks:
    session_end: {type: command, command: ./bye.sh}
  future_setting: true
aliases:
  a: {path: a.yaml, ignored_key: 1}
future_top: {x: 1}
`))
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(canonical, "version: v1\n"), canonical)
	assert.NotContains(t, canonical, "# comment")
	assert.NotContains(t, canonical, "ignored_key", "docker-agent drops unknown alias keys")
	assert.NotContains(t, doc, "version")
	assert.Equal(t, "openai/gpt-5-mini", doc["default_model"])
	settings := doc["settings"].(map[string]any)
	assert.Equal(t, true, settings["future_setting"])
	hooks := settings["hooks"].(map[string]any)["session_end"].([]any)
	assert.Equal(t, "./bye.sh", hooks[0].(map[string]any)["command"])
	assert.Contains(t, doc, "future_top")

	_, out, err := Render(doc)
	require.NoError(t, err)
	assert.Equal(t, canonical, out, "rendering the canonical document gives the canonical file")

	_, _, err = Canonical([]byte("settings: [1, 2\n"))
	require.Error(t, err)
	assert.NotRegexp(t, `\[\d+:\d+\]`, err.Error())
}
