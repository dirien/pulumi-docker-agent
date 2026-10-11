// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

//go:build unix

package provider

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func requireSymlink(t *testing.T, path string) {
	t.Helper()
	info, err := os.Lstat(path)
	require.NoError(t, err)
	require.NotZero(t, info.Mode()&os.ModeSymlink, "%s must still be a symbolic link", path)
}

// TestUserSettingsSymlinkedConfig is the regression test for dotfile managers that link
// ~/.config/cagent/config.yaml into a repository: the link is kept, the repository file
// is written, refreshed, backed up and restored.
func TestUserSettingsSymlinkedConfig(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	dotfile := filepath.Join(dir, "dotfiles", "cagent.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(dotfile), 0o755))
	require.NoError(t, os.WriteFile(dotfile, []byte(existingConfig), 0o644))
	link := filepath.Join(dir, "home", ".config", "cagent", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(link), 0o700))
	require.NoError(t, os.Symlink(dotfile, link))
	l := newFileLifecycle(t, userSettingsToken)
	in := map[string]any{"path": link, "settings": map[string]any{"sound": true}}

	// The conflict check reads through the link.
	assert.Contains(t, failureText(l.checkRaw(in).Failures), link+" already exists with different content")

	in["overwrite"] = true
	out := l.create(l.check(in), false)
	requireSymlink(t, link)
	assert.Equal(t, link, out.Get("filePath").AsString())
	assert.Equal(t, dotfile+".pulumi-backup", out.Get("backupPath").AsString())
	assert.Equal(t, "version: v1\nsettings:\n  sound: true\n", readFile(t, dotfile))

	// Refresh reads through the link: no drift, then drift after an edit of the dotfile.
	assert.Equal(t, readFile(t, dotfile), l.refresh().Properties.Get("yaml").AsString())
	require.NoError(t, os.WriteFile(dotfile, []byte("version: v1\n"), 0o600))
	assert.Equal(t, "version: v1\n", l.refresh().Properties.Get("yaml").AsString())
	l.update(l.check(in), false)
	requireSymlink(t, link)
	assert.Contains(t, readFile(t, dotfile), "sound: true")

	// Delete restores the dotfile's original content and keeps the link.
	l.delete()
	requireSymlink(t, link)
	assert.Equal(t, existingConfig, readFile(t, dotfile))
	assertMode(t, dotfile, 0o644)
	assertNoFile(t, dotfile+".pulumi-backup")
}

// TestAgentsIgnoreDanglingSymlink: a link to a file that does not exist yet. The provider
// creates the file it points to, and delete removes only that file, never the link.
func TestAgentsIgnoreDanglingSymlink(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "shared", "agentsignore")
	project := filepath.Join(dir, "project")
	require.NoError(t, os.MkdirAll(project, 0o755))
	link := filepath.Join(project, ".agentsignore")
	require.NoError(t, os.Symlink("../shared/agentsignore", link))
	l := newFileLifecycle(t, agentsIgnoreToken)

	out := l.create(l.check(map[string]any{"directory": project, "patterns": []any{".env"}}), false)
	_, hasBackup := out.GetOk("backupPath")
	assert.False(t, hasBackup)
	requireSymlink(t, link)
	assert.Equal(t, ".env\n", readFile(t, target))

	l.delete()
	requireSymlink(t, link)
	assertNoFile(t, target)
}
