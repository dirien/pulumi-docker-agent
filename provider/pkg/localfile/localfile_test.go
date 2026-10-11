// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package localfile

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolve(t *testing.T) {
	t.Parallel()
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	cwd, err := os.Getwd()
	require.NoError(t, err)

	cases := map[string]string{
		"~":                            home,
		"~/.config/cagent/config.yaml": filepath.Join(home, ".config", "cagent", "config.yaml"),
		"rel/dir/../file":              filepath.Join(cwd, "rel", "file"),
		"/abs/./x":                     "/abs/x",
	}
	for in, want := range cases {
		got, err := Resolve(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}
	for in, msg := range map[string]string{
		"":            "must not be empty",
		"  ":          "must not be empty",
		"~bob/x":      "only `~` and `~/`",
		"a\x00b":      "NUL",
		"~other/conf": "only `~` and `~/`",
	} {
		_, err := Resolve(in)
		assert.ErrorContains(t, err, msg, "%q", in)
	}
}

func TestWrite(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "a", "b")
	path := filepath.Join(dir, "config.yaml")

	require.NoError(t, Write(path, []byte("one\n"), 0o600, 0o700))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "one\n", string(data))
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
		dinfo, err := os.Stat(dir)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o700), dinfo.Mode().Perm())
	}

	// Replacing tightens a looser mode and leaves no temporary files behind.
	require.NoError(t, os.Chmod(path, 0o644))
	require.NoError(t, Write(path, []byte("two\n"), 0o600, 0o700))
	data, err = os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "two\n", string(data))
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)

	// A directory at the target is never replaced.
	err = Write(dir, []byte("x"), 0o600, 0o700)
	assert.ErrorContains(t, err, "is a directory")
}

func TestRead(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, exists, err := Read(filepath.Join(dir, "missing"))
	require.NoError(t, err)
	assert.False(t, exists)

	_, _, err = Read(dir)
	assert.ErrorContains(t, err, "is a directory")

	path := filepath.Join(dir, "f")
	require.NoError(t, os.WriteFile(path, []byte("hello"), 0o600))
	data, exists, err := Read(path)
	require.NoError(t, err)
	assert.True(t, exists)
	assert.Equal(t, "hello", string(data))

	big := filepath.Join(dir, "big")
	require.NoError(t, os.WriteFile(big, []byte(strings.Repeat("x", MaxReadSize+10)), 0o600))
	data, _, err = Read(big)
	require.NoError(t, err)
	assert.Len(t, data, MaxReadSize+1)
}

func TestRemove(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, Remove(filepath.Join(dir, "missing")))
	path := filepath.Join(dir, "f")
	require.NoError(t, os.WriteFile(path, []byte("x"), 0o600))
	require.NoError(t, Remove(path))
	_, err := os.Stat(path)
	assert.True(t, os.IsNotExist(err))
	assert.ErrorContains(t, Remove(dir), "refusing to remove")
}

func TestBackupRestore(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("mine\n"), 0o600))

	backup, err := Backup(path)
	require.NoError(t, err)
	assert.Equal(t, path+BackupSuffix, backup)
	_, err = os.Stat(path)
	assert.True(t, os.IsNotExist(err), "the file is moved, not copied")

	// An existing backup is never replaced.
	require.NoError(t, os.WriteFile(path, []byte("second\n"), 0o600))
	_, err = Backup(path)
	assert.ErrorContains(t, err, "already exists")
	data, err := os.ReadFile(backup)
	require.NoError(t, err)
	assert.Equal(t, "mine\n", string(data))

	restored, err := Restore(backup)
	require.NoError(t, err)
	assert.True(t, restored)
	data, err = os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "mine\n", string(data), "the backup replaces the current file")

	restored, err = Restore(backup)
	require.NoError(t, err)
	assert.False(t, restored, "a missing backup is reported, not an error")
	_, err = Restore(path)
	assert.ErrorContains(t, err, "not a backup")
}
