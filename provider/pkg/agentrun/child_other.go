// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

//go:build !linux && !darwin && !windows

package agentrun

import (
	"os"
	"syscall"
)

// childSysProcAttr starts a run child in the provider's process group.
func childSysProcAttr() *syscall.SysProcAttr { return nil }

// childJob does nothing on this system: what a run child leaves running is not stopped.
type childJob struct{}

func newChildJob(func(string)) childJob { return childJob{} }
func (*childJob) assign(*os.Process)    {}
func (*childJob) finish() int           { return 0 }
func (*childJob) close()                {}

// takeProtocolStdout keeps the standard output for the protocol and makes os.Stdout stderr.
// Without a descriptor of its own, what writes to the descriptor itself still reaches the
// protocol on this system.
func takeProtocolStdout() (*os.File, error) {
	proto := os.Stdout
	os.Stdout = os.Stderr
	return proto, nil
}

// redirectStdin does nothing on this system.
func redirectStdin(*os.File) {}

// writeFD1 writes to the standard output the process started with (a test's stray output).
func writeFD1(s string) { _, _ = syscall.Write(syscall.Stdout, []byte(s)) }
