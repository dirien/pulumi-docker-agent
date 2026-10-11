// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package usersettings

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docker/docker-agent/pkg/paths"
	"github.com/docker/docker-agent/pkg/userconfig"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type scalar struct {
	V *string `pulumi:"v,optional" yaml:"v"`
}

func (s scalar) YAMLValue() (any, error) {
	if s.V == nil {
		return nil, errors.New("v is required")
	}
	return "scalar:" + *s.V, nil
}

type inner struct {
	Name  string         `pulumi:"name"           yaml:"name"`
	Score *float64       `pulumi:"score,optional" yaml:"score"`
	Opts  map[string]any `pulumi:"opts,optional"  yaml:"opts"`
}

type outer struct {
	Flag   *bool            `pulumi:"flag,optional"    yaml:"flag"`
	Count  *int             `pulumi:"count,optional"   yaml:"count"`
	Items  []inner          `pulumi:"items,optional"   yaml:"items"`
	ByName map[string]inner `pulumi:"byName,optional"  yaml:"by_name"`
	Budget *scalar          `pulumi:"budget,optional"  yaml:"budget"`
	Unset  *string          `pulumi:"unset,optional"   yaml:"unset"`
	Path   *string          `pulumi:"path,optional"    yaml:"-"`
	Extra  map[string]any   `pulumi:"extra,optional"   yaml:",inline"`
}

func ptr[T any](v T) *T { return &v }

func TestEncode(t *testing.T) {
	t.Parallel()
	got, err := Encode(outer{
		Flag:   ptr(false),
		Count:  ptr(0),
		Items:  []inner{{Name: "a", Score: ptr(2.0), Opts: map[string]any{"size": 32768.0, "ratio": 0.5}}},
		ByName: map[string]inner{"x": {Name: "x"}},
		Budget: &scalar{V: ptr("high")},
		Path:   ptr("/tmp/ignored"),
		Extra:  map[string]any{"future": 3.0, "nested": map[string]any{"n": 1.0}},
	})
	require.NoError(t, err)
	assert.Equal(t, map[string]any{
		"flag":  false,
		"count": int64(0),
		"items": []any{map[string]any{
			"name":  "a",
			"score": 2.0, // typed floats stay floats
			"opts":  map[string]any{"size": int64(32768), "ratio": 0.5},
		}},
		"by_name": map[string]any{"x": map[string]any{"name": "x"}},
		"budget":  "scalar:high",
		"future":  int64(3),
		"nested":  map[string]any{"n": int64(1)},
	}, got)
}

func TestEncodeErrors(t *testing.T) {
	t.Parallel()
	_, err := Encode(outer{Flag: ptr(true), Extra: map[string]any{"flag": false}})
	assert.ErrorContains(t, err, `extra key "flag" is a typed input`)

	// A typed key that is not set is still reserved.
	_, err = Encode(outer{Extra: map[string]any{"count": 1.0}})
	assert.ErrorContains(t, err, `extra key "count" is a typed input`)

	_, err = Encode(outer{Items: []inner{{Name: "a"}}, Budget: &scalar{}})
	assert.ErrorContains(t, err, "budget: v is required")

	type untagged struct {
		A *string `pulumi:"a,optional"`
	}
	_, err = Encode(untagged{A: ptr("x")})
	assert.ErrorContains(t, err, "has no yaml tag")
}

// docsExample is the "Complete Example" of
// https://docs.docker.com/ai/docker-agent/configuration/user-settings/.
const docsExample = `settings:
  theme: auto
  theme_dark: dracula
  theme_light: default-light
  lean: false
  expand_thinking: false
  split_diff_view: true
  render_images: true
  hide_tool_results: false
  sound: true
  sound_threshold: 10
  snapshot: true
  cache_stable_prompts: true
  warn_on_cache_miss: true
  busy_send_mode: queue
  interrupt_confirmation: double-tap
  restore_tabs: true
  tab_title_max_length: 24
  layout:
    sidebar_position: right
    section_spacing: normal
  permissions:
    deny:
      - "shell:cmd=sudo*"
    allow:
      - "read_*"
  hooks:
    session_start:
      - type: command
        command: "~/.config/cagent/hooks/session-start.sh"
  keybindings:
    - action: "commands"
      keys: ["f2", "ctrl+k"]
`

func docsDoc(t *testing.T) map[string]any {
	t.Helper()
	var doc map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(docsExample), &doc))
	return doc
}

func TestRenderDocsExample(t *testing.T) {
	t.Parallel()
	cfg, out, err := Render(docsDoc(t))
	require.NoError(t, err)
	// docker-agent omits false plain booleans (hide_tool_results, lean) but keeps optional ones.
	assert.True(t, strings.HasPrefix(out, "version: v1\nsettings:\n  expand_thinking: false\n"), out)
	assert.NotContains(t, out, "lean:")

	// Semantically identical to docker-agent's own reading of the documented file.
	want := &userconfig.Config{}
	err = yaml.Unmarshal([]byte(docsExample), want)
	require.NoError(t, err)
	want.Version = FormatVersion
	assert.Equal(t, want.Settings, cfg.Settings)

	errs, warnings := Validate(cfg)
	assert.Empty(t, errs)
	assert.Empty(t, warnings)
}

func TestRenderDeterministic(t *testing.T) {
	t.Parallel()
	doc := docsDoc(t)
	doc["aliases"] = map[string]any{"z": map[string]any{"path": "z.yaml"}, "a": map[string]any{"path": "a.yaml"}}
	doc["providers"] = map[string]any{"p2": map[string]any{"base_url": "https://b"}, "p1": map[string]any{"base_url": "https://a"}}
	doc["zz_future"] = true
	doc["aa_future"] = map[string]any{"b": 1, "a": 2}
	_, first, err := Render(doc)
	require.NoError(t, err)
	for range 30 {
		_, again, err := Render(doc)
		require.NoError(t, err)
		require.Equal(t, first, again)
	}
	assert.Less(t, strings.Index(first, "  a:\n    path: a.yaml"), strings.Index(first, "  z:\n    path: z.yaml"))
	assert.Less(t, strings.Index(first, "aa_future"), strings.Index(first, "zz_future"))
}

func TestRenderVersionIsReserved(t *testing.T) {
	t.Parallel()
	_, _, err := Render(map[string]any{"version": "v2"})
	assert.ErrorContains(t, err, `"version" is managed by the provider`)

	_, out, err := Render(map[string]any{})
	require.NoError(t, err)
	assert.Equal(t, "version: v1\n", out)
}

func TestRenderRejectsWhatDockerAgentRejects(t *testing.T) {
	t.Parallel()
	_, _, err := Render(map[string]any{"providers": map[string]any{
		"p": map[string]any{"base_url": "https://x", "thinking_budget": "enormous"},
	}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `invalid thinking_budget effort "enormous"`)
	assert.NotContains(t, err.Error(), "\x1b[")
	assert.NotRegexp(t, `\[\d+:\d+\]`, err.Error())
}

// TestRenderMatchesDockerAgentSave proves the rendered file is exactly what docker-agent
// writes: docker-agent loads it (including its own validation) and saves it back
// byte-for-byte unchanged.
func TestRenderMatchesDockerAgentSave(t *testing.T) { //nolint:paralleltest // paths.SetConfigDir is global.
	doc := docsDoc(t)
	doc["aliases"] = map[string]any{"rev": map[string]any{"path": "docker.io/x/y:1", "safety": "restricted", "sandbox": true}}
	doc["providers"] = map[string]any{"gw": map[string]any{
		"base_url": "https://llm.example.com/v1", "token_key": "GW_KEY", "thinking_budget": "high",
		"task_budget": map[string]any{"total": 4096}, "provider_opts": map[string]any{"context_size": 32768},
	}}
	doc["default_model"] = map[string]any{"provider": "gw", "model": "qwen3-coder"}
	doc["models_gateway"] = "https://gateway.example.com"
	doc["board"] = map[string]any{
		"projects": []any{map[string]any{"name": "svc", "path": "/src/svc"}},
		"columns":  []any{map[string]any{"id": "dev", "name": "Dev", "emoji": "🔨"}},
	}
	doc["credential_helper"] = map[string]any{"command": "pass", "args": []any{"show", "docker"}}
	doc["sandbox_allowlist"] = []any{"registry.npmjs.org:443", "api.example.com"}
	doc["future_top_level"] = map[string]any{"enabled": true}
	_, out, err := Render(doc)
	require.NoError(t, err)

	dir := t.TempDir()
	paths.SetConfigDir(dir)
	t.Cleanup(func() { paths.SetConfigDir("") })
	file := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(file, []byte(out), 0o600))
	cfg, err := userconfig.Load()
	require.NoError(t, err)
	require.NoError(t, cfg.Save())
	saved, err := os.ReadFile(file)
	require.NoError(t, err)
	assert.Equal(t, out, string(saved))
}

func TestValidate(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		doc      map[string]any
		err      string
		warning  string
		noErrors bool
	}{
		"settings safety": {
			doc: map[string]any{"settings": map[string]any{"safety": "yolo"}},
			err: `settings.safety: invalid safety mode "yolo"`,
		},
		"alias safety": {
			doc: map[string]any{"aliases": map[string]any{"a": map[string]any{"path": "a.yaml", "safety": "unsafe"}}},
			err: `aliases.a: safety: invalid safety mode "unsafe"`,
		},
		"alias name": {
			doc: map[string]any{"aliases": map[string]any{"-bad": map[string]any{"path": "a.yaml"}}},
			err: `invalid alias name "-bad"`,
		},
		"alias path": {
			doc: map[string]any{"aliases": map[string]any{"a": map[string]any{"path": ""}}},
			err: "aliases.a: agent path cannot be empty",
		},
		"busy send mode": {
			doc: map[string]any{"settings": map[string]any{"busy_send_mode": "later"}},
			err: `settings.busy_send_mode: invalid value "later" (valid: steer, queue)`,
		},
		"interrupt confirmation": {
			doc: map[string]any{"settings": map[string]any{"interrupt_confirmation": "twice"}},
			err: `settings.interrupt_confirmation: invalid value "twice"`,
		},
		"sidebar position": {
			doc: map[string]any{"settings": map[string]any{"layout": map[string]any{"sidebar_position": "middle"}}},
			err: `settings.layout.sidebar_position: invalid value "middle"`,
		},
		"section spacing": {
			doc: map[string]any{"settings": map[string]any{"layout": map[string]any{"section_spacing": "tight"}}},
			err: `settings.layout.section_spacing: invalid value "tight"`,
		},
		"sidebar info mode": {
			doc: map[string]any{"settings": map[string]any{"layout": map[string]any{"sidebar_info_mode": "full"}}},
			err: `settings.layout.sidebar_info_mode: invalid value "full"`,
		},
		"negative tab title": {
			doc: map[string]any{"settings": map[string]any{"tab_title_max_length": -1}},
			err: "settings.tab_title_max_length must not be negative",
		},
		"negative sound threshold": {
			doc: map[string]any{"settings": map[string]any{"sound_threshold": -5}},
			err: "settings.sound_threshold must not be negative",
		},
		"hooks": {
			doc: map[string]any{"settings": map[string]any{"hooks": map[string]any{
				"session_start": []any{map[string]any{"type": "evaluator", "evaluator": "x"}},
			}}},
			err: "settings.hooks.session_start: evaluator hooks are only supported on",
		},
		"keybinding without action": {
			doc: map[string]any{"settings": map[string]any{"keybindings": []any{map[string]any{"action": "", "keys": []any{"f2"}}}}},
			err: "settings.keybindings[0]: action is required",
		},
		"keybinding without keys": {
			doc: map[string]any{"settings": map[string]any{"keybindings": []any{map[string]any{"action": "quit", "keys": []any{}}}}},
			err: "settings.keybindings[0]: at least one key is required",
		},
		"unknown keybinding action": {
			doc:     map[string]any{"settings": map[string]any{"keybindings": []any{map[string]any{"action": "fly", "keys": []any{"f2"}}}}},
			warning: `does not know the action "fly"`,
		},
		"malformed key": {
			doc:     map[string]any{"settings": map[string]any{"keybindings": []any{map[string]any{"action": "quit", "keys": []any{"ctrl+entre"}}}}},
			warning: `ignores the malformed key "ctrl+entre"`,
		},
		"openai provider without base url": {
			doc: map[string]any{"providers": map[string]any{"local": map[string]any{"token_key": "X"}}},
			err: "base_url is required for OpenAI-compatible providers",
		},
		"provider api type": {
			doc: map[string]any{"providers": map[string]any{"p": map[string]any{"base_url": "https://x", "api_type": "soap"}}},
			err: "invalid api_type 'soap'",
		},
		"provider auth": {
			doc: map[string]any{"providers": map[string]any{"p": map[string]any{
				"provider": "anthropic", "auth": map[string]any{"type": "magic"},
			}}},
			err: "providers.p:",
		},
		"default model without model": {
			doc: map[string]any{"default_model": map[string]any{"provider": "openai", "temperature": 0.3}},
			err: "default_model: provider and model are required",
		},
		"default model compaction threshold": {
			doc: map[string]any{"default_model": map[string]any{"provider": "openai", "model": "gpt-5", "compaction_threshold": 2}},
			err: "default_model: compaction_threshold",
		},
		"sandbox host with comma": {
			doc: map[string]any{"sandbox_allowlist": []any{"a.com,b.com"}},
			err: "contains comma or whitespace",
		},
		"empty sandbox host": {
			doc: map[string]any{"sandbox_allowlist": []any{""}},
			err: "sandbox_allowlist[0]: host must not be empty",
		},
		"duplicate sandbox host": {
			doc:     map[string]any{"sandbox_allowlist": []any{"a.com", "a.com"}},
			warning: `sandbox_allowlist[1]: "a.com" is listed more than once`,
		},
		"board project path": {
			doc: map[string]any{"board": map[string]any{"projects": []any{map[string]any{"name": "x", "path": ""}}}},
			err: "board.projects[0]: path is required",
		},
		"board duplicate column": {
			doc: map[string]any{"board": map[string]any{"columns": []any{
				map[string]any{"id": "dev", "name": "Dev"}, map[string]any{"id": "dev", "name": "Again"},
			}}},
			err: `board.columns[1]: id "dev" is already used by board.columns[0]`,
		},
		"board column without id and name": {
			doc: map[string]any{"board": map[string]any{"columns": []any{map[string]any{"emoji": "x"}}}},
			err: "board.columns[0]: id or name is required",
		},
		"credential helper": {
			doc: map[string]any{"credential_helper": map[string]any{"args": []any{"x"}}},
			err: "credential_helper.command is required",
		},
		"valid": {
			doc: map[string]any{
				"settings": map[string]any{"safety": "balanced", "keybindings": []any{
					map[string]any{"action": "editor_newline", "keys": []any{"shift+enter", "ctrl+j", "f12"}},
				}},
				"aliases":       map[string]any{"my-agent_1": map[string]any{"path": "./agent.yaml", "safety": "autonomous"}},
				"providers":     map[string]any{"claude": map[string]any{"provider": "anthropic"}},
				"default_model": map[string]any{"provider": "anthropic", "model": "claude-sonnet-4-5"},
			},
			noErrors: true,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cfg, _, err := Render(tc.doc)
			require.NoError(t, err)
			errs, warnings := Validate(cfg)
			if tc.err != "" {
				assert.Contains(t, strings.Join(errs, "\n"), tc.err)
			}
			if tc.warning != "" {
				assert.Empty(t, errs)
				assert.Contains(t, strings.Join(warnings, "\n"), tc.warning)
			}
			if tc.noErrors {
				assert.Empty(t, errs)
				assert.Empty(t, warnings)
			}
		})
	}
}

func TestCleanError(t *testing.T) {
	t.Parallel()
	msg := "\x1b[31m[3:5] invalid thing\x1b[0m\n>  3 |   token: s3cr3t-value\n         ^\n"
	assert.Equal(t, "invalid thing", CleanError(msg))
}

func TestValidKey(t *testing.T) {
	t.Parallel()
	for _, k := range []string{"f2", "f63", "ctrl+k", "alt+shift+enter", "x", "ctrl+1", "super+space"} {
		assert.True(t, validKey(k), k)
	}
	for _, k := range []string{"", "f64", "f0", "ctrl+", "entre", "ctrl+foo", "hold+x", "ctrl k"} {
		assert.False(t, validKey(k), k)
	}
}
