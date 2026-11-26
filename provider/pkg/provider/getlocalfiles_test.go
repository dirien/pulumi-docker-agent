// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"os"
	"path/filepath"
	"testing"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dirien/pulumi-docker-agent/provider/pkg/agentsignore"
	"github.com/dirien/pulumi-docker-agent/provider/pkg/usersettings"
)

const (
	getUserSettingsToken = "docker-agent:index:getUserSettings"
	getAgentsIgnoreToken = "docker-agent:index:getAgentsIgnore"
)

// TestGetUserSettingsFunction reads a hand-written user configuration into the typed fields,
// without changing the file, and treats a missing file as empty.
func TestGetUserSettingsFunction(t *testing.T) {
	t.Parallel()
	s := server(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "config.yaml")
	const handWritten = `# My docker-agent settings.
default_model:
  provider: anthropic
  model: claude-sonnet-4-5
  stream_idle_timeout: -1
aliases:
  pirate: {path: ./pirate.yaml, yolo: true}
settings:
  theme: dracula
  experimental: {mcp_tasks: true}
  shiny_new_setting: 1
sandbox_allowlist: [api.github.com]
`
	require.NoError(t, os.WriteFile(file, []byte(handWritten), 0o600))

	inv, err := s.Invoke(p.InvokeRequest{Token: getUserSettingsToken, Args: pm(map[string]any{"path": file})})
	require.NoError(t, err)
	out := inv.Return
	assert.True(t, out.Get("exists").AsBool())
	assert.Equal(t, file, out.Get("filePath").AsString())
	assert.Equal(t, handWritten, out.Get("yaml").AsString(), "yaml is the file as it is on disk")
	assert.Equal(t, usersettings.SHA256(handWritten), out.Get("sha256").AsString())
	model := out.Get("defaultModel").AsMap()
	assert.Equal(t, "anthropic", model.Get("provider").AsString())
	assert.Equal(t, "claude-sonnet-4-5", model.Get("model").AsString())
	assert.True(t, model.Get("streamIdleTimeout").AsMap().Get("disabled").AsBool())
	assert.True(t, out.Get("aliases").AsMap().Get("pirate").AsMap().Get("yolo").AsBool())
	settings := out.Get("settings").AsMap()
	assert.Equal(t, "dracula", settings.Get("theme").AsString())
	assert.True(t, settings.Get("experimental").AsMap().Get("mcpTasks").AsBool())
	assert.Equal(t, 1.0, settings.Get("extra").AsMap().Get("shiny_new_setting").AsNumber())
	assert.Equal(t, "api.github.com", out.Get("sandboxAllowlist").AsArray().Get(0).AsString())
	assert.Equal(t, handWritten, readFile(t, file), "the function never writes")

	tests := map[string]struct {
		path    string
		wantErr string
	}{
		"missing file":   {path: filepath.Join(dir, "none.yaml")},
		"not YAML":       {path: writeTemp(t, dir, "bad.yaml", "settings: [unclosed"), wantErr: "parsing"},
		"unknown fields": {path: writeTemp(t, dir, "typo.yaml", "settings:\n  theme: [a, b]\n"), wantErr: "typo.yaml"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			inv, err := s.Invoke(p.InvokeRequest{Token: getUserSettingsToken, Args: pm(map[string]any{"path": tc.path})})
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.False(t, inv.Return.Get("exists").AsBool())
			assert.Equal(t, tc.path, inv.Return.Get("filePath").AsString())
			assert.Empty(t, inv.Return.Get("yaml").AsString())
			assert.Empty(t, inv.Return.Get("sha256").AsString())
		})
	}
}

// TestGetUserSettingsRoundTrip reads back what UserSettings renders: the typed fields of the
// function render the same file again.
func TestGetUserSettingsRoundTrip(t *testing.T) {
	t.Parallel()
	s := server(t)
	args := UserSettingsArgs{
		Settings:     &UserGlobalSettings{Theme: ptr("dracula"), KeepAwake: ptr(true)},
		DefaultModel: &UserModel{Provider: ptr("openai"), Model: ptr("gpt-5-mini"), ContextWindow: ptr(200000)},
	}
	rendered, err := renderUserSettings(args)
	require.NoError(t, err)
	file := writeTemp(t, t.TempDir(), "config.yaml", rendered)

	inv, err := s.Invoke(p.InvokeRequest{Token: getUserSettingsToken, Args: pm(map[string]any{"path": file})})
	require.NoError(t, err)
	settings := inv.Return.Get("settings").AsMap()
	assert.True(t, settings.Get("keepAwake").AsBool())
	assert.Equal(t, 200000.0, inv.Return.Get("defaultModel").AsMap().Get("contextWindow").AsNumber())
	assert.Equal(t, rendered, inv.Return.Get("yaml").AsString())
}

// TestGetAgentsIgnoreFunction reads `.agentsignore` by directory or by file path.
func TestGetAgentsIgnoreFunction(t *testing.T) {
	t.Parallel()
	s := server(t)
	dir := t.TempDir()
	const content = "# Managed by hand.\n# Keep secrets out.\n.env\n*.pem\n\n# keys\n!public.key\n"
	file := writeTemp(t, dir, agentsignore.FileName, content)
	empty := t.TempDir()

	tests := map[string]struct {
		directory    string
		wantFile     string
		wantPatterns []string
		wantComment  string
		wantErr      string
	}{
		"directory": {directory: dir, wantFile: file, wantPatterns: []string{".env", "*.pem", "!public.key"},
			wantComment: "Managed by hand.\nKeep secrets out."},
		"file path": {directory: file, wantFile: file, wantPatterns: []string{".env", "*.pem", "!public.key"},
			wantComment: "Managed by hand.\nKeep secrets out."},
		"missing file":  {directory: empty, wantFile: filepath.Join(empty, agentsignore.FileName), wantPatterns: []string{}},
		"another file":  {directory: writeTemp(t, dir, "notes.txt", "x"), wantErr: "neither a directory nor"},
		"missing input": {wantErr: ""},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			args := map[string]any{}
			if tc.directory != "" {
				args["directory"] = tc.directory
			}
			inv, err := s.Invoke(p.InvokeRequest{Token: getAgentsIgnoreToken, Args: pm(args)})
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			out := inv.Return
			if tc.directory == "" {
				// The Pulumi project directory (the test's working directory) has no .agentsignore.
				assert.False(t, out.Get("exists").AsBool())
				return
			}
			assert.Equal(t, tc.wantFile, out.Get("filePath").AsString())
			var patterns []string
			for _, v := range out.Get("patterns").AsArray().All {
				patterns = append(patterns, v.AsString())
			}
			if len(tc.wantPatterns) == 0 {
				assert.Empty(t, patterns)
				assert.False(t, out.Get("exists").AsBool())
				assert.Empty(t, out.Get("content").AsString())
				return
			}
			assert.Equal(t, tc.wantPatterns, patterns)
			assert.Equal(t, tc.wantComment, out.Get("comment").AsString())
			assert.True(t, out.Get("exists").AsBool())
			assert.Equal(t, content, out.Get("content").AsString())
			assert.Equal(t, agentsignore.SHA256(content), out.Get("sha256").AsString())
		})
	}
	assert.Equal(t, content, readFile(t, file), "the function never writes")
}

func writeTemp(t *testing.T, dir, name, content string) string {
	t.Helper()
	file := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(file, []byte(content), 0o600))
	return file
}
