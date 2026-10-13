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

// childExecutable is the provider's own executable for a run child: /proc/self/exe is the
// running binary also when its file was replaced or removed since.
func childExecutable() (string, error) { return "/proc/self/exe", nil }

// protectChild makes the run child not dumpable: its /proc files (environ, fd, mem) belong to
// root, so the processes of the same user that the run starts cannot read Request.Env or the
// request from it, and it never dumps core. The processes it starts are dumpable again.
func protectChild() { _ = unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0) }
