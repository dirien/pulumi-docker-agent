// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package agentrun

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

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
		"the end"
	events := make(chan childEvent, 16)
	go readChildOutputLimited(strings.NewReader(input), events, 250)
	var got []childEvent
	for ev := range events {
		got = append(got, ev)
	}
	require.Len(t, got, 8)
	assert.Equal(t, "a stray line", got[0].stray)
	assert.Equal(t, msgHello, got[1].msg.Type)
	assert.Equal(t, "no newline", got[2].stray, "the stray part of the line")
	assert.Equal(t, "p", got[3].msg.Message, "the message after it")
	assert.ErrorContains(t, got[4].err, "a message larger than", "over the limit of this reader")
	assert.ErrorContains(t, got[5].err, "reading a message of the agent run process")
	assert.ErrorContains(t, got[6].err, "a message larger than")
	assert.Equal(t, "the end", got[7].stray)

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
	if runtime.GOOS == "windows" {
		// The standard output handle the child started with is the protocol's pipe.
		assert.Contains(t, all, "DEBUG the agent run process wrote to stdout: fd1: "+redacted)
	} else {
		assert.Contains(t, all, "DEBUG the agent run process wrote to stderr: fd1: "+redacted,
			"file descriptor 1 of the child is a copy of its stderr")
	}
	assert.NotContains(t, all, fakeKey)
	assert.NotContains(t, all, parentOnly)
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
				Arguments: `{"cmd":"echo K=$FAKE_API_KEY B=$BOTH_VAR P=$PROC_ONLY_SECRET D=$DOCKER_CONTEXT"}`}}}
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
				assert.Contains(t, res.Output, "K= B=the-provider-value P="+procSecret+" D=a-base-variable",
					"the shell sees the provider's environment, not env")
			} else {
				for k := range sdk {
					if k != "FAKE_API_KEY" && k != "BOTH_VAR" && k != childCookieVar {
						assert.True(t, isBaseEnvVar(k), "%s is not a variable of the base environment", k)
					}
				}
				assert.Contains(t, res.Output, "K= B= P= D=", "the shell sees the system variables only")
			}
		})
	}
}

// TestChildRequestEnvIsNotInTheExecEnvironment: the child is started with the base
// environment only; env reaches it over stdin.
func TestChildRequestEnvIsNotInTheExecEnvironment(t *testing.T) {
	environ := []string{"PATH=/bin", "HOME=/h", "SECRET=x", "HTTPS_PROXY=http://proxy", "no_proxy=localhost",
		"GOTRACEBACK=wer", "GOMEMLIMIT=1GiB", "=C:=C:\\"}
	assert.Equal(t, []string{"PATH=/bin", "HOME=/h", "HTTPS_PROXY=http://proxy", "no_proxy=localhost", "GOMEMLIMIT=1GiB"},
		childExecEnv(environ, false))
	assert.Equal(t, []string{"PATH=/bin", "HOME=/h", "SECRET=x", "HTTPS_PROXY=http://proxy", "no_proxy=localhost",
		"GOMEMLIMIT=1GiB"}, childExecEnv(environ, true))
	for _, v := range baseEnvVars {
		assert.False(t, isSystemEnvVar(v), "%s is emptied for tools", v)
	}
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

// TestChildWriterDropsLogsNotMessages: logs are dropped when the queue is full; other
// messages wait.
func TestChildWriterDropsLogsNotMessages(t *testing.T) {
	r, w, err := os.Pipe()
	require.NoError(t, err)
	cw := newChildWriter(w, func() {})
	// Nobody reads yet: the pipe fills, then the queue.
	for i := 0; i < 4*childQueue; i++ {
		cw.log(slog.LevelWarn, strings.Repeat("l", 1024))
	}
	done := make(chan struct{})
	go func() {
		cw.send(childMessage{Type: msgResult, Result: &childResult{Output: "kept"}})
		cw.close()
		_ = w.Close()
		close(done)
	}()
	var last childMessage
	n := 0
	sc := bufio.NewScanner(r)
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		n++
		require.NoError(t, json.Unmarshal([]byte(strings.TrimPrefix(sc.Text(), childMagic)), &last))
	}
	<-done
	assert.Equal(t, "kept", last.Result.Output)
	assert.Less(t, n, 4*childQueue+1, "some logs were dropped")
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
