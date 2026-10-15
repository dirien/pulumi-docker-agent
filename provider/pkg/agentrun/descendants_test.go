// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

//go:build linux || darwin

package agentrun

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// The tests of descendants.go: what a Claude Code CLI starts in sessions of its own (as
// Claude Code runs its shell commands) is stopped on every end of a run, and nothing else is.

// failProcTable makes every read of the process table fail, in this process and in run
// children (childtest.go).
func failProcTable(t *testing.T, err error) {
	t.Helper()
	prev, prevFault := readProcTable, testFaults.procTableError
	readProcTable = func(uint64) ([]procEntry, error) { return nil, err }
	testFaults.procTableError = err.Error()
	t.Cleanup(func() { readProcTable, testFaults.procTableError = prev, prevFault })
}

// fakeProcTable makes the process table the given entries.
func fakeProcTable(t *testing.T, entries ...procEntry) {
	t.Helper()
	prev := readProcTable
	readProcTable = func(uint64) ([]procEntry, error) { return slices.Clone(entries), nil }
	t.Cleanup(func() { readProcTable = prev })
}

// debugLog collects the debug records of a run.
type debugLog struct {
	mu   sync.Mutex
	msgs []string
}

func (d *debugLog) log(level slog.Level, msg string) {
	if level == slog.LevelDebug {
		d.mu.Lock()
		defer d.mu.Unlock()
		d.msgs = append(d.msgs, msg)
	}
}

func (d *debugLog) String() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return strings.Join(d.msgs, "\n")
}

// startSleep starts `sleep 30` with the attributes, reaps it when it ends, and kills it at the
// end of the test.
func startSleep(t *testing.T, attr *syscall.SysProcAttr) *exec.Cmd {
	t.Helper()
	cmd := exec.Command("sleep", "30")
	cmd.SysProcAttr = attr
	require.NoError(t, cmd.Start())
	go func() { _ = cmd.Wait() }()
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	return cmd
}

// procKeys and groupIDs return what a cleanup step would signal.
func procKeys(tg procTargets) []procKey {
	var out []procKey
	for _, p := range tg.procs {
		out = append(out, p.procKey)
	}
	return out
}

func groupIDs(tg procTargets) []int {
	var out []int
	for _, g := range tg.groups {
		out = append(out, g.pgid)
	}
	return out
}

// assertGone checks that the processes are gone. A process that got SIGKILL right before
// Run returned may take a moment to end.
func assertGone(t *testing.T, pids []int, msg string) {
	t.Helper()
	assert.Eventually(t, func() bool { return allGone(pids) }, 500*time.Millisecond, 10*time.Millisecond,
		"%s: still running: %v", msg, running(pids))
}

// assertStoppedGracefully checks that each of the fake's command shells got SIGTERM once
// (<pidfile>.term) and had the time to stop after it (<pidfile>.stopped).
func assertStoppedGracefully(t *testing.T, f fakeCLI, shells ...int) {
	t.Helper()
	lines := func(ext string) []string {
		b, _ := os.ReadFile(f.pidfile + ext)
		return strings.Fields(string(b))
	}
	term, stopped := lines(".term"), lines(".stopped")
	for _, pid := range shells {
		p := strconv.Itoa(pid)
		assert.Contains(t, stopped, p, "the shell %d was killed before it could stop", pid)
		n := 0
		for _, l := range term {
			if l == p {
				n++
			}
		}
		assert.Equal(t, 1, n, "the shell %d got SIGTERM %d times", pid, n)
	}
}

// TestHarnessStopsWhatTheCLILeftRunning: Claude Code leaves the commands it ran in the
// background running when it exits. The runner recorded them while the CLI ran, stops them
// when it exits, and warns how many there were.
func TestHarnessStopsWhatTheCLILeftRunning(t *testing.T) {
	for _, c := range []struct {
		name, mode string
		pids       int
		ignoreTerm bool
		shells     []int // the shells among the PIDs
	}{
		// A shell in a session of its own and its sleep.
		{"a command in the background", "background-session", 2, false, []int{0}},
		{"a command in the background that ignores SIGTERM", "background-session", 2, true, nil},
		// Also a sleep whose parent exited before it could be recorded: it is in the shell's
		// group and session.
		{"a process whose parent exited at once", "session-orphan", 3, false, []int{0}},
		// A sleep in a session of its own whose parent exited after it was recorded: it
		// belongs to init when the CLI exits.
		{"a daemon", "double-fork", 1, false, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := useFakeClaude(t)
			t.Setenv("FAKE_CLAUDE_MODE", c.mode)
			if c.ignoreTerm {
				t.Setenv("FAKE_CLAUDE_IGNORE_TERM", "1")
			} else {
				// A process that honours SIGTERM is gone long before the SIGKILL would come.
				prev := postExitGrace
				postExitGrace = 8 * time.Second
				t.Cleanup(func() { postExitGrace = prev })
			}
			start := time.Now()
			res, err := Run(t.Context(), f.request("Write NOTES.md"))
			took := time.Since(start)
			pids := waitPIDs(t, f.pidfile, c.pids)
			killAllAtCleanup(t, pids)
			require.NoError(t, err)
			assert.Equal(t, "Done: NOTES.md lists the changes.", res.Output)
			assertGone(t, pids, "what the CLI left running is gone when Run returns")
			assert.Equal(t, []string{leftRunningWarning(c.pids)}, res.Warnings)
			if !c.ignoreTerm {
				assert.Less(t, took, postExitGrace, "the processes got SIGTERM")
			}
			for _, i := range c.shells {
				assertStoppedGracefully(t, f, pids[i])
			}
		})
	}
}

func TestLeftRunningWarning(t *testing.T) {
	assert.Equal(t, "claude-code left 1 process running when it exited (for example a command it ran in the "+
		"background); AgentRun stopped it", leftRunningWarning(1))
	assert.Equal(t, "claude-code left 3 processes running when it exited (for example a command it ran in the "+
		"background); AgentRun stopped them", leftRunningWarning(3))
}

// TestHarnessNoWaitWhenNothingIsLeft: after a CLI that left nothing running, the run does not
// wait for the grace period.
func TestHarnessNoWaitWhenNothingIsLeft(t *testing.T) {
	f := useFakeClaude(t)
	prev := postExitGrace
	postExitGrace = 8 * time.Second
	t.Cleanup(func() { postExitGrace = prev })
	start := time.Now()
	res, err := Run(t.Context(), f.request("Write NOTES.md"))
	require.NoError(t, err)
	assert.Empty(t, res.Warnings)
	assert.Less(t, time.Since(start), 4*time.Second)
}

// TestHarnessTimeoutStopsSessionCommands: when the run times out, a command in the
// foreground and one in the background, each a shell in a session of its own with a child,
// are stopped with the CLI.
func TestHarnessTimeoutStopsSessionCommands(t *testing.T) {
	for _, c := range []struct {
		name string
		env  string
	}{
		{"everything honours SIGTERM", ""},
		// The CLI carries on after SIGTERM: it exits only because its foreground command
		// ended, which got SIGTERM from the runner.
		{"the CLI carries on after SIGTERM", "FAKE_CLAUDE_TRAP_TERM"},
		{"everything ignores SIGTERM", "FAKE_CLAUDE_IGNORE_TERM"},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := useFakeClaude(t)
			t.Setenv("FAKE_CLAUDE_MODE", "session-timeout")
			if c.env != "" {
				t.Setenv(c.env, "1")
			}
			grace := 8 * time.Second
			if c.env == "FAKE_CLAUDE_IGNORE_TERM" {
				grace = time.Second
			}
			shortGrace(t, grace)
			var warnings []string
			req := f.request("Write NOTES.md")
			req.Timeout = 2 * time.Second
			req.Warn = func(msg string) { warnings = append(warnings, msg) }
			start := time.Now()
			_, err := Run(t.Context(), req)
			took := time.Since(start)
			pids := waitPIDs(t, f.pidfile, 4)
			killAllAtCleanup(t, pids)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "the agent did not finish within 2s (timeoutSeconds)")
			assertGone(t, pids, "the commands are gone when Run returns")
			if c.env != "FAKE_CLAUDE_IGNORE_TERM" {
				assertStoppedGracefully(t, f, pids[0], pids[2])
			}
			assert.NotContains(t, strings.Join(warnings, "\n"), "running when it exited", "a stopped CLI is not "+
				"reported for what it left running")
			if c.env == "FAKE_CLAUDE_IGNORE_TERM" {
				assert.GreaterOrEqual(t, took, req.Timeout+grace-100*time.Millisecond, "killed after the grace period")
			} else {
				assert.Less(t, took, req.Timeout+grace, "the commands got SIGTERM when the run timed out")
			}
			if c.env == "FAKE_CLAUDE_TRAP_TERM" {
				assert.NotEmpty(t, f.invocations(t)[0]["term"], "the CLI got SIGTERM")
			}
		})
	}
}

// TestHarnessSignalStopsSessionCommands: when Pulumi stops the provider (SIGINT), the
// commands of a running CLI are gone before the provider exits.
func TestHarnessSignalStopsSessionCommands(t *testing.T) {
	for _, ignoreTerm := range []bool{false, true} {
		t.Run(fmt.Sprintf("ignoring SIGTERM %v", ignoreTerm), func(t *testing.T) {
			f := useFakeClaude(t)
			var env []string
			if ignoreTerm {
				env = append(env, "FAKE_CLAUDE_IGNORE_TERM=1")
			}
			cmd, leader, _, stderr := startHarnessHelper(t, f, "session-timeout", env...)
			pids := waitPIDs(t, f.pidfile, 4)
			killAllAtCleanup(t, pids)
			start := time.Now()
			require.NoError(t, cmd.Process.Signal(syscall.SIGINT))
			require.NoError(t, waitHelper(t, cmd), "the provider shuts down as the SDK makes it, with exit status 0")
			assert.Contains(t, stderr.String(), "claude was stopped because Pulumi is stopping the provider (SIGINT)")
			assert.True(t, gone(leader), "the CLI is gone before the provider exits")
			assertGone(t, pids, "the commands are gone before the provider exits")
			if !ignoreTerm {
				assertStoppedGracefully(t, f, pids[0], pids[2])
			}
			if ignoreTerm {
				assert.GreaterOrEqual(t, time.Since(start), signalGrace, "killed after signalGrace")
			} else {
				assert.Less(t, time.Since(start), signalGrace, "the commands got SIGTERM")
			}
		})
	}
}

// TestHarnessLeavesUnrelatedProcessesAlone: processes that the provider starts while a CLI
// runs (in its own process group, as docker-agent starts MCP servers of other agents, in a
// group of their own, in a session of their own) are not stopped with the CLI.
func TestHarnessLeavesUnrelatedProcessesAlone(t *testing.T) {
	f := useFakeClaude(t)
	t.Setenv("FAKE_CLAUDE_MODE", "background-session")
	type result struct {
		res *Result
		err error
	}
	done := make(chan result, 1)
	go func() {
		res, err := Run(context.Background(), f.request("Write NOTES.md"))
		done <- result{res, err}
	}()
	// The CLI runs for 1 s after it started its command.
	cmds := waitPIDs(t, f.pidfile, 2)
	killAllAtCleanup(t, cmds)
	unrelated := []*exec.Cmd{
		startSleep(t, nil),
		startSleep(t, &syscall.SysProcAttr{Setpgid: true}),
		startSleep(t, &syscall.SysProcAttr{Setsid: true}),
	}
	r := <-done
	require.NoError(t, r.err)
	assert.Equal(t, []string{leftRunningWarning(2)}, r.res.Warnings)
	assertGone(t, cmds, "the CLI's command is gone")
	for i, c := range unrelated {
		assert.False(t, gone(c.Process.Pid), "unrelated process %d was stopped", i)
	}
}

// TestProcTreeNeverSignalsAReusedPID: a recorded process whose PID now belongs to a process
// with another start time is not signalled, nor the group it led.
func TestProcTreeNeverSignalsAReusedPID(t *testing.T) {
	for _, reused := range []bool{true, false} {
		t.Run(fmt.Sprintf("reused %v", reused), func(t *testing.T) {
			cli := startSleep(t, &syscall.SysProcAttr{Setpgid: true})
			other := startSleep(t, &syscall.SysProcAttr{Setpgid: true})
			cliEntry, ok := readProc(cli.Process.Pid)
			require.True(t, ok)
			otherEntry, ok := readProc(other.Process.Pid)
			require.True(t, ok)
			recorded := otherEntry
			recorded.ppid = cli.Process.Pid
			if reused {
				// The recorded process had this PID before: it started at another time.
				recorded.start++
			}
			fakeProcTable(t, cliEntry, recorded)
			tree := newProcTree(cli.Process.Pid, "", nil)
			require.NotNil(t, tree)
			t.Cleanup(tree.stopPolling)
			tg := tree.scan()
			require.Equal(t, []procKey{recorded.procKey}, procKeys(tg), "the child of the CLI is recorded")
			require.Equal(t, []int{other.Process.Pid}, groupIDs(tg), "and the group it leads")
			tree.kill(tg)
			if reused {
				time.Sleep(200 * time.Millisecond)
				assert.False(t, gone(other.Process.Pid), "a process with a reused PID was signalled")
			} else {
				assert.Eventually(t, func() bool { return gone(other.Process.Pid) }, 2*time.Second, 10*time.Millisecond,
					"the recorded process was not signalled")
			}
		})
	}
}

// TestProcTreeClosesReusedGroups: a group whose leader's PID belongs to another process, or
// that was seen without members, is never signalled again.
func TestProcTreeClosesReusedGroups(t *testing.T) {
	cli := startSleep(t, &syscall.SysProcAttr{Setpgid: true})
	cliEntry, ok := readProc(cli.Process.Pid)
	require.True(t, ok)
	leader := procEntry{procKey: procKey{pid: 999990, start: cliEntry.start + 1}, ppid: cli.Process.Pid, pgid: 999990,
		sid: 999990}
	member := procEntry{procKey: procKey{pid: 999991, start: cliEntry.start + 1}, ppid: 999990, pgid: 999990, sid: 999990}
	var mu sync.Mutex
	table := []procEntry{cliEntry, leader, member}
	prev := readProcTable
	readProcTable = func(uint64) ([]procEntry, error) {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(table), nil
	}
	t.Cleanup(func() { readProcTable = prev })
	tree := newProcTree(cli.Process.Pid, "", nil)
	require.NotNil(t, tree)
	t.Cleanup(tree.stopPolling)
	tree.stopPolling()
	assert.Equal(t, []int{999990}, groupIDs(tree.scan()))

	// The leader exited; its group lives on with the member.
	mu.Lock()
	table = []procEntry{cliEntry, member}
	mu.Unlock()
	assert.Equal(t, []int{999990}, groupIDs(tree.scan()), "a group outlives its leader")
	// The member exited too: the ID is free and may be reused.
	mu.Lock()
	table = []procEntry{cliEntry}
	mu.Unlock()
	assert.Empty(t, groupIDs(tree.scan()))
	mu.Lock()
	table = []procEntry{cliEntry, {procKey: procKey{pid: 999992, start: cliEntry.start + 2}, ppid: 1, pgid: 999990,
		sid: 999990}}
	mu.Unlock()
	assert.Empty(t, groupIDs(tree.scan()), "a group seen without members stays closed")

	// A group whose leader's PID now belongs to a process with another start time.
	tree2 := newProcTree(cli.Process.Pid, "", nil)
	require.NotNil(t, tree2)
	tree2.stopPolling()
	mu.Lock()
	table = []procEntry{cliEntry, leader, member}
	mu.Unlock()
	assert.Equal(t, []int{999990}, groupIDs(tree2.scan()))
	reused := leader
	reused.start++
	reused.ppid = 1
	mu.Lock()
	table = []procEntry{cliEntry, reused, member}
	mu.Unlock()
	assert.Empty(t, groupIDs(tree2.scan()))
}

// TestProcTreeNeverSignalsTheProvider: a table that puts the provider (here the test), and a
// process of its group in a recorded session, under the CLI does not make the tree signal the
// provider or its group.
func TestProcTreeNeverSignalsTheProvider(t *testing.T) {
	cli := startSleep(t, &syscall.SysProcAttr{Setpgid: true})
	cliEntry, ok := readProc(cli.Process.Pid)
	require.True(t, ok)
	self, ok := readProc(os.Getpid())
	require.True(t, ok)
	self.ppid = cli.Process.Pid
	start := cliEntry.start
	fakeProcTable(t, cliEntry, self,
		procEntry{procKey: procKey{pid: 999994, start: start}, ppid: cli.Process.Pid, pgid: 999994, sid: 999994},
		procEntry{procKey: procKey{pid: 999995, start: start}, ppid: 999994, pgid: unix.Getpgrp(), sid: 999994})
	tree := newProcTree(cli.Process.Pid, "", nil)
	require.NotNil(t, tree)
	t.Cleanup(tree.stopPolling)

	got := make(chan os.Signal, 1)
	signal.Notify(got, syscall.SIGWINCH)
	defer signal.Stop(got)
	tg := tree.scan()
	assert.NotContains(t, procKeys(tg), self.procKey)
	assert.NotContains(t, groupIDs(tg), unix.Getpgrp())
	// Nor when a cleanup step is handed them.
	tg.procs = append(tg.procs, procTarget{self.procKey, self.pgid})
	tg.groups = append(tg.groups, groupTarget{unix.Getpgrp(), -1}, groupTarget{os.Getpid(), -1},
		groupTarget{unix.Getpgrp(), 999994})
	tree.send(tg, false, syscall.SIGWINCH)
	select {
	case <-got:
		t.Fatal("the tree signalled the provider or its group")
	case <-time.After(300 * time.Millisecond):
	}
}

// TestHarnessTrackingProblemsFallBack: when the processes of the CLI cannot be tracked, the
// run is not affected: the CLI's group is stopped as without tracking, and the reason is
// logged at debug level.
func TestHarnessTrackingProblemsFallBack(t *testing.T) {
	for _, c := range []struct {
		name, log string
		setup     func(t *testing.T)
	}{
		{"the table cannot be read", "reading the process table: no table", func(t *testing.T) {
			failProcTable(t, errors.New("no table"))
		}},
		{"the table is too large", "the process table is too large", func(t *testing.T) {
			prev := maxProcTable
			maxProcTable = 1
			t.Cleanup(func() { maxProcTable = prev })
		}},
		{"the CLI is not in the table", "tracking the processes of claude is off", func(t *testing.T) {
			prev := readProc
			readProc = func(int) (procEntry, bool) { return procEntry{}, false }
			testFaults.noProcs = true
			t.Cleanup(func() { readProc, testFaults.noProcs = prev, false })
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := useFakeClaude(t)
			c.setup(t)
			t.Setenv("FAKE_CLAUDE_MODE", "leftover")
			var debug debugLog
			req := f.request("Write NOTES.md")
			req.Log = debug.log
			res, err := Run(t.Context(), req)
			require.NoError(t, err)
			assert.Equal(t, "Done: NOTES.md lists the changes.", res.Output)
			child := waitPID(t, f.pidfile)
			killAtCleanup(t, child)
			assert.True(t, gone(child), "the process the CLI left in its group is stopped as before")
			assert.Contains(t, debug.String(), c.log)
		})
	}
}

// noPolls makes the tree read the process table only when the CLI starts and at the cleanup
// steps, so that only what runs then is found below the CLI.
func noPolls(t *testing.T) {
	prev := procPollInterval
	procPollInterval = time.Hour
	t.Cleanup(func() { procPollInterval = prev })
}

// TestHarnessCookieFindsWhatTheTreeMissed: a process whose parent exited before a poll saw
// it (`cmd >/dev/null 2>&1 &` in a shell that exits at once, a double fork) is found by the
// run's cookie in its environment. Without polls, the tree cannot find these.
func TestHarnessCookieFindsWhatTheTreeMissed(t *testing.T) {
	for _, c := range []struct {
		mode string
		pids int
	}{{"oneshot", 3}, {"oneshot-daemon", 1}} {
		t.Run(c.mode, func(t *testing.T) {
			f := useFakeClaude(t)
			noPolls(t)
			t.Setenv("FAKE_CLAUDE_MODE", c.mode)
			res, err := Run(t.Context(), f.request("Write NOTES.md"))
			pids := waitPIDs(t, f.pidfile, c.pids)
			killAllAtCleanup(t, pids)
			require.NoError(t, err)
			assertGone(t, pids, "what the CLI left running is gone when Run returns")
			assert.Equal(t, []string{leftRunningWarning(c.pids)}, res.Warnings)
		})
	}
}

// TestHarnessClearedEnvironmentEscapes documents the limit of the cookie: a process that
// cleared its environment (env -i), whose parent exited before a poll saw it, and that left
// the CLI's group and session, is not found.
func TestHarnessClearedEnvironmentEscapes(t *testing.T) {
	f := useFakeClaude(t)
	noPolls(t)
	t.Setenv("FAKE_CLAUDE_MODE", "env-cleared")
	res, err := Run(t.Context(), f.request("Write NOTES.md"))
	pid := waitPID(t, f.pidfile)
	killAtCleanup(t, pid)
	require.NoError(t, err)
	assert.False(t, gone(pid), "a process without the cookie, out of the tree's reach, keeps running")
	assert.Empty(t, res.Warnings)
}

func TestRunCookie(t *testing.T) {
	a, b := newRunCookie(), newRunCookie()
	assert.Len(t, a, 32, "128 bits, hex")
	assert.NotEqual(t, a, b)
	env := withRunCookie([]string{"A=1", runCookieVar + "=outer", "B=2"}, a)
	assert.Equal(t, []string{"A=1", "B=2", runCookieVar + "=" + a}, env, "an outer run's cookie is replaced")
	assert.Contains(t, withRunCookie(nil, a), runCookieVar+"="+a)
}

func TestEnvironHas(t *testing.T) {
	const entry = runCookieVar + "=0123456789abcdef0123456789abcdef"
	for _, c := range []struct {
		env  string
		want bool
	}{
		{"A=1\x00" + entry + "\x00B=2\x00", true},
		{entry + "\x00", true},
		{"A=1\x00" + entry + "0\x00", false},               // a longer value
		{"A=1\x00" + entry[:len(entry)-1] + "\x00", false}, // a shorter one
		{"X" + entry + "\x00", false},                      // another variable
		{"A=1\x00" + entry, false},                         // cut off: not NUL-terminated
		{"A=" + entry + "\x00", false},                     // inside another value
		{"", false},
	} {
		assert.Equal(t, c.want, environHas([]byte(c.env), entry), "%q", c.env)
	}
}

func TestProcargsHas(t *testing.T) {
	const entry = runCookieVar + "=0123456789abcdef0123456789abcdef"
	buf := func(argc uint32, rest string) []byte {
		b := make([]byte, 4, 4+len(rest))
		binary.NativeEndian.PutUint32(b, argc)
		return append(b, rest...)
	}
	for _, c := range []struct {
		name          string
		b             []byte
		found, hidden bool
	}{
		{"the entry", buf(2, "/bin/sleep\x00\x00\x00sleep\x0030\x00A=1\x00"+entry+"\x00\x00ptr_munge=\x00"), true, false},
		{"an argument is not the environment", buf(2, "/bin/sleep\x00\x00sleep\x00"+entry+"\x00A=1\x00"), false, false},
		{"a longer value", buf(1, "/bin/sleep\x00sleep\x00"+entry+"x\x00"), false, false},
		{"a short buffer", buf(5, "/bin/sleep\x00sleep\x00"), false, false},
		{"not even argc", []byte{1, 0}, false, false},
		// macOS left out the environment and the apple strings.
		{"a hidden environment", buf(2, "/bin/sleep\x00\x00\x00sleep\x0030\x00"), false, true},
	} {
		found, hidden := procargsHas(c.b, entry)
		assert.Equal(t, c.found, found, c.name)
		assert.Equal(t, c.hidden, hidden, c.name)
	}
}

// TestProcHasCookie reads the environment of real processes.
func TestProcHasCookie(t *testing.T) {
	cookie := newRunCookie()
	entry := runCookieVar + "=" + cookie
	for _, c := range []struct {
		name string
		env  []string
		want bool
	}{
		{"the cookie", []string{"A=1", entry}, true},
		{"a longer value", []string{entry + "0"}, false},
		{"a shorter value", []string{entry[:len(entry)-1]}, false},
		{"another run's cookie", []string{runCookieVar + "=" + newRunCookie()}, false},
		{"a cleared environment", []string{}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			cmd := exec.Command("/bin/sleep", "30")
			cmd.Env = c.env
			require.NoError(t, cmd.Start())
			go func() { _ = cmd.Wait() }()
			t.Cleanup(func() { _ = cmd.Process.Kill() })
			e, ok := readProc(cmd.Process.Pid)
			require.True(t, ok)
			buf := make([]byte, max(environBufSize, 1))
			// The environment is in place once the process ran exec.
			if c.want {
				assert.Eventually(t, func() bool { found, _ := procHasCookie(&e, entry, buf); return found }, 2*time.Second,
					10*time.Millisecond)
			} else {
				time.Sleep(100 * time.Millisecond)
				found, hidden := procHasCookie(&e, entry, buf)
				assert.False(t, found)
				assert.False(t, hidden, "a process of the user shows its environment")
			}
		})
	}
}

// TestProcTreeCapCountsRunningProcesses: the cap on records counts the processes that run,
// not every process the CLI ever had, so a long run that ran many commands still records a
// command it starts later. The command drops the cookie, so only the tree can find it.
func TestProcTreeCapCountsRunningProcesses(t *testing.T) {
	prev := maxTrackedProcs
	maxTrackedProcs = 4
	t.Cleanup(func() { maxTrackedProcs = prev })
	pidfile := filepath.Join(t.TempDir(), "pid")
	// Six short commands, one after the other, each seen by a poll, then a command in a
	// session of its own without the cookie, which the CLI leaves running.
	cmd := exec.Command("sh", "-c", `for i in 1 2 3 4 5 6; do sleep 0.5; done
env -u `+runCookieVar+` sh -c "$2" sh sleep 30 >/dev/null 2>&1 </dev/null & echo $! >"$1"; sleep 1; exit 0`,
		"sh", pidfile, insessionShell)
	var debug []string
	p, err := startHarness(cmd, func(m string) { debug = append(debug, m) })
	require.NoError(t, err)
	<-p.done
	pid := waitPID(t, pidfile)
	killAtCleanup(t, pid)
	assert.True(t, gone(pid), "the background command was not stopped (debug: %q)", debug)
}

// insessionShell is the fake claude's insession_sh: `sh -c "$insessionShell" sh cmd args`
// runs cmd in a session of its own.
const insessionShell = `if command -v setsid >/dev/null 2>&1; then exec setsid "$@"; else exec perl -MPOSIX -e "POSIX::setsid(); exec @ARGV" "$@"; fi`

// TestHarnessNoWarningForWhatTheCLIWasStopping: a child in the CLI's group that the CLI
// asked to stop right before it exited (as Claude Code stops its MCP servers) and that
// takes a moment to end is stopped, without a warning that the CLI left it running.
func TestHarnessNoWarningForWhatTheCLIWasStopping(t *testing.T) {
	cmd := exec.Command("sh", "-c", `sh -c 'trap "sleep 0.2; exit 0" TERM; while :; do sleep 0.05; done' &
c=$!; sleep 0.6; kill -TERM $c; exit 0`)
	p, err := startHarness(cmd, nil)
	require.NoError(t, err)
	<-p.done
	assert.Zero(t, p.leftRunning)
	assert.False(t, groupAlive(p.pgid), "the CLI's group is gone")
}

// TestProcTreeNeverSignalsAClosedGroup: a group that was seen without members is not
// signalled even when, later, its leader is gone and a group with its ID has members (a
// reused ID).
func TestProcTreeNeverSignalsAClosedGroup(t *testing.T) {
	cli := startSleep(t, &syscall.SysProcAttr{Setpgid: true})
	cliEntry, ok := readProc(cli.Process.Pid)
	require.True(t, ok)
	// A real group whose leader exited: sh starts sleep in its group and exits.
	pidfile := filepath.Join(t.TempDir(), "pid")
	sh := exec.Command("sh", "-c", `sleep 30 >/dev/null 2>&1 </dev/null & echo $! >"$1"`, "sh", pidfile)
	sh.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	require.NoError(t, sh.Run())
	member := waitPID(t, pidfile)
	killAtCleanup(t, member)
	g := sh.Process.Pid
	var mu sync.Mutex
	table := []procEntry{cliEntry, {procKey: procKey{pid: g, start: cliEntry.start + 1}, ppid: cli.Process.Pid, pgid: g,
		sid: g}}
	prev := readProcTable
	readProcTable = func(uint64) ([]procEntry, error) {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(table), nil
	}
	t.Cleanup(func() { readProcTable = prev })
	tree := newProcTree(cli.Process.Pid, "", nil)
	require.NotNil(t, tree)
	tree.stopPolling()
	tg := tree.scan()
	require.Equal(t, []int{g}, groupIDs(tg), "the recorded leader's group")
	// A read sees the group without members: the ID is free.
	mu.Lock()
	table = []procEntry{cliEntry}
	mu.Unlock()
	tree.scan()
	tree.kill(tg)
	time.Sleep(200 * time.Millisecond)
	assert.False(t, gone(member), "a group whose ID may have been reused was signalled")
}

// TestProcTreeIgnoresOlderChildren: a process that started before the process the table
// names as its parent is not its child (macOS reports a debugger as the parent of a process
// it attached to): it is not recorded, nor the group and session it leads.
func TestProcTreeIgnoresOlderChildren(t *testing.T) {
	cli := startSleep(t, &syscall.SysProcAttr{Setpgid: true})
	cliEntry, ok := readProc(cli.Process.Pid)
	require.True(t, ok)
	debugger := procEntry{procKey: procKey{pid: 999980, start: cliEntry.start + 10}, ppid: cli.Process.Pid, pgid: 999980,
		sid: 999980}
	attached := procEntry{procKey: procKey{pid: 999981, start: cliEntry.start + 5}, ppid: 999980, pgid: 999981, sid: 999981}
	child := procEntry{procKey: procKey{pid: 999982, start: cliEntry.start + 20}, ppid: 999980, pgid: 999980, sid: 999980}
	fakeProcTable(t, cliEntry, debugger, attached, child)
	tree := newProcTree(cli.Process.Pid, "", nil)
	require.NotNil(t, tree)
	tree.stopPolling()
	tg := tree.scan()
	assert.ElementsMatch(t, []procKey{debugger.procKey, child.procKey}, procKeys(tg))
	assert.Equal(t, []int{999980}, groupIDs(tg), "not the attached process's group")
}

// TestProcTreeSweepReadsOnlyOwnProcesses: the cookie sweep reads the environments of the
// provider's user's processes only, not of other users' processes or kernel threads, so
// those do not use up its budget.
func TestProcTreeSweepReadsOnlyOwnProcesses(t *testing.T) {
	cli := startSleep(t, &syscall.SysProcAttr{Setpgid: true})
	cliEntry, ok := readProc(cli.Process.Pid)
	require.True(t, ok)
	own := procEntry{procKey: procKey{pid: 999970, start: cliEntry.start + 1}, ppid: 1, pgid: 999970, sid: 999970,
		uid: os.Geteuid()}
	other := own
	other.pid, other.pgid, other.sid, other.uid = 999971, 999971, 999971, os.Geteuid()+1
	kthread := own
	kthread.pid, kthread.pgid, kthread.sid, kthread.kthread = 999972, 0, 0, true
	fakeProcTable(t, cliEntry, own, other, kthread)
	var mu sync.Mutex
	var read []int
	prev := readCookie
	readCookie = func(e *procEntry, entry string, buf []byte) (bool, bool) {
		mu.Lock()
		defer mu.Unlock()
		read = append(read, e.pid)
		return e.pid == own.pid, false
	}
	t.Cleanup(func() { readCookie = prev })
	tree := newProcTree(cli.Process.Pid, newRunCookie(), nil)
	require.NotNil(t, tree)
	tree.stopPolling()
	assert.Equal(t, []procKey{own.procKey}, procKeys(tree.scan()))
	assert.Equal(t, []int{own.pid}, read, "only the user's own process is read")

	// With a budget of one read, that read is the user's process.
	prevMax := maxCookieReads
	maxCookieReads = 1
	t.Cleanup(func() { maxCookieReads = prevMax })
	for range 5 {
		read = nil
		tree := newProcTree(cli.Process.Pid, newRunCookie(), nil)
		require.NotNil(t, tree)
		tree.stopPolling()
		assert.Equal(t, []procKey{own.procKey}, procKeys(tree.scan()))
	}
}

// TestProcTreeCookieSweepIsTimeBoxed: an environment read that blocks (a process stuck in a
// page fault holds its memory lock) does not hold up the cleanup: the sweep gives up after
// cookieSweepTimeout and says so at debug level.
func TestProcTreeCookieSweepIsTimeBoxed(t *testing.T) {
	cli := startSleep(t, &syscall.SysProcAttr{Setpgid: true})
	cliEntry, ok := readProc(cli.Process.Pid)
	require.True(t, ok)
	stuck := procEntry{procKey: procKey{pid: 999960, start: cliEntry.start + 1}, ppid: 1, pgid: 999960, sid: 999960,
		uid: os.Geteuid()}
	fakeProcTable(t, cliEntry, stuck)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	prev := readCookie
	readCookie = func(*procEntry, string, []byte) (bool, bool) {
		<-release
		return true, false
	}
	t.Cleanup(func() { readCookie = prev })
	prevTimeout := cookieSweepTimeout
	cookieSweepTimeout = 200 * time.Millisecond
	t.Cleanup(func() { cookieSweepTimeout = prevTimeout })
	var debug debugLog
	tree := newProcTree(cli.Process.Pid, newRunCookie(), func(m string) { debug.log(slog.LevelDebug, m) })
	require.NotNil(t, tree)
	tree.stopPolling()
	done := make(chan procTargets, 1)
	start := time.Now()
	go func() { done <- tree.scan() }()
	select {
	case tg := <-done:
		assert.Less(t, time.Since(start), 2*time.Second)
		assert.Empty(t, tg.procs, "an abandoned read finds nothing")
		assert.Contains(t, debug.String(), "took longer than 200ms")
	case <-time.After(10 * time.Second):
		t.Fatal("a blocked environment read held up the cleanup")
	}
}

// TestHarnessCookieWalksBelowWhatItFinds: below a process the cookie found, the tree finds
// its children, also one without the cookie in a session of its own.
func TestHarnessCookieWalksBelowWhatItFinds(t *testing.T) {
	f := useFakeClaude(t)
	noPolls(t)
	t.Setenv("FAKE_CLAUDE_MODE", "cookie-parent")
	res, err := Run(t.Context(), f.request("Write NOTES.md"))
	pids := waitPIDs(t, f.pidfile, 2)
	killAllAtCleanup(t, pids)
	require.NoError(t, err)
	assertGone(t, pids, "the cookie's process and its child without the cookie are gone")
	assert.Equal(t, []string{leftRunningWarning(2)}, res.Warnings)
}

// runChildSupervision is the supervision of a run child (child_unix.go), for the tests of its
// session.
func runChildSupervision(debug func(string)) supervision {
	return supervision{name: "the agent run", kind: "agent run", cookieVar: childCookieVar, backstop: true,
		ownSession: true, debug: debug}
}

// TestProcTreeSessionSweep: once a run child exited, the processes of the provider's user that
// are still in its session and started after it are recorded and counted as left running, and
// nothing else: not while it runs, not another user's processes, not one that started before
// it (a reused PID), not zombies or kernel threads, never the provider, not its own group
// (its group signal reaches those), not one that left the session; and nothing at all once the
// session was closed (the root's PID with another process, or a read without members).
func TestProcTreeSessionSweep(t *testing.T) {
	root := startSleep(t, &syscall.SysProcAttr{Setsid: true})
	rootEntry, ok := readProc(root.Process.Pid)
	require.True(t, ok)
	sid, uid, start := root.Process.Pid, os.Geteuid(), rootEntry.start
	require.Equal(t, sid, rootEntry.sid, "the root leads a session of its own")
	proc := func(pid, pgid, session int, started uint64) procEntry {
		return procEntry{procKey: procKey{pid: pid, start: started}, ppid: 1, pgid: pgid, sid: session, uid: uid}
	}
	// `sleep &` of a shell that exited: its group outlives the shell.
	left := proc(999940, 999939, sid, start+1)
	// A group leader that started in the root's clock tick.
	leader := proc(999941, 999941, sid, start)
	otherUser := proc(999942, 999942, sid, start+1)
	otherUser.uid = uid + 1
	older := proc(999943, 999943, sid, start-1)
	zombie := proc(999944, 999939, sid, start+1)
	zombie.zombie = true
	kthread := proc(999945, 999945, sid, start+1)
	kthread.kthread = true
	inRootGroup := proc(999946, sid, sid, start+1)
	escaped := proc(999947, 999947, 999947, start+1) // setsid
	inProviderGroup := proc(999948, unix.Getpgrp(), sid, start+1)
	self, ok := readProc(os.Getpid())
	require.True(t, ok)
	self.sid, self.start = sid, start+1
	members := []procEntry{left, leader, otherUser, older, zombie, kthread, inRootGroup, escaped, inProviderGroup, self}

	exited := rootEntry
	exited.zombie = true
	reused := rootEntry
	reused.start++
	var mu sync.Mutex
	var table []procEntry
	setTable := func(entries ...procEntry) {
		mu.Lock()
		defer mu.Unlock()
		table = entries
	}
	prev := readProcTable
	readProcTable = func(uint64) ([]procEntry, error) {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(table), nil
	}
	t.Cleanup(func() { readProcTable = prev })
	newTree := func() *procTree {
		t.Helper()
		setTable(append([]procEntry{rootEntry}, members...)...)
		tree := newProcTreeOf(root.Process.Pid, runChildSupervision(nil), "")
		require.NotNil(t, tree)
		tree.stopPolling()
		require.Equal(t, sid, tree.session)
		return tree
	}

	tree := newTree()
	tg := tree.scan()
	assert.Empty(t, procKeys(tg), "nothing while the root runs: it stops its own processes")

	// The root exited (Linux: not reaped yet; macOS: reaped).
	for _, rootLeft := range [][]procEntry{{exited}, nil} {
		tree := newTree()
		setTable(append(rootLeft, members...)...)
		tg := tree.scan()
		assert.ElementsMatch(t, []procKey{left.procKey, leader.procKey}, procKeys(tg))
		assert.Equal(t, []int{leader.pid}, groupIDs(tg), "the group a found process leads")
		assert.Equal(t, 2, tg.outside, "both count as left running")

		got := make(chan os.Signal, 1)
		signal.Notify(got, syscall.SIGWINCH)
		tree.send(tg, false, syscall.SIGWINCH)
		select {
		case <-got:
			t.Fatal("the session sweep signalled the provider")
		case <-time.After(100 * time.Millisecond):
		}
		signal.Stop(got)
	}

	// The root's PID belongs to another process: the session is closed for good.
	tree = newTree()
	late := proc(999949, 999949, sid, start+5)
	setTable(reused, late)
	assert.Empty(t, procKeys(tree.scan()), "a session whose root's PID was reused")
	setTable(exited, late)
	assert.Empty(t, procKeys(tree.scan()), "a closed session stays closed")

	// A read without members (the root reaped, nothing left in the session): its ID may be
	// reused, also by a session whose leader is already gone.
	tree = newTree()
	setTable(escaped)
	assert.Empty(t, procKeys(tree.scan()))
	setTable(escaped, late)
	assert.Empty(t, procKeys(tree.scan()), "a session seen without members stays closed")

	// A root that is not in a session of its own has none to sweep.
	cli := startSleep(t, &syscall.SysProcAttr{Setpgid: true})
	var debug debugLog
	setTable()
	noSession := newProcTreeOf(cli.Process.Pid, runChildSupervision(func(m string) { debug.log(slog.LevelDebug, m) }), "")
	require.NotNil(t, noSession)
	noSession.stopPolling()
	assert.Zero(t, noSession.session)
	assert.Contains(t, debug.String(), "is not in a session of its own")
}
