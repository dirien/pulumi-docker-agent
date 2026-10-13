// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

//go:build linux || darwin

package agentrun

import "os/exec"

// startCodex starts a codex CLI (or one of the checks before the first turn) in a process
// group of its own, supervised as harness_unix.go describes, with the stop that interrupts the
// CLI first (codexSupervision).
func startCodex(cmd *exec.Cmd, debug func(string)) (*harnessProcess, error) {
	return startSupervised(cmd, codexSupervision(debug))
}
