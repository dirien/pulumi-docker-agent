// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

//go:build linux || darwin

package agentrun

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
	goruntime "runtime"
	"slices"
	"sync"
	"syscall"
	"time"
)

// A Claude Code CLI runs in a process group of its own, so that the tools it runs in that
// group (and only those) can be stopped with it:
//
//   - a cancelled or timed-out run sends SIGTERM and SIGCONT to the group (SIGCONT wakes a
//     CLI that was stopped), and SIGKILL after harnessGrace;
//   - after the CLI exited, the rest of the group gets SIGTERM and SIGCONT, and SIGKILL
//     after postExitGrace. On Linux this happens while the exited CLI is not reaped yet, so
//     its PID still reserves the group's ID; on macOS right after it was reaped (the kernel
//     does not reuse the ID of a group that still has members);
//   - a SIGINT to the provider (WatchShutdownSignals) stops new CLIs from starting and
//     sends the groups of the running ones SIGTERM and SIGCONT, and SIGKILL after
//     signalGrace. The watcher only observes the signal: the Pulumi SDK's own handler
//     (provider.MainContext) ends the provider as it always does;
//   - on Linux the kernel sends SIGTERM to a CLI when the provider dies, also by SIGKILL
//     (Pdeathsig, harness_linux.go).
//
// What can run after the provider ended:
//
//   - Pulumi stops a provider with SIGINT, and with SIGKILL 5 s later. After a SIGINT the
//     SDK stops the gRPC server gracefully, which waits for the operations in flight. A CLI
//     only runs inside one (an AgentRun create, update or delete), and that operation ends
//     only after the CLI exited and the rest of its group was stopped: within about
//     signalGrace + max(postExitGrace, harnessDrain) = 4 s. So on both systems the CLI and
//     the processes of its group are gone before the provider exits. That leaves the
//     processes that left the group, which no group signal reaches: Claude Code runs each
//     shell command in a session of its own and stops the ones it tracks when it gets
//     SIGTERM; a daemon that a tool started stays;
//   - when the provider dies before that (SIGKILL after 5 s, a crash, a SIGTERM from
//     outside Pulumi, which the provider does not handle), nothing in the provider stops
//     the groups any more. On Linux every CLI that still runs gets SIGTERM from the kernel
//     and shuts down; the other processes of its group get no signal and stay unless the
//     CLI stops them. On macOS, which has no parent-death signal, the CLIs and their groups
//     keep running unattended (with --dangerously-skip-permissions) until they finish or
//     fail writing to the closed output pipe.

// startHarness starts the CLI in a process group of its own and watches it until it exited
// and its group was stopped. It fails with errShuttingDown after a SIGINT.
func startHarness(cmd *exec.Cmd) (*harnessProcess, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	setPdeathsig(cmd.SysProcAttr)
	WatchShutdownSignals()
	p := &harnessProcess{exited: make(chan struct{}), done: make(chan struct{})}
	started := make(chan error, 1)
	go func() {
		// The parent-death signal is tied to the thread that started the process: the thread
		// must not exit before the process was reaped.
		goruntime.LockOSThread()
		if err := harnesses.start(p, cmd); err != nil {
			goruntime.UnlockOSThread()
			started <- err
			return
		}
		started <- nil

		pinned := waitUnreaped(p.pgid)
		if pinned {
			signalGroup(p.pgid, syscall.SIGTERM, syscall.SIGCONT)
		}
		p.waitErr = cmd.Wait()
		p.state = cmd.ProcessState
		p.exitedAt = time.Now()
		goruntime.UnlockOSThread()
		close(p.exited)
		if !pinned {
			signalGroup(p.pgid, syscall.SIGTERM, syscall.SIGCONT)
		}
		stopGroups([]int{p.pgid}, postExitGrace, false)
		p.mu.Lock()
		p.finished = true
		if p.kill != nil {
			p.kill.Stop()
		}
		p.mu.Unlock()
		harnesses.remove(p)
		close(p.done)
	}()
	if err := <-started; err != nil {
		return nil, err
	}
	return p, nil
}

// stop asks the CLI and its group to stop, and kills the group after harnessGrace.
func (p *harnessProcess) stop() { p.stopWithin(harnessGrace) }

// stopWithin sends SIGTERM and SIGCONT to the group, the first time, and kills the group
// after grace, or at the earlier deadline of a previous call. Once the group is gone it does
// nothing, so that a reused group ID is never signalled.
func (p *harnessProcess) stopWithin(grace time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.finished {
		return
	}
	if !p.stopping {
		p.stopping = true
		signalGroup(p.pgid, syscall.SIGTERM, syscall.SIGCONT)
	}
	deadline := time.Now().Add(grace)
	if p.kill != nil {
		if !deadline.Before(p.killAt) {
			return
		}
		p.kill.Stop()
	}
	p.killAt = deadline
	p.kill = time.AfterFunc(grace, func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		if !p.finished {
			signalGroup(p.pgid, syscall.SIGKILL)
		}
	})
}

// stopForShutdown stops the CLI within signalGrace because the provider got SIGINT.
func (p *harnessProcess) stopForShutdown() {
	p.mu.Lock()
	if !p.finished {
		p.shutdown = true
	}
	p.mu.Unlock()
	p.stopWithin(signalGrace)
}

// signalGroup sends signals to a process group. A group that is gone is not an error.
func signalGroup(pgid int, sigs ...syscall.Signal) {
	for _, sig := range sigs {
		_ = syscall.Kill(-pgid, sig)
	}
}

// groupAlive reports whether a process group still has members (zombies count).
func groupAlive(pgid int) bool {
	return !errors.Is(syscall.Kill(-pgid, 0), syscall.ESRCH)
}

// stopGroups waits up to grace for the groups to be gone, after sending them SIGTERM and
// SIGCONT when term is set, and kills the ones that remain.
func stopGroups(pgids []int, grace time.Duration, term bool) {
	if term {
		for _, g := range pgids {
			signalGroup(g, syscall.SIGTERM, syscall.SIGCONT)
		}
	}
	deadline := time.Now().Add(grace)
	for {
		pgids = slices.DeleteFunc(pgids, func(g int) bool { return !groupAlive(g) })
		if len(pgids) == 0 || !time.Now().Before(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	for _, g := range pgids {
		signalGroup(g, syscall.SIGKILL)
	}
}

// harnessRegistry tracks the CLIs that run, and whether the provider is shutting down. A
// CLI starts under the lock, so the signal watcher sees every CLI that started before it
// took the lock, and none starts after that.
type harnessRegistry struct {
	sync.Mutex
	running  map[*harnessProcess]bool
	shutdown bool
}

var harnesses = &harnessRegistry{running: map[*harnessProcess]bool{}}

// errShuttingDown is the error of a CLI that would start after the provider got SIGINT.
var errShuttingDown = errors.New("the provider is shutting down (Pulumi asked it to stop), so no claude CLI starts")

// start starts the CLI and tracks it, unless the provider is shutting down.
func (h *harnessRegistry) start(p *harnessProcess, cmd *exec.Cmd) error {
	h.Lock()
	defer h.Unlock()
	if h.shutdown {
		return errShuttingDown
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	p.pgid = cmd.Process.Pid
	h.running[p] = true
	return nil
}

func (h *harnessRegistry) remove(p *harnessProcess) {
	h.Lock()
	defer h.Unlock()
	delete(h.running, p)
}

// shutDown refuses every later start and returns the CLIs that run.
func (h *harnessRegistry) shutDown() []*harnessProcess {
	h.Lock()
	defer h.Unlock()
	h.shutdown = true
	out := make([]*harnessProcess, 0, len(h.running))
	for p := range h.running {
		out = append(out, p)
	}
	return out
}

var watchSignals sync.Once

// WatchShutdownSignals makes a SIGINT to the process (Pulumi stops a provider with SIGINT)
// stop the Claude Code CLIs of coding-harness agents: no CLI starts any more, and the
// groups of the running ones get SIGTERM and SIGCONT, and SIGKILL after signalGrace. Every
// later SIGINT does the same for the CLIs that still run.
//
// It only observes SIGINT: the process ends as its own handler decides. In the provider
// binary that is the Pulumi SDK's provider.MainContext, which stops the gRPC server
// gracefully. A process without a SIGINT handler of its own no longer ends on SIGINT. SIGTERM
// is not watched and still ends the process at once.
//
// The provider calls it when the engine configures it, after the Pulumi SDK installed its
// own handler; startHarness and Run (for a run that allows harness agents) call it too.
func WatchShutdownSignals() {
	watchSignals.Do(func() {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, os.Interrupt)
		go func() {
			for range ch {
				for _, p := range harnesses.shutDown() {
					p.stopForShutdown()
				}
			}
		}()
	})
}
