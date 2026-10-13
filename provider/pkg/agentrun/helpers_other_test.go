// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

//go:build !linux && !darwin

package agentrun

// runHelperModes does nothing: the helper processes of the tests run on Linux and macOS.
func runHelperModes() {}
