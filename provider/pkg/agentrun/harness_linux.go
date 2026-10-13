// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package agentrun

import (
	"errors"
	"os"
	"path/filepath"
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

// childExecutables are the provider's own executable for a run child, in the order to try:
// /proc/self/exe is the running binary also when its file was replaced or removed since; where
// it cannot be used (/proc is not mounted, or exec of it is denied), the binary's path.
func childExecutables() ([]string, error) {
	exes := []string{"/proc/self/exe"}
	if exe, err := os.Executable(); err == nil {
		exes = append(exes, exe)
	} else if filepath.IsAbs(os.Args[0]) {
		// Without /proc, os.Executable cannot tell; Pulumi starts a plugin by its absolute path.
		exes = append(exes, os.Args[0])
	}
	return exes, nil
}

// protectChild makes the run child not dumpable: its /proc files (environ, fd, mem) belong to
// root, so the processes of the same user that the run starts cannot read Request.Env or the
// request from it, and it never dumps core. The processes it starts are dumpable again.
func protectChild() { _ = unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0) }
