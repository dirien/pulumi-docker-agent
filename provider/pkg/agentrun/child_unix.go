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
	goruntime "runtime"
	"syscall"

	"golang.org/x/sys/unix"
)

// On Linux and macOS a run child is supervised like a Claude Code CLI (harness_unix.go), as a
// backstop: it runs in a session of its own (and so in a process group of its own), gets
// SIGTERM from the kernel when the provider dies (Linux; on macOS it stops when its stdin
// closes), and carries childCookieVar, which every process it starts inherits. A cancellation
// sends it SIGTERM and a shutdown SIGINT; it stops its own CLIs and tools, and is killed after
// childGrace (signalGrace for a shutdown). After it exited, the rest of its group, the
// processes started under it and the processes of the provider's user still in its session
// get SIGTERM, and SIGKILL after postExitGrace: what its tools left running (a command in the
// background), which the run reports. The session finds what the process table's polls and
// the cookie miss: docker-agent's shell tool runs each command in a process group of its own
// (in the child's session), and `sleep 600 &` of a shell that exits at once is orphaned before
// a poll sees it, while macOS hides the environment of platform binaries such as /bin/sleep.
// What escapes is a process that left the session too (setsid), was orphaned before a poll
// saw it, and whose environment does not show the cookie (hidden, or cleared with env -i).

// unixRunChild is a run child on Linux and macOS.
type unixRunChild struct{ p *harnessProcess }

// startRunChild starts a run child under the backstop supervisor. closeStdin is not needed:
// a cancellation is a signal.
func startRunChild(cmd *exec.Cmd, _ func(), debug func(string)) (runChildProcess, error) {
	p, err := startSupervised(cmd, supervision{name: "the agent run", kind: "agent run", cookieVar: childCookieVar,
		backstop: true, ownSession: true, debug: debug})
	if err != nil {
		return nil, err
	}
	return unixRunChild{p}, nil
}

func (c unixRunChild) stop()                    { c.p.stop() }
func (c unixRunChild) kill()                    { c.p.killNow() }
func (c unixRunChild) exited() <-chan struct{}  { return c.p.exited }
func (c unixRunChild) done() <-chan struct{}    { return c.p.done }
func (c unixRunChild) stoppedForShutdown() bool { return c.p.stoppedForShutdown() }

func (c unixRunChild) state() (*os.ProcessState, error) { return c.p.state, c.p.waitErr }

// expire kills the child without marking it as stopping, so that the cleanup after its exit
// still counts what it left running.
func (c unixRunChild) expire() {
	c.p.mu.Lock()
	defer c.p.mu.Unlock()
	c.p.signalSelfLocked(syscall.SIGKILL)
}

func (c unixRunChild) leftRunning() int {
	c.p.mu.Lock()
	defer c.p.mu.Unlock()
	return c.p.leftRunning
}

// takeChildStdio takes the protocol (stdout) and the request (stdin) away from the standard
// streams of a run child: both get descriptors of their own, closed on exec (F_DUPFD_CLOEXEC),
// so that no process the child starts holds them, and the end of the protocol follows the
// child's exit. File descriptor 1 becomes a copy of stderr, and 0 one of /dev/null, so that
// whatever writes to stdout (os.Stdout, or the descriptor itself) cannot corrupt the protocol.
func takeChildStdio() (proto, input *os.File, err error) {
	out, err := unix.FcntlInt(1, unix.F_DUPFD_CLOEXEC, 3)
	if err != nil {
		return nil, nil, err
	}
	in, err := unix.FcntlInt(0, unix.F_DUPFD_CLOEXEC, 3)
	if err != nil {
		return nil, nil, err
	}
	if err := unix.Dup2(2, 1); err != nil {
		return nil, nil, err
	}
	null, err := os.Open(os.DevNull)
	if err != nil {
		return nil, nil, err
	}
	defer null.Close() //nolint:errcheck
	if err := unix.Dup2(int(null.Fd()), 0); err != nil {
		return nil, nil, err
	}
	os.Stdout = os.Stderr
	return os.NewFile(uintptr(out), "agent-run-protocol"), os.NewFile(uintptr(in), "agent-run-request"), nil
}

// watchChildSignals cancels the run on SIGTERM (a cancellation, also the parent-death signal)
// and SIGINT (a shutdown), and ignores SIGPIPE: a write to a broken pipe fails with EPIPE
// instead of ending the child before it stopped its tools.
func watchChildSignals(cancel context.CancelCauseFunc) func() {
	ch := make(chan os.Signal, 4)
	signal.Notify(ch, syscall.SIGTERM, syscall.SIGINT, syscall.SIGPIPE)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			case sig := <-ch:
				switch sig {
				case syscall.SIGTERM:
					cancel(errChildCanceled)
				case syscall.SIGINT:
					cancel(errChildShutdown)
				}
			}
		}
	}()
	return func() {
		signal.Stop(ch)
		close(done)
	}
}

// writeFD1 writes to file descriptor 1 directly (a test's stray output).
func writeFD1(s string) { _, _ = syscall.Write(1, []byte(s)) }

// fdTarget says what a file's descriptor refers to (on Linux its /proc/self/fd target, for
// example pipe:[1234]); empty elsewhere or for a reader that is no file (a test's fault).
func fdTarget(r any) string {
	f, ok := r.(*os.File)
	if !ok || goruntime.GOOS != "linux" {
		return ""
	}
	t, _ := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", f.Fd()))
	return t
}

// snapshotProcLimits and applyProcLimits carry the tracking limits and the faults of the
// process table into a run child of a test binary (childtest.go).
func snapshotProcLimits(t *childTest) {
	t.ProcPollInterval, t.CookieSweepTimeout = procPollInterval, cookieSweepTimeout
	t.MaxProcTable, t.MaxTrackedProcs, t.MaxCookieReads = maxProcTable, maxTrackedProcs, maxCookieReads
}

func applyProcLimits(t *childTest) {
	procPollInterval, cookieSweepTimeout = t.ProcPollInterval, t.CookieSweepTimeout
	maxProcTable, maxTrackedProcs, maxCookieReads = t.MaxProcTable, t.MaxTrackedProcs, t.MaxCookieReads
	if msg := t.ProcTableError; msg != "" {
		readProcTable = func(uint64) ([]procEntry, error) { return nil, errors.New(msg) }
	}
	if t.NoProcs {
		readProc = func(int) (procEntry, bool) { return procEntry{}, false }
	}
}
