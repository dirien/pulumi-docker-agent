// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package agentrun

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"syscall"
	"time"
)

// lockFile takes an exclusive advisory lock (flock) on path, creating the file, and waits at most
// timeout for another process to release it. unlock releases it. The lock is released when the
// process ends too.
func lockFile(ctx context.Context, path string, timeout time.Duration) (unlock func(), err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // a lock file next to the login
	if err != nil {
		return nil, err
	}
	fd := int(f.Fd()) //nolint:gosec // a file descriptor fits an int
	deadline := time.Now().Add(timeout)
	for {
		err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() {
				_ = syscall.Flock(fd, syscall.LOCK_UN)
				_ = f.Close()
			}, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EINTR) {
			_ = f.Close()
			return nil, err
		}
		if time.Now().After(deadline) {
			_ = f.Close()
			return nil, fmt.Errorf("another process held %s for more than %s", path, timeout)
		}
		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// lockUnavailable reports an error of lockFile that says the lock cannot be had at all, rather
// than that another process holds it: a lock file that cannot be created or opened (EACCES,
// EPERM, EROFS: a read-only directory), or a file system without flock (ENOLCK, EOPNOTSUPP,
// ENOTSUP, ENOSYS: some NFS, SMB and FUSE mounts).
func lockUnavailable(err error) bool {
	return errors.Is(err, fs.ErrPermission) || errors.Is(err, errors.ErrUnsupported) ||
		errors.Is(err, syscall.EROFS) || errors.Is(err, syscall.ENOLCK)
}
