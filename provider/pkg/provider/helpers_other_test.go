// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

//go:build !linux && !darwin

package provider

// runHelperModes does nothing: the signal helper runs on Linux and macOS.
func runHelperModes() {}
