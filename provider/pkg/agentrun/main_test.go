// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package agentrun

import (
	"os"
	"testing"
)

// TestMain makes the test binary a run child when Run starts it as one (child.go), and a
// helper process when a test starts it as one (harness_test.go, child_test.go).
func TestMain(m *testing.M) {
	MaybeRunChild()
	runHelperModes()
	os.Exit(m.Run())
}
