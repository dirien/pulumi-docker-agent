// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package agentrun

import (
	"errors"
	"syscall"

	"golang.org/x/sys/unix"
)

// setPdeathsig makes the kernel send SIGTERM to the CLI when the provider dies, also by
// SIGKILL. The CLI then shuts down and stops the shell commands it tracks.
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
