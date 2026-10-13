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
	"strconv"
	"strings"
	"sync"
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
	// runHelperCancelEnv makes the SIGINT cancel the run's context too, as Pulumi cancels the
	// operation before it stops the provider.
	runHelperCancelEnv = "AGENTRUN_TEST_RUN_HELPER_CANCEL"
	// runHelperClaudeEnv makes the helper run the claude-code agent (the fake from PATH)
	// instead of a hanging run.
	runHelperClaudeEnv = "AGENTRUN_TEST_RUN_HELPER_CLAUDE"
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
	if os.Getenv(runHelperClaudeEnv) != "" {
		req = Request{Name: "helper", YAML: []byte(claudeAgent), Prompt: "Write NOTES.md", InheritEnvironment: true,
			AllowHarnessAgents: true}
	}
	runCtx := context.Background()
	if os.Getenv(runHelperCancelEnv) != "" {
		runCtx = ctx
	}
	done := make(chan error, 1)
	go func() {
		_, err := Run(runCtx, req)
		done <- err
	}()
	var err error
	select {
	case <-ctx.Done():
		select {
		case err = <-done:
		case <-time.After(time.Minute):
			return 4
		}
	case err = <-done:
		if ctx.Err() == nil {
			fmt.Fprintln(os.Stderr, "run helper: the run ended early:", err)
			return 3
		}
	}
	fmt.Fprintln(os.Stderr, "run helper: the run ended:", err)
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

// TestChildShutdownWinsOverCancel: Pulumi cancels the operation and stops the provider (in
// either order); the run ends with the shutdown's error, not the cancellation's (review
// finding 9).
func TestChildShutdownWinsOverCancel(t *testing.T) {
	cmd, dir, stderr, child := startRunHelper(t, runHelperCancelEnv+"=1")
	require.NoError(t, cmd.Process.Signal(syscall.SIGINT))
	require.NoError(t, waitHelper(t, cmd), "%s", stderr)
	assert.Contains(t, []string{"Pulumi is stopping the provider", "the provider canceled the agent run"},
		readCause(t, filepath.Join(dir, "cause")))
	assert.True(t, gone(child), "the child is gone before the provider exits")
	assert.Contains(t, stderr.String(), "run helper: the run ended: "+errStoppedForShutdown.Error())
}

// TestChildShutdownStopsClaude: SIGINT through the whole chain, provider → run child → Claude
// Code CLI (review finding 8). The CLI and the command it runs in a session of its own ignore
// SIGTERM, so they need the SIGKILL of the run child's own supervisor, which the provider's
// backstop cuts by killing the run child at the same time: the backstop then finds them by the
// run child's cookie. All of them are gone before the provider exits, within the 5 s Pulumi
// gives it.
func TestChildShutdownStopsClaude(t *testing.T) {
	f := useFakeClaude(t)
	t.Setenv("FAKE_CLAUDE_IGNORE_TERM", "1")
	exe, err := os.Executable()
	require.NoError(t, err)
	cmd := exec.Command(exe, "-test.run=^$")
	cmd.Env = append(os.Environ(), runHelperEnv+"="+t.TempDir(), runHelperClaudeEnv+"=1", "FAKE_CLAUDE_MODE=hang-detached")
	stderr := &strings.Builder{}
	cmd.Stderr = stderr
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	detached := waitPID(t, f.pidfile)
	killAtCleanup(t, detached)
	leader, err := strconv.Atoi(f.invocations(t)[0].get("pid"))
	require.NoError(t, err)
	killAtCleanup(t, leader)
	e, ok := readProc(leader)
	require.True(t, ok, "the CLI runs")
	child := e.ppid
	killAtCleanup(t, child)
	require.NotEqual(t, cmd.Process.Pid, child, "the CLI runs under the run child, not the provider")
	pe, ok := readProc(child)
	require.True(t, ok)
	require.Equal(t, cmd.Process.Pid, pe.ppid, "the run child runs under the provider")

	start := time.Now()
	require.NoError(t, cmd.Process.Signal(syscall.SIGINT))
	require.NoError(t, waitHelper(t, cmd), "the provider shuts down as the SDK makes it, with exit status 0: %s", stderr)
	took := time.Since(start)
	assert.True(t, gone(leader), "the CLI is gone before the provider exits")
	assert.True(t, gone(detached), "so is the command it runs in a session of its own")
	assert.True(t, gone(child), "and the run child")
	assert.Less(t, took, 5*time.Second, "within the time Pulumi gives the provider before SIGKILL")
	assert.Contains(t, stderr.String(), "run helper: the run ended: "+errStoppedForShutdown.Error())
}

// TestChildProtocolIsNotInherited: the processes a run child starts hold neither its protocol
// nor its request descriptor (both are closed on exec), so the end of the protocol follows the
// child's exit (review finding 2).
func TestChildProtocolIsNotInherited(t *testing.T) {
	if goruntime.GOOS != "linux" {
		t.Skip("reads /proc")
	}
	file := filepath.Join(t.TempDir(), "fds")
	srv := fakeopenai.New(callOnce("shell", `{"cmd":"for f in /proc/$$/fd/*; do readlink $f; done"}`))
	defer srv.Close()
	req := request(srv, shellAgent(""), "go")
	req.ToolApproval = ToolApprovalAllow
	req.test = &childFaults{ProtocolFile: file}
	res, err := Run(t.Context(), req)
	require.NoError(t, err)
	b, err := os.ReadFile(file)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	require.Len(t, lines, 2, "%s", b)
	assert.Contains(t, res.Output, "pipe:[", "the shell lists its own pipes: %s", res.Output)
	for _, line := range lines {
		_, target, _ := strings.Cut(line, " ")
		require.True(t, strings.HasPrefix(target, "pipe:["), "%q", line)
		assert.NotContains(t, res.Output, target, "the shell holds the run child's %s", line)
	}
}

// TestChildExecutableFallback: a run child starts from the next executable when one cannot be
// started (on Linux /proc/self/exe, then the binary's path), and when none can, the error says
// that the binary is gone or cannot be executed (review finding 5).
func TestChildExecutableFallback(t *testing.T) {
	dir := t.TempDir()
	missing, notExec := filepath.Join(dir, "missing"), filepath.Join(dir, "not-executable")
	require.NoError(t, os.WriteFile(notExec, []byte("#!/bin/sh\n"), 0o600))
	newCmd := func(exe string) *exec.Cmd { return exec.Command(exe, "-c", "exit 0") }
	var mu sync.Mutex
	var debug []string
	record := func(msg string) {
		mu.Lock()
		defer mu.Unlock()
		debug = append(debug, msg)
	}
	proc, err := startRunChildFrom([]string{missing, "/bin/sh"}, newCmd, func() {}, record)
	require.NoError(t, err)
	<-proc.done()
	mu.Lock()
	assert.Contains(t, strings.Join(debug, "\n"), "starting the agent run process with "+missing+": ")
	mu.Unlock()
	for _, exe := range []string{missing, notExec} {
		_, err := startRunChildFrom([]string{exe}, newCmd, func() {}, func(string) {})
		require.Error(t, err)
		assert.True(t, strings.HasPrefix(err.Error(), "the provider binary "+exe+" cannot be started for the agent run: "),
			err.Error())
		assert.True(t, strings.HasSuffix(err.Error(), "; was the plugin upgraded or removed while Pulumi ran?"), err.Error())
	}

	exes, err := childExecutables()
	require.NoError(t, err)
	self, err := os.Executable()
	require.NoError(t, err)
	if goruntime.GOOS == "linux" {
		assert.Equal(t, []string{"/proc/self/exe", self}, exes)
	} else {
		assert.Equal(t, []string{self}, exes)
	}
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

// TestChildIsNotDumpable: on Linux the processes a run starts (of the same user) cannot read
// the run child's /proc files, while they can read their own (review finding 8).
func TestChildIsNotDumpable(t *testing.T) {
	if goruntime.GOOS != "linux" {
		t.Skip("PR_SET_DUMPABLE is Linux-only")
	}
	if os.Geteuid() == 0 {
		t.Skip("root reads the /proc files of every process, dumpable or not")
	}
	// The shell's parent is the run child.
	srv := fakeopenai.New(callOnce("shell", `{"cmd":"cat /proc/$$/environ >/dev/null && echo own=readable; `+
		`cat /proc/$PPID/environ >/dev/null; echo child=$?"}`))
	defer srv.Close()
	req := request(srv, shellAgent(""), "go")
	req.ToolApproval = ToolApprovalAllow
	res, err := Run(t.Context(), req)
	require.NoError(t, err)
	assert.Contains(t, res.Output, "own=readable", "the shell itself is dumpable: %s", res.Output)
	assert.Contains(t, res.Output, "environ: Permission denied", "/proc/<child>/environ: %s", res.Output)
	assert.Contains(t, res.Output, "child=1")
	assert.NotContains(t, res.Output, fakeKey)
}
