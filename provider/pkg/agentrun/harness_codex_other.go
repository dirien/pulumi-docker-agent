// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

//go:build !linux && !darwin

package agentrun

import (
	"errors"
	"os/exec"
)

// startCodex fails: coding-harness agents run on Linux and macOS only (harnessSupported
// rejects them before a CLI would start).
func startCodex(*exec.Cmd, func(string)) (*harnessProcess, error) {
	return nil, errors.New(harnessOSMessage())
}
