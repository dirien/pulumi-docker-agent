// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/integration"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	userSettingsToken = "docker-agent:index:UserSettings"
	agentsIgnoreToken = "docker-agent:index:AgentsIgnore"
)

// fileLifecycle drives one resource through the engine's calls against an in-process
// provider and keeps the latest state and inputs.
type fileLifecycle struct {
	t      *testing.T
	s      integration.Server
	typ    string
	name   string
	id     string
	state  property.Map
	inputs property.Map
}

func newFileLifecycle(t *testing.T, typ string) *fileLifecycle {
	t.Helper()
	return &fileLifecycle{t: t, s: server(t), typ: typ, name: "res"}
}

// check runs Check like the engine: with the previous inputs once the resource exists.
func (l *fileLifecycle) check(inputs map[string]any) property.Map {
	l.t.Helper()
	resp := l.checkRaw(inputs)
	require.Empty(l.t, resp.Failures, failureText(resp.Failures))
	return resp.Inputs
}

func (l *fileLifecycle) checkRaw(inputs map[string]any) p.CheckResponse {
	l.t.Helper()
	resp, err := l.s.Check(p.CheckRequest{Urn: urn(l.typ, l.name), State: l.inputs, Inputs: pm(inputs)})
	require.NoError(l.t, err)
	return resp
}

func (l *fileLifecycle) create(inputs property.Map, dryRun bool) property.Map {
	l.t.Helper()
	resp, err := l.s.Create(p.CreateRequest{Urn: urn(l.typ, l.name), Properties: inputs, DryRun: dryRun})
	require.NoError(l.t, err)
	if !dryRun {
		l.id, l.state, l.inputs = resp.ID, resp.Properties, inputs
	}
	return resp.Properties
}

func (l *fileLifecycle) diff(inputs property.Map) p.DiffResponse {
	l.t.Helper()
	resp, err := l.s.Diff(p.DiffRequest{ID: l.id, Urn: urn(l.typ, l.name), State: l.state, Inputs: inputs, OldInputs: l.inputs})
	require.NoError(l.t, err)
	return resp
}

func (l *fileLifecycle) update(inputs property.Map, dryRun bool) property.Map {
	l.t.Helper()
	resp, err := l.s.Update(p.UpdateRequest{
		ID: l.id, Urn: urn(l.typ, l.name), State: l.state, Inputs: inputs, OldInputs: l.inputs, DryRun: dryRun,
	})
	require.NoError(l.t, err)
	if !dryRun {
		l.state, l.inputs = resp.Properties, inputs
	}
	return resp.Properties
}

func (l *fileLifecycle) refresh() p.ReadResponse {
	l.t.Helper()
	resp, err := l.s.Read(p.ReadRequest{ID: l.id, Urn: urn(l.typ, l.name), Properties: l.state, Inputs: l.inputs})
	require.NoError(l.t, err)
	if resp.ID != "" {
		l.state = resp.Properties
	}
	return resp
}

func (l *fileLifecycle) delete() {
	l.t.Helper()
	require.NoError(l.t, l.s.Delete(p.DeleteRequest{ID: l.id, Urn: urn(l.typ, l.name), Properties: l.state, OldInputs: l.inputs}))
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

func assertNoFile(t *testing.T, path string) {
	t.Helper()
	_, err := os.Stat(path)
	assert.True(t, os.IsNotExist(err), "%s should not exist (err=%v)", path, err)
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, want, info.Mode().Perm(), path)
}

func TestLocalFileResourcesInSchema(t *testing.T) {
	t.Parallel()
	resp, err := server(t).GetSchema(p.GetSchemaRequest{})
	require.NoError(t, err)
	var schema struct {
		Resources map[string]struct {
			Description     string                    `json:"description"`
			InputProperties map[string]map[string]any `json:"inputProperties"`
			Properties      map[string]map[string]any `json:"properties"`
			RequiredInputs  []string                  `json:"requiredInputs"`
		} `json:"resources"`
		Types map[string]map[string]any `json:"types"`
	}
	require.NoError(t, json.Unmarshal([]byte(resp.Schema), &schema))

	us, ok := schema.Resources[userSettingsToken]
	require.True(t, ok)
	for _, in := range []string{"settings", "aliases", "providers", "defaultModel", "modelsGateway", "board",
		"credentialHelper", "sandboxAllowlist", "extra", "path", "overwrite", "skipValidation"} {
		assert.Contains(t, us.InputProperties, in)
		assert.NotEmpty(t, us.InputProperties[in]["description"], in)
	}
	for _, out := range []string{"yaml", "sha256", "filePath", "backupPath"} {
		assert.Contains(t, us.Properties, out)
	}
	assert.Contains(t, us.Description, "{{% examples %}}")
	assert.Contains(t, us.InputProperties["path"]["description"], "machine that runs Pulumi")

	ai, ok := schema.Resources[agentsIgnoreToken]
	require.True(t, ok)
	assert.Equal(t, []string{"patterns"}, ai.RequiredInputs)
	for _, out := range []string{"content", "sha256", "filePath", "backupPath"} {
		assert.Contains(t, ai.Properties, out)
	}
	assert.Contains(t, ai.InputProperties, "overwrite")

	for _, typ := range []string{"UserGlobalSettings", "UserLayoutSettings", "UserHooks", "UserHook", "UserHookMatcher",
		"UserAlias", "UserProvider", "UserModel", "UserBoard", "UserCredentialHelper", "UserKeybinding"} {
		def, ok := schema.Types["docker-agent:index:"+typ]
		if assert.True(t, ok, typ) {
			assert.NotEmpty(t, def["description"], typ)
		}
	}
	hook := schema.Types["docker-agent:index:UserHook"]["properties"].(map[string]any)["type"].(map[string]any)
	assert.Equal(t, "command", hook["default"])
}

func userSettingsInputs(path, theme string) map[string]any {
	in := map[string]any{
		"settings": map[string]any{
			"theme":  theme,
			"sound":  true,
			"safety": "balanced",
			"layout": map[string]any{"sidebarPosition": "left", "hideUsage": true},
			"hooks": map[string]any{
				"sessionStart": []any{map[string]any{"command": "~/.config/cagent/hooks/start.sh"}},
				"preToolUse": []any{map[string]any{
					"matcher": "shell", "hooks": []any{map[string]any{"type": "builtin", "command": "redact_secrets"}},
				}},
			},
			"keybindings": []any{map[string]any{"action": "commands", "keys": []any{"f2", "ctrl+k"}}},
		},
		"aliases": map[string]any{
			"reviewer": map[string]any{"path": "docker.io/myorg/reviewer:latest", "safety": "restricted"},
		},
		"providers": map[string]any{
			"gateway": map[string]any{
				"baseUrl": "https://llm.example.com/v1", "tokenKey": "GATEWAY_API_KEY",
				"thinkingBudget": map[string]any{"effort": "high"}, "providerOpts": map[string]any{"context_size": 32768},
			},
		},
		"defaultModel":     map[string]any{"provider": "gateway", "model": "qwen3-coder"},
		"sandboxAllowlist": []any{"registry.npmjs.org:443"},
	}
	if path != "" {
		in["path"] = path
	}
	return in
}

const wantUserSettingsYAML = `version: v1
default_model: gateway/qwen3-coder
aliases:
  reviewer:
    path: docker.io/myorg/reviewer:latest
    safety: restricted
providers:
  gateway:
    base_url: https://llm.example.com/v1
    token_key: GATEWAY_API_KEY
    provider_opts:
      context_size: 32768
    thinking_budget: high
settings:
  theme: dracula
  safety: balanced
  sound: true
  hooks:
    pre_tool_use:
    - matcher: shell
      hooks:
      - type: builtin
        command: redact_secrets
    session_start:
    - type: command
      command: ~/.config/cagent/hooks/start.sh
  keybindings:
  - action: commands
    keys:
    - f2
    - ctrl+k
  layout:
    sidebar_position: left
    hide_usage: true
sandbox_allowlist:
- registry.npmjs.org:443
`

func TestUserSettingsLifecycle(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "home", ".config", "cagent", "config.yaml")
	l := newFileLifecycle(t, userSettingsToken)

	inputs := l.check(userSettingsInputs(path, "dracula"))

	// Preview: the real document, and nothing on disk.
	preview := l.create(inputs, true)
	assert.Equal(t, wantUserSettingsYAML, preview.Get("yaml").AsString())
	assert.Equal(t, path, preview.Get("filePath").AsString())
	assertNoFile(t, filepath.Join(dir, "home"))

	// Create writes the file with mode 0600 and creates the directories with 0700.
	out := l.create(inputs, false)
	assert.Equal(t, wantUserSettingsYAML, out.Get("yaml").AsString())
	assert.Len(t, out.Get("sha256").AsString(), 64)
	assert.Equal(t, wantUserSettingsYAML, readFile(t, path))
	assertMode(t, path, 0o600)
	assertMode(t, filepath.Dir(path), 0o700)

	// No changes: no diff, and a refresh finds no drift.
	same := l.check(userSettingsInputs(path, "dracula"))
	assert.False(t, l.diff(same).HasChanges)
	assert.Equal(t, wantUserSettingsYAML, l.refresh().Properties.Get("yaml").AsString())

	// An update preview does not touch the file; the update rewrites it.
	changed := l.check(userSettingsInputs(path, "nord"))
	d := l.diff(changed)
	assert.True(t, d.HasChanges)
	assert.Contains(t, d.DetailedDiff, "settings")
	previewed := l.update(changed, true)
	assert.Contains(t, previewed.Get("yaml").AsString(), "theme: nord")
	assert.Equal(t, wantUserSettingsYAML, readFile(t, path))
	l.update(changed, false)
	assert.Contains(t, readFile(t, path), "theme: nord")

	// Drift: an edit outside of Pulumi is picked up by refresh and reverted by the next update.
	require.NoError(t, os.WriteFile(path, []byte("version: v1\nsettings:\n  theme: hacked\n"), 0o644))
	refreshed := l.refresh()
	assert.Equal(t, "version: v1\nsettings:\n  theme: hacked\n", refreshed.Properties.Get("yaml").AsString())
	d = l.diff(changed)
	assert.True(t, d.HasChanges)
	assert.Contains(t, d.DetailedDiff, "yaml")
	assert.NotContains(t, d.DetailedDiff, "settings")
	l.update(changed, false)
	assert.Contains(t, readFile(t, path), "theme: nord")
	assertMode(t, path, 0o600)

	// Moving the file writes the new path and removes the old one.
	moved := filepath.Join(dir, "elsewhere", "config.yaml")
	movedInputs := l.check(userSettingsInputs(moved, "nord"))
	d = l.diff(movedInputs)
	assert.Contains(t, d.DetailedDiff, "path")
	l.update(movedInputs, false)
	assertNoFile(t, path)
	assert.Contains(t, readFile(t, moved), "theme: nord")

	// A deleted file is drift: refresh keeps the resource with empty content, and the next
	// update writes the file again.
	require.NoError(t, os.Remove(moved))
	refreshed = l.refresh()
	assert.Equal(t, "res", refreshed.ID)
	assert.Empty(t, refreshed.Properties.Get("yaml").AsString())
	assert.Contains(t, l.diff(movedInputs).DetailedDiff, "yaml")
	l.update(movedInputs, false)
	assert.Contains(t, readFile(t, moved), "theme: nord")

	// Without a backup, delete removes the file and tolerates it being gone already.
	_, hasBackup := l.state.GetOk("backupPath")
	assert.False(t, hasBackup)
	l.delete()
	assertNoFile(t, moved)
	l.delete()
}

func TestUserSettingsRenderOnly(t *testing.T) {
	t.Parallel()
	l := newFileLifecycle(t, userSettingsToken)
	inputs := l.check(userSettingsInputs("", "dracula"))
	out := l.create(inputs, false)
	assert.Equal(t, wantUserSettingsYAML, out.Get("yaml").AsString())
	_, hasPath := out.GetOk("filePath")
	assert.False(t, hasPath)
	assert.Equal(t, wantUserSettingsYAML, l.refresh().Properties.Get("yaml").AsString())

	// Adding a path later writes the file.
	path := filepath.Join(t.TempDir(), "config.yaml")
	withPath := l.check(userSettingsInputs(path, "dracula"))
	assert.Contains(t, l.diff(withPath).DetailedDiff, "path")
	l.update(withPath, false)
	assert.Equal(t, wantUserSettingsYAML, readFile(t, path))

	// Removing it again deletes the file.
	l.update(l.check(userSettingsInputs("", "dracula")), false)
	assertNoFile(t, path)
	l.delete()
}

func TestUserSettingsCheckFailures(t *testing.T) {
	t.Parallel()
	s := server(t)
	cases := map[string]struct {
		inputs map[string]any
		want   string
	}{
		"safety": {
			inputs: map[string]any{"settings": map[string]any{"safety": "yolo"}},
			want:   `settings.safety: invalid safety mode "yolo"`,
		},
		"busy send mode": {
			inputs: map[string]any{"settings": map[string]any{"busySendMode": "later"}},
			want:   `settings.busy_send_mode: invalid value "later" (valid: steer, queue)`,
		},
		"alias name": {
			inputs: map[string]any{"aliases": map[string]any{"bad name": map[string]any{"path": "a.yaml"}}},
			want:   `invalid alias name "bad name"`,
		},
		"missing required alias path": {
			inputs: map[string]any{"aliases": map[string]any{"a": map[string]any{}}},
			want:   "path",
		},
		"extra repeats a typed key": {
			inputs: map[string]any{"settings": map[string]any{"theme": "x", "extra": map[string]any{"theme": "y"}}},
			want:   `settings.extra: extra key "theme" is a typed input`,
		},
		"extra sets version": {
			inputs: map[string]any{"extra": map[string]any{"version": "v9"}},
			want:   `"version" is managed by the provider`,
		},
		"thinking budget": {
			inputs: map[string]any{"providers": map[string]any{"p": map[string]any{
				"baseUrl": "https://x", "thinkingBudget": map[string]any{"effort": "high", "tokens": 10},
			}}},
			want: "thinkingBudget: set either effort or tokens, not both",
		},
		"unknown effort": {
			inputs: map[string]any{"providers": map[string]any{"p": map[string]any{
				"baseUrl": "https://x", "thinkingBudget": map[string]any{"effort": "enormous"},
			}}},
			want: `invalid thinking_budget effort "enormous"`,
		},
		"hook": {
			inputs: map[string]any{"settings": map[string]any{"hooks": map[string]any{
				"sessionEnd": []any{map[string]any{"command": "x", "onError": "block"}},
			}}},
			want: "settings.hooks.session_end: on_error block is not supported by this event",
		},
		"sandbox host": {
			inputs: map[string]any{"sandboxAllowlist": []any{"a.com b.com"}},
			want:   "contains comma or whitespace",
		},
		"path": {
			inputs: map[string]any{"path": "~someone/config.yaml"},
			want:   "only `~` and `~/`",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			resp := check(t, s, userSettingsToken, "us", tc.inputs)
			require.NotEmpty(t, resp.Failures)
			text := failureText(resp.Failures)
			assert.Contains(t, text, tc.want)
			assert.NotContains(t, text, "\x1b[")
		})
	}
}

func TestUserSettingsSkipValidation(t *testing.T) {
	t.Parallel()
	l := newFileLifecycle(t, userSettingsToken)
	inputs := l.check(map[string]any{
		"skipValidation": true,
		"settings":       map[string]any{"busySendMode": "from-the-future"},
	})
	out := l.create(inputs, false)
	assert.Contains(t, out.Get("yaml").AsString(), "busy_send_mode: from-the-future")

	// Decoding errors still fail: the document must be readable by docker-agent.
	resp := check(t, l.s, userSettingsToken, "us", map[string]any{
		"skipValidation": true,
		"settings":       map[string]any{"theme": "x", "extra": map[string]any{"theme": "y"}},
	})
	assert.NotEmpty(t, resp.Failures)
}

func TestUserSettingsSecretsAndUnknowns(t *testing.T) {
	t.Parallel()
	l := newFileLifecycle(t, userSettingsToken)

	// A secret input makes the rendered document secret, and failures never echo it.
	secret := pm(map[string]any{"settings": map[string]any{"hooks": map[string]any{
		"sessionStart": []any{map[string]any{"command": "notify", "env": map[string]any{}}},
	}}})
	env := property.New(map[string]property.Value{"TOKEN": property.New("s3cr3t-t0ken").WithSecret(true)})
	hooks := secret.Get("settings").AsMap().Get("hooks").AsMap()
	start := hooks.Get("sessionStart").AsArray().AsSlice()
	start[0] = property.New(start[0].AsMap().Set("env", env))
	hooks = hooks.Set("sessionStart", property.New(start))
	secret = secret.Set("settings", property.New(secret.Get("settings").AsMap().Set("hooks", property.New(hooks))))
	resp, err := l.s.Check(p.CheckRequest{Urn: urn(userSettingsToken, "us"), Inputs: secret})
	require.NoError(t, err)
	require.Empty(t, resp.Failures, failureText(resp.Failures))
	out := l.create(resp.Inputs, false)
	assert.True(t, out.Get("yaml").Secret())
	assert.True(t, out.Get("sha256").Secret())

	// Unknown content: yaml and sha256 are unknown in the preview, filePath is known.
	path := filepath.Join(t.TempDir(), "config.yaml")
	withUnknown := pm(map[string]any{"path": path, "settings": map[string]any{"sound": true}})
	withUnknown = withUnknown.Set("settings", property.New(withUnknown.Get("settings").AsMap().Set("theme", property.New(property.Computed))))
	cr, err := l.s.Check(p.CheckRequest{Urn: urn(userSettingsToken, "us"), Inputs: withUnknown})
	require.NoError(t, err)
	require.Empty(t, cr.Failures)
	prev := l.create(cr.Inputs, true)
	assert.True(t, prev.Get("yaml").IsComputed())
	assert.True(t, prev.Get("sha256").IsComputed())
	assert.Equal(t, path, prev.Get("filePath").AsString())
	assertNoFile(t, path)

	// Unknown path: the document is known, filePath is not.
	unknownPath := pm(map[string]any{"settings": map[string]any{"sound": true}}).Set("path", property.New(property.Computed))
	cr, err = l.s.Check(p.CheckRequest{Urn: urn(userSettingsToken, "us"), Inputs: unknownPath})
	require.NoError(t, err)
	prev = l.create(cr.Inputs, true)
	assert.Equal(t, "version: v1\nsettings:\n  sound: true\n", prev.Get("yaml").AsString())
	assert.True(t, prev.Get("filePath").IsComputed())
}

func TestUserSettingsCheckRedactsSecrets(t *testing.T) {
	t.Parallel()
	s := server(t)
	inputs := pm(map[string]any{"settings": map[string]any{}})
	inputs = inputs.Set("settings", property.New(inputs.Get("settings").AsMap().Set("safety",
		property.New("super-secret-mode").WithSecret(true))))
	resp, err := s.Check(p.CheckRequest{Urn: urn(userSettingsToken, "us"), Inputs: inputs})
	require.NoError(t, err)
	text := failureText(resp.Failures)
	assert.Contains(t, text, "settings.safety: invalid safety mode")
	assert.NotContains(t, text, "super-secret-mode")
}

func TestAgentsIgnoreLifecycle(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "project")
	file := filepath.Join(dir, ".agentsignore")
	l := newFileLifecycle(t, agentsIgnoreToken)
	in := func(patterns ...any) map[string]any {
		return map[string]any{"directory": dir, "comment": "Managed by Pulumi.", "patterns": patterns}
	}

	inputs := l.check(in(".env", "*.key", "!public.key", "build/"))
	want := "# Managed by Pulumi.\n\n.env\n*.key\n!public.key\nbuild/\n"
	prev := l.create(inputs, true)
	assert.Equal(t, want, prev.Get("content").AsString())
	assertNoFile(t, dir)

	out := l.create(inputs, false)
	assert.Equal(t, want, out.Get("content").AsString())
	assert.Equal(t, file, out.Get("filePath").AsString())
	assert.Equal(t, want, readFile(t, file))
	assertMode(t, file, 0o644)

	assert.False(t, l.diff(l.check(in(".env", "*.key", "!public.key", "build/"))).HasChanges)

	// Order matters: reordering is an update.
	reordered := l.check(in("*.key", ".env", "!public.key", "build/"))
	assert.Contains(t, l.diff(reordered).DetailedDiff, "patterns")
	l.update(reordered, true)
	assert.Equal(t, want, readFile(t, file), "previews never write")
	l.update(reordered, false)
	assert.True(t, strings.HasSuffix(readFile(t, file), "*.key\n.env\n!public.key\nbuild/\n"))

	// Drift.
	require.NoError(t, os.WriteFile(file, []byte("nothing\n"), 0o644))
	assert.Equal(t, "nothing\n", l.refresh().Properties.Get("content").AsString())
	assert.Contains(t, l.diff(reordered).DetailedDiff, "content")
	l.update(reordered, false)
	assert.Contains(t, readFile(t, file), "!public.key")

	l.delete()
	assertNoFile(t, file)
	_, err := os.Stat(dir)
	assert.NoError(t, err, "the project directory is kept")
}

func TestAgentsIgnoreCheck(t *testing.T) {
	t.Parallel()
	s := server(t)
	resp := check(t, s, agentsIgnoreToken, "ai", map[string]any{"patterns": []any{"ok", " lead", "#c", "docs/**.md"}})
	text := failureText(resp.Failures)
	assert.Contains(t, text, "patterns: patterns[1]")
	assert.Contains(t, text, "patterns[2]")
	assert.Contains(t, text, "patterns[3]")
	assert.NotContains(t, text, "patterns[0]")

	resp = check(t, s, agentsIgnoreToken, "ai", map[string]any{})
	assert.Contains(t, failureText(resp.Failures), "patterns")

	// Empty lists are allowed (an empty file hides nothing).
	resp = check(t, s, agentsIgnoreToken, "ai", map[string]any{"patterns": []any{}})
	assert.Empty(t, resp.Failures)

	// Unknown patterns skip validation; the content is unknown in the preview.
	inputs := pm(map[string]any{"directory": t.TempDir()}).Set("patterns", property.New(property.Computed))
	cr, err := s.Check(p.CheckRequest{Urn: urn(agentsIgnoreToken, "ai"), Inputs: inputs})
	require.NoError(t, err)
	require.Empty(t, cr.Failures)
	created, err := s.Create(p.CreateRequest{Urn: urn(agentsIgnoreToken, "ai"), Properties: cr.Inputs, DryRun: true})
	require.NoError(t, err)
	assert.True(t, created.Properties.Get("content").IsComputed())
	assert.False(t, created.Properties.Get("filePath").IsComputed())
}

func TestLocalFileImportUnsupported(t *testing.T) {
	t.Parallel()
	s := server(t)
	for _, typ := range []string{userSettingsToken, agentsIgnoreToken} {
		_, err := s.Read(p.ReadRequest{ID: "/some/path", Urn: urn(typ, "imported")})
		assert.ErrorContains(t, err, "importing is not supported", typ)
	}
}

// existingConfig is a hand-written user configuration with an alias that UserSettings
// does not declare.
const existingConfig = "version: v1\naliases:\n  mine:\n    path: docker.io/me/agent:latest\n"

// TestUserSettingsExistingFileIsNotClobbered is the regression test for an existing
// ~/.config/cagent/config.yaml: the preview fails, Create refuses as well, and with
// `overwrite` the file is backed up and restored on delete.
func TestUserSettingsExistingFileIsNotClobbered(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(existingConfig), 0o644))
	l := newFileLifecycle(t, userSettingsToken)
	in := map[string]any{"path": path, "settings": map[string]any{"sound": true}}

	// The preview (Check) reports the conflict.
	resp := l.checkRaw(in)
	require.Len(t, resp.Failures, 1)
	assert.Equal(t, "path", resp.Failures[0].Property)
	assert.Contains(t, resp.Failures[0].Reason, path+" already exists with different content")
	assert.Contains(t, resp.Failures[0].Reason, "`overwrite: true`")
	assert.NotContains(t, resp.Failures[0].Reason, "docker.io/me/agent", "file content never appears in messages")

	// Create enforces it too (the file may appear between preview and update).
	_, err := l.s.Create(p.CreateRequest{Urn: urn(userSettingsToken, "res"), Properties: resp.Inputs})
	require.ErrorContains(t, err, "already exists with different content")
	assert.Equal(t, existingConfig, readFile(t, path))
	assertNoFile(t, path+".pulumi-backup")

	// With overwrite, the existing file is moved aside and restored on delete.
	in["overwrite"] = true
	inputs := l.check(in)
	out := l.create(inputs, false)
	backup := path + ".pulumi-backup"
	assert.Equal(t, backup, out.Get("backupPath").AsString())
	assert.Equal(t, "version: v1\nsettings:\n  sound: true\n", readFile(t, path))
	assertMode(t, path, 0o600)
	assert.Equal(t, existingConfig, readFile(t, backup))

	// Updates rewrite the file and keep the backup.
	in["settings"] = map[string]any{"theme": "nord"}
	updated := l.update(l.check(in), false)
	assert.Equal(t, backup, updated.Get("backupPath").AsString())
	assert.Contains(t, readFile(t, path), "theme: nord")
	assert.Equal(t, existingConfig, readFile(t, backup))

	l.delete()
	assert.Equal(t, existingConfig, readFile(t, path), "delete restores the original file")
	assertMode(t, path, 0o644)
	assertNoFile(t, backup)
}

func TestUserSettingsExistingIdenticalFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.yaml")
	const rendered = "version: v1\nsettings:\n  sound: true\n"
	require.NoError(t, os.WriteFile(path, []byte(rendered), 0o600))
	l := newFileLifecycle(t, userSettingsToken)

	// Nothing would be lost, so no `overwrite` is needed, but the file is still backed up
	// and comes back on delete.
	out := l.create(l.check(map[string]any{"path": path, "settings": map[string]any{"sound": true}}), false)
	assert.Equal(t, path+".pulumi-backup", out.Get("backupPath").AsString())
	l.delete()
	assert.Equal(t, rendered, readFile(t, path))
	assertNoFile(t, path+".pulumi-backup")
}

func TestUserSettingsExistingBackupIsNeverReplaced(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(existingConfig), 0o600))
	require.NoError(t, os.WriteFile(path+".pulumi-backup", []byte("older\n"), 0o600))
	l := newFileLifecycle(t, userSettingsToken)

	resp := l.checkRaw(map[string]any{"path": path, "overwrite": true, "settings": map[string]any{"sound": true}})
	assert.Contains(t, failureText(resp.Failures), path+".pulumi-backup already exists")
	_, err := l.s.Create(p.CreateRequest{Urn: urn(userSettingsToken, "res"), Properties: resp.Inputs})
	require.Error(t, err)
	assert.Equal(t, existingConfig, readFile(t, path))
	assert.Equal(t, "older\n", readFile(t, path+".pulumi-backup"))
}

func TestUserSettingsMoveRestoresBackup(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	first := filepath.Join(dir, "a", "config.yaml")
	second := filepath.Join(dir, "b", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(first), 0o700))
	require.NoError(t, os.WriteFile(first, []byte(existingConfig), 0o600))
	l := newFileLifecycle(t, userSettingsToken)
	in := map[string]any{"path": first, "overwrite": true, "settings": map[string]any{"sound": true}}
	l.create(l.check(in), false)

	// Moving away restores the original at the old path.
	in["path"] = second
	out := l.update(l.check(in), false)
	assert.Equal(t, existingConfig, readFile(t, first))
	assertNoFile(t, first+".pulumi-backup")
	assert.Contains(t, readFile(t, second), "sound: true")
	_, hasBackup := out.GetOk("backupPath")
	assert.False(t, hasBackup, "nothing existed at the new path")

	// Moving onto a file that exists needs `overwrite` again.
	in["path"] = first
	in["overwrite"] = false
	resp := l.checkRaw(in)
	assert.Contains(t, failureText(resp.Failures), first+" already exists with different content")

	l.delete()
	assertNoFile(t, second)
	assert.Equal(t, existingConfig, readFile(t, first))
}

func TestUserSettingsMissingBackupOnDelete(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(existingConfig), 0o600))
	l := newFileLifecycle(t, userSettingsToken)
	l.create(l.check(map[string]any{"path": path, "overwrite": true}), false)
	require.NoError(t, os.Remove(path+".pulumi-backup"))
	l.delete()
	assertNoFile(t, path)
}

func TestAgentsIgnoreExistingFileIsNotClobbered(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := filepath.Join(dir, ".agentsignore")
	require.NoError(t, os.WriteFile(file, []byte("secrets.env\n"), 0o644))
	l := newFileLifecycle(t, agentsIgnoreToken)
	in := map[string]any{"directory": dir, "patterns": []any{"*.key"}}

	resp := l.checkRaw(in)
	require.Len(t, resp.Failures, 1)
	assert.Equal(t, "directory", resp.Failures[0].Property)
	assert.Contains(t, resp.Failures[0].Reason, file+" already exists with different content")
	_, err := l.s.Create(p.CreateRequest{Urn: urn(agentsIgnoreToken, "res"), Properties: resp.Inputs})
	require.Error(t, err)
	assert.Equal(t, "secrets.env\n", readFile(t, file))

	in["overwrite"] = true
	out := l.create(l.check(in), false)
	assert.Equal(t, file+".pulumi-backup", out.Get("backupPath").AsString())
	assert.Equal(t, "*.key\n", readFile(t, file))
	l.delete()
	assert.Equal(t, "secrets.env\n", readFile(t, file))
	assertNoFile(t, file+".pulumi-backup")
}
