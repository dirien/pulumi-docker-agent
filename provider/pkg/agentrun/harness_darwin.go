// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package agentrun

import "syscall"

// setPdeathsig does nothing: macOS has no parent-death signal.
func setPdeathsig(*syscall.SysProcAttr) {}

// waitUnreaped does not wait: the group is signalled after the CLI was reaped.
func waitUnreaped(int) bool { return false }
