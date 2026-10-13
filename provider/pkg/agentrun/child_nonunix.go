// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

//go:build !linux && !darwin

package agentrun

import (
	"context"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// Without process groups to signal (Windows), a run child is supervised through its stdin and
// a job object (child_windows.go):
//
//   - it starts in a process group of its own (CREATE_NEW_PROCESS_GROUP), so the CTRL_BREAK
//     with which Pulumi stops the provider's group does not end it before it cleaned up;
//   - a cancellation closes its stdin, which makes it cancel the run and stop its tools; it
//     is killed after childGrace (signalGrace when the provider shuts down);
//   - it runs in a job object that kills its processes when the job is closed: when the
//     provider dies, and after the child ended, when what it left running is stopped and
//     counted.

// stdinRunChild is a run child supervised through its stdin.
type stdinRunChild struct {
	cmd        *exec.Cmd
	closeStdin func()
	job        childJob

	exitedCh, doneCh chan struct{}
	waitErr          error

	mu       sync.Mutex
	finished bool
	stopping bool
	shutdown bool
	killer   *time.Timer
	killAt   time.Time
	left     int
}

// runChildren are the run children that run, and whether the provider is shutting down.
var runChildren = struct {
	sync.Mutex
	running  map[*stdinRunChild]bool
	shutdown bool
}{running: map[*stdinRunChild]bool{}}

// startRunChild starts a run child; closeStdin closes its stdin.
func startRunChild(cmd *exec.Cmd, closeStdin func(), debug func(string)) (runChildProcess, error) {
	cmd.SysProcAttr = childSysProcAttr()
	WatchShutdownSignals()
	c := &stdinRunChild{cmd: cmd, closeStdin: closeStdin, exitedCh: make(chan struct{}), doneCh: make(chan struct{})}
	runChildren.Lock()
	if runChildren.shutdown {
		runChildren.Unlock()
		return nil, shuttingDownError("agent run")
	}
	// The child starts nothing before it has read the request, which is written after it was
	// assigned to the job.
	c.job = newChildJob(debug)
	if err := cmd.Start(); err != nil {
		runChildren.Unlock()
		c.job.close()
		return nil, err
	}
	c.job.assign(cmd.Process)
	runChildren.running[c] = true
	runChildren.Unlock()

	go func() {
		c.waitErr = cmd.Wait()
		close(c.exitedCh)
		left := c.job.finish()
		c.mu.Lock()
		c.finished = true
		if c.killer != nil {
			c.killer.Stop()
		}
		if !c.stopping {
			c.left = left
		}
		c.mu.Unlock()
		runChildren.Lock()
		delete(runChildren.running, c)
		runChildren.Unlock()
		close(c.doneCh)
	}()
	return c, nil
}

func (c *stdinRunChild) stop() { c.stopWithin(childGrace(), false) }

// stopWithin closes the child's stdin the first time, and kills it after grace, or at the
// earlier deadline of a previous call.
func (c *stdinRunChild) stopWithin(grace time.Duration, shutdown bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.finished {
		return
	}
	if shutdown {
		c.shutdown = true
	}
	if !c.stopping {
		c.stopping = true
		c.closeStdin()
	}
	deadline := time.Now().Add(grace)
	if c.killer != nil {
		if !deadline.Before(c.killAt) {
			return
		}
		c.killer.Stop()
	}
	c.killAt = deadline
	c.killer = time.AfterFunc(grace, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if !c.finished {
			_ = c.cmd.Process.Kill()
		}
	})
}

func (c *stdinRunChild) kill() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stopping = true
	if !c.finished {
		_ = c.cmd.Process.Kill()
	}
}

func (c *stdinRunChild) exited() <-chan struct{} { return c.exitedCh }
func (c *stdinRunChild) done() <-chan struct{}   { return c.doneCh }

func (c *stdinRunChild) state() (*os.ProcessState, error) { return c.cmd.ProcessState, c.waitErr }

func (c *stdinRunChild) stoppedForShutdown() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.shutdown
}

func (c *stdinRunChild) leftRunning() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.left
}

// shutDownRunChildren returns the run children that run, refusing every later one.
func shutDownRunChildren() []*stdinRunChild {
	runChildren.Lock()
	defer runChildren.Unlock()
	runChildren.shutdown = true
	out := make([]*stdinRunChild, 0, len(runChildren.running))
	for c := range runChildren.running {
		out = append(out, c)
	}
	return out
}

var watchSignals sync.Once

// WatchShutdownSignals makes an interrupt of the process (Pulumi stops a provider with
// CTRL_BREAK on Windows) stop the agent runs: none starts any more, and the running run
// children are asked to stop and killed after signalGrace. It only observes the interrupt:
// the process ends as its own handler decides (the Pulumi SDK's provider.MainContext). The
// provider calls it when the engine configures it; Run calls it too.
func WatchShutdownSignals() {
	watchSignals.Do(func() {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, os.Interrupt)
		go func() {
			for range ch {
				for _, c := range shutDownRunChildren() {
					c.stopWithin(signalGrace, true)
				}
			}
		}()
	})
}

// takeChildStdio takes the protocol (stdout) and the request (stdin) away from the standard
// streams of a run child: os.Stdout becomes os.Stderr and os.Stdin the null device, and the
// process's standard handles follow (child_windows.go).
func takeChildStdio() (proto, input *os.File, err error) {
	proto, input = os.Stdout, os.Stdin
	null, err := os.Open(os.DevNull)
	if err != nil {
		return nil, nil, err
	}
	os.Stdout, os.Stdin = os.Stderr, null
	redirectStdHandles(null)
	return proto, input, nil
}

// watchChildSignals cancels the run on an interrupt (a shutdown) and on SIGTERM (Windows
// sends it when the console closes or the session ends).
func watchChildSignals(cancel context.CancelCauseFunc) func() {
	ch := make(chan os.Signal, 2)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			case sig := <-ch:
				if sig == os.Interrupt {
					cancel(errChildShutdown)
				} else {
					cancel(errChildCanceled)
				}
			}
		}
	}()
	return func() {
		signal.Stop(ch)
		close(done)
	}
}

// childExecutable is the provider's own executable for a run child.
func childExecutable() (string, error) { return os.Executable() }

// protectChild does nothing on this system.
func protectChild() {}

// writeFD1 writes to the standard output the process started with (a test's stray output).
func writeFD1(s string) { _, _ = syscall.Write(syscall.Stdout, []byte(s)) }

// snapshotProcLimits and applyProcLimits do nothing: no process table is read on this system.
func snapshotProcLimits(*childTest) {}
func applyProcLimits(*childTest)    {}
