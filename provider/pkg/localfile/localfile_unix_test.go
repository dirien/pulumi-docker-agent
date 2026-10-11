// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

//go:build unix

package localfile

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteHonoursModeDespiteUmask(t *testing.T) { //nolint:paralleltest // umask is process-wide.
	old := syscall.Umask(0o077)
	t.Cleanup(func() { syscall.Umask(old) })
	path := filepath.Join(t.TempDir(), ".agentsignore")
	require.NoError(t, Write(path, []byte("x\n"), 0o644, 0o755))
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o644), info.Mode().Perm())
}

func symlink(t *testing.T, target, link string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(link), 0o755))
	require.NoError(t, os.Symlink(target, link))
}

func isSymlink(t *testing.T, path string) bool {
	t.Helper()
	info, err := os.Lstat(path)
	require.NoError(t, err)
	return info.Mode()&os.ModeSymlink != 0
}

func TestTarget(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	realFile := filepath.Join(dir, "dotfiles", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(realFile), 0o755))
	require.NoError(t, os.WriteFile(realFile, []byte("x"), 0o600))

	plain := filepath.Join(dir, "plain.yaml")
	got, err := Target(plain)
	require.NoError(t, err)
	assert.Equal(t, plain, got, "a missing file is its own target")

	abs := filepath.Join(dir, "abs.yaml")
	symlink(t, realFile, abs)
	rel := filepath.Join(dir, "home", "rel.yaml")
	symlink(t, "../dotfiles/config.yaml", rel)
	chain := filepath.Join(dir, "chain.yaml")
	symlink(t, rel, chain)
	for _, link := range []string{abs, rel, chain} {
		got, err := Target(link)
		require.NoError(t, err)
		assert.Equal(t, realFile, got, link)
	}

	dangling := filepath.Join(dir, "dangling.yaml")
	symlink(t, "dotfiles/missing.yaml", dangling)
	got, err = Target(dangling)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "dotfiles", "missing.yaml"), got)

	loop := filepath.Join(dir, "loop")
	symlink(t, loop, loop)
	_, err = Target(loop)
	assert.ErrorContains(t, err, "too many levels of symbolic links")
}

// TestWriteKeepsSymlink is the regression test for dotfile managers: writing through a
// link replaces the file it points to and keeps the link.
func TestWriteKeepsSymlink(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	realFile := filepath.Join(dir, "dotfiles", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(realFile), 0o755))
	require.NoError(t, os.WriteFile(realFile, []byte("old: true\n"), 0o644))
	link := filepath.Join(dir, "home", ".config", "cagent", "config.yaml")
	symlink(t, realFile, link)

	require.NoError(t, Write(link, []byte("new: true\n"), 0o600, 0o700))
	assert.True(t, isSymlink(t, link), "the link must survive the write")
	data, err := os.ReadFile(realFile)
	require.NoError(t, err)
	assert.Equal(t, "new: true\n", string(data))
	info, err := os.Stat(realFile)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	entries, err := os.ReadDir(filepath.Dir(realFile))
	require.NoError(t, err)
	assert.Len(t, entries, 1, "no temporary file is left next to the target")

	content, exists, err := Read(link)
	require.NoError(t, err)
	assert.True(t, exists)
	assert.Equal(t, "new: true\n", string(content))

	// A dangling link: the file it names is created, the link is kept.
	missing := filepath.Join(dir, "dotfiles", "agentsignore")
	dangling := filepath.Join(dir, "project", ".agentsignore")
	symlink(t, missing, dangling)
	_, exists, err = Read(dangling)
	require.NoError(t, err)
	assert.False(t, exists)
	require.NoError(t, Write(dangling, []byte("*.key\n"), 0o644, 0o755))
	assert.True(t, isSymlink(t, dangling))
	data, err = os.ReadFile(missing)
	require.NoError(t, err)
	assert.Equal(t, "*.key\n", string(data))
}

func TestRemoveKeepsSymlink(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	realFile := filepath.Join(dir, "realFile.yaml")
	require.NoError(t, os.WriteFile(realFile, []byte("x"), 0o600))
	link := filepath.Join(dir, "link.yaml")
	symlink(t, realFile, link)

	require.NoError(t, Remove(link))
	_, err := os.Stat(realFile)
	assert.True(t, os.IsNotExist(err), "the file the link points to is removed")
	assert.True(t, isSymlink(t, link), "the link itself is kept")
	require.NoError(t, Remove(link), "removing through a dangling link is a no-op")

	dirLink := filepath.Join(dir, "dirlink")
	symlink(t, dir, dirLink)
	assert.ErrorContains(t, Remove(dirLink), "refusing to remove")
}

func TestBackupFollowsSymlink(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	realFile := filepath.Join(dir, "dotfiles", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(realFile), 0o755))
	require.NoError(t, os.WriteFile(realFile, []byte("mine\n"), 0o644))
	link := filepath.Join(dir, "config.yaml")
	symlink(t, realFile, link)

	backup, err := Backup(link)
	require.NoError(t, err)
	assert.Equal(t, realFile+BackupSuffix, backup, "the backup sits next to the file the link points to")
	assert.True(t, isSymlink(t, link))
	require.NoError(t, Write(link, []byte("managed\n"), 0o600, 0o700))

	restored, err := Restore(backup)
	require.NoError(t, err)
	assert.True(t, restored)
	data, err := os.ReadFile(link)
	require.NoError(t, err)
	assert.Equal(t, "mine\n", string(data))
	info, err := os.Stat(realFile)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o644), info.Mode().Perm(), "the original mode comes back")
	assert.True(t, isSymlink(t, link))
}
