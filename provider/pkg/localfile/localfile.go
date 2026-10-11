// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

// Package localfile reads, writes and removes the local files managed by the provider's
// file resources (UserSettings and AgentsIgnore). Paths are resolved on the machine that
// runs the Pulumi engine.
package localfile

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// MaxReadSize caps how much of a managed file is read during a refresh. Managed files are
// small configuration files; anything larger is reported as drift without loading it.
const MaxReadSize = 4 << 20

// Resolve turns a user-supplied path into an absolute, cleaned path. A leading `~` or
// `~/` is replaced with the home directory of the user running the provider. Relative
// paths are resolved against the provider's working directory, which Pulumi sets to the
// project directory.
func Resolve(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("path must not be empty")
	}
	if strings.ContainsRune(path, 0) {
		return "", errors.New("path must not contain NUL bytes")
	}
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolving %q: %w", path, err)
		}
		path = filepath.Join(home, path[1:])
	} else if strings.HasPrefix(path, "~") {
		return "", fmt.Errorf("path %q: only `~` and `~/` (the current user's home directory) are supported", path)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolving %q: %w", path, err)
	}
	return abs, nil
}

// Read returns the content of path, following symbolic links. exists is false (and err
// nil) when the file does not exist, including through a dangling link. A directory at
// path is an error. Files larger than MaxReadSize are truncated to MaxReadSize+1 bytes,
// which never equals a rendered document of at most MaxReadSize bytes, so callers detect
// them as drift.
func Read(path string) (content []byte, exists bool, err error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, false, err
	}
	if info.IsDir() {
		return nil, true, fmt.Errorf("%s is a directory", path)
	}
	content, err = io.ReadAll(io.LimitReader(f, MaxReadSize+1))
	if err != nil {
		return nil, true, err
	}
	return content, true, nil
}

// Symbolic links are followed, never replaced: dotfile managers commonly link
// `~/.config/cagent/config.yaml` into a repository, and the link must keep pointing there.
// Target resolves the link chain of the final path element; Write, Remove and Backup
// operate on the file it returns, and Read follows links through os.Open.

// maxLinks bounds the length of a symlink chain, like the kernel's ELOOP limit.
const maxLinks = 40

// Target returns the file that path stands for: path itself, or, when path is a symbolic
// link (or a chain of links), the file the last link points to. The link target does not
// need to exist (a dangling link resolves to the file it names). Directories in the path
// are not resolved: writes go through them.
func Target(path string) (string, error) {
	current := path
	for range maxLinks {
		info, err := os.Lstat(current)
		if errors.Is(err, fs.ErrNotExist) {
			return current, nil
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&fs.ModeSymlink == 0 {
			return current, nil
		}
		dest, err := os.Readlink(current)
		if err != nil {
			return "", err
		}
		if !filepath.IsAbs(dest) {
			dest = filepath.Join(filepath.Dir(current), dest)
		}
		current = filepath.Clean(dest)
	}
	return "", fmt.Errorf("%s: too many levels of symbolic links", path)
}

// Write atomically replaces the file at path with data, following a symbolic link at
// path (the link is kept and its target is replaced). Missing parent directories of the
// target are created with dirMode. The data is written to a temporary file in the
// target's directory whose mode is set before any byte is written, flushed to disk and
// renamed over the target, so readers see either the old or the new content and the file
// is never readable with a wider mode.
func Write(path string, data []byte, mode, dirMode os.FileMode) (err error) {
	target, err := Target(path)
	if err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	dir := filepath.Dir(target)
	if info, statErr := os.Stat(target); statErr == nil && info.IsDir() {
		return fmt.Errorf("%s is a directory", path)
	}
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return fmt.Errorf("creating directory %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(target)+".*.tmp")
	if err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	tmpName := tmp.Name()
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()
	if err = tmp.Chmod(mode); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if _, err = tmp.Write(data); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err = tmp.Sync(); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err = os.Rename(tmpName, target); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// Remove deletes the file at path. When path is a symbolic link, the file it points to is
// deleted and the link is kept (dangling). A missing file is not an error. Directories are
// never removed.
func Remove(path string) error {
	target, err := Target(path)
	if err != nil {
		return err
	}
	info, err := os.Lstat(target)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory; refusing to remove it", path)
	}
	if err := os.Remove(target); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// BackupSuffix is appended to a file's name to form its backup's name.
const BackupSuffix = ".pulumi-backup"

// Backup moves the existing file at path (following a symbolic link) aside to
// `<file>.pulumi-backup` in the same directory and returns the backup's path. It fails,
// without changing anything, when a backup already exists, so an older backup is never
// lost. The backup keeps the file's content and mode.
func Backup(path string) (string, error) {
	target, err := Target(path)
	if err != nil {
		return "", err
	}
	backup := target + BackupSuffix
	if _, err := os.Lstat(backup); err == nil {
		return "", fmt.Errorf("%s already exists; move it away first, it may hold an earlier version of %s",
			backup, path)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	if err := os.Rename(target, backup); err != nil {
		return "", fmt.Errorf("backing up %s: %w", path, err)
	}
	return backup, nil
}

// Restore moves a backup made by Backup back to the file it was taken from, replacing
// whatever is there now. restored is false when the backup no longer exists.
func Restore(backup string) (restored bool, err error) {
	if !strings.HasSuffix(backup, BackupSuffix) {
		return false, fmt.Errorf("%s is not a backup made by the provider", backup)
	}
	if _, err := os.Lstat(backup); errors.Is(err, fs.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	if err := os.Rename(backup, strings.TrimSuffix(backup, BackupSuffix)); err != nil {
		return false, fmt.Errorf("restoring %s: %w", backup, err)
	}
	return true, nil
}
