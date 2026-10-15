// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package agentrun

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/docker/docker-agent/pkg/atomicfile"
	"github.com/docker/docker-agent/pkg/chatgpt"
	"github.com/docker/docker-agent/pkg/paths"
)

// A run child is a new process: what the tests of this package change in it (the process
// handling's durations and limits, faults of the process table, a CA certificate of a fake
// model server) would be lost. In a test binary the parent therefore sends the tests' values
// with the request (childTest), and a run child of a test binary applies them before it runs.
// Neither happens outside a test binary (testing.Testing).

// childTest is what the tests change in a run child.
type childTest struct {
	HarnessGrace       time.Duration `json:"harnessGrace"`
	PostExitGrace      time.Duration `json:"postExitGrace"`
	HarnessDrain       time.Duration `json:"harnessDrain"`
	SignalGrace        time.Duration `json:"signalGrace"`
	ToolsetStopTimeout time.Duration `json:"toolsetStopTimeout"`
	HarnessOS          string        `json:"harnessOS"`
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
	// ChatGPTLockTimeout is chatgptLockTimeout; ChatGPTRefresh replaces the refresh of the
	// ChatGPT login (testFaults.chatgptRefresh).
	ChatGPTLockTimeout time.Duration       `json:"chatgptLockTimeout"`
	ChatGPTRefresh     *chatgptRefreshFake `json:"chatgptRefresh,omitempty"`

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
	// SlowTeardown makes the child wait this long after it sent its result or error, before it
	// stops the runtime and the toolsets.
	SlowTeardown time.Duration `json:"slowTeardown,omitempty"`
	// BrokenAfterEnd makes the child close its writer after it sent its result or error, and
	// write a broken message to the protocol.
	BrokenAfterEnd bool `json:"brokenAfterEnd,omitempty"`
	// ProtocolFile is where the child writes what its protocol and request descriptors are
	// (Linux: their /proc/self/fd targets), before the run.
	ProtocolFile string `json:"protocolFile,omitempty"`
}

// testFaults are faults that tests inject into this process (the process table) and that a
// run child must get too, because they replace functions there. Never set outside tests.
var testFaults struct {
	procTableError string
	noProcs        bool
	rootCAs        []string
	chatgptRefresh *chatgptRefreshFake
}

// chatgptRefreshFake is a refresh of the ChatGPT login in a run child (chatgptAccessToken): the
// token endpoint of the chatgpt package cannot be redirected. An expired copy gets the tokens
// Access and Refresh, valid for an hour, or fails with Err; Calls, when set, is a file that
// gets a line per refresh.
type chatgptRefreshFake struct {
	Access  string `json:"access,omitempty"`
	Refresh string `json:"refresh,omitempty"`
	Err     string `json:"err,omitempty"`
	Calls   string `json:"calls,omitempty"`
}

// accessToken is chatgpt.AccessToken with the fake refresh.
func (f chatgptRefreshFake) accessToken(context.Context) (string, error) {
	c, err := chatgpt.Load()
	if err != nil {
		return "", err
	}
	if !c.Expired() {
		return c.AccessToken, nil
	}
	if f.Calls != "" {
		if lf, err := os.OpenFile(f.Calls, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
			_, _ = lf.WriteString("refresh\n")
			_ = lf.Close()
		}
	}
	if f.Err != "" {
		return "", errors.New(f.Err)
	}
	c.AccessToken, c.RefreshToken, c.ExpiresAt = f.Access, f.Refresh, time.Now().Add(time.Hour)
	data, err := json.MarshalIndent(c, "", "  ") //nolint:gosec // a test login, saved as chatgpt.AccessToken saves it
	if err != nil {
		return "", err
	}
	if err := atomicfile.Write(filepath.Join(paths.GetConfigDir(), chatgptAuthFile), bytes.NewReader(data), 0o600); err != nil {
		return "", err
	}
	return c.AccessToken, nil
}

// snapshotChildTest returns the tests' current values and the faults of one run.
func snapshotChildTest(f *childFaults) *childTest {
	t := &childTest{
		HarnessGrace: harnessGrace, PostExitGrace: postExitGrace, HarnessDrain: harnessDrain, SignalGrace: signalGrace,
		ToolsetStopTimeout: toolsetStopTimeout, HarnessOS: harnessOS, ProcTableError: testFaults.procTableError,
		NoProcs: testFaults.noProcs, RootCAs: testFaults.rootCAs,
		InterruptGrace: interruptGrace, CodexProbeTimeout: codexProbeTimeout,
		ChatGPTLockTimeout: chatgptLockTimeout, ChatGPTRefresh: testFaults.chatgptRefresh,
	}
	snapshotProcLimits(t)
	if f != nil {
		t.childFaults = *f
	}
	return t
}

// apply sets the tests' values in the run child and injects the faults. It returns stop and
// the exit status when the child must not run the request (input is the request's stream).
func (t *childTest) apply(ctx context.Context, w *childWriter, input io.Reader) (status int, stop bool) {
	harnessGrace, postExitGrace, harnessDrain, signalGrace = t.HarnessGrace, t.PostExitGrace, t.HarnessDrain, t.SignalGrace
	toolsetStopTimeout = t.ToolsetStopTimeout
	interruptGrace, codexProbeTimeout = t.InterruptGrace, t.CodexProbeTimeout
	harnessOS = t.HarnessOS
	chatgptLockTimeout = t.ChatGPTLockTimeout
	if t.ChatGPTRefresh != nil {
		chatgptAccessToken = t.ChatGPTRefresh.accessToken
	}
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
	if f.ProtocolFile != "" {
		_ = os.WriteFile(f.ProtocolFile, []byte("protocol "+fdTarget(w.out)+"\nrequest "+fdTarget(input)+"\n"), 0o600)
	}
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
		msg := fmt.Sprintf("hung until canceled: %v", context.Cause(ctx))
		if errors.Is(context.Cause(ctx), context.DeadlineExceeded) {
			// As run reports the parent's deadline, which the child shares: both may notice it first.
			msg = "the operation's time limit (customTimeouts) ran out before the agent finished"
		}
		w.send(childMessage{Type: msgError, Message: msg})
		return 0, true
	}
	return 0, false
}

// afterEnd runs after the child sent its result or error, before its teardown.
func (t *childTest) afterEnd(w *childWriter) {
	if t.BrokenAfterEnd {
		w.close()
		_, _ = w.out.Write([]byte(childMagic + "{broken\n"))
	}
	if t.SlowTeardown > 0 {
		time.Sleep(t.SlowTeardown)
	}
}

// writeStray writes s, on a line of its own, to os.Stdout, to file descriptor 1 and to stderr.
func writeStray(s string) {
	fmt.Fprintln(os.Stdout, "stdout: "+s)
	writeFD1("fd1: " + s + "\n")
	fmt.Fprintln(os.Stderr, "stderr: "+s)
}
