// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

//go:build !linux && !darwin && !freebsd && !netbsd && !openbsd && !dragonfly && !windows

package agentrun

import (
	"context"
	"time"
)

// lockFile takes no lock on systems without flock or LockFileEx: the ChatGPT login's
// compare-and-swap still keeps a login another process wrote.
func lockFile(context.Context, string, time.Duration) (unlock func(), err error) {
	return func() {}, nil
}
