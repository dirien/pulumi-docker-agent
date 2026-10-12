// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

//go:build linux || darwin

package provider

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"

	p "github.com/pulumi/pulumi-go-provider"
	pulumirpc "github.com/pulumi/pulumi/sdk/v3/proto/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// signalHelperEnv runs the test binary as a helper that sends itself SIGINT, after building
// the provider ("build") or after the engine configured it ("configure").
const signalHelperEnv = "PROVIDER_TEST_SIGNAL_HELPER"

func TestMain(m *testing.M) {
	if mode := os.Getenv(signalHelperEnv); mode != "" {
		os.Exit(runSignalHelper(mode))
	}
	os.Exit(m.Run())
}

// runSignalHelper has no SIGINT handler of its own, so a SIGINT ends it unless the provider
// installed one.
func runSignalHelper(mode string) int {
	prov := Provider()
	if mode == "configure" {
		srv, err := p.RawServer(Name, "0.1.2", prov)(nil)
		if err != nil {
			return 2
		}
		if _, err := srv.Configure(context.Background(), &pulumirpc.ConfigureRequest{AcceptSecrets: true}); err != nil {
			return 2
		}
	}
	_ = syscall.Kill(os.Getpid(), syscall.SIGINT)
	time.Sleep(time.Second)
	return 0
}

// TestProviderWatchesSIGINTOnlyOnceConfigured: the provider binary must not observe SIGINT
// before the Pulumi SDK installed its own handler, which ends the provider (a SIGINT then
// would be swallowed), and must observe it once the engine configured the provider, to stop
// the Claude Code CLIs.
func TestProviderWatchesSIGINTOnlyOnceConfigured(t *testing.T) {
	exe, err := os.Executable()
	require.NoError(t, err)
	run := func(mode string) error {
		cmd := exec.Command(exe, "-test.run=^$")
		cmd.Env = append(os.Environ(), signalHelperEnv+"="+mode)
		return cmd.Run()
	}

	err = run("build")
	var exit *exec.ExitError
	require.True(t, errors.As(err, &exit), "building the provider installs no SIGINT handler: %v", err)
	ws, ok := exit.Sys().(syscall.WaitStatus)
	require.True(t, ok)
	assert.True(t, ws.Signaled() && ws.Signal() == syscall.SIGINT, "%v", err)

	assert.NoError(t, run("configure"), "Configure starts the SIGINT watcher")
}
