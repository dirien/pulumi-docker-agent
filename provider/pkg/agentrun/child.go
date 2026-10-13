// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package agentrun

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	version "github.com/dirien/pulumi-docker-agent/provider"
)

// Every agent run runs in a run child: the provider's own executable, started again with the
// hidden first argument childArg (main and the TestMain of the packages whose tests run agents
// call MaybeRunChild first). The model SDKs read the process environment themselves, so only
// a process of its own can give them exactly the environment AgentRun promises (childenv.go);
// docker-agent's process-global state (paths.SetRoot, the slog default) and a panic in one of
// its goroutines stay in the child too.
//
// The parent (Run) keeps the run slot and Request.Start, creates the state root and the
// temporary working directory (and removes them when the child and what it left are gone),
// and starts the child. Then:
//
//   - parent to child, on the child's stdin: the request as one JSON document (childRequest:
//     Request.Env, the secret inputs, the parent's deadline, and in a test binary the tests'
//     overrides). stdin stays open for the whole run: its end means the parent is gone;
//   - child to parent, on the child's stdout: one JSON message per line after childMagic
//     (childMessage), from one writer goroutine; a version handshake first (once the child
//     read the request), then progress, logs and warnings, and a result or an error last. The child moves everything else away
//     from the protocol: os.Stdout is os.Stderr, and on Linux and macOS file descriptor 1 is
//     a copy of stderr. The parent logs lines without childMagic and the child's stderr
//     (panics included) at debug level, redacted;
//   - the child redacts what it sends with its own redaction set (it never gets the
//     credentials of the provider's environment it does not inherit), and the parent redacts
//     everything again with the provider's full set, decides whether the answer is secret,
//     and reports the summary;
//   - the parent owns the end of the run: it calls the Request callbacks from Run's goroutine,
//     supervises the child (child_unix.go, child_windows.go) and writes the error of a run that
//     was canceled, timed out, stopped for a shutdown, or whose child ended without a result.
//     The child gets the parent's deadline, so its own errors for those cases read the same.
//
// The child cancels its run on SIGTERM (a cancellation), SIGINT (Pulumi stops the provider),
// the end of stdin and a broken stdout (the parent is gone); it ignores SIGPIPE, so that it
// can still stop its tools after the parent died.

const (
	// childArg is the hidden first argument of a run child.
	childArg = "__docker-agent-run"
	// childTestArg comes before childArg in a test binary: a test binary whose TestMain does
	// not call MaybeRunChild runs no tests.
	childTestArg = "-test.run=^$"
	// childProtocol is the version of the protocol between the provider and a run child.
	childProtocol = 1
	// childMagic starts each protocol line. NUL bytes keep any other output from passing for
	// a message.
	childMagic = "\x00docker-agent-run\x00"
	// maxChildMessage bounds one protocol message (an answer can be large).
	maxChildMessage = 64 << 20
	// childGraceMargin is added to the time a canceled child may take to stop its CLIs.
	childGraceMargin = 2 * time.Second
	// childQueue is the length of the child's message queue; logs are dropped when it is full.
	childQueue = 256
	// childFlushTimeout bounds the time the child waits for its last messages to be written.
	childFlushTimeout = 30 * time.Second
)

// childHandshakeTimeout bounds the time a child takes to start and send its handshake; a
// variable, so that tests can shorten it.
var childHandshakeTimeout = 30 * time.Second

// childGrace is how long a canceled run child may take to stop (its Claude Code CLIs get
// harnessGrace, their output harnessDrain) before it is killed.
func childGrace() time.Duration { return harnessGrace + harnessDrain + childGraceMargin }

var (
	// childEntry is set when MaybeRunChild ran in a process that is no run child: only such
	// a process starts run children, so that a program without the call (a test binary
	// whose TestMain lacks it) never starts copies of itself.
	childEntry atomic.Bool
	// inChild is set in a run child, which never starts another one.
	inChild atomic.Bool
)

// MaybeRunChild runs the process as a run child and exits, when its arguments make it one
// (Run starts the provider's own executable as a run child). Otherwise it returns at once.
// The provider's main calls it first, and so does the TestMain of every package whose tests
// run agents; Run refuses to run in a process that did not call it.
func MaybeRunChild() {
	if !isChildInvocation(os.Args) {
		childEntry.Store(true)
		return
	}
	os.Exit(runChild())
}

// isChildInvocation reports whether the arguments are those of a run child.
func isChildInvocation(args []string) bool {
	rest := args[min(len(args), 1):]
	return slices.Equal(rest, []string{childArg}) ||
		(testing.Testing() && slices.Equal(rest, []string{childTestArg, childArg}))
}

// childArgs are the arguments of a run child.
func childArgs() []string {
	if testing.Testing() {
		return []string{childTestArg, childArg}
	}
	return []string{childArg}
}

// childRequest is the request a run child gets on stdin.
type childRequest struct {
	Protocol int    `json:"protocol"`
	Version  string `json:"version"`

	Name               string            `json:"name,omitempty"`
	YAML               []byte            `json:"yaml"`
	Agent              string            `json:"agent,omitempty"`
	Prompt             string            `json:"prompt"`
	Env                map[string]string `json:"env,omitempty"`
	InheritEnvironment bool              `json:"inheritEnvironment,omitempty"`
	ToolApproval       ToolApproval      `json:"toolApproval"`
	AllowHookApprovals bool              `json:"allowHookApprovals,omitempty"`
	Timeout            time.Duration     `json:"timeout"`
	Secrets            []string          `json:"secrets,omitempty"`
	Logging            Logging           `json:"logging"`
	HideNames          bool              `json:"hideNames,omitempty"`
	AllowHarnessAgents bool              `json:"allowHarnessAgents,omitempty"`
	AnnounceHarness    bool              `json:"announceHarness,omitempty"`
	// Log and Warn report whether the parent has Request.Log and Request.Warn.
	Log  bool `json:"log,omitempty"`
	Warn bool `json:"warn,omitempty"`

	// WorkingDir and StateRoot exist; the parent created the temporary ones.
	WorkingDir string `json:"workingDir"`
	StateRoot  string `json:"stateRoot"`
	// Deadline is the deadline of the parent's context, zero without one.
	Deadline time.Time `json:"deadline,omitzero"`
	// WithheldToken: the provider's environment has a CLAUDE_CODE_OAUTH_TOKEN that the child's
	// environment does not inherit (procenv.go).
	WithheldToken bool `json:"withheldToken,omitempty"`
	// Test holds the tests' overrides; only a test binary sends and honours it (childtest.go).
	Test *childTest `json:"test,omitempty"`
}

// Kinds of childMessage.
const (
	msgHello    = "hello"
	msgTimed    = "timed" // the run's timeout started
	msgProgress = "progress"
	msgLog      = "log"
	msgWarn     = "warn"
	msgResult   = "result"
	msgError    = "error"
)

// childMessage is a message of a run child.
type childMessage struct {
	Type string `json:"t"`
	// hello
	Protocol int    `json:"protocol,omitempty"`
	Version  string `json:"version,omitempty"`
	// progress, log, warn, error
	Message    string `json:"msg,omitempty"`
	Persistent bool   `json:"persistent,omitempty"`
	Level      int    `json:"level,omitempty"`
	// error: the variables a MissingCredentialsError names (the parent adds its hints).
	Missing []string `json:"missing,omitempty"`
	// result
	Result *childResult `json:"result,omitempty"`
}

// childResult is the Result of a run child, before the parent adds what it decides itself.
type childResult struct {
	Output              string          `json:"output"`
	StructuredOutput    json.RawMessage `json:"structuredOutput,omitempty"`
	HasStructuredOutput bool            `json:"hasStructuredOutput,omitempty"`
	AgentName           string          `json:"agentName"`
	Usage               Usage           `json:"usage"`
	ToolCalls           []string        `json:"toolCalls"`
	RejectedToolCalls   []string        `json:"rejectedToolCalls,omitempty"`
	Warnings            []string        `json:"warnings,omitempty"`
	// OutputContainsSecret: the answer contains a value of the child's redaction set (an env
	// value or a secret input it saw).
	OutputContainsSecret bool `json:"outputContainsSecret,omitempty"`
	// CostNote is Result.costNote.
	CostNote string `json:"costNote,omitempty"`
}

// --- the child --------------------------------------------------------------------------------

// Why a run child cancels its run.
var (
	errChildCanceled = errors.New("the provider canceled the agent run")
	errChildShutdown = errors.New("Pulumi is stopping the provider") //nolint:staticcheck // a proper name
	errParentGone    = errors.New("the provider ended")
)

// runChild is the run child's main function. It returns the exit status.
func runChild() int {
	proto, input, err := takeChildStdio()
	if err != nil {
		fmt.Fprintln(os.Stderr, "agent run:", err)
		return 2
	}
	inChild.Store(true)
	protectChild()
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	stopSignals := watchChildSignals(cancel)
	defer stopSignals()
	// The child's own CLIs stop on SIGINT, and none starts after it.
	WatchShutdownSignals()

	w := newChildWriter(proto, func() { cancel(errParentGone) })
	status := childMain(ctx, cancel, input, w)
	w.close()
	return status
}

// childMain reads the request, answers with the handshake, and runs the request.
func childMain(ctx context.Context, cancel context.CancelCauseFunc, input io.Reader, w *childWriter) int {
	var creq childRequest
	dec := json.NewDecoder(input)
	err := dec.Decode(&creq)
	if creq.Test == nil || !creq.Test.SkipHello || !testing.Testing() {
		w.send(childMessage{Type: msgHello, Protocol: childProtocol, Version: version.Version})
	}
	if err != nil {
		w.send(childMessage{Type: msgError, Message: "reading the request of the agent run: " + err.Error()})
		return 1
	}
	// The end of stdin: the parent is gone (or, on Windows, canceled the run).
	go func() {
		_, _ = io.Copy(io.Discard, io.MultiReader(dec.Buffered(), input))
		cancel(errParentGone)
	}()
	if creq.Protocol != childProtocol || creq.Version != version.Version {
		w.send(childMessage{Type: msgError, Message: fmt.Sprintf("the agent run process is version %s of the provider "+
			"(protocol %d), but the provider is version %s (protocol %d): the provider's executable was replaced while it "+
			"ran; run the command again", version.Version, childProtocol, creq.Version, creq.Protocol)})
		return 1
	}
	debug := func(msg string) {
		if creq.Log {
			w.log(slog.LevelDebug, msg)
		}
	}
	if !creq.Deadline.IsZero() {
		var stop context.CancelFunc
		ctx, stop = context.WithDeadline(ctx, creq.Deadline)
		defer stop()
	}
	if creq.Test != nil && testing.Testing() {
		if status, stop := creq.Test.apply(ctx, w); stop {
			return status
		}
		useTestLogin(creq.StateRoot)
	}
	// Before docker-agent reads anything.
	applyRequestEnv(creq.Env, debug)
	if creq.Test != nil && testing.Testing() && creq.Test.EnvFile != "" {
		// What the model SDKs see.
		_ = os.WriteFile(creq.Test.EnvFile, []byte(strings.Join(os.Environ(), "\n")), 0o600)
	}

	req := Request{
		Name: creq.Name, YAML: creq.YAML, Agent: creq.Agent, Prompt: creq.Prompt, Env: creq.Env,
		InheritEnvironment: creq.InheritEnvironment, WorkingDir: creq.WorkingDir, ToolApproval: creq.ToolApproval,
		AllowHookApprovals: creq.AllowHookApprovals, Timeout: creq.Timeout, Secrets: creq.Secrets, Logging: creq.Logging,
		HideNames: creq.HideNames, AllowHarnessAgents: creq.AllowHarnessAgents, AnnounceHarness: creq.AnnounceHarness,
		withheldToken: creq.WithheldToken,
	}
	if creq.Log {
		req.Log = w.log
	}
	if creq.Logging != LoggingNone {
		req.Progress = func(p Progress) {
			w.send(childMessage{Type: msgProgress, Message: p.Message, Persistent: p.Persistent})
		}
	}
	if creq.Warn {
		req.Warn = func(msg string) { w.send(childMessage{Type: msgWarn, Message: msg}) }
	}
	res, err := runRequest(ctx, req, creq.StateRoot, func() { w.send(childMessage{Type: msgTimed}) })
	if err != nil {
		var missing []string
		if mce, ok := errors.AsType[*MissingCredentialsError](err); ok {
			missing = mce.Variables
		}
		w.send(childMessage{Type: msgError, Message: err.Error(), Missing: missing})
		return 0
	}
	cr := &childResult{
		Output: res.Output, HasStructuredOutput: res.HasStructuredOutput, AgentName: res.AgentName, Usage: res.Usage,
		ToolCalls: res.ToolCalls, RejectedToolCalls: res.RejectedToolCalls, Warnings: res.Warnings,
		OutputContainsSecret: res.OutputContainsSecret, CostNote: res.costNote,
	}
	if res.HasStructuredOutput {
		if cr.StructuredOutput, err = json.Marshal(res.StructuredOutput); err != nil {
			w.send(childMessage{Type: msgError, Message: "encoding the structured output: " + err.Error()})
			return 0
		}
	}
	w.send(childMessage{Type: msgResult, Result: cr})
	return 0
}

// childWriter writes a run child's messages from one goroutine. send waits for room in the
// queue; log drops the record when the queue is full, so that logging never holds up the run.
// When a write fails (the parent is gone), broken is called once and later messages are
// dropped.
type childWriter struct {
	out    io.Writer
	queue  chan childMessage
	done   chan struct{}
	broken func()
	closed atomic.Bool
	mu     sync.RWMutex // held for reading while sending, for writing while closing
}

func newChildWriter(out io.Writer, broken func()) *childWriter {
	w := &childWriter{out: out, queue: make(chan childMessage, childQueue), done: make(chan struct{}), broken: broken}
	go w.loop()
	return w
}

func (w *childWriter) loop() {
	defer close(w.done)
	failed := false
	for m := range w.queue {
		if failed {
			continue
		}
		b, err := json.Marshal(m)
		if err != nil {
			b, _ = json.Marshal(childMessage{Type: msgLog, Level: int(slog.LevelDebug), Message: "encoding a message: " + err.Error()})
		}
		line := make([]byte, 0, len(childMagic)+len(b)+1)
		line = append(append(append(line, childMagic...), b...), '\n')
		if _, err := w.out.Write(line); err != nil {
			failed = true
			w.broken()
		}
	}
}

// send queues a message, waiting for room.
func (w *childWriter) send(m childMessage) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if !w.closed.Load() {
		w.queue <- m
	}
}

// log queues a log record unless the queue is full.
func (w *childWriter) log(level slog.Level, msg string) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.closed.Load() {
		return
	}
	select {
	case w.queue <- childMessage{Type: msgLog, Level: int(level), Message: msg}:
	default:
	}
}

// close writes what is queued, waiting at most childFlushTimeout.
func (w *childWriter) close() {
	w.closed.Store(true)
	w.mu.Lock()
	close(w.queue)
	w.mu.Unlock()
	select {
	case <-w.done:
	case <-time.After(childFlushTimeout):
	}
}

// --- the parent -------------------------------------------------------------------------------

// errStoppedForShutdown is the error of a run that a SIGINT to the provider stopped.
var errStoppedForShutdown = errors.New("the agent run was stopped because Pulumi is stopping the provider (SIGINT)")

// childEvent is what the parent reads from a run child: a message, a line of other output, or
// a protocol error.
type childEvent struct {
	msg   *childMessage
	stray string
	err   error
}

// childOutcome is the end of a run child.
type childOutcome struct {
	result *childResult
	err    error
	// fromChild reports that err is the child's own error, and missing the variables it says
	// are missing.
	fromChild bool
	missing   []string
	// warnings are the parent's own: what the child left running.
	warnings []string
}

// runInChild runs the request in a run child, from Run once the run holds the slot.
func runInChild(ctx context.Context, req Request, approval ToolApproval, timeout time.Duration, secrets []string,
) (*Result, error) {
	names := newNameHider(req)
	// The summary measures the run from here, as it did in process.
	prog := newProgress(req, secrets, names)
	warn := func(msg string) {
		if req.Warn != nil {
			req.Warn(redact(msg, secrets))
		}
	}

	debug := func(msg string) {
		if req.Log != nil {
			req.Log(slog.LevelDebug, redact(names.hide(msg), secrets))
		}
	}

	stateRoot, err := os.MkdirTemp("", stateRootPrefix)
	if err != nil {
		return nil, fmt.Errorf("creating docker-agent state directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(stateRoot) }()
	workingDir, cleanup, err := runWorkingDir(req.WorkingDir)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	// The model checks a preview did not make (a `reference`, a delete prompt).
	if req.AnnounceHarness {
		for _, w := range modelRunWarnings(ctx, req) {
			warn(w)
		}
	}
	// The ChatGPT login of this machine, for a configuration that names a chatgpt model
	// (chatgpt.go); its tokens join both redaction sets.
	login, tokens := prepareChatGPTLogin(ctx, req, stateRoot)
	if len(tokens) > 0 {
		secrets = append(slices.Clone(secrets), tokens...)
		prog.secrets = secrets
		req.Secrets = append(slices.Clone(req.Secrets), tokens...)
	}

	creq := childRequest{
		Protocol: childProtocol, Version: version.Version,
		Name: req.Name, YAML: req.YAML, Agent: req.Agent, Prompt: req.Prompt, Env: req.Env,
		InheritEnvironment: req.InheritEnvironment, ToolApproval: approval, AllowHookApprovals: req.AllowHookApprovals,
		Timeout: timeout, Secrets: req.Secrets, Logging: req.Logging, HideNames: req.HideNames,
		AllowHarnessAgents: req.AllowHarnessAgents, AnnounceHarness: req.AnnounceHarness,
		Log: req.Log != nil, Warn: req.Warn != nil,
		WorkingDir: workingDir, StateRoot: stateRoot,
		WithheldToken: withheldClaudeToken(req),
	}
	if req.Progress == nil {
		creq.Logging = LoggingNone
	}
	if d, ok := ctx.Deadline(); ok {
		creq.Deadline = d
	}
	if testing.Testing() {
		creq.Test = snapshotChildTest(req.test)
	}

	// The child hid the names in what it sent, as runs in process did; the parent redacts it
	// again, with the provider's credentials.
	out := superviseChild(ctx, req, creq, secrets, names)
	// A login the run refreshed goes back to this machine's login (chatgpt.go).
	secrets = append(secrets, login.finish(ctx, debug)...)
	if out.err != nil {
		for _, w := range out.warnings {
			warn(w)
		}
		msg := out.err.Error()
		if out.fromChild {
			msg = parentErrorHints(ctx, req, msg, out.missing, login)
		}
		if len(out.missing) > 0 {
			return nil, &MissingCredentialsError{Variables: out.missing, msg: redact(msg, secrets)}
		}
		return nil, errors.New(redact(msg, secrets))
	}
	cr := out.result
	res := &Result{
		Output: cr.Output, HasStructuredOutput: cr.HasStructuredOutput, AgentName: cr.AgentName, Usage: cr.Usage,
		ToolCalls: cr.ToolCalls, RejectedToolCalls: cr.RejectedToolCalls, Warnings: append(cr.Warnings, out.warnings...),
		costNote: cr.CostNote,
	}
	if res.ToolCalls == nil {
		res.ToolCalls = []string{}
	}
	if res.HasStructuredOutput {
		if err := json.Unmarshal(cr.StructuredOutput, &res.StructuredOutput); err != nil {
			return nil, fmt.Errorf("decoding the structured output of the agent run: %w", err)
		}
	}
	for i, w := range res.Warnings {
		res.Warnings[i] = redact(w, secrets)
	}
	// The child knows the values it saw (env, secret inputs), the parent the credentials of the
	// provider's environment, which a child without inheritEnvironment never sees.
	res.OutputContainsSecret = cr.OutputContainsSecret || containsSecret(res.Output, secrets)
	if res.OutputContainsSecret {
		res.Warnings = append(res.Warnings, "the answer contains the value of a secret (an `env` value, a secret input or "+
			"a credential of the environment); output and structuredOutput are stored as secrets")
	}
	prog.summary(res)
	return res, nil
}

// runWorkingDir returns the run's working directory: workingDir, absolute and checked, or a
// new temporary directory, which cleanup removes.
func runWorkingDir(workingDir string) (dir string, cleanup func(), err error) {
	if workingDir == "" {
		wd, err := os.MkdirTemp("", "pulumi-docker-agent-work-")
		if err != nil {
			return "", nil, fmt.Errorf("creating working directory: %w", err)
		}
		return wd, func() { _ = os.RemoveAll(wd) }, nil
	}
	abs, err := filepath.Abs(workingDir)
	if err != nil {
		return "", nil, fmt.Errorf("resolving workingDir: %w", err)
	}
	if fi, err := os.Stat(abs); err != nil || !fi.IsDir() {
		return "", nil, fmt.Errorf("workingDir %q is not an existing directory", workingDir)
	}
	return abs, func() {}, nil
}

// withheldClaudeToken reports that the provider's environment has a CLAUDE_CODE_OAUTH_TOKEN
// that a run child without an inherited environment does not get, and env does not set.
func withheldClaudeToken(req Request) bool {
	_, set := req.Env[claudeOAuthTokenVar]
	return !req.InheritEnvironment && !set && os.Getenv(claudeOAuthTokenVar) != ""
}

// runChildProcess is a started run child (child_unix.go, child_windows.go).
type runChildProcess interface {
	// stop asks the child to stop (a cancellation) and kills it after childGrace.
	stop()
	// kill kills the child at once.
	kill()
	// exited is closed when the child exited; state returns how then.
	exited() <-chan struct{}
	state() (*os.ProcessState, error)
	// done is closed when what the child left running was stopped too.
	done() <-chan struct{}
	// stoppedForShutdown reports whether a SIGINT to the provider stopped the child.
	stoppedForShutdown() bool
	// leftRunning is how many processes the child left running when it ended on its own,
	// which were stopped. It is set when done is closed.
	leftRunning() int
}

// Why the parent stopped a run child.
type childStop int

const (
	stopNone childStop = iota
	stopContext
	stopTimeout
	stopProtocol
)

// superviseChild starts a run child for the request and supervises it until it ended and
// what it left running was stopped. It calls the request's callbacks on its own goroutine:
// what the child sends redacted with secrets, and what else the child writes (and the
// tracker's messages) without the names of a secret configuration too.
func superviseChild(ctx context.Context, req Request, creq childRequest, secrets []string, names *nameHider,
) childOutcome {
	clean := func(s string) string { return redact(s, secrets) }
	logDebugMsg := func(msg string) {
		if req.Log != nil {
			req.Log(slog.LevelDebug, redact(names.hide(msg), secrets))
		}
	}
	body, err := json.Marshal(creq)
	if err != nil {
		return childOutcome{err: fmt.Errorf("encoding the agent run request: %w", err)}
	}
	exe, err := childExecutable()
	if err != nil {
		return childOutcome{err: fmt.Errorf("starting the agent run process: %w", err)}
	}

	// The child's stdio are pipes of the parent's own, so Go starts no copy goroutines and
	// Wait never waits for a process the child left behind.
	var files []*os.File
	closeAll := func() {
		for _, f := range files {
			_ = f.Close()
		}
	}
	pipe := func() (r, w *os.File, err error) {
		if r, w, err = os.Pipe(); err == nil {
			files = append(files, r, w)
		}
		return r, w, err
	}
	stdinR, stdinW, err1 := pipe()
	stdoutR, stdoutW, err2 := pipe()
	stderrR, stderrW, err3 := pipe()
	if err := errors.Join(err1, err2, err3); err != nil {
		closeAll()
		return childOutcome{err: fmt.Errorf("creating pipes for the agent run process: %w", err)}
	}
	defer closeAll()
	var closeStdin sync.Once
	closeIn := func() { closeStdin.Do(func() { _ = stdinW.Close() }) }

	cmd := exec.Command(exe, childArgs()...)
	cmd.Env = childExecEnv(os.Environ(), creq.InheritEnvironment)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdinR, stdoutW, stderrW
	// The tracker's debug messages come from its goroutines; they are logged from this one.
	debugs := make(chan string, 64)
	debug := func(msg string) {
		select {
		case debugs <- msg:
		default:
		}
	}
	proc, err := startRunChild(cmd, closeIn, debug)
	// The child has its own copies of these ends.
	_, _, _ = stdinR.Close(), stdoutW.Close(), stderrW.Close()
	if err != nil {
		if errors.Is(err, errShuttingDown) {
			return childOutcome{err: err}
		}
		return childOutcome{err: fmt.Errorf("starting the agent run process: %w", err)}
	}

	go func() {
		_, _ = stdinW.Write(append(body, '\n'))
	}()
	msgs := make(chan childEvent)
	go readChildOutput(stdoutR, msgs)
	strays := make(chan string, childQueue)
	tail := &childStderr{}
	go readChildStderr(stderrR, tail, strays)

	var (
		reason    childStop
		protoErr  error
		hello     bool
		result    *childResult
		childErr  string
		exitedAt  time.Time
		drainC    <-chan time.Time
		ctxDone   = ctx.Done()
		exited    = proc.exited()
		handshake = time.NewTimer(childHandshakeTimeout)
		// The child enforces timeoutSeconds itself, from when it starts to load its configuration;
		// the backstop ends a child that does not, childGrace later.
		backstop  *time.Timer
		backstopC <-chan time.Time
	)
	// The variables of a MissingCredentialsError of the child.
	var childMissing []string
	defer handshake.Stop()
	defer func() {
		if backstop != nil {
			backstop.Stop()
		}
	}()
	stop := func(r childStop, kill bool) {
		if reason == stopNone {
			reason = r
		}
		if kill {
			proc.kill()
		} else {
			proc.stop()
		}
	}
	handle := func(ev childEvent) {
		switch {
		case ev.err != nil:
			if protoErr == nil {
				protoErr = ev.err
			}
			stop(stopProtocol, true)
		case ev.msg == nil:
			logDebugMsg("the agent run process wrote to stdout: " + ev.stray)
		default:
			m := ev.msg
			switch m.Type {
			case msgHello:
				hello = true
				handshake.Stop()
				if m.Protocol != childProtocol || m.Version != version.Version {
					protoErr = fmt.Errorf("the agent run process is version %s of the provider (protocol %d), but the "+
						"provider is version %s (protocol %d): the provider's executable was replaced while it ran; run "+
						"the command again", m.Version, m.Protocol, version.Version, childProtocol)
					stop(stopProtocol, true)
				}
			case msgTimed:
				if backstop == nil {
					backstop = time.NewTimer(creq.Timeout + childGrace())
					backstopC = backstop.C
				}
			case msgProgress:
				if req.Progress != nil {
					req.Progress(Progress{Message: clean(m.Message), Persistent: m.Persistent})
				}
			case msgLog:
				if req.Log != nil {
					req.Log(slog.Level(m.Level), clean(m.Message))
				}
			case msgWarn:
				if req.Warn != nil {
					req.Warn(clean(m.Message))
				}
			case msgResult:
				// A result after the parent canceled the run does not count: in process, the run
				// was canceled when the cancellation came before the end of the conversation.
				if result == nil && childErr == "" && m.Result != nil && reason != stopContext {
					result = m.Result
				}
			case msgError:
				if result == nil && childErr == "" {
					childErr, childMissing = m.Message, m.Missing
				}
			}
		}
	}
	for msgs != nil || exited != nil {
		select {
		case ev, ok := <-msgs:
			if !ok {
				msgs = nil
				continue
			}
			handle(ev)
		case line, ok := <-strays:
			if !ok {
				strays = nil
				continue
			}
			logDebugMsg("the agent run process wrote to stderr: " + line)
		case msg := <-debugs:
			logDebugMsg(msg)
		case <-exited:
			exited, exitedAt = nil, time.Now()
			// The output is read for at most harnessDrain after the child exited.
			drainC = time.After(harnessDrain)
		case <-drainC:
			drainC = nil
			_ = stdoutR.Close()
		case <-ctxDone:
			ctxDone = nil
			stop(stopContext, false)
		case <-handshake.C:
			if !hello {
				protoErr = fmt.Errorf("the agent run process did not start: no handshake within %s", childHandshakeTimeout)
				stop(stopProtocol, true)
			}
		case <-backstopC:
			backstopC = nil
			stop(stopTimeout, true)
		}
	}

	// What the child or a process it left behind writes to stderr is read for the rest of
	// harnessDrain.
	closeIn()
	drain := time.NewTimer(max(time.Until(exitedAt.Add(harnessDrain)), 0))
	for strays != nil {
		select {
		case line, ok := <-strays:
			if !ok {
				strays = nil
				continue
			}
			logDebugMsg("the agent run process wrote to stderr: " + line)
		case msg := <-debugs:
			logDebugMsg(msg)
		case <-drain.C:
			_ = stderrR.Close()
		}
	}
	drain.Stop()
	for done := proc.done(); done != nil; {
		select {
		case <-done:
			done = nil
		case msg := <-debugs:
			logDebugMsg(msg)
		}
	}
	for empty := false; !empty; {
		select {
		case msg := <-debugs:
			logDebugMsg(msg)
		default:
			empty = true
		}
	}

	out := childOutcome{}
	if n := proc.leftRunning(); n > 0 {
		out.warnings = append(out.warnings, childLeftRunningWarning(n))
	}
	switch err := ctx.Err(); {
	case result != nil && protoErr == nil:
		out.result = result
	case err != nil:
		if errors.Is(err, context.DeadlineExceeded) {
			out.err = errors.New("the operation's time limit (customTimeouts) ran out before the agent finished")
		} else {
			out.err = fmt.Errorf("the agent run was canceled: %w", err)
		}
	case proc.stoppedForShutdown():
		out.err = errStoppedForShutdown
	case childErr != "":
		out.err, out.fromChild, out.missing = errors.New(childErr), true, childMissing
	case protoErr != nil:
		out.err = protoErr
	case reason == stopTimeout:
		out.err = fmt.Errorf("the agent did not finish within %s (timeoutSeconds)", creq.Timeout)
	default:
		state, waitErr := proc.state()
		out.err = noResultError(state, waitErr, names.hide(tail.text(secrets)))
	}
	return out
}

// noResultError is the error of a run child that ended without a result.
func noResultError(state *os.ProcessState, waitErr error, stderr string) error {
	var how string
	switch {
	case state == nil:
		how = fmt.Sprintf("could not be waited for (%v)", waitErr)
	case state.ExitCode() < 0:
		how = "was stopped (" + state.String() + ")"
	default:
		how = fmt.Sprintf("ended with exit status %d", state.ExitCode())
	}
	msg := "the agent run process " + how + " without a result"
	if stderr != "" {
		msg += ": " + stderr
	}
	return errors.New(msg)
}

// childLeftRunningWarning says that a run child left n processes running when it ended (a
// command a tool ran in the background, a server a toolset did not stop), which the parent
// stopped. It names no command: a command line can contain a secret.
func childLeftRunningWarning(n int) string {
	what, them := "1 process", "it"
	if n > 1 {
		what, them = fmt.Sprintf("%d processes", n), "them"
	}
	return fmt.Sprintf("the agent run left %s running when it ended (for example a command a tool ran in the "+
		"background); AgentRun stopped %s", what, them)
}

// readChildOutput reads a run child's stdout: protocol messages and other lines. It closes
// events at the end of the output.
func readChildOutput(r io.Reader, events chan<- childEvent) {
	readChildOutputLimited(r, events, maxChildMessage)
}

// readChildOutputLimited is readChildOutput with a limit for one message.
func readChildOutputLimited(r io.Reader, events chan<- childEvent, limit int) {
	defer close(events)
	br := bufio.NewReaderSize(r, 64<<10)
	magic := []byte(childMagic)
	for {
		line, tooLong, err := readLine(br, limit)
		if len(line) > 0 || tooLong {
			// A write without a newline before a message shares its line.
			i := bytes.Index(line, magic)
			if i != 0 {
				stray := line
				if i > 0 {
					stray = line[:i]
				}
				if s := strings.TrimSpace(string(stray)); s != "" {
					events <- childEvent{stray: truncate(s, maxResult)}
				}
			}
			switch {
			case i >= 0 && tooLong:
				events <- childEvent{err: fmt.Errorf("the agent run process sent a message larger than %d bytes", limit)}
			case i >= 0:
				var m childMessage
				if err := json.Unmarshal(line[i+len(magic):], &m); err != nil {
					events <- childEvent{err: fmt.Errorf("reading a message of the agent run process: %w", err)}
				} else {
					events <- childEvent{msg: &m}
				}
			}
		}
		if err != nil {
			return
		}
	}
}

// readLine reads a line of at most limit bytes (without the newline). A longer line is
// read to its end and returned cut, with tooLong set.
func readLine(br *bufio.Reader, limit int) (line []byte, tooLong bool, err error) {
	for {
		chunk, err := br.ReadSlice('\n')
		if len(line)+len(chunk) > limit {
			tooLong = true
			if len(line) < limit {
				line = append(line, chunk[:limit-len(line)]...)
			}
		} else {
			line = append(line, chunk...)
		}
		switch {
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case err != nil:
			return line, tooLong, err
		}
		return bytes.TrimSuffix(bytes.TrimSuffix(line, []byte("\n")), []byte("\r")), tooLong, nil
	}
}

// childStderr keeps the end of a run child's stderr, and the start of a panic, for the error
// of a child that ended without a result.
type childStderr struct {
	tail stderrTail
	mu   sync.Mutex
	// panic is the first line that starts a Go panic or fatal error.
	panic string
}

// text returns what to show of stderr, redacted: the panic, if the child panicked, else the
// end of stderr.
func (s *childStderr) text(secrets []string) string {
	s.mu.Lock()
	p := s.panic
	s.mu.Unlock()
	if p != "" {
		return truncate(redact(p, secrets), maxReason*5)
	}
	return s.tail.text(secrets)
}

// readChildStderr reads a run child's stderr into tail and sends its lines, without ever
// blocking the child: a line is dropped when lines is full. It closes lines at the end.
func readChildStderr(r io.Reader, s *childStderr, lines chan<- string) {
	defer close(lines)
	br := bufio.NewReaderSize(r, 64<<10)
	for {
		line, _, err := readLine(br, maxStreamLine)
		if len(line) > 0 {
			_, _ = s.tail.Write(append(line, '\n'))
			text := string(line)
			if strings.HasPrefix(text, "panic: ") || strings.HasPrefix(text, "fatal error: ") {
				s.mu.Lock()
				if s.panic == "" {
					s.panic = text
				}
				s.mu.Unlock()
			}
			if t := strings.TrimSpace(text); t != "" {
				select {
				case lines <- truncate(t, maxResult):
				default:
				}
			}
		}
		if err != nil {
			return
		}
	}
}
