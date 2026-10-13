// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"os"
	"testing"

	"github.com/dirien/pulumi-docker-agent/provider/pkg/agentrun"
)

// TestMain makes the test binary an agent run child when an AgentRun starts it as one
// (agentrun.MaybeRunChild), and the signal helper when signal_test.go starts it as one.
func TestMain(m *testing.M) {
	agentrun.MaybeRunChild()
	runHelperModes()
	os.Exit(m.Run())
}
