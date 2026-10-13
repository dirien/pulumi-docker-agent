// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

//go:build linux || darwin

package agentrun

// runHelperModes runs the test binary as one of the tests' helper processes when its
// environment says so: the harness helper (harness_test.go) or the run helper
// (child_unix_test.go).
func runHelperModes() {
	runHarnessHelperMode()
	runRunHelperMode()
}
