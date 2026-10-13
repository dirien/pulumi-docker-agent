// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

//go:build linux || darwin

package agentrun

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dirien/pulumi-docker-agent/provider/pkg/agentrun/fakeopenai"
)

// The lifecycle of a run child on Linux and macOS: a provider-like helper (the test binary
// again) runs a hanging run, and the tests signal or kill the helper.

const (
	// runHelperEnv runs the test binary as the run helper (runRunHelper), in that directory.
	runHelperEnv = "AGENTRUN_TEST_RUN_HELPER"
	// runHelperStrayEnv makes the hanging child write this after its run was canceled.
	runHelperStrayEnv = "AGENTRUN_TEST_RUN_HELPER_STRAY"
	// runHelperLateEnv makes the helper try another run after the SIGINT.
	runHelperLateEnv = "AGENTRUN_TEST_RUN_HELPER_LATE"
)

func runRunHelperMode() {
	if dir := os.Getenv(runHelperEnv); dir != "" {
		os.Exit(runRunHelper(dir))
	}
}

// runRunHelper behaves like the provider binary: the Pulumi SDK's handler ends it with exit
// status 0 on SIGINT, after the operation in flight (a run whose child hangs until it is
// canceled) ended. It reports how the run ended on stderr.
func runRunHelper(dir string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	req := Request{Name: "helper", YAML: []byte(rootAgent), Prompt: "hi", test: &childFaults{Hang: true,
		CauseFile: filepath.Join(dir, "cause"), PIDFile: filepath.Join(dir, "pid"), Stray: os.Getenv(runHelperStrayEnv)}}
	done := make(chan error, 1)
	go func() {
		_, err := Run(context.Background(), req)
		done <- err
	}()
	select {
	case <-ctx.Done():
	case err := <-done:
		fmt.Fprintln(os.Stderr, "run helper: the run ended early:", err)
		return 3
	}
	select {
	case err := <-done:
		fmt.Fprintln(os.Stderr, "run helper: the run ended:", err)
	case <-time.After(time.Minute):
		return 4
	}
	if os.Getenv(runHelperLateEnv) != "" {
		_, err := Run(context.Background(), req)
		fmt.Fprintln(os.Stderr, "run helper: a run after SIGINT:", err)
	}
	return 0
}

// startRunHelper starts the run helper and returns it with its directory, its stderr (once
// it ended) and the PID of its run child.
func startRunHelper(t *testing.T, env ...string) (cmd *exec.Cmd, dir string, stderr *strings.Builder, child int) {
	t.Helper()
	exe, err := os.Executable()
	require.NoError(t, err)
	dir = t.TempDir()
	cmd = exec.Command(exe, "-test.run=^$")
	cmd.Env = append(append(os.Environ(), runHelperEnv+"="+dir), env...)
	stderr = &strings.Builder{}
	cmd.Stderr = stderr
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	child = waitPID(t, filepath.Join(dir, "pid"))
	killAtCleanup(t, child)
	return cmd, dir, stderr, child
}

// TestChildShutdown: when Pulumi stops the provider (SIGINT), the run child gets SIGINT, stops,
// and the run ends with the shutdown's error before the provider exits; no run starts after.
func TestChildShutdown(t *testing.T) {
	cmd, dir, stderr, child := startRunHelper(t, runHelperLateEnv+"=1")
	start := time.Now()
	require.NoError(t, cmd.Process.Signal(syscall.SIGINT))
	require.NoError(t, waitHelper(t, cmd), "the provider shuts down as the SDK makes it, with exit status 0: %s", stderr)
	assert.Less(t, time.Since(start), signalGrace, "the child stopped on SIGINT")
	assert.Equal(t, "Pulumi is stopping the provider", readCause(t, filepath.Join(dir, "cause")))
	assert.True(t, gone(child), "the child is gone before the provider exits")
	assert.Contains(t, stderr.String(), "run helper: the run ended: "+errStoppedForShutdown.Error())
	assert.Contains(t, stderr.String(), "run helper: a run after SIGINT: the provider is shutting down (Pulumi asked it "+
		"to stop), so no agent run starts")
}

// TestChildProviderDeath: when the provider dies (SIGKILL), its run child cancels the run: the
// end of its stdin (and on Linux the parent-death signal) tells it. Writing to its broken
// stderr and stdout afterwards does not end it (SIGPIPE), so it gets to stop its tools.
func TestChildProviderDeath(t *testing.T) {
	cmd, dir, _, child := startRunHelper(t, runHelperStrayEnv+"=written after the provider died")
	require.NoError(t, cmd.Process.Kill())
	_ = waitHelper(t, cmd)
	cause := readCause(t, filepath.Join(dir, "cause"))
	if goruntime.GOOS == "linux" {
		assert.Contains(t, []string{"the provider ended", "the provider canceled the agent run"}, cause,
			"stdin's end or the parent-death signal")
	} else {
		assert.Equal(t, "the provider ended", cause, "stdin's end")
	}
	assert.Eventually(t, func() bool { return gone(child) }, 5*time.Second, 20*time.Millisecond, "the child ended")
}

// TestChildBackstopStopsWhatToolsLeftRunning: a command a shell tool left running in the
// background (its parent shell exited at once) is found by the run child's cookie, which the
// processes AgentRun controls keep in both modes, and stopped when the run ends, with a warning.
func TestChildBackstopStopsWhatToolsLeftRunning(t *testing.T) {
	for _, inherit := range []bool{false, true} {
		t.Run(fmt.Sprintf("inheritEnvironment %v", inherit), func(t *testing.T) {
			pidfile := filepath.Join(t.TempDir(), "pid")
			srv := fakeopenai.New(callOnce("shell", fmt.Sprintf(`{"cmd":"sleep 300 >/dev/null 2>&1 & echo $! > %s"}`,
				pidfile)))
			defer srv.Close()
			req := request(srv, shellAgent(""), "go")
			req.ToolApproval = ToolApprovalAllow
			req.InheritEnvironment = inherit
			res, err := Run(t.Context(), req)
			require.NoError(t, err)
			pid := waitPID(t, pidfile)
			killAtCleanup(t, pid)
			assertGone(t, []int{pid}, "the command is gone when Run returns")
			assert.Equal(t, []string{childLeftRunningWarning(1)}, res.Warnings)
		})
	}
}

// TestChildTrackingProblemsAreLogged: the tracker of a run child says what it tracks.
func TestChildTrackingProblemsAreLogged(t *testing.T) {
	failProcTable(t, errors.New("no table"))
	srv := fakeopenai.New(nil)
	defer srv.Close()
	var debug debugLog
	req := request(srv, rootAgent, "hi")
	req.Log = debug.log
	res, err := Run(t.Context(), req)
	require.NoError(t, err)
	assert.Empty(t, res.Warnings)
	assert.Contains(t, debug.String(), "tracking the processes of the agent run: reading the process table: no table")
}

// TestChildIsNotDumpable: on Linux the processes a run starts cannot read the run child's
// /proc files, and its exec environment holds no env value anyway.
func TestChildIsNotDumpable(t *testing.T) {
	if goruntime.GOOS != "linux" {
		t.Skip("PR_SET_DUMPABLE is Linux-only")
	}
	srv := fakeopenai.New(callOnce("shell", `{"cmd":"cat /proc/$PPID/environ; echo; ls -l /proc/$PPID/environ"}`))
	defer srv.Close()
	req := request(srv, shellAgent(""), "go")
	req.ToolApproval = ToolApprovalAllow
	res, err := Run(t.Context(), req)
	require.NoError(t, err)
	assert.NotContains(t, res.Output, fakeKey)
	assert.Contains(t, res.Output, "root", "/proc/<child>/environ belongs to root: %s", res.Output)
}
