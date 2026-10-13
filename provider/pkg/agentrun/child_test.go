// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package agentrun

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker-agent/pkg/config"
	"github.com/docker/docker-agent/pkg/config/latest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	version "github.com/dirien/pulumi-docker-agent/provider"
	"github.com/dirien/pulumi-docker-agent/provider/pkg/agentrun/fakeopenai"
)

// The tests of child.go: every run runs in a run child, the test binary started again
// (TestMain calls MaybeRunChild). Request.test makes one child misbehave (childtest.go).

// logRecorder collects a run's log records.
type logRecorder struct {
	mu   sync.Mutex
	msgs []string
}

func (l *logRecorder) log(level slog.Level, msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.msgs = append(l.msgs, level.String()+" "+msg)
}

func (l *logRecorder) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.msgs, "\n")
}

// hangingRequest is a run whose child waits for its cancellation instead of running (no model
// is called).
func hangingRequest(t *testing.T) (Request, string) {
	t.Helper()
	cause := filepath.Join(t.TempDir(), "cause")
	return Request{Name: "test", YAML: []byte(rootAgent), Prompt: "hi",
		test: &childFaults{Hang: true, CauseFile: cause}}, cause
}

// readCause returns why a hanging child ended, once it wrote it.
func readCause(t *testing.T, file string) string {
	t.Helper()
	var cause string
	require.Eventually(t, func() bool {
		b, err := os.ReadFile(file)
		cause = string(b)
		return err == nil && cause != ""
	}, 10*time.Second, 10*time.Millisecond, "the child wrote no cause")
	return cause
}

func TestChildInvocation(t *testing.T) {
	assert.True(t, isChildInvocation([]string{"provider", childArg}))
	assert.True(t, isChildInvocation([]string{"provider.test", childTestArg, childArg}), "in a test binary")
	assert.False(t, isChildInvocation([]string{"provider"}))
	assert.False(t, isChildInvocation([]string{"provider", "-test.run=^$"}))
	assert.False(t, isChildInvocation([]string{"provider", childArg, "more"}))
	assert.False(t, isChildInvocation([]string{"provider", "x", childArg}))
	assert.Equal(t, []string{childTestArg, childArg}, childArgs(), "a test binary's child runs no tests")
}

// TestChildSpawnGuard: a run child never starts another, and a program that did not call
// MaybeRunChild never starts copies of itself.
func TestChildSpawnGuard(t *testing.T) {
	req := Request{YAML: []byte(rootAgent), Prompt: "hi"}
	inChild.Store(true)
	_, err := Run(t.Context(), req)
	inChild.Store(false)
	assert.EqualError(t, err, "an agent run cannot start another agent run")

	childEntry.Store(false)
	_, err = Run(t.Context(), req)
	childEntry.Store(true)
	assert.ErrorContains(t, err, "must call agentrun.MaybeRunChild first")
}

// TestReadChildOutput: protocol lines, other output (also before a message on the same line),
// messages larger than bufio's line buffer, and a message over the limit.
func TestReadChildOutput(t *testing.T) {
	big := strings.Repeat("x", 200<<10)
	line := func(m childMessage) string {
		b, err := json.Marshal(m)
		require.NoError(t, err)
		return childMagic + string(b) + "\n"
	}
	input := "a stray line\n" +
		line(childMessage{Type: msgHello, Protocol: 1, Version: "v"}) +
		"no newline" + line(childMessage{Type: msgProgress, Message: "p"}) +
		line(childMessage{Type: msgResult, Result: &childResult{Output: big}}) +
		childMagic + "{not json\n" +
		childMagic + strings.Repeat("y", 300) + "\n" +
		strings.Repeat("z", 300) + "\n" +
		"the end"
	events := make(chan childEvent, 16)
	go readChildOutputLimited(strings.NewReader(input), events, 250)
	var got []childEvent
	for ev := range events {
		got = append(got, ev)
	}
	require.Len(t, got, 9)
	assert.Equal(t, "a stray line", got[0].stray)
	assert.Equal(t, msgHello, got[1].msg.Type)
	assert.Equal(t, "no newline", got[2].stray, "the stray part of the line")
	assert.Equal(t, "p", got[3].msg.Message, "the message after it")
	assert.ErrorContains(t, got[4].err, "a message larger than", "over the limit of this reader")
	assert.ErrorContains(t, got[5].err, "reading a message of the agent run process")
	assert.ErrorContains(t, got[6].err, "a message larger than")
	assert.Equal(t, strings.Repeat("z", 250), got[7].stray, "a stray line over the limit, raw")
	assert.True(t, got[7].cut)
	assert.False(t, got[0].cut)
	assert.Equal(t, "the end", got[8].stray)

	// The real limit takes a large answer.
	events = make(chan childEvent, 4)
	go readChildOutput(strings.NewReader(line(childMessage{Type: msgResult, Result: &childResult{Output: big}})), events)
	ev := <-events
	require.NoError(t, ev.err)
	assert.Equal(t, big, ev.msg.Result.Output)
}

// TestChildLargeAnswer: an answer far larger than a pipe's buffer and bufio's line limit.
func TestChildLargeAnswer(t *testing.T) {
	answer := strings.Repeat("A long answer. ", 1<<16)
	srv := fakeopenai.New(func(fakeopenai.Request) fakeopenai.Reply { return fakeopenai.Reply{Content: answer} })
	defer srv.Close()
	res, err := Run(t.Context(), request(srv, rootAgent, "hi"))
	require.NoError(t, err)
	assert.Len(t, res.Output, len(answer))
	assert.True(t, answer == res.Output, "the answer arrives unchanged")
}

// TestChildStructuredOutput: the structured output survives the protocol exactly.
func TestChildStructuredOutput(t *testing.T) {
	const doc = `{"n":12345678901234567,"f":0.1,"s":"x\u2028y","l":[null,true],"o":{}}`
	srv := fakeopenai.New(func(fakeopenai.Request) fakeopenai.Reply { return fakeopenai.Reply{Content: doc} })
	defer srv.Close()
	body := rootAgent + "    structured_output:\n      name: any\n      schema:\n        type: object\n"
	res, err := Run(t.Context(), request(srv, body, "hi"))
	require.NoError(t, err)
	var want any
	require.NoError(t, json.Unmarshal([]byte(doc), &want))
	assert.True(t, res.HasStructuredOutput)
	assert.Equal(t, want, res.StructuredOutput)
	assert.Equal(t, doc, res.Output)
}

// TestChildStrayOutputIsLoggedAndRedacted: what the child writes to stdout (through os.Stdout
// or the file descriptor) or stderr reaches the log at debug level, redacted twice: with the
// child's secrets (env) and with the provider's (a credential the child does not inherit).
func TestChildStrayOutputIsLoggedAndRedacted(t *testing.T) {
	const parentOnly = "parent-only-credential-0123456789"
	t.Setenv("PARENT_ONLY_TOKEN", parentOnly)
	srv := fakeopenai.New(nil)
	defer srv.Close()
	var logs logRecorder
	req := request(srv, rootAgent, "hi")
	req.Log = logs.log
	req.test = &childFaults{Stray: "stray " + fakeKey + " " + parentOnly}
	res, err := Run(t.Context(), req)
	require.NoError(t, err)
	assert.Equal(t, "You said: hi", res.Output, "the protocol is intact")

	all := logs.String()
	const redacted = "stray [secret] [secret]"
	assert.Contains(t, all, "DEBUG the agent run process wrote to stderr: stdout: "+redacted, "os.Stdout is os.Stderr")
	assert.Contains(t, all, "DEBUG the agent run process wrote to stderr: stderr: "+redacted)
	switch runtime.GOOS {
	case "linux", "darwin", "windows":
		assert.Contains(t, all, "DEBUG the agent run process wrote to stderr: fd1: "+redacted,
			"file descriptor 1 (the standard output handle on Windows) of the child is its stderr")
	default:
		// The standard output descriptor the child started with is the protocol's pipe.
		assert.Contains(t, all, "DEBUG the agent run process wrote to stdout: fd1: "+redacted)
	}
	assert.NotContains(t, all, fakeKey)
	assert.NotContains(t, all, parentOnly)
}

// TestChildStrayLineIsRedactedBeforeItIsShortened: a stray line so long that the shortening
// cuts the secret in it loses the whole secret, not only its end (review finding 1).
func TestChildStrayLineIsRedactedBeforeItIsShortened(t *testing.T) {
	srv := fakeopenai.New(nil)
	defer srv.Close()
	var logs logRecorder
	req := request(srv, rootAgent, "hi")
	req.Log = logs.log
	// fakeKey straddles rune 500 of the lines the child writes ("stderr: …", "fd1: …").
	req.test = &childFaults{Stray: strings.Repeat("x", 480) + " " + fakeKey}
	_, err := Run(t.Context(), req)
	require.NoError(t, err)
	all := logs.String()
	assert.Contains(t, all, "DEBUG the agent run process wrote to stderr: stderr: "+strings.Repeat("x", 480)+" [secret]")
	assert.NotContains(t, all, fakeKey[:8])
}

func TestWithoutSecretHead(t *testing.T) {
	secrets := []string{fakeKey}
	assert.Equal(t, "cut here: [secret]", withoutSecretHead("cut here: "+fakeKey[:11], secrets))
	assert.Equal(t, "nothing to cut", withoutSecretHead("nothing to cut", secrets))
	assert.Equal(t, "whole "+fakeKey, withoutSecretHead("whole "+fakeKey, secrets), "redaction takes a whole one")
}

// TestChildSlowCallbacks: a parent whose callbacks are slow still gets the result of a child
// that exited long before its last messages were read: only the child holds the protocol's
// pipe, so the parent reads it to its end (review finding 2).
func TestChildSlowCallbacks(t *testing.T) {
	prev := harnessDrain
	harnessDrain = 300 * time.Millisecond
	t.Cleanup(func() { harnessDrain = prev })
	srv := fakeopenai.New(func(r fakeopenai.Request) fakeopenai.Reply {
		if len(r.ToolResults()) == 0 { // 120 shell calls, about 450 bytes of output each
			var calls []fakeopenai.ToolCall
			for i := range 120 {
				calls = append(calls, fakeopenai.ToolCall{Name: "shell",
					Arguments: `{"cmd":"printf '%0450d' ` + strconv.Itoa(i) + `"}`})
			}
			return fakeopenai.Reply{ToolCalls: calls}
		}
		return fakeopenai.Reply{Content: "all done"}
	})
	defer srv.Close()
	req := request(srv, shellAgent(""), "go")
	req.ToolApproval = ToolApprovalAllow
	req.Logging = LoggingFull
	req.Progress = func(Progress) { time.Sleep(10 * time.Millisecond) }
	res, err := Run(t.Context(), req)
	require.NoError(t, err)
	assert.Equal(t, "all done", res.Output)
	assert.Len(t, res.ToolCalls, 120)
}

// TestChildBrokenMessageAfterTheResult: a protocol error after the result does not discard
// it.
func TestChildBrokenMessageAfterTheResult(t *testing.T) {
	srv := fakeopenai.New(nil)
	defer srv.Close()
	var logs logRecorder
	req := request(srv, rootAgent, "hi")
	req.Log = logs.log
	req.test = &childFaults{BrokenAfterEnd: true}
	res, err := Run(t.Context(), req)
	require.NoError(t, err)
	assert.Equal(t, "You said: hi", res.Output)
	assert.Contains(t, logs.String(), "DEBUG the agent run process sent a broken message: reading a message of the "+
		"agent run process")
}

// TestChildTeardownAfterTheResult: as in process, the run is judged by the end of its
// conversation: a teardown (stopping the runtime and the toolsets) that outlasts
// customTimeouts or the timeoutSeconds backstop does not fail a run that ended in time, and
// one that outlasts childTeardownGrace is cut (review finding 4).
func TestChildTeardownAfterTheResult(t *testing.T) {
	srv := fakeopenai.New(nil)
	defer srv.Close()
	shorten := func(t *testing.T, ps ...*time.Duration) {
		for _, p := range ps {
			prev := *p
			*p = 100 * time.Millisecond
			t.Cleanup(func() { *p = prev })
		}
	}
	t.Run("customTimeouts", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
		defer cancel()
		req := request(srv, rootAgent, "hi")
		req.test = &childFaults{SlowTeardown: time.Second}
		res, err := Run(ctx, req)
		require.NoError(t, err)
		assert.Equal(t, "You said: hi", res.Output)
	})
	t.Run("timeoutSeconds", func(t *testing.T) {
		shorten(t, &harnessGrace, &harnessDrain)
		req := request(srv, rootAgent, "hi")
		req.Timeout = 500 * time.Millisecond
		req.test = &childFaults{SlowTeardown: req.Timeout + childGrace() + 500*time.Millisecond}
		start := time.Now()
		res, err := Run(t.Context(), req)
		require.NoError(t, err)
		assert.Equal(t, "You said: hi", res.Output)
		assert.GreaterOrEqual(t, time.Since(start), req.test.SlowTeardown, "the backstop did not end the teardown")
	})
	t.Run("childTeardownGrace", func(t *testing.T) {
		shorten(t, &harnessGrace, &harnessDrain, &toolsetStopTimeout)
		var logs logRecorder
		req := request(srv, rootAgent, "hi")
		req.Log = logs.log
		req.test = &childFaults{SlowTeardown: time.Minute}
		start := time.Now()
		res, err := Run(t.Context(), req)
		took := time.Since(start)
		require.NoError(t, err)
		assert.Equal(t, "You said: hi", res.Output)
		assert.Empty(t, res.Warnings, "the child left nothing running")
		assert.GreaterOrEqual(t, took, childTeardownGrace())
		assert.Less(t, took, childTeardownGrace()+5*time.Second, "the child was killed")
		assert.Contains(t, logs.String(), fmt.Sprintf("DEBUG the agent run process did not stop its runtime and "+
			"toolsets within %s after the run ended; AgentRun stopped it", childTeardownGrace()))
	})
}

// TestChildErrorBeforeTheDeadline: an error the child reported before the parent's deadline
// is the run's error, also when the deadline passes during the teardown (review finding 9).
func TestChildErrorBeforeTheDeadline(t *testing.T) {
	srv := fakeopenai.New(func(fakeopenai.Request) fakeopenai.Reply {
		return fakeopenai.Reply{Status: 401, ErrorMessage: "Incorrect API key provided"}
	})
	defer srv.Close()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	req := request(srv, rootAgent, "hi")
	req.test = &childFaults{SlowTeardown: 2 * time.Second}
	_, err := Run(ctx, req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the agent run failed: ")
	assert.Contains(t, err.Error(), "Incorrect API key provided")
	assert.Error(t, ctx.Err(), "the deadline passed during the teardown")
}

// TestChildEndOutcome: the precedence of how a run ends (childEnd.outcome, review finding 9).
func TestChildEndOutcome(t *testing.T) {
	noResult := func() error { return errors.New("no result") }
	res := &childResult{Output: "x"}
	canceled, deadline := context.Canceled, context.DeadlineExceeded
	const customTimeouts = "the operation's time limit (customTimeouts) ran out before the agent finished"
	for _, c := range []struct {
		name      string
		end       childEnd
		want      string
		fromChild bool
	}{
		{"a result wins", childEnd{result: res, shutdown: true, ctxErr: canceled, reason: stopTimeout}, "", false},
		{"the child's error before a stop wins over a shutdown and the context",
			childEnd{childErr: "the agent run failed: 401", first: true, shutdown: true, ctxErr: deadline, reason: stopContext},
			"the agent run failed: 401", true},
		{"a shutdown wins over a canceled context", childEnd{childErr: "the agent run was canceled: context canceled",
			shutdown: true, ctxErr: canceled, reason: stopContext}, errStoppedForShutdown.Error(), false},
		{"a shutdown wins over the deadline", childEnd{shutdown: true, ctxErr: deadline, reason: stopContext},
			errStoppedForShutdown.Error(), false},
		{"the deadline", childEnd{childErr: "late", ctxErr: deadline, reason: stopContext}, customTimeouts, false},
		{"the cancellation", childEnd{childErr: "late", ctxErr: canceled, reason: stopContext},
			"the agent run was canceled: context canceled", false},
		{"a protocol error", childEnd{protoErr: errors.New("broken"), reason: stopProtocol}, "broken", false},
		{"the backstop", childEnd{childErr: "late", reason: stopTimeout},
			"the agent did not finish within 1s (timeoutSeconds)", false},
		{"a late error of the child", childEnd{childErr: "late"}, "late", true},
		{"no result", childEnd{}, "no result", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			out := c.end.outcome(time.Second, noResult)
			if c.want == "" {
				require.NoError(t, out.err)
				assert.Equal(t, res, out.result)
				return
			}
			require.EqualError(t, out.err, c.want)
			assert.Nil(t, out.result)
			assert.Equal(t, c.fromChild, out.fromChild)
		})
	}
}

// TestChildEndsWithoutResult: a child that panics or exits without a result fails the run
// with its exit status and what it wrote, redacted.
func TestChildEndsWithoutResult(t *testing.T) {
	t.Run("panic", func(t *testing.T) {
		req := Request{YAML: []byte(rootAgent), Prompt: "hi", Env: map[string]string{"FAKE_API_KEY": fakeKey},
			test: &childFaults{Panic: "boom with " + fakeKey}}
		_, err := Run(t.Context(), req)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "the agent run process ended with exit status 2 without a result: panic: boom with "+
			"[secret]")
		assert.NotContains(t, err.Error(), fakeKey)
	})
	t.Run("exit", func(t *testing.T) {
		req := Request{YAML: []byte(rootAgent), Prompt: "hi", test: &childFaults{Exit: 3, Stray: "about to exit"}}
		_, err := Run(t.Context(), req)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "the agent run process ended with exit status 3 without a result: ")
		assert.Contains(t, err.Error(), "stderr: about to exit", "the end of its stderr")
	})
}

// TestChildHandshake: a child that does not answer with the handshake (another program) is
// stopped.
func TestChildHandshake(t *testing.T) {
	prev := childHandshakeTimeout
	childHandshakeTimeout = 500 * time.Millisecond
	t.Cleanup(func() { childHandshakeTimeout = prev })
	req, _ := hangingRequest(t)
	req.test.SkipHello = true
	start := time.Now()
	_, err := Run(t.Context(), req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the agent run process did not start: no handshake within 500ms")
	assert.Less(t, time.Since(start), 10*time.Second)
}

// TestChildVersionMismatch: a child of another build of the provider refuses the request.
func TestChildVersionMismatch(t *testing.T) {
	events := make(chan childEvent, 4)
	r, w, err := os.Pipe()
	require.NoError(t, err)
	go readChildOutput(r, events)
	cw := newChildWriter(w, func() {})
	in := strings.NewReader(`{"protocol":1,"version":"0.0.0-other"}` + "\n")
	ctx, cancel := context.WithCancelCause(t.Context())
	defer cancel(nil)
	assert.Equal(t, 1, childMain(ctx, cancel, in, cw))
	cw.close()
	_ = w.Close()
	var msgs []*childMessage
	for ev := range events {
		require.NoError(t, ev.err)
		msgs = append(msgs, ev.msg)
	}
	require.Len(t, msgs, 2)
	assert.Equal(t, msgHello, msgs[0].Type)
	assert.Equal(t, msgError, msgs[1].Type)
	assert.Contains(t, msgs[1].Message, "but the provider is version 0.0.0-other (protocol 1): the provider's executable "+
		"was replaced while it ran")
}

// TestChildCancellation: a canceled run ends with today's error, and the child cancels its run.
func TestChildCancellation(t *testing.T) {
	req, causeFile := hangingRequest(t)
	ctx, cancel := context.WithCancel(t.Context())
	req.Progress = func(p Progress) {
		if p.Message == "hanging" {
			cancel()
		}
	}
	_, err := Run(ctx, req)
	require.EqualError(t, err, "the agent run was canceled: context canceled")
	want := "the provider canceled the agent run" // SIGTERM
	if runtime.GOOS == "windows" {
		want = "the provider ended" // its stdin was closed
	}
	assert.Equal(t, want, readCause(t, causeFile))
}

// TestChildDeadline: the child gets the parent's deadline (customTimeouts), so both end the
// run with the same error.
func TestChildDeadline(t *testing.T) {
	req, causeFile := hangingRequest(t)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	_, err := Run(ctx, req)
	require.EqualError(t, err, "the operation's time limit (customTimeouts) ran out before the agent finished")
	// The parent stops the child at the same time.
	assert.Contains(t, []string{"context deadline exceeded", "the provider canceled the agent run"},
		readCause(t, causeFile))

	// Without the parent's stop, the child's own deadline ends it.
	cause := filepath.Join(t.TempDir(), "cause")
	b, err := json.Marshal(childRequest{Protocol: childProtocol, Version: version.Version, YAML: []byte(rootAgent),
		Prompt: "hi", Deadline: time.Now().Add(200 * time.Millisecond),
		Test: snapshotChildTest(&childFaults{Hang: true, CauseFile: cause})})
	require.NoError(t, err)
	inR, inW, err := os.Pipe()
	require.NoError(t, err)
	defer inW.Close() //nolint:errcheck
	_, err = inW.Write(append(b, '\n'))
	require.NoError(t, err)
	cw := newChildWriter(io.Discard, func() {})
	cctx, ccancel := context.WithCancelCause(t.Context())
	defer ccancel(nil)
	assert.Zero(t, childMain(cctx, ccancel, inR, cw))
	cw.close()
	assert.Equal(t, "context deadline exceeded", readCause(t, cause))
}

// TestChildTimeoutBackstop: a child that does not end at timeoutSeconds is killed after the
// grace, and the run ends with the timeout's error.
func TestChildTimeoutBackstop(t *testing.T) {
	for _, p := range []*time.Duration{&harnessGrace, &harnessDrain} {
		prev := *p
		*p = 100 * time.Millisecond
		t.Cleanup(func() { *p = prev })
	}
	req, _ := hangingRequest(t)
	req.Timeout = time.Second
	start := time.Now()
	_, err := Run(t.Context(), req)
	took := time.Since(start)
	require.EqualError(t, err, "the agent did not finish within 1s (timeoutSeconds)")
	assert.GreaterOrEqual(t, took, req.Timeout+childGrace())
	assert.Less(t, took, req.Timeout+childGrace()+5*time.Second)
}

// TestChildEnvironment: what the model SDKs (the child's process environment) and a shell
// tool see. env reaches the SDKs in both modes and the shell in neither; without
// inheritEnvironment the child has only the base environment.
func TestChildEnvironment(t *testing.T) {
	const procSecret = "proc-only-secret-value"
	t.Setenv("PROC_ONLY_SECRET", procSecret)
	t.Setenv("BOTH_VAR", "the-provider-value")
	t.Setenv("DOCKER_CONTEXT", "a-base-variable")
	t.Setenv("GOTRACEBACK", "crash")
	srv := fakeopenai.New(func(r fakeopenai.Request) fakeopenai.Reply {
		if len(r.ToolResults()) == 0 {
			return fakeopenai.Reply{ToolCalls: []fakeopenai.ToolCall{{Name: "shell",
				Arguments: `{"cmd":"echo K=$FAKE_API_KEY B=$BOTH_VAR P=$PROC_ONLY_SECRET D=$DOCKER_CONTEXT ` +
					`U=$(printenv FAKE_API_KEY >/dev/null && echo set || echo unset)"}`}}}
		}
		return fakeopenai.Reply{Content: toolResult(r)}
	})
	defer srv.Close()

	for _, inherit := range []bool{false, true} {
		t.Run(map[bool]string{false: "not inherited", true: "inherited"}[inherit], func(t *testing.T) {
			envFile := filepath.Join(t.TempDir(), "env")
			req := request(srv, shellAgent(""), "go")
			req.Env["BOTH_VAR"] = "the-env-value"
			req.ToolApproval = ToolApprovalAllow
			req.InheritEnvironment = inherit
			req.test = &childFaults{EnvFile: envFile}
			res, err := Run(t.Context(), req)
			require.NoError(t, err)

			b, err := os.ReadFile(envFile)
			require.NoError(t, err)
			sdk := map[string]string{}
			for _, kv := range strings.Split(string(b), "\n") {
				if k, v, ok := strings.Cut(kv, "="); ok {
					sdk[k] = v
				}
			}
			assert.Equal(t, fakeKey, sdk["FAKE_API_KEY"], "the SDKs see env")
			assert.Equal(t, "the-env-value", sdk["BOTH_VAR"], "env wins")
			assert.Equal(t, os.Getenv("PATH"), sdk["PATH"])
			assert.Equal(t, "a-base-variable", sdk["DOCKER_CONTEXT"], "a base variable")
			assert.NotContains(t, sdk, "GOTRACEBACK", "no core dumps with env in them")
			if runtime.GOOS != "windows" {
				assert.NotEmpty(t, sdk[childCookieVar], "the child's cookie")
			}
			if inherit {
				assert.Equal(t, procSecret, sdk["PROC_ONLY_SECRET"], "the provider's environment")
				assert.Contains(t, res.Output, "K= B=the-provider-value P="+procSecret+" D=a-base-variable U=unset",
					"the shell sees the provider's environment, not env: a variable only env sets is unset")
			} else {
				for k := range sdk {
					if k != "FAKE_API_KEY" && k != "BOTH_VAR" && k != childCookieVar {
						assert.True(t, isBaseEnvVar(k), "%s is not a variable of the base environment", k)
					}
				}
				assert.Contains(t, res.Output, "K= B= P= D= U=set", "the shell sees the system variables only, the "+
					"others are empty")
			}
		})
	}
}

// TestChildRequestEnvIsNotInTheExecEnvironment: the child is started with the base
// environment only; env reaches it over stdin.
func TestChildRequestEnvIsNotInTheExecEnvironment(t *testing.T) {
	environ := []string{"PATH=/bin", "HOME=/h", "SECRET=x", "HTTPS_PROXY=http://proxy", "no_proxy=localhost",
		"GOTRACEBACK=wer", "GOMEMLIMIT=1GiB", "GOPROXY=https://proxy.corp", "GITHUB_TOKEN=ghp_x", "=C:=C:\\"}
	assert.Equal(t, []string{"PATH=/bin", "HOME=/h", "HTTPS_PROXY=http://proxy", "no_proxy=localhost", "GOMEMLIMIT=1GiB",
		"GOPROXY=https://proxy.corp"}, childExecEnv(environ, false), "the tool installation's Go settings, no GitHub token")
	assert.Equal(t, []string{"PATH=/bin", "HOME=/h", "SECRET=x", "HTTPS_PROXY=http://proxy", "no_proxy=localhost",
		"GOMEMLIMIT=1GiB", "GOPROXY=https://proxy.corp", "GITHUB_TOKEN=ghp_x"}, childExecEnv(environ, true))
	for _, v := range baseEnvVars {
		assert.False(t, isSystemEnvVar(v), "%s is emptied for tools", v)
	}
}

// TestInheritedToolsetsDoNotSeeEnv: with inheritEnvironment, shell and script tools see the
// provider's environment exactly: a variable that only env sets is unset for them, not empty
// (review finding 6); without it, every non-system variable is empty.
func TestInheritedToolsetsDoNotSeeEnv(t *testing.T) {
	t.Setenv("BOTH_VAR", "the-provider-value")
	// A script's cmd may name only its args and env as $NAME.
	const show = `K=$(printenv FAKE_API_KEY || echo unset) B=$BOTH_VAR T=$TOOLSET_VAR`
	srv := fakeopenai.New(func(r fakeopenai.Request) fakeopenai.Reply {
		if len(r.ToolResults()) == 0 {
			return fakeopenai.Reply{ToolCalls: []fakeopenai.ToolCall{
				{Name: "shell", Arguments: `{"cmd":"echo shell: ` + show + `"}`},
				{Name: "show_env", Arguments: `{}`},
			}}
		}
		return fakeopenai.Reply{Content: toolResult(r)}
	})
	defer srv.Close()
	body := `agents:
  root:
    model: m
    instruction: Use your tools.
    toolsets:
      - type: shell
        env:
          TOOLSET_VAR: from-shell-env
      - type: script
        env:
          TOOLSET_VAR: from-script-env
        shell:
          show_env:
            cmd: 'echo "script: K=$(printenv FAKE_API_KEY || echo unset) B=$(printenv BOTH_VAR) T=$(printenv TOOLSET_VAR)"'
            description: Shows the environment
`
	for _, inherit := range []bool{true, false} {
		t.Run(fmt.Sprintf("inheritEnvironment %v", inherit), func(t *testing.T) {
			req := request(srv, body, "go")
			req.ToolApproval = ToolApprovalAllow
			req.InheritEnvironment = inherit
			res, err := Run(t.Context(), req)
			require.NoError(t, err)
			if inherit {
				assert.Contains(t, res.Output, "shell: K=unset B=the-provider-value T=from-shell-env")
				assert.Contains(t, res.Output, "script: K=unset B=the-provider-value T=from-script-env")
			} else {
				assert.Contains(t, res.Output, "shell: K= B= T=from-shell-env")
				assert.Contains(t, res.Output, "script: K= B= T=from-script-env")
			}
			assert.NotContains(t, res.Output, fakeKey)
		})
	}
}

// TestInheritedToolsetOptions: a toolset with an option that only docker-agent's constructor
// can set keeps that constructor (and overridden variables).
func TestInheritedToolsetOptions(t *testing.T) {
	yes := true
	rc := &config.RuntimeConfig{}
	script := map[string]latest.ScriptShellToolConfig{"x": {Cmd: "true", Description: "x"}}
	for _, c := range []struct {
		typ string
		ts  latest.Toolset
		ok  bool
	}{
		{"shell", latest.Toolset{}, true},
		{"shell", latest.Toolset{SudoAskpass: &yes}, false},
		{"background_jobs", latest.Toolset{}, true},
		{"background_jobs", latest.Toolset{Recall: &yes}, false},
		{"script", latest.Toolset{Shell: script}, true},
		{"lsp", latest.Toolset{}, false},
	} {
		set, ok, err := inheritedToolset(t.Context(), c.typ, c.ts, rc)
		require.NoError(t, err, c.typ)
		assert.Equal(t, c.ok, ok, "%s %+v", c.typ, c.ts)
		assert.Equal(t, c.ok, set != nil, c.typ)
	}
	_, ok, err := inheritedToolset(t.Context(), "script", latest.Toolset{}, rc)
	assert.True(t, ok)
	assert.EqualError(t, err, "shell is required for script toolset", "docker-agent's error")
}

// TestChildRequestEnvOverrides: the variables of env that the processes AgentRun controls
// get instead.
func TestChildRequestEnvOverrides(t *testing.T) {
	t.Setenv("BOTH_VAR", "provider")
	t.Setenv("DOLLAR_VAR", "a $value")
	env := map[string]string{"BOTH_VAR": "e", "ENV_ONLY": "e", "DOLLAR_VAR": "e", "PATH": "/env/bin", childCookieVar: "e"}
	t.Setenv(childCookieVar, "cookie")
	assert.Equal(t, map[string]string{"BOTH_VAR": "provider", "ENV_ONLY": "", "DOLLAR_VAR": "a $value",
		"PATH": os.Getenv("PATH"), childCookieVar: "cookie"}, requestEnvOverrides(env, true))
	assert.Equal(t, map[string]string{"BOTH_VAR": "", "ENV_ONLY": "", "DOLLAR_VAR": "", "PATH": os.Getenv("PATH"),
		childCookieVar: "cookie"}, requestEnvOverrides(env, false))
	tool := toolEnvOverrides(Request{Env: env, InheritEnvironment: true})
	assert.Equal(t, "", tool["DOLLAR_VAR"], "docker-agent would expand $value")
	assert.Equal(t, "provider", tool["BOTH_VAR"])
	tool = toolEnvOverrides(Request{Env: env})
	assert.Equal(t, "", tool["BOTH_VAR"], "every non-system variable is emptied")
	assert.NotContains(t, tool, "HOME")
	assert.Equal(t, "cookie", tool[childCookieVar], "the cookie survives the emptying")
}

// TestApplyRequestEnv: names and values that cannot be environment variables are not set.
func TestApplyRequestEnv(t *testing.T) {
	prev := childEnviron.Load()
	t.Cleanup(func() { childEnviron.Store(prev) })
	t.Setenv("APPLY_OK", "")
	var debug []string
	applyRequestEnv(map[string]string{"APPLY_OK": "yes", "A=B": "x", "NUL": "a\x00b", "": "empty"},
		func(m string) { debug = append(debug, m) })
	assert.Equal(t, "yes", os.Getenv("APPLY_OK"))
	assert.NotContains(t, strings.Join(*childEnviron.Load(), "\n"), "APPLY_OK=yes", "the copy is from before")
	require.Len(t, debug, 3)
	for _, m := range debug {
		assert.Contains(t, m, "is not set in the environment of the agent run")
		assert.NotContains(t, m, "a\x00b")
	}
}

// TestChildWriterDropsOnlyDebugRecords: DEBUG and INFO records are dropped when the queue is
// full; WARN and ERROR records and the other messages wait, and close writes everything
// queued and reports the drops (review finding 3).
func TestChildWriterDropsOnlyDebugRecords(t *testing.T) {
	r, w, err := os.Pipe()
	require.NoError(t, err)
	cw := newChildWriter(w, func() {})
	// Nobody reads yet: the pipe fills, then the queue.
	for range 4 * childQueue {
		cw.log(slog.LevelDebug, strings.Repeat("d", 1024))
	}
	cw.log(slog.LevelInfo, "an info record")
	done := make(chan struct{})
	go func() {
		for i := range 2 * childQueue {
			cw.log([]slog.Level{slog.LevelWarn, slog.LevelError}[i%2], "record "+strconv.Itoa(i))
		}
		cw.send(childMessage{Type: msgResult, Result: &childResult{Output: "kept"}})
		cw.close()
		_ = w.Close()
		close(done)
	}()
	var msgs []childMessage
	sc := bufio.NewScanner(r)
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		var m childMessage
		require.NoError(t, json.Unmarshal([]byte(strings.TrimPrefix(sc.Text(), childMagic)), &m))
		msgs = append(msgs, m)
	}
	<-done
	debugs, records := 0, 0
	for _, m := range msgs {
		switch {
		case m.Type == msgLog && m.Level == int(slog.LevelDebug) && strings.HasPrefix(m.Message, "ddd"):
			debugs++
		case m.Type == msgLog && m.Level >= int(slog.LevelWarn):
			assert.Equal(t, "record "+strconv.Itoa(records), m.Message, "in order")
			records++
		}
	}
	assert.Equal(t, 2*childQueue, records, "no WARN or ERROR record was dropped")
	assert.Less(t, debugs, 4*childQueue, "debug records were dropped")
	require.GreaterOrEqual(t, len(msgs), 2)
	require.NotNil(t, msgs[len(msgs)-2].Result)
	assert.Equal(t, "kept", msgs[len(msgs)-2].Result.Output)
	assert.Equal(t, childMessage{Type: msgLog, Level: int(slog.LevelDebug), Message: fmt.Sprintf("the agent run process "+
		"dropped %d debug records: the provider read its messages too slowly", 4*childQueue+1-debugs)}, msgs[len(msgs)-1])
}

// TestChildStdinEOFCancels: the end of the child's stdin (the parent is gone, or on Windows
// canceled the run) cancels the run.
func TestChildStdinEOFCancels(t *testing.T) {
	cause := filepath.Join(t.TempDir(), "cause")
	b, err := json.Marshal(childRequest{Protocol: childProtocol, Version: version.Version, YAML: []byte(rootAgent),
		Prompt: "hi", Test: snapshotChildTest(&childFaults{Hang: true, CauseFile: cause})})
	require.NoError(t, err)
	inR, inW, err := os.Pipe()
	require.NoError(t, err)
	outR, outW, err := os.Pipe()
	require.NoError(t, err)
	events := make(chan childEvent, 16)
	go readChildOutput(outR, events)
	cw := newChildWriter(outW, func() {})
	ctx, cancel := context.WithCancelCause(t.Context())
	defer cancel(nil)
	done := make(chan int, 1)
	go func() { done <- childMain(ctx, cancel, inR, cw) }()
	_, err = inW.Write(append(b, '\n'))
	require.NoError(t, err)
	require.NoError(t, inW.Close())
	select {
	case status := <-done:
		assert.Zero(t, status)
	case <-time.After(10 * time.Second):
		t.Fatal("the child did not end when its stdin ended")
	}
	cw.close()
	_ = outW.Close()
	var last *childMessage
	for ev := range events {
		if ev.msg != nil {
			last = ev.msg
		}
	}
	assert.Equal(t, "the provider ended", readCause(t, cause))
	require.NotNil(t, last)
	assert.Equal(t, childMessage{Type: msgError, Message: "hung until canceled: the provider ended"}, *last)
}

func TestChildLeftRunningWarning(t *testing.T) {
	assert.Equal(t, "the agent run left 1 process running when it ended (for example a command a tool ran in the "+
		"background); AgentRun stopped it", childLeftRunningWarning(1))
	assert.Equal(t, "the agent run left 2 processes running when it ended (for example a command a tool ran in the "+
		"background); AgentRun stopped them", childLeftRunningWarning(2))
}

// TestChildAnswerSecretness: the answer is secret when it contains a value the child knows
// (env) or one only the provider knows (a credential of its environment that the child does
// not inherit); the warning is added once.
func TestChildAnswerSecretness(t *testing.T) {
	const parentOnly = "parent-only-credential-9876543210"
	t.Setenv("PARENT_ONLY_API_KEY", parentOnly)
	for _, c := range []struct {
		name, answer string
		secret       bool
	}{
		{"an env value", "the key is " + fakeKey, true},
		{"a credential of the provider", "the key is " + parentOnly, true},
		{"none", "nothing secret", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			srv := fakeopenai.New(func(fakeopenai.Request) fakeopenai.Reply { return fakeopenai.Reply{Content: c.answer} })
			defer srv.Close()
			res, err := Run(t.Context(), request(srv, rootAgent, "hi"))
			require.NoError(t, err)
			assert.Equal(t, c.answer, res.Output)
			assert.Equal(t, c.secret, res.OutputContainsSecret)
			n := 0
			for _, w := range res.Warnings {
				if strings.Contains(w, "the answer contains the value of a secret") {
					n++
				}
			}
			assert.Equal(t, map[bool]int{true: 1, false: 0}[c.secret], n, "%q", res.Warnings)
		})
	}
}
