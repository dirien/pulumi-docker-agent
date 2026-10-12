// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package agentrun

import (
	"errors"
	"syscall"

	"golang.org/x/sys/unix"
)

// setPdeathsig makes the kernel send SIGTERM to the CLI when the provider dies, also by
// SIGKILL. That reaches the CLI only: the processes it started get no signal from the
// kernel, and the provider can no longer stop them (harness_unix.go).
func setPdeathsig(a *syscall.SysProcAttr) { a.Pdeathsig = syscall.SIGTERM }

// waitUnreaped waits until the process exited without reaping it, so that its PID keeps
// reserving its process group's ID. It reports false when it could not wait.
func waitUnreaped(pid int) bool {
	for {
		var info unix.Siginfo
		err := unix.Waitid(unix.P_PID, pid, &info, unix.WEXITED|unix.WNOWAIT, nil)
		if !errors.Is(err, unix.EINTR) {
			return err == nil
		}
	}
}
