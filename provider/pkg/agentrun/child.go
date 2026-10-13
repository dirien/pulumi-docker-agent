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
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	version "github.com/dirien/pulumi-docker-agent/provider"
	"github.com/dirien/pulumi-docker-agent/provider/pkg/validate"
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
//     read the request), then progress, logs and warnings, and a result or an error as soon
//     as the conversation ended. Then the child stops the runtime and its toolsets, whose
//     logs still follow, and exits. The child moves everything else away from the protocol:
//     os.Stdout is os.Stderr, on Linux and macOS file descriptor 1 is a copy of stderr, and
//     on Windows the standard output handle is stderr. The protocol has a descriptor (handle)
//     of its own that no process the child starts inherits, so its end follows the child's
//     exit, and the parent reads it to the end. The parent logs lines without childMagic and
//     the child's stderr (panics included) at debug level, redacted;
//   - the child redacts what it sends with its own redaction set (it never gets the
//     credentials of the provider's environment it does not inherit), and the parent redacts
//     everything again with the provider's full set, decides whether the answer is secret,
//     and reports the summary;
//   - the parent owns the end of the run: it calls the Request callbacks from Run's goroutine,
//     supervises the child (child_unix.go, child_windows.go) and writes the error of a run that
//     was canceled, timed out, stopped for a shutdown, or whose child ended without a result
//     (childEnd.outcome). The child gets the parent's deadline, so its own errors for those
//     cases read the same. As in process, the run is judged by the end of the conversation:
//     once the result or the error arrived, customTimeouts and timeoutSeconds no longer
//     count, and the teardown gets childTeardownGrace before the child is killed.
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
	// childQueue is the length of the child's message queue; DEBUG and INFO records are
	// dropped when it is full.
	childQueue = 256
)

// childHandshakeTimeout bounds the time a child takes to start and send its handshake; a
// variable, so that tests can shorten it.
var childHandshakeTimeout = 30 * time.Second

// childGrace is how long a canceled run child may take to stop (its Claude Code CLIs get
// harnessGrace, their output harnessDrain) before it is killed.
func childGrace() time.Duration { return harnessGrace + harnessDrain + childGraceMargin }

// childTeardownGrace is how long a run child may take, once it sent its result or error, to
// stop the runtime and its toolsets (stopToolSets gives them toolsetStopTimeout) and to send
// its last messages before it is killed.
func childTeardownGrace() time.Duration { return toolsetStopTimeout + childGrace() }

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
	// Before anything else: nothing may write to the protocol but the child's writer.
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
		if status, stop := creq.Test.apply(ctx, w, input); stop {
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
	// The result or the error goes out as soon as the conversation ended, before the runtime and
	// the toolsets are stopped: the parent judges the run by it, and gives the teardown its own
	// time (childTeardownGrace).
	runRequest(ctx, req, creq.StateRoot, func() { w.send(childMessage{Type: msgTimed}) }, func(res *Result, err error) {
		w.send(endMessage(res, err))
		if creq.Test != nil && testing.Testing() {
			creq.Test.afterEnd(w)
		}
	})
	return 0
}

// endMessage is the message that ends a run: its result, or its error.
func endMessage(res *Result, err error) childMessage {
	if err != nil {
		var missing []string
		if mce, ok := errors.AsType[*MissingCredentialsError](err); ok {
			missing = mce.Variables
		}
		return childMessage{Type: msgError, Message: err.Error(), Missing: missing}
	}
	cr := &childResult{
		Output: res.Output, HasStructuredOutput: res.HasStructuredOutput, AgentName: res.AgentName, Usage: res.Usage,
		ToolCalls: res.ToolCalls, RejectedToolCalls: res.RejectedToolCalls, Warnings: res.Warnings,
		OutputContainsSecret: res.OutputContainsSecret, CostNote: res.costNote,
	}
	if res.HasStructuredOutput {
		b, err := json.Marshal(res.StructuredOutput)
		if err != nil {
			return childMessage{Type: msgError, Message: "encoding the structured output: " + err.Error()}
		}
		cr.StructuredOutput = b
	}
	return childMessage{Type: msgResult, Result: cr}
}

// childWriter writes a run child's messages from one goroutine, the only writer of the
// protocol. send waits for room in the queue, and so does log for a WARN or ERROR record (the
// provider makes some of them Pulumi warnings); log drops a DEBUG or INFO record when the
// queue is full, so that debug logging never holds up the run, and close reports how many it
// dropped. When a write fails (the parent is gone), broken is called once and later messages
// are dropped.
type childWriter struct {
	out     io.Writer
	queue   chan childMessage
	done    chan struct{}
	broken  func()
	closed  atomic.Bool
	dropped atomic.Int64
	mu      sync.RWMutex // held for reading while sending, for writing while closing
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

// log queues a log record: a WARN or ERROR record like send, a DEBUG or INFO record unless
// the queue is full.
func (w *childWriter) log(level slog.Level, msg string) {
	m := childMessage{Type: msgLog, Level: int(level), Message: msg}
	if level >= slog.LevelWarn {
		w.send(m)
		return
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.closed.Load() {
		return
	}
	select {
	case w.queue <- m:
	default:
		w.dropped.Add(1)
	}
}

// close writes everything queued and returns once it was written or a write failed: nothing
// is dropped, not even under pressure. The parent reads the protocol to its end, and kills a
// child that takes too long.
func (w *childWriter) close() {
	if w.closed.Swap(true) {
		<-w.done
		return
	}
	w.mu.Lock()
	if n := w.dropped.Load(); n > 0 {
		w.queue <- childMessage{Type: msgLog, Level: int(slog.LevelDebug), Message: fmt.Sprintf("the agent run process "+
			"dropped %d debug records: the provider read its messages too slowly", n)}
	}
	close(w.queue)
	w.mu.Unlock()
	<-w.done
}

// --- the parent -------------------------------------------------------------------------------

// errStoppedForShutdown is the error of a run that a SIGINT to the provider stopped.
var errStoppedForShutdown = errors.New("the agent run was stopped because Pulumi is stopping the provider (SIGINT)")

// childEvent is what the parent reads from a run child: a message, a line of other output
// (raw, at most maxStreamLine bytes; cut reports that the line was longer), or a protocol
// error.
type childEvent struct {
	msg   *childMessage
	stray string
	cut   bool
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
			req.Log(slog.LevelDebug, names.hide(redact(msg, secrets)))
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
	// expire kills the child at once without making that a stop: what it left running is
	// still counted (leftRunning). For a child whose teardown took too long.
	expire()
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
	// Redacted first, then without the names: hiding a name inside a secret would split it.
	logDebugMsg := func(msg string) {
		if req.Log != nil {
			req.Log(slog.LevelDebug, names.hide(redact(msg, secrets)))
		}
	}
	// strayText is a line the child wrote outside the protocol, for the debug log: redacted,
	// then without the names, then shortened. Shortening first would keep the start of a
	// secret that redaction no longer recognizes; for the same reason a line that was cut
	// loses the start of a secret it ends with.
	strayText := func(line string, cut bool) string {
		if cut {
			line = withoutSecretHead(line, secrets)
		}
		return truncate(names.hide(redact(line, secrets)), maxResult)
	}
	logStray := func(where, text string) {
		if req.Log != nil {
			req.Log(slog.LevelDebug, "the agent run process wrote to "+where+": "+text)
		}
	}
	body, err := json.Marshal(creq)
	if err != nil {
		return childOutcome{err: fmt.Errorf("encoding the agent run request: %w", err)}
	}
	exes, err := childExecutables()
	if err != nil {
		return childOutcome{err: fmt.Errorf("finding the provider binary for the agent run: %w", err)}
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

	newCmd := func(exe string) *exec.Cmd {
		cmd := exec.Command(exe, childArgs()...) //nolint:gosec // the provider's own executable
		cmd.Env = childExecEnv(os.Environ(), creq.InheritEnvironment)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = stdinR, stdoutW, stderrW
		return cmd
	}
	// The tracker's debug messages come from its goroutines; they are logged from this one.
	debugs := make(chan string, 64)
	debug := func(msg string) {
		select {
		case debugs <- msg:
		default:
		}
	}
	proc, err := startRunChildFrom(exes, newCmd, closeIn, debug)
	// The child has its own copies of these ends.
	_, _, _ = stdinR.Close(), stdoutW.Close(), stderrW.Close()
	if err != nil {
		return childOutcome{err: err}
	}

	go func() {
		_, _ = stdinW.Write(append(body, '\n'))
	}()
	stdout := &waitingReader{r: stdoutR}
	msgs := make(chan childEvent)
	go readChildOutput(stdout, msgs)
	strays := make(chan string, childQueue)
	tail := &childStderr{}
	go readChildStderr(stderrR, tail, strays, strayText)

	var (
		reason childStop
		end    childEnd
		hello  bool
		// ended is set when the child sent its result or error: the end of the conversation.
		ended     bool
		exitedAt  time.Time
		stuckC    <-chan time.Time
		teardownC <-chan time.Time
		ctxDone   = ctx.Done()
		exited    = proc.exited()
		handshake = time.NewTimer(childHandshakeTimeout)
		// The child enforces timeoutSeconds itself, from when it starts to load its configuration;
		// the backstop ends a child that does not, childGrace later.
		backstop  *time.Timer
		backstopC <-chan time.Time
	)
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
	// conversationEnded takes the child's result or error. As in process, the run is judged by
	// the end of the conversation: the timeoutSeconds backstop and the parent's context no
	// longer count, and the child gets childTeardownGrace to stop the runtime and its toolsets.
	conversationEnded := func(m *childMessage) {
		ended = true
		switch {
		case m.Type == msgResult && m.Result != nil && reason == stopNone:
			// A result after the parent stopped the run does not count: in process, the run was
			// canceled when the cancellation came before the end of the conversation.
			end.result = m.Result
		case m.Type == msgError:
			// An error that came after the context ended (the child has the same deadline) or
			// after a stop is a consequence of it.
			end.childErr, end.missing = m.Message, m.Missing
			end.first = reason == stopNone && !proc.stoppedForShutdown() && ctx.Err() == nil
		}
		if backstop != nil {
			backstop.Stop()
		}
		backstopC, ctxDone = nil, nil
		teardownC = time.After(childTeardownGrace())
	}
	handle := func(ev childEvent) {
		switch {
		case ev.err != nil:
			// After the end of the conversation, or once the child is being stopped (a kill can
			// cut a line), a broken message changes nothing.
			if ended || reason != stopNone || proc.stoppedForShutdown() {
				logDebugMsg("the agent run process sent a broken message: " + ev.err.Error())
				return
			}
			end.protoErr = ev.err
			stop(stopProtocol, true)
		case ev.msg == nil:
			logStray("stdout", strayText(ev.stray, ev.cut))
		default:
			m := ev.msg
			switch m.Type {
			case msgHello:
				hello = true
				handshake.Stop()
				if m.Protocol != childProtocol || m.Version != version.Version {
					end.protoErr = fmt.Errorf("the agent run process is version %s of the provider (protocol %d), but the "+
						"provider is version %s (protocol %d): the provider's executable was replaced while it ran; run "+
						"the command again", m.Version, m.Protocol, version.Version, childProtocol)
					stop(stopProtocol, true)
				}
			case msgTimed:
				if backstop == nil && !ended {
					backstop = time.NewTimer(creq.Timeout + childGrace())
					backstopC = backstop.C
				}
			case msgProgress:
				if req.Progress != nil {
					req.Progress(Progress{Message: redact(m.Message, secrets), Persistent: m.Persistent})
				}
			case msgLog:
				if req.Log != nil {
					req.Log(slog.Level(m.Level), redact(m.Message, secrets))
				}
			case msgWarn:
				if req.Warn != nil {
					req.Warn(redact(m.Message, secrets))
				}
			case msgResult, msgError:
				if !ended {
					conversationEnded(m)
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
			logStray("stderr", line)
		case msg := <-debugs:
			logDebugMsg(msg)
		case <-exited:
			exited, exitedAt = nil, time.Now()
			// Only the child holds the protocol's pipe, so its end follows the exit, and what the
			// child wrote before is read however slowly the callbacks take it. Should a process
			// the child started hold the pipe after all, the output is no longer read once a read
			// has waited harnessDrain for it.
			stuckC = time.After(harnessDrain)
		case <-stuckC:
			if d := stdout.waiting(); d < harnessDrain {
				stuckC = time.After(harnessDrain - d)
				continue
			}
			stuckC = nil
			logDebugMsg(fmt.Sprintf("the protocol of the agent run process stayed open %s after it exited (a process "+
				"it started holds it); it is no longer read", harnessDrain))
			_ = stdoutR.Close()
		case <-ctxDone:
			ctxDone = nil
			stop(stopContext, false)
		case <-handshake.C:
			if !hello {
				end.protoErr = fmt.Errorf("the agent run process did not start: no handshake within %s", childHandshakeTimeout)
				stop(stopProtocol, true)
			}
		case <-backstopC:
			backstopC = nil
			stop(stopTimeout, true)
		case <-teardownC:
			teardownC = nil
			logDebugMsg(fmt.Sprintf("the agent run process did not stop its runtime and toolsets within %s after the "+
				"run ended; AgentRun stopped it", childTeardownGrace()))
			proc.expire()
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
			logStray("stderr", line)
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

	end.shutdown, end.ctxErr, end.reason = proc.stoppedForShutdown(), ctx.Err(), reason
	out := end.outcome(creq.Timeout, func() error {
		state, waitErr := proc.state()
		return noResultError(state, waitErr, tail.text(secrets, names))
	})
	if n := proc.leftRunning(); n > 0 {
		out.warnings = append(out.warnings, childLeftRunningWarning(n))
	}
	return out
}

// childEnd is what the parent knows of a run child once the child and what it left running
// ended.
type childEnd struct {
	result *childResult
	// childErr is the child's own error, and missing the variables it says are missing; first
	// reports that the error came before the parent's context ended, before the parent stopped
	// the child and before Pulumi stopped the provider.
	childErr string
	first    bool
	missing  []string
	// shutdown: a SIGINT to the provider stopped the child. ctxErr is the error of the
	// parent's context.
	shutdown bool
	ctxErr   error
	protoErr error
	reason   childStop
}

// outcome decides how the run ended, in this order:
//
//   - the result the child sent;
//   - an error the child sent before the context ended and before it was stopped: the run's
//     own error (a rejected key, a missing variable), also when Pulumi stops the provider or
//     the deadline passes right after;
//   - the shutdown, also when the context was canceled too: Pulumi cancels the operation
//     before it sends SIGINT;
//   - the context: its deadline (customTimeouts), then its cancellation;
//   - a protocol error, then the timeoutSeconds backstop;
//   - an error the child sent while it was being stopped;
//   - noResult: the child ended without a result.
func (e childEnd) outcome(timeout time.Duration, noResult func() error) childOutcome {
	var out childOutcome
	switch {
	case e.result != nil:
		out.result = e.result
	case e.childErr != "" && e.first:
		out.err, out.fromChild, out.missing = errors.New(e.childErr), true, e.missing
	case e.shutdown:
		out.err = errStoppedForShutdown
	case errors.Is(e.ctxErr, context.DeadlineExceeded):
		out.err = errors.New("the operation's time limit (customTimeouts) ran out before the agent finished")
	case e.ctxErr != nil:
		out.err = fmt.Errorf("the agent run was canceled: %w", e.ctxErr)
	case e.protoErr != nil:
		out.err = e.protoErr
	case e.reason == stopTimeout:
		out.err = fmt.Errorf("the agent did not finish within %s (timeoutSeconds)", timeout)
	case e.childErr != "":
		out.err, out.fromChild, out.missing = errors.New(e.childErr), true, e.missing
	default:
		out.err = noResult()
	}
	return out
}

// startRunChildFrom starts a run child from the first of the executables that starts (on
// Linux /proc/self/exe, then the binary's path). When none does because of its file, the
// error says that the binary is gone or cannot be executed.
func startRunChildFrom(exes []string, newCmd func(string) *exec.Cmd, closeStdin func(), debug func(string),
) (runChildProcess, error) {
	var err error
	for i, exe := range exes {
		var proc runChildProcess
		proc, err = startRunChild(newCmd(exe), closeStdin, debug)
		switch {
		case err == nil:
			return proc, nil
		case errors.Is(err, errShuttingDown):
			return nil, err
		case i < len(exes)-1:
			debug(fmt.Sprintf("starting the agent run process with %s: %v; trying %s", exe, err, exes[i+1]))
		}
	}
	if cannotExec(err) {
		return nil, fmt.Errorf("the provider binary %s cannot be started for the agent run: %w; was the plugin upgraded "+
			"or removed while Pulumi ran?", exes[len(exes)-1], err)
	}
	return nil, fmt.Errorf("starting the agent run process: %w", err)
}

// cannotExec reports whether starting a program failed because of its file: it is gone, not
// executable, or no program.
func cannotExec(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission) || errors.Is(err, syscall.ENOEXEC)
}

// waitingReader records since when a Read has been waiting for data.
type waitingReader struct {
	r     io.Reader
	since atomic.Int64 // UnixNano when the pending Read started; 0 without one
}

func (w *waitingReader) Read(p []byte) (int, error) {
	w.since.Store(time.Now().UnixNano())
	defer w.since.Store(0)
	return w.r.Read(p)
}

// waiting returns how long the pending Read has been waiting, 0 without one.
func (w *waitingReader) waiting() time.Duration {
	if s := w.since.Load(); s != 0 {
		return time.Since(time.Unix(0, s))
	}
	return 0
}

// withoutSecretHead replaces the start of a secret that s ends with by [secret]: s was cut,
// and redaction does not recognize the part of a secret before the cut.
func withoutSecretHead(s string, secrets []string) string {
	cut := 0
	for _, secret := range secrets {
		secret = strings.TrimSpace(secret)
		for n := min(len(secret)-1, len(s)); n > cut; n-- {
			if strings.HasSuffix(s, secret[:n]) {
				cut = n
				break
			}
		}
	}
	if cut == 0 {
		return s
	}
	return s[:len(s)-cut] + validate.Redacted
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
				// Raw: the parent redacts it before it shortens it.
				stray, cut := line, tooLong && i < 0
				if i > 0 {
					stray = line[:i]
				}
				if len(stray) > maxStreamLine {
					stray, cut = stray[:maxStreamLine], true
				}
				if s := strings.TrimSpace(string(stray)); s != "" {
					events <- childEvent{stray: s, cut: cut}
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

// text returns what to show of stderr, redacted and without the names: the panic, if the
// child panicked, else the end of stderr.
func (s *childStderr) text(secrets []string, names *nameHider) string {
	s.mu.Lock()
	p := s.panic
	s.mu.Unlock()
	if p != "" {
		return truncate(names.hide(redact(p, secrets)), maxReason*5)
	}
	return names.hide(s.tail.text(secrets))
}

// readChildStderr reads a run child's stderr into tail and sends its lines, made fit for the
// log by clean (cut reports a line longer than maxStreamLine), without ever blocking the
// child: a line is dropped when lines is full. It closes lines at the end.
func readChildStderr(r io.Reader, s *childStderr, lines chan<- string, clean func(line string, cut bool) string) {
	defer close(lines)
	br := bufio.NewReaderSize(r, 64<<10)
	for {
		line, tooLong, err := readLine(br, maxStreamLine)
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
				case lines <- clean(t, tooLong):
				default:
				}
			}
		}
		if err != nil {
			return
		}
	}
}
