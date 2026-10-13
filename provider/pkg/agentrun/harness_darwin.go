// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package agentrun

import (
	"os"
	"syscall"
)

// setPdeathsig does nothing: macOS has no parent-death signal.
func setPdeathsig(*syscall.SysProcAttr) {}

// waitUnreaped does not wait: the group is signalled after the CLI was reaped.
func waitUnreaped(int) bool { return false }

// childExecutable is the provider's own executable for a run child.
func childExecutable() (string, error) { return os.Executable() }

// protectChild does nothing: macOS has no per-process dumpable flag (a process of the same
// user can read another's environment block only, which holds no Request.Env, see
// childenv.go).
func protectChild() {}
