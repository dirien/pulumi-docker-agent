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

// redirectStdHandles does nothing on this system.
func redirectStdHandles(*os.File) {}
