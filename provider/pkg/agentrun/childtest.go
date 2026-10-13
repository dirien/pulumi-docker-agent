// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package agentrun

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"
)

// A run child is a new process: what the tests of this package change in it (the process
// handling's durations and limits, faults of the process table, a CA certificate of a fake
// model server) would be lost. In a test binary the parent therefore sends the tests' values
// with the request (childTest), and a run child of a test binary applies them before it runs.
// Neither happens outside a test binary (testing.Testing).

// childTest is what the tests change in a run child.
type childTest struct {
	HarnessGrace  time.Duration `json:"harnessGrace"`
	PostExitGrace time.Duration `json:"postExitGrace"`
	HarnessDrain  time.Duration `json:"harnessDrain"`
	SignalGrace   time.Duration `json:"signalGrace"`
	HarnessOS     string        `json:"harnessOS"`
	// The codex runner's durations (harness_codex.go).
	InterruptGrace     time.Duration `json:"interruptGrace"`
	CodexProbeTimeout  time.Duration `json:"codexProbeTimeout"`
	ProcPollInterval   time.Duration `json:"procPollInterval"`
	CookieSweepTimeout time.Duration `json:"cookieSweepTimeout"`
	MaxProcTable       int           `json:"maxProcTable"`
	MaxTrackedProcs    int           `json:"maxTrackedProcs"`
	MaxCookieReads     int           `json:"maxCookieReads"`
	// ProcTableError makes every read of the process table fail with it; NoProcs makes every
	// process unknown (testFaults).
	ProcTableError string `json:"procTableError,omitempty"`
	NoProcs        bool   `json:"noProcs,omitempty"`
	// RootCAs are PEM certificates the child's HTTP clients trust (testFaults.rootCAs).
	RootCAs []string `json:"rootCAs,omitempty"`

	// The faults a test asks one run for (Request.test).
	childFaults
}

// childFaults are what a test makes one run child do.
type childFaults struct {
	// Stray is written to os.Stdout, to file descriptor 1 and to stderr before the run.
	Stray string `json:"stray,omitempty"`
	// Panic makes the child panic with it before the run.
	Panic string `json:"panic,omitempty"`
	// Exit makes the child exit with the status before the run, without a result.
	Exit int `json:"exit,omitempty"`
	// Hang makes the child wait for the run's cancellation instead of running, then write
	// Stray to stderr and stdout, write the cancellation's cause to CauseFile and send it as
	// the error.
	Hang      bool   `json:"hang,omitempty"`
	CauseFile string `json:"causeFile,omitempty"`
	// PIDFile is where a hanging child writes its PID.
	PIDFile string `json:"pidFile,omitempty"`
	// EnvFile is where the child writes its environment (os.Environ, after Request.Env was set).
	EnvFile string `json:"envFile,omitempty"`
	// SkipHello makes the child send no handshake (as a different program would not).
	SkipHello bool `json:"skipHello,omitempty"`
}

// testFaults are faults that tests inject into this process (the process table) and that a
// run child must get too, because they replace functions there. Never set outside tests.
var testFaults struct {
	procTableError string
	noProcs        bool
	rootCAs        []string
}

// snapshotChildTest returns the tests' current values and the faults of one run.
func snapshotChildTest(f *childFaults) *childTest {
	t := &childTest{
		HarnessGrace: harnessGrace, PostExitGrace: postExitGrace, HarnessDrain: harnessDrain, SignalGrace: signalGrace,
		HarnessOS: harnessOS, ProcTableError: testFaults.procTableError, NoProcs: testFaults.noProcs,
		RootCAs:        testFaults.rootCAs,
		InterruptGrace: interruptGrace, CodexProbeTimeout: codexProbeTimeout,
	}
	snapshotProcLimits(t)
	if f != nil {
		t.childFaults = *f
	}
	return t
}

// apply sets the tests' values in the run child and injects the faults. It returns stop and
// the exit status when the child must not run the request.
func (t *childTest) apply(ctx context.Context, w *childWriter) (status int, stop bool) {
	harnessGrace, postExitGrace, harnessDrain, signalGrace = t.HarnessGrace, t.PostExitGrace, t.HarnessDrain, t.SignalGrace
	interruptGrace, codexProbeTimeout = t.InterruptGrace, t.CodexProbeTimeout
	harnessOS = t.HarnessOS
	applyProcLimits(t)
	if len(t.RootCAs) > 0 {
		pool := x509.NewCertPool()
		for _, pem := range t.RootCAs {
			pool.AppendCertsFromPEM([]byte(pem))
		}
		if dt, ok := http.DefaultTransport.(*http.Transport); ok {
			dt.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
		}
	}
	f := t.childFaults
	if f.Stray != "" && !f.Hang {
		writeStray(f.Stray)
	}
	switch {
	case f.Panic != "":
		panic(f.Panic)
	case f.Exit != 0:
		os.Exit(f.Exit)
	case f.Hang:
		if f.PIDFile != "" {
			_ = os.WriteFile(f.PIDFile, []byte(strconv.Itoa(os.Getpid())), 0o600)
		}
		w.send(childMessage{Type: msgTimed})
		w.send(childMessage{Type: msgProgress, Message: "hanging", Persistent: true})
		<-ctx.Done()
		if f.Stray != "" {
			// After the parent is gone: the write to the broken stderr must not end the child.
			writeStray(f.Stray)
			w.send(childMessage{Type: msgProgress, Message: f.Stray})
		}
		if f.CauseFile != "" {
			_ = os.WriteFile(f.CauseFile, []byte(context.Cause(ctx).Error()), 0o600)
		}
		w.send(childMessage{Type: msgError, Message: fmt.Sprintf("hung until canceled: %v", context.Cause(ctx))})
		return 0, true
	}
	return 0, false
}

// writeStray writes s, on a line of its own, to os.Stdout, to file descriptor 1 and to stderr.
func writeStray(s string) {
	fmt.Fprintln(os.Stdout, "stdout: "+s)
	writeFD1("fd1: " + s + "\n")
	fmt.Fprintln(os.Stderr, "stderr: "+s)
}
