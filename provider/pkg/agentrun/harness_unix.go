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
	"sync"
	"syscall"
	"time"
)

// A Claude Code CLI runs in a process group of its own, and the processes started under it
// are found by procTree (descendants.go): recorded while it runs, and found by the run's
// cookie at each cleanup step. Claude Code runs every shell command in a session of its own,
// out of the group's reach, and leaves the ones it ran in the background running when it
// exits. Whatever the CLI does with them, the runner stops the CLI's group and those
// processes (with the groups and sessions they created) on every end of a run:
//
//   - a cancelled or timed-out run sends SIGTERM and SIGCONT to the group and those
//     processes (SIGCONT wakes a stopped one), and SIGKILL to what is left after harnessGrace,
//     or postExitGrace after the CLI exited if that comes first. The process table is read
//     right before, while the CLI's group is stopped (SIGSTOP): the CLI cannot start another
//     process between the read and the signals. Its commands in other sessions keep running
//     and can start more; the later reads find those;
//   - after the CLI exited, the rest of the group and those processes get SIGTERM and SIGCONT
//     (unless a stop sent them already), and SIGKILL after postExitGrace (or at the earlier
//     deadline of a stop). On Linux this happens while the exited CLI is not reaped yet, so
//     its PID still reserves the group's ID; on macOS right after it was reaped (the kernel
//     does not reuse the ID of a group that still has members). When the CLI exited on its
//     own and left processes running outside its group, the run reports how many it stopped
//     (harnessProcess.leftRunning); what is left in the CLI's own group (an MCP server the CLI
//     was still shutting down) is stopped without a report;
//   - a SIGINT to the provider (WatchShutdownSignals) stops new CLIs from starting and
//     sends the running ones, their groups and their processes SIGTERM and SIGCONT, and
//     SIGKILL after signalGrace. The watcher only observes the signal: the Pulumi SDK's own
//     handler (provider.MainContext) ends the provider as it always does;
//   - on Linux the kernel sends SIGTERM to a CLI when the provider dies, also by SIGKILL
//     (Pdeathsig, harness_linux.go). That reaches the CLI only.
//
// What can run after the provider ended:
//
//   - Pulumi stops a provider with SIGINT, and with SIGKILL 5 s later. After a SIGINT the
//     SDK stops the gRPC server gracefully, which waits for the operations in flight. A CLI
//     only runs inside one (an AgentRun create, update or delete), and that operation ends
//     only after the CLI exited and the rest of its group and its processes were stopped:
//     within about signalGrace + max(postExitGrace, harnessDrain) = 4 s. So on both systems
//     they are gone before the provider exits. That leaves what procTree did not find (see
//     its limits);
//   - when the provider dies before that (SIGKILL after 5 s, a crash, a SIGTERM from
//     outside Pulumi, which the provider does not handle), nothing in the provider stops
//     anything any more. On Linux every CLI that still runs gets SIGTERM from the kernel;
//     the other processes of its group and everything it started in other sessions get no
//     signal and keep running unless the CLI stops them. On macOS, which has no parent-death
//     signal, the CLIs and everything they started keep running unattended (with
//     --dangerously-skip-permissions) until they finish or fail writing to the closed
//     output pipe.

// startHarness starts the CLI in a process group of its own, records the processes started
// under it, and watches it until it exited and its group and those processes were stopped.
// debug receives tracking problems. It fails with errShuttingDown after a SIGINT.
func startHarness(cmd *exec.Cmd, debug func(string)) (*harnessProcess, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	setPdeathsig(cmd.SysProcAttr)
	// Every process the CLI starts inherits the cookie, unless it clears its environment.
	cookie := newRunCookie()
	if cookie != "" {
		cmd.Env = withRunCookie(cmd.Env, cookie)
	}
	WatchShutdownSignals()
	p := &harnessProcess{exited: make(chan struct{}), done: make(chan struct{})}
	started := make(chan error, 1)
	go func() {
		// The parent-death signal is tied to the thread that started the process: the thread
		// must not exit before the process was reaped.
		goruntime.LockOSThread()
		if err := harnesses.start(p, cmd, cookie, debug); err != nil {
			goruntime.UnlockOSThread()
			started <- err
			return
		}
		started <- nil

		var left procTargets
		pinned := waitUnreaped(p.pgid)
		if pinned {
			left = p.termAfterExit()
		}
		p.waitErr = cmd.Wait()
		p.state = cmd.ProcessState
		p.exitedAt = time.Now()
		goruntime.UnlockOSThread()
		close(p.exited)
		if !pinned {
			left = p.termAfterExit()
		}
		p.killAfterExit(left)
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

// termAfterExit ends the recording when the CLI exited, and sends SIGTERM and SIGCONT to the
// rest of its group and to the processes started under it that still run (what a stop did
// not signal already), which it returns. When the CLI exited on its own, it counts them.
func (p *harnessProcess) termAfterExit() procTargets {
	p.tree.stopPolling()
	left := p.tree.scan()
	p.mu.Lock()
	stopping := p.stopping
	p.mu.Unlock()
	if !stopping {
		signalGroup(p.pgid, syscall.SIGTERM, syscall.SIGCONT)
	}
	p.tree.term(left)
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.stopping {
		p.leftRunning = left.outside
	}
	return left
}

// killAfterExit waits up to postExitGrace after the exit (or until the deadline of a stop)
// for the rest of the CLI's group and the processes started under it to end, and kills what
// is left. When nothing is left it returns at once.
func (p *harnessProcess) killAfterExit(left procTargets) {
	p.mu.Lock()
	deadline := p.exitedAt.Add(postExitGrace)
	if p.kill != nil && p.killAt.Before(deadline) {
		deadline = p.killAt
	}
	p.mu.Unlock()
	for (groupAlive(p.pgid) || p.tree.running(left)) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if groupAlive(p.pgid) {
		signalGroup(p.pgid, syscall.SIGKILL)
	}
	if !left.empty() || p.tree.tracking() {
		// Read the table again: what the stopped processes started meanwhile is killed too.
		p.tree.kill(p.tree.scan())
	}
}

// stop asks the CLI, its group and the processes started under it to stop, and kills them
// after harnessGrace.
func (p *harnessProcess) stop() { p.stopWithin(harnessGrace) }

// stopWithin sends SIGTERM and SIGCONT to the group and the processes started under the CLI,
// the first time, and kills them after grace, or at the earlier deadline of a previous call
// (killAfterExit kills them postExitGrace after the CLI exited, if that comes first).
// Once the group is gone it does nothing, so that a reused group ID is never signalled.
func (p *harnessProcess) stopWithin(grace time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.finished {
		return
	}
	if !p.stopping {
		p.stopping = true
		left := p.scanFrozen()
		signalGroup(p.pgid, syscall.SIGTERM, syscall.SIGCONT)
		p.tree.term(left)
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
			left := p.scanFrozen()
			signalGroup(p.pgid, syscall.SIGKILL)
			p.tree.kill(left)
		}
	})
}

// scanFrozen reads the process table while the CLI's group is stopped (SIGSTOP): the CLI
// and the rest of its group cannot start a process between the read and the signals that
// follow, which end with SIGCONT or SIGKILL. Processes in other sessions keep running; what
// they start is reached through their recorded groups and sessions, the cookie, or the
// later reads. Without tracking it does nothing.
func (p *harnessProcess) scanFrozen() procTargets {
	if p.tree == nil {
		return procTargets{}
	}
	signalGroup(p.pgid, syscall.SIGSTOP)
	return p.tree.scan()
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

// start starts the CLI, begins recording the processes started under it, and tracks it,
// unless the provider is shutting down.
func (h *harnessRegistry) start(p *harnessProcess, cmd *exec.Cmd, cookie string, debug func(string)) error {
	h.Lock()
	defer h.Unlock()
	if h.shutdown {
		return errShuttingDown
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	p.pgid = cmd.Process.Pid
	p.tree = newProcTree(p.pgid, cookie, debug)
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
// running ones, their groups and the processes started under them get SIGTERM and SIGCONT,
// and SIGKILL after signalGrace. Every later SIGINT does the same for the CLIs that still
// run.
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
