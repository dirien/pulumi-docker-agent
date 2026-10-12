// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"os"
	"path/filepath"
	"testing"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/integration"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// richUserSettings uses every kind of input UserSettings has: scalars, enums, nested objects,
// lists, maps, the scalar thinking and task budgets, the `provider/model` shorthand docker-agent
// writes for a default model, and `extra` keys at both levels.
func richUserSettings() map[string]any {
	return map[string]any{
		"settings": map[string]any{
			"theme": "auto", "themeDark": "dracula", "safety": "balanced", "sound": true, "tabTitleMaxLength": 30,
			"expandThinking": false, "busySendMode": "queue",
			"layout":      map[string]any{"sidebarPosition": "left", "hideUsage": true},
			"permissions": map[string]any{"deny": []any{"shell:cmd=sudo*"}, "allow": []any{"read_*"}},
			"keybindings": []any{map[string]any{"action": "commands", "keys": []any{"f2", "ctrl+k"}}},
			"hooks": map[string]any{
				"sessionStart": []any{map[string]any{"type": "command", "command": "~/hooks/start.sh", "timeout": 30,
					"env": map[string]any{"SLACK_TOKEN": importedHookToken}}},
				"preToolUse": []any{map[string]any{"matcher": "shell", "hooks": []any{
					map[string]any{"type": "builtin", "command": "redact_secrets"},
				}}},
			},
			"extra": map[string]any{"future_setting": map[string]any{"level": 3}},
		},
		"aliases": map[string]any{
			"reviewer": map[string]any{"path": "docker.io/myorg/reviewer:latest", "safety": "restricted", "sandbox": true},
		},
		"providers": map[string]any{
			"gateway": map[string]any{
				"baseUrl": "https://llm.example.com/v1", "tokenKey": "GATEWAY_API_KEY", "temperature": 0.2,
				"thinkingBudget": map[string]any{"effort": "high"}, "taskBudget": map[string]any{"total": 4096},
				"providerOpts": map[string]any{"context_size": 32768},
				"auth": map[string]any{"type": "workload_identity_federation", "workloadIdentityFederation": map[string]any{
					"federationRuleId": "fdrl_rule", "organizationId": "org", "identityToken": map[string]any{
						"url": "https://idp.example.com/token", "headers": map[string]any{"Authorization": importedIdpHeader},
					},
				}},
			},
			"local": map[string]any{"baseUrl": "http://localhost:11434/v1", "thinkingBudget": map[string]any{"tokens": 2048}},
		},
		"defaultModel":  map[string]any{"provider": "gateway", "model": "qwen3-coder"},
		"modelsGateway": "https://gateway.example.com",
		"board": map[string]any{
			"projects": []any{map[string]any{"name": "svc", "path": "/src/svc"}},
			"columns":  []any{map[string]any{"id": "dev", "name": "Dev", "emoji": "🔨", "prompt": "Implement it."}},
		},
		"credentialHelper": map[string]any{"command": "pass", "args": []any{"show", "docker"}},
		"sandboxAllowlist": []any{"registry.npmjs.org:443"},
		"extra":            map[string]any{"future_top_level": map[string]any{"enabled": true}},
	}
}

// Credentials in richUserSettings, in values the schema declares secret.
const (
	importedHookToken = "xoxb-0123456789abcdef"
	importedIdpHeader = "Bearer idp-0123456789abcdef"
)

// renderLocalFile renders inputs with a render-only resource of typ and returns the checked
// inputs and the rendered content (output contentKey).
func renderLocalFile(t *testing.T, s integration.Server, typ, contentKey string, inputs map[string]any) (property.Map, string) {
	t.Helper()
	checked := check(t, s, typ, "render", inputs)
	require.Empty(t, checked.Failures, failureText(checked.Failures))
	created, err := s.Create(p.CreateRequest{Urn: urn(typ, "render"), Properties: checked.Inputs})
	require.NoError(t, err)
	return checked.Inputs, created.Properties.Get(contentKey).AsString()
}

// importLocalFile imports the file named by id like the engine does and checks that the
// result needs no update: Check keeps the read inputs, Diff finds no changes, and the read
// state is what a preview of Create computes.
func importLocalFile(t *testing.T, s integration.Server, typ, id string) p.ReadResponse {
	t.Helper()
	u := urn(typ, "imported")
	read, err := s.Read(p.ReadRequest{ID: id, Urn: u})
	require.NoError(t, err)

	checked, err := s.Check(p.CheckRequest{Urn: u, State: read.Inputs, Inputs: read.Inputs})
	require.NoError(t, err)
	require.Empty(t, checked.Failures, failureText(checked.Failures))
	assert.True(t, property.New(read.Inputs).Equals(property.New(checked.Inputs)),
		"Check changed the imported inputs:\nread:    %v\nchecked: %v", read.Inputs, checked.Inputs)

	diff, err := s.Diff(p.DiffRequest{ID: read.ID, Urn: u, State: read.Properties, OldInputs: read.Inputs, Inputs: checked.Inputs})
	require.NoError(t, err)
	assert.False(t, diff.HasChanges, "the first preview after the import has changes: %v", diff.DetailedDiff)

	preview, err := s.Create(p.CreateRequest{Urn: u, Properties: checked.Inputs, DryRun: true})
	require.NoError(t, err)
	assert.True(t, property.New(preview.Properties).Equals(property.New(read.Properties)),
		"the imported state differs from what Create computes:\nread:    %v\ncreated: %v", read.Properties, preview.Properties)
	return read
}

// TestUserSettingsImport adopts a configuration file that docker-agent (or the provider)
// wrote: the imported inputs are exactly the inputs that wrote it, the file is not touched by
// an update without changes, and deleting the resource leaves the adopted file in place.
func TestUserSettingsImport(t *testing.T) {
	t.Parallel()
	s := server(t)
	original, content := renderLocalFile(t, s, userSettingsToken, "yaml", richUserSettings())
	dir := t.TempDir()
	file := filepath.Join(dir, "cagent", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(file), 0o700))
	require.NoError(t, os.WriteFile(file, []byte(content), 0o600))

	read := importLocalFile(t, s, userSettingsToken, "file://"+file)
	assert.Equal(t, file, read.ID, "the ID of an adopted file is its absolute path")
	assert.Equal(t, content, read.Properties.Get("yaml").AsString())
	assert.Equal(t, file, read.Properties.Get("filePath").AsString())
	assert.True(t, read.Properties.Get("backupPath").IsNull(), "an adopted file has no backup")
	assert.Equal(t, file, read.Inputs.Get("path").AsString())
	assert.True(t, withoutSecrets(property.New(original)).Equals(withoutSecrets(property.New(read.Inputs.Delete("path")))),
		"the imported inputs differ from the inputs that rendered the file:\nwant: %v\ngot:  %v", original, read.Inputs)

	// The values the schema declares secret are secret in the imported inputs and state, and so
	// are the rendered file and its hash, which contain them (importLocalFile checks that the
	// state is what Create computes from the imported inputs).
	assert.Contains(t, content, importedHookToken)
	assert.Contains(t, content, importedIdpHeader)
	for what, m := range map[string]property.Map{"inputs": read.Inputs, "state": read.Properties} {
		hook := m.Get("settings").AsMap().Get("hooks").AsMap().Get("sessionStart").AsArray().Get(0).AsMap()
		assert.True(t, hook.Get("env").Secret(), "%s: hook env", what)
		identity := m.Get("providers").AsMap().Get("gateway").AsMap().Get("auth").AsMap().
			Get("workloadIdentityFederation").AsMap().Get("identityToken").AsMap()
		assert.True(t, identity.Get("headers").Secret(), "%s: identity token headers", what)
		assert.False(t, identity.Get("url").Secret(), "%s: only the values the schema declares secret", what)
		assert.False(t, m.Get("settings").AsMap().Get("theme").Secret(), "%s: theme", what)
	}
	assert.True(t, read.Properties.Get("yaml").Secret(), "yaml contains the secrets")
	assert.True(t, read.Properties.Get("sha256").Secret())
	assert.False(t, read.Properties.Get("filePath").Secret())

	// An update (here: forced, without changes) rewrites the same bytes, without `overwrite`
	// and without a backup.
	u := urn(userSettingsToken, "imported")
	updated, err := s.Update(p.UpdateRequest{ID: read.ID, Urn: u, State: read.Properties, OldInputs: read.Inputs, Inputs: read.Inputs})
	require.NoError(t, err)
	assert.True(t, updated.Properties.Get("backupPath").IsNull())
	assert.Equal(t, content, readFile(t, file))
	assertNoFile(t, file+".pulumi-backup")

	// A refresh finds no drift and keeps the secrets.
	refreshed, err := s.Read(p.ReadRequest{ID: read.ID, Urn: u, Properties: updated.Properties, Inputs: read.Inputs})
	require.NoError(t, err)
	assert.Equal(t, content, refreshed.Properties.Get("yaml").AsString())
	assert.True(t, refreshed.Properties.Get("yaml").Secret())
	assert.True(t, refreshed.Inputs.Get("settings").AsMap().Get("hooks").AsMap().Get("sessionStart").AsArray().Get(0).
		AsMap().Get("env").Secret())

	// Pulumi did not create the file, so deleting the resource leaves it in place.
	require.NoError(t, s.Delete(p.DeleteRequest{ID: read.ID, Urn: u, Properties: updated.Properties, OldInputs: read.Inputs}))
	assert.Equal(t, content, readFile(t, file))
}

// TestUserSettingsImportHandWritten imports a file written by hand, with comments, the hook
// and model shorthands docker-agent accepts, and a key a newer docker-agent added. The
// import works and keeps the file as it is; the inputs render docker-agent's form of it.
func TestUserSettingsImportHandWritten(t *testing.T) {
	t.Parallel()
	s := server(t)
	file := filepath.Join(t.TempDir(), "config.yaml")
	const handWritten = `# My docker-agent settings.
default_model: anthropic/claude-sonnet-4-5
aliases:
  pirate: {path: ./pirate.yaml, yolo: true}
settings:
  theme: dracula
  hooks:
    session_end: {type: command, command: ./bye.sh}
  shiny_new_setting: 1
`
	require.NoError(t, os.WriteFile(file, []byte(handWritten), 0o600))
	read := importLocalFile(t, s, userSettingsToken, file)
	in := read.Inputs
	assert.Equal(t, "anthropic", in.Get("defaultModel").AsMap().Get("provider").AsString())
	assert.Equal(t, "claude-sonnet-4-5", in.Get("defaultModel").AsMap().Get("model").AsString())
	assert.True(t, in.Get("aliases").AsMap().Get("pirate").AsMap().Get("yolo").AsBool())
	settings := in.Get("settings").AsMap()
	assert.Equal(t, 1.0, settings.Get("extra").AsMap().Get("shiny_new_setting").AsNumber())
	hook := settings.Get("hooks").AsMap().Get("sessionEnd").AsArray().Get(0).AsMap()
	assert.Equal(t, "./bye.sh", hook.Get("command").AsString())
	assert.Equal(t, "command", hook.Get("type").AsString())
	assert.False(t, in.Get("overwrite").AsBool())
	assert.NotEqual(t, handWritten, read.Properties.Get("yaml").AsString())
	assert.Equal(t, handWritten, readFile(t, file), "an import never writes")
}

func TestUserSettingsImportErrors(t *testing.T) {
	t.Parallel()
	s := server(t)
	dir := t.TempDir()
	invalid := filepath.Join(dir, "invalid.yaml")
	require.NoError(t, os.WriteFile(invalid, []byte("aliases:\n  '-bad': {path: a.yaml}\n"), 0o600))
	untypedHook := filepath.Join(dir, "hook.yaml")
	require.NoError(t, os.WriteFile(untypedHook, []byte("settings:\n  hooks:\n    session_end: {command: ./bye.sh}\n"), 0o600))
	broken := filepath.Join(dir, "broken.yaml")
	require.NoError(t, os.WriteFile(broken, []byte("settings: [1, 2\n"), 0o600))
	for id, want := range map[string]string{
		filepath.Join(dir, "missing.yaml"): "does not exist",
		dir:                                "is a directory",
		invalid:                            "invalid docker-agent user settings",
		broken:                             "parsing " + broken,
		untypedHook:                        "settings.hooks.session_end[0]: type is required",
	} {
		_, err := s.Read(p.ReadRequest{ID: id, Urn: urn(userSettingsToken, "imported")})
		assert.ErrorContains(t, err, want, id)
	}
}

// TestAgentsIgnoreImport adopts an `.agentsignore` by directory and by file: the comment and
// patterns are the inputs that wrote it, and deleting the resource leaves the file in place.
func TestAgentsIgnoreImport(t *testing.T) {
	t.Parallel()
	s := server(t)
	inputs := map[string]any{
		"comment":  "Managed by Pulumi.\n\nKeep secrets away from docker-agent.",
		"patterns": []any{".env", ".env.*", "*.pem", "!public.pem", "build/"},
	}
	original, content := renderLocalFile(t, s, agentsIgnoreToken, "content", inputs)
	dir := t.TempDir()
	file := filepath.Join(dir, ".agentsignore")
	require.NoError(t, os.WriteFile(file, []byte(content), 0o644))

	for _, id := range []string{dir, file, "file://" + dir} {
		read := importLocalFile(t, s, agentsIgnoreToken, id)
		assert.Equal(t, file, read.ID)
		assert.Equal(t, content, read.Properties.Get("content").AsString())
		assert.Equal(t, dir, read.Inputs.Get("directory").AsString())
		assert.True(t, original.Equals(read.Inputs.Delete("directory")),
			"the imported inputs differ from the inputs that rendered the file:\nwant: %v\ngot:  %v", original, read.Inputs)
	}

	// Moving the resource to another directory writes the new file and leaves the adopted
	// one in place.
	read := importLocalFile(t, s, agentsIgnoreToken, dir)
	u := urn(agentsIgnoreToken, "imported")
	other := t.TempDir()
	moved := read.Inputs.Set("directory", property.New(other))
	updated, err := s.Update(p.UpdateRequest{ID: read.ID, Urn: u, State: read.Properties, OldInputs: read.Inputs, Inputs: moved})
	require.NoError(t, err)
	assert.Equal(t, content, readFile(t, file))
	assert.Equal(t, content, readFile(t, filepath.Join(other, ".agentsignore")))

	// The moved file was created by Pulumi, so deleting the resource now removes it.
	require.NoError(t, s.Delete(p.DeleteRequest{ID: read.ID, Urn: u, Properties: updated.Properties, OldInputs: moved}))
	assertNoFile(t, filepath.Join(other, ".agentsignore"))
	assert.Equal(t, content, readFile(t, file))

	// Deleting the imported resource leaves the adopted file.
	require.NoError(t, s.Delete(p.DeleteRequest{ID: read.ID, Urn: u, Properties: read.Properties, OldInputs: read.Inputs}))
	assert.Equal(t, content, readFile(t, file))
}

func TestAgentsIgnoreImportHandWritten(t *testing.T) {
	t.Parallel()
	s := server(t)
	dir := t.TempDir()
	const handWritten = "\n# Secrets\n#   indented note\n\n  .env  \n# certificates\n*.pem\n\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".agentsignore"), []byte(handWritten), 0o644))
	read := importLocalFile(t, s, agentsIgnoreToken, filepath.Join(dir, ".agentsignore"))
	assert.Equal(t, "Secrets\n  indented note", read.Inputs.Get("comment").AsString())
	assert.Equal(t, property.NewArray([]property.Value{property.New(".env"), property.New("*.pem")}), read.Inputs.Get("patterns").AsArray())
	assert.Equal(t, handWritten, readFile(t, filepath.Join(dir, ".agentsignore")))

	invalid := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(invalid, ".agentsignore"), []byte("docs/a**b/*.md\n"), 0o644))
	_, err := s.Read(p.ReadRequest{ID: invalid, Urn: urn(agentsIgnoreToken, "imported")})
	assert.ErrorContains(t, err, "which go-git never matches")

	notIgnore := filepath.Join(dir, "README.md")
	require.NoError(t, os.WriteFile(notIgnore, []byte("hi"), 0o644))
	_, err = s.Read(p.ReadRequest{ID: notIgnore, Urn: urn(agentsIgnoreToken, "imported")})
	assert.ErrorContains(t, err, "is neither a directory nor a .agentsignore file")
	_, err = s.Read(p.ReadRequest{ID: t.TempDir(), Urn: urn(agentsIgnoreToken, "imported")})
	assert.ErrorContains(t, err, "does not exist")
}
