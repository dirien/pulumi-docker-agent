// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

//go:build !linux && !darwin

package agentrun

import (
	"errors"
	"os/exec"
)

// startHarness fails: coding-harness agents run on Linux and macOS only (harnessSupported
// rejects them before a CLI would start).
func startHarness(*exec.Cmd, func(string)) (*harnessProcess, error) {
	return nil, errors.New(harnessOSMessage())
}

// procTree records the processes of a CLI on Linux and macOS (descendants.go).
type procTree struct{}

func (*harnessProcess) stop() {}
