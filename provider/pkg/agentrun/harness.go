// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package agentrun

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker-agent/pkg/codingharness"
	"github.com/docker/docker-agent/pkg/config/latest"
	"github.com/docker/docker-agent/pkg/harness"
	baseharness "github.com/rumpl/harness"

	"github.com/dirien/pulumi-docker-agent/provider/pkg/validate"
)

// Coding-harness agents (`harness: {type: claude-code}`) hand their turn to the Claude Code
// CLI. docker-agent's own driver (pkg/codingharness on rumpl/harness) runs the CLI in the
// provider's working directory with the provider's whole environment, passes the prompt as
// an argument, discards stderr and stops the CLI only with SIGKILL. AgentRun therefore runs
// the CLI itself and reuses only the driver's arguments and stream parser:
//
//   - in the run's working directory, with the environment of claudeEnv (procenv.go);
//   - with the prompt on stdin, so it is never part of argv (no 128 KiB argument limit, no
//     option injection, not visible in /proc/<pid>/cmdline);
//   - in its own process group. A cancelled or timed-out run asks the group to stop
//     (SIGTERM, SIGCONT), keeps reading its output so that the CLI can shut down, and kills
//     the group after harnessGrace. After the CLI exits, the rest of its group gets the same
//     treatment with postExitGrace (harness_unix.go). Claude Code runs every shell command
//     in a session of its own, which the group signals do not reach; it stops the ones it
//     tracks when it gets SIGTERM;
//   - with the end of stderr kept, redacted, for error messages;
//   - reporting only Claude Code's final result as the agent's answer: docker-agent would
//     otherwise join the text of every turn into it.
//
// Only claude-code is supported, and only on Linux and macOS: the other harness types pass
// the prompt as an argument and need their own error handling, and Windows has no process
// groups to stop the CLI's tools with.

// Process handling. Variables, so that tests can shorten them.
var (
	// harnessGrace is how long a cancelled CLI may take to stop before its group is killed.
	harnessGrace = 10 * time.Second
	// postExitGrace is how long the rest of the group may take to stop after the CLI exited.
	postExitGrace = time.Second
	// harnessDrain is how long the runner keeps reading the output pipes after the CLI
	// exited, in total: a process the CLI started can hold them open.
	harnessDrain = 2 * time.Second
	// signalGrace is how long the CLIs' groups get after a SIGINT before they are killed.
	// Pulumi kills a plugin 5 s after it asked it to stop.
	signalGrace = 2 * time.Second
)

// Limits of what the runner keeps from the CLI's output.
const (
	maxStreamLine    = 16 << 20 // as rumpl/harness
	maxToolArguments = 64 << 10
	stderrKeep       = 16 << 10
	stderrShow       = 4 << 10
)

// harnessToolCategory is the tool category docker-agent gives the tool calls a harness
// reports.
const harnessToolCategory = "harness"

// harnessOS is the operating system harness agents run on; tests override it.
var harnessOS = goruntime.GOOS

// geteuid is os.Geteuid; tests override it.
var geteuid = os.Geteuid

func harnessOSSupported() bool { return harnessOS == "linux" || harnessOS == "darwin" }

func harnessOSMessage() string {
	return "coding-harness agents in AgentRun are supported on Linux and macOS, not on " + harnessOS
}

// harnessSupported reports why a harness configuration cannot run in AgentRun (H2, H3).
func harnessSupported(cfg *latest.HarnessConfig) error {
	switch {
	case cfg == nil:
		return errors.New("harness config is nil")
	case cfg.Type != codingharness.TypeClaudeCode:
		return fmt.Errorf("%s harness agents are not supported by AgentRun yet: only %s is; run them with docker agent run",
			cfg.Type, codingharness.TypeClaudeCode)
	case !harnessOSSupported():
		return errors.New(harnessOSMessage())
	}
	return nil
}

// harnessRun is what the Claude Code CLIs of one run share.
type harnessRun struct {
	dir     string
	env     claudeEnvironment
	secrets []string
	info    *harnessInfo
}

// harnessInfo is what the CLIs of a run reported about themselves, and what their
// environment selects. The runner sets it on docker-agent's goroutine; Run reads it
// afterwards.
type harnessInfo struct {
	mu                           sync.Mutex
	seen                         bool
	version, model, apiKeySource string
	// emptyResults counts the CLI runs whose empty final result was replaced by their text.
	emptyResults int
	// noKey is what the CLIs' environment bills when Claude Code reports apiKeySource none.
	noKey noKeySource
}

func (h *harnessInfo) set(version, model, apiKeySource string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.seen, h.version, h.model, h.apiKeySource = true, version, model, apiKeySource
}

func (h *harnessInfo) emptyResult() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.emptyResults++
}

func (h *harnessInfo) setNoKey(n noKeySource) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.noKey = n
}

// harnessReport is what Run reports about the CLIs of a run.
type harnessReport struct {
	seen                         bool
	version, model, apiKeySource string
	emptyResults                 int
	noKey                        noKeySource
}

// get returns what the last CLI of the run reported in its init line, and the rest.
func (h *harnessInfo) get() harnessReport {
	h.mu.Lock()
	defer h.mu.Unlock()
	return harnessReport{h.seen, h.version, h.model, h.apiKeySource, h.emptyResults, h.noKey}
}

// newHarnessFactory returns the harness factory of a run. docker-agent calls it for every
// turn of a harness agent, with the agent's harness configuration.
func newHarnessFactory(h harnessRun) harness.Factory {
	return func(cfg *latest.HarnessConfig) (harness.Provider, error) {
		if err := harnessSupported(cfg); err != nil {
			return nil, err
		}
		p, err := codingharness.NewProvider(cfg)
		if err != nil {
			return nil, err
		}
		cp, ok := p.(baseharness.ResumableCommandProvider)
		if !ok {
			return nil, errors.New("the claude-code harness driver has no command form")
		}
		return &claudeHarness{p: cp, run: h}, nil
	}
}

// claudeHarness runs one turn of a claude-code agent.
type claudeHarness struct {
	p   baseharness.ResumableCommandProvider
	run harnessRun
}

func (c *claudeHarness) Name() string { return c.p.Name() }

func (c *claudeHarness) Run(ctx context.Context, prompt string, handle func(harness.Event)) error {
	return c.exec(ctx, c.p.PrintArgs(prompt), prompt, handle)
}

func (c *claudeHarness) Resume(ctx context.Context, sessionID, prompt string, handle func(harness.Event)) error {
	if sessionID == "" {
		return errors.New("resume: session ID is empty")
	}
	return c.exec(ctx, c.p.ResumeArgs(sessionID, prompt), prompt, handle)
}

func (c *claudeHarness) exec(ctx context.Context, args []string, prompt string, handle func(harness.Event)) error {
	args, err := stdinArgs(args, prompt)
	if err != nil {
		return err
	}
	return runClaude(ctx, c.run, args, prompt, &claudeStream{parser: c.p, handle: handle, info: c.run.info})
}

var errUnexpectedArgs = errors.New("unexpected claude-code arguments from the harness driver")

// stdinArgs drops the prompt from the driver's arguments, which end in `-p <prompt>`: the
// prompt goes to stdin, and `-p` alone is the print flag. Arguments of another shape fail
// closed rather than leak the prompt into argv.
func stdinArgs(args []string, prompt string) ([]string, error) {
	n := len(args)
	if n < 3 || args[0] != "claude" || args[n-2] != "-p" || args[n-1] != prompt {
		return nil, errUnexpectedArgs
	}
	return args[:n-1], nil
}

// errClaudeNotFound is the error when no claude CLI is on the PATH of the CLI's environment.
var errClaudeNotFound = errors.New("the claude-code harness needs the claude CLI, which was not found on PATH: install " +
	"Claude Code on the machine that runs pulumi up, or set PATH in env")

// lookPathIn finds an executable in a PATH. Unlike exec.LookPath it takes the PATH of the
// CLI's environment, and it skips empty and relative entries: the CLI starts in the working
// directory, which may be a repository nobody reviewed.
func lookPathIn(file, path string) (string, error) {
	for _, dir := range filepath.SplitList(path) {
		if dir == "" || !filepath.IsAbs(dir) {
			continue
		}
		p := filepath.Join(dir, file)
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() && fi.Mode().Perm()&0o111 != 0 {
			return p, nil
		}
	}
	return "", errClaudeNotFound
}

// harnessProcess is a started Claude Code CLI and its process group (harness_unix.go).
type harnessProcess struct {
	pgid int
	// exited is closed when the CLI exited and was reaped; state, waitErr and exitedAt are
	// set then.
	exited   chan struct{}
	state    *os.ProcessState
	waitErr  error
	exitedAt time.Time
	// done is closed when the rest of the group was stopped after the CLI exited.
	done chan struct{}

	mu       sync.Mutex
	finished bool // the group is gone: no more signals
	stopping bool // SIGTERM was sent
	shutdown bool // stopped because the provider is shutting down
	kill     *time.Timer
	killAt   time.Time
}

// stoppedForShutdown reports whether the CLI was stopped because the provider got SIGINT.
func (p *harnessProcess) stoppedForShutdown() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.shutdown
}

// drainLeft is what is left of the time the runner reads the output after the CLI exited.
func (p *harnessProcess) drainLeft() time.Duration {
	return time.Until(p.exitedAt.Add(harnessDrain))
}

// runClaude runs the CLI with the prompt on stdin and streams its events. It returns when
// the CLI exited, its output was read and the rest of its group was stopped.
func runClaude(ctx context.Context, h harnessRun, args []string, prompt string, s *claudeStream) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	bin, err := lookPathIn(args[0], h.env.path())
	if err != nil {
		return err
	}
	// The child's stdio are pipes of the runner's own, so Go starts no copy goroutines and
	// Wait never waits for a process the CLI left behind.
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
		return fmt.Errorf("creating pipes for claude: %w", err)
	}

	cmd := exec.Command(bin, args[1:]...)
	cmd.Dir = h.dir
	cmd.Env = h.env.list()
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdinR, stdoutW, stderrW
	proc, err := startHarness(cmd)
	// The child has its own copies of these ends.
	_, _, _ = stdinR.Close(), stdoutW.Close(), stderrW.Close()
	if err != nil {
		closeAll()
		return fmt.Errorf("starting claude: %w", err)
	}

	var closeStdin sync.Once
	stdinDone := make(chan struct{})
	go func() {
		defer close(stdinDone)
		_, _ = io.WriteString(stdinW, prompt)
		closeStdin.Do(func() { _ = stdinW.Close() })
	}()
	tail := &stderrTail{}
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		_, _ = io.Copy(tail, stderrR)
	}()

	streamErr := s.read(ctx, stdoutR, proc)
	<-proc.exited
	// A process the CLI started can hold stdin open without reading it.
	closeStdin.Do(func() { _ = stdinW.Close() })
	<-stdinDone
	// stderr shares the drain time of stdout; a cancelled run does not report it.
	if ctx.Err() == nil {
		drain := time.NewTimer(max(proc.drainLeft(), 0))
		select {
		case <-stderrDone:
		case <-drain.C:
		}
		drain.Stop()
	}
	_ = stderrR.Close()
	<-stderrDone
	<-proc.done
	if err := s.result(ctx, proc, streamErr, tail.text(h.secrets)); err != nil {
		return err
	}
	return s.answer()
}

// claudeStream turns the CLI's stream-json output into docker-agent harness events.
type claudeStream struct {
	parser baseharness.Provider
	handle func(harness.Event)
	info   *harnessInfo

	// canceled stops the events: after a cancellation the output is read and discarded.
	canceled bool
	args     map[string]*toolArguments

	// text is the text the CLI streamed, by turn (the text between tool calls), which is not
	// forwarded: the answer is the final result, unless that is empty (see answer).
	text []string
	turn strings.Builder

	sawResult      bool
	resultText     string // the final result's text
	deferredTool   string // the tool a result without an answer waits for
	isError        bool
	errorText      string
	apiErrorStatus int
}

// toolArguments are the streamed arguments of a tool call.
type toolArguments struct {
	b         strings.Builder
	truncated bool
}

type readResult struct {
	data []byte
	err  error
}

// read reads the CLI's output until it ends, and for at most harnessDrain after the CLI
// exited: a process the CLI left behind can keep the pipe open and keep writing. When ctx is
// done it stops the CLI and keeps reading until the CLI exited. It returns a stream error (a
// line over 16 MiB, a read error), after which the CLI is stopped too.
func (s *claudeStream) read(ctx context.Context, stdout *os.File, proc *harnessProcess) error {
	reads := make(chan readResult)
	stopReading := make(chan struct{})
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for {
			buf := make([]byte, 32<<10)
			n, err := stdout.Read(buf)
			select {
			case reads <- readResult{buf[:n], err}:
			case <-stopReading:
				return
			}
			if err != nil {
				return
			}
		}
	}()
	defer func() {
		close(stopReading)
		_ = stdout.Close()
		<-readerDone
	}()

	lines := lineStream{handle: s.line}
	var streamErr error
	fail := func(err error) {
		if streamErr == nil {
			streamErr = err
			s.canceled = true
			proc.stop()
		}
	}
	ctxDone, exited := ctx.Done(), (<-chan struct{})(proc.exited)
	eof := false
	// The drain time starts when the CLI exited and is not extended by reads.
	var drain *time.Timer
	var drainC <-chan time.Time
	defer func() {
		if drain != nil {
			drain.Stop()
		}
	}()
	for {
		select {
		case <-ctxDone:
			ctxDone = nil
			s.canceled = true
			proc.stop()
			if exited == nil {
				// The CLI is gone; what is left is the output of processes it left behind.
				return streamErr
			}
		case <-exited:
			exited = nil
			// After a cancellation or a stream error the output is discarded anyway.
			if eof || s.canceled {
				return streamErr
			}
			drain = time.NewTimer(max(proc.drainLeft(), 0))
			drainC = drain.C
		case <-drainC:
			return streamErr
		case r := <-reads:
			if len(r.data) > 0 && streamErr == nil {
				if err := lines.write(r.data); err != nil {
					fail(err)
				}
			}
			if r.err != nil {
				if errors.Is(r.err, io.EOF) {
					if err := lines.finish(); err != nil {
						fail(err)
					}
				} else {
					fail(r.err)
				}
				eof, reads = true, nil
				if exited == nil {
					return streamErr
				}
			}
		}
	}
}

// line handles one line of the CLI's output.
func (s *claudeStream) line(line string) error {
	if s.canceled {
		return nil
	}
	s.inspect(line)
	for _, ev := range s.parser.ParseStreamLine(line) {
		switch ev.Type {
		case baseharness.EventText:
			// The answer is the final result: docker-agent stores it as the agent's message
			// when no text was streamed.
			s.turn.WriteString(ev.Text)
			continue
		case baseharness.EventToolCallStart, baseharness.EventToolResult:
			s.endTurn()
		case baseharness.EventToolCallDelta:
			s.accumulate(ev.ToolID, ev.ToolArgs)
		case baseharness.EventToolCall:
			s.endTurn()
			if strings.TrimSpace(ev.ToolArgs) == "" {
				ev.ToolArgs = s.toolArgs(ev.ToolID)
			}
			delete(s.args, ev.ToolID)
		case baseharness.EventResult:
			s.sawResult = true
			s.resultText = ev.Result
		}
		s.handle(adaptEvent(ev))
	}
	return nil
}

// endTurn keeps the text streamed since the last tool call.
func (s *claudeStream) endTurn() {
	if t := strings.TrimSpace(s.turn.String()); t != "" {
		s.text = append(s.text, t)
	}
	s.turn.Reset()
}

// answer gives a successful run whose final result is empty an answer. Claude Code reports
// success with an empty result when its last message ends without text (for example after a
// tool call, or with thinking only), and when it stopped for a deferred tool call. The
// answer is then the text it streamed during the run, as docker-agent would store it, with
// a warning; without any text, or for a deferred tool call, which an unattended run cannot
// answer, the run fails.
func (s *claudeStream) answer() error {
	if strings.TrimSpace(s.resultText) != "" {
		return nil
	}
	if s.deferredTool != "" {
		return fmt.Errorf("claude stopped for a deferred call of its %s tool, which needs an interactive session",
			s.deferredTool)
	}
	s.endTurn()
	text := strings.Join(s.text, "\n\n")
	if text == "" {
		return errors.New("claude returned an empty result and no text")
	}
	if s.info != nil {
		s.info.emptyResult()
	}
	s.handle(harness.Event{Type: harness.EventText, Text: text})
	return nil
}

// accumulate collects the streamed arguments of a tool call, up to maxToolArguments: the
// driver reports a streamed call without its arguments.
func (s *claudeStream) accumulate(id, delta string) {
	if id == "" {
		return
	}
	if s.args == nil {
		s.args = map[string]*toolArguments{}
	}
	a := s.args[id]
	if a == nil {
		a = &toolArguments{}
		s.args[id] = a
	}
	if a.truncated || a.b.Len()+len(delta) > maxToolArguments {
		a.truncated = true
		return
	}
	a.b.WriteString(delta)
}

// toolArgs returns the accumulated arguments of a tool call as compact JSON with sorted keys,
// as the driver formats the arguments of a call it reads from a whole message.
func (s *claudeStream) toolArgs(id string) string {
	a := s.args[id]
	if a == nil || a.truncated {
		return ""
	}
	raw := a.b.String()
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	var obj map[string]any
	if dec.Decode(&obj) != nil || obj == nil {
		return raw
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if enc.Encode(obj) != nil {
		return raw
	}
	return strings.TrimSpace(b.String())
}

// inspect reads what the driver's parser ignores: the CLI's init line (version, model and
// credential source) and error results (is_error, errors). Only lines that can be one of
// them are decoded twice.
func (s *claudeStream) inspect(line string) {
	switch {
	case strings.Contains(line, `"type":"result"`):
		var r struct {
			Type            string `json:"type"`
			Subtype         string `json:"subtype"`
			IsError         bool   `json:"is_error"`
			Result          any    `json:"result"`
			Errors          []any  `json:"errors"`
			APIErrorStatus  any    `json:"api_error_status"`
			DeferredToolUse *struct {
				Name string `json:"name"`
			} `json:"deferred_tool_use"`
		}
		if json.Unmarshal([]byte(line), &r) != nil || r.Type != "result" {
			return
		}
		s.sawResult = true
		if r.DeferredToolUse != nil {
			s.deferredTool = cmp.Or(r.DeferredToolUse.Name, "a")
		}
		if !r.IsError {
			return
		}
		s.isError = true
		var errs []string
		for _, e := range r.Errors {
			if t, ok := e.(string); ok && strings.TrimSpace(t) != "" {
				errs = append(errs, t)
			}
		}
		if t, ok := r.Result.(string); ok && strings.TrimSpace(t) != "" {
			s.errorText = t
		} else if len(errs) > 0 {
			s.errorText = strings.Join(errs, "; ")
		} else {
			s.errorText = r.Subtype
		}
		if n, ok := r.APIErrorStatus.(float64); ok {
			s.apiErrorStatus = int(n)
		}
	case strings.Contains(line, `"subtype":"init"`):
		var r struct {
			Type         string `json:"type"`
			Subtype      string `json:"subtype"`
			APIKeySource string `json:"apiKeySource"`
			Version      string `json:"claude_code_version"`
			Model        string `json:"model"`
		}
		if json.Unmarshal([]byte(line), &r) == nil && r.Type == "system" && r.Subtype == "init" && s.info != nil {
			s.info.set(r.Version, r.Model, r.APIKeySource)
		}
	}
}

// result maps the end of a CLI run to the error docker-agent reports (as "harness
// claude-code failed: ..."), with a hint for the usual causes.
func (s *claudeStream) result(ctx context.Context, proc *harnessProcess, streamErr error, stderr string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if streamErr != nil {
		return fmt.Errorf("reading the output of claude: %w", streamErr)
	}
	state := proc.state
	if state == nil {
		return fmt.Errorf("waiting for claude: %w", proc.waitErr)
	}
	if (!state.Success() || !s.sawResult) && proc.stoppedForShutdown() {
		return errors.New("claude was stopped because Pulumi is stopping the provider (SIGINT)")
	}
	hint := claudeHint(s.errorText, stderr, s.apiErrorStatus)
	switch {
	case !state.Success():
		msg := "claude exited with status " + fmt.Sprint(state.ExitCode())
		if state.ExitCode() < 0 {
			msg = "claude was stopped (" + state.String() + ")"
		}
		if detail := firstNonEmpty(s.errorText, stderr); detail != "" {
			msg += ": " + detail
		}
		return errors.New(msg + hint)
	case s.isError:
		return errors.New("claude reported an error: " + s.errorText + hint)
	case !s.sawResult:
		msg := "claude ended without a result"
		if stderr != "" {
			msg += ": " + stderr
		}
		return errors.New(msg + hint)
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// Hints for the usual causes of a failing CLI.
const (
	authHint = "Claude Code could not authenticate: log in with claude on this machine, or create a long-lived token " +
		"with claude setup-token and pass it as the secret env value CLAUDE_CODE_OAUTH_TOKEN"
	contentionHint = "another Claude Code process (for example your interactive session) was refreshing the login at " +
		"the same time: retry, or use claude setup-token with CLAUDE_CODE_OAUTH_TOKEN"
	rootHint = "Claude Code refuses to run as root: run pulumi up as a non-root user, or, in an isolated container, set " +
		"IS_SANDBOX=1 in env"
)

// claudeHint returns at most one hint, to append to an error: for a login another Claude
// Code process was refreshing, for a failed authentication (the API status or Claude Code's
// own wording), or for a CLI that refused to run as root.
func claudeHint(errorText, stderr string, apiErrorStatus int) string {
	text := errorText + "\n" + stderr
	switch {
	case strings.Contains(text, "another Claude Code process is refreshing it"):
		return "; " + contentionHint
	case apiErrorStatus == 401 || apiErrorStatus == 403 ||
		containsAny(text, "Failed to authenticate", "OAuth session expired", "Invalid API key", "/login"):
		return "; " + authHint
	case strings.Contains(stderr, "root/sudo"):
		return "; " + rootHint
	}
	return ""
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// adaptEvent converts a driver event to docker-agent's harness event, as pkg/codingharness
// does.
func adaptEvent(ev baseharness.Event) harness.Event {
	out := harness.Event{
		Type:       harness.EventType(ev.Type),
		Text:       ev.Text,
		Result:     ev.Result,
		SessionID:  ev.SessionID,
		ToolID:     ev.ToolID,
		ToolName:   ev.ToolName,
		ToolArgs:   ev.ToolArgs,
		ToolOutput: ev.ToolOutput,
		ToolError:  ev.ToolError,
		Reasoning:  ev.Reasoning,
	}
	if ev.Usage != nil {
		out.Usage = &harness.Usage{
			InputTokens:              ev.Usage.InputTokens,
			OutputTokens:             ev.Usage.OutputTokens,
			CacheReadInputTokens:     ev.Usage.CacheReadInputTokens,
			CacheCreationInputTokens: ev.Usage.CacheCreationInputTokens,
			TotalCostUSD:             ev.Usage.TotalCostUSD,
		}
	}
	return out
}

// stderrTail keeps the last stderrKeep bytes the CLI wrote to stderr.
type stderrTail struct {
	mu      sync.Mutex
	buf     []byte
	dropped bool // the start of buf is not the start of stderr
}

func (t *stderrTail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if over := len(t.buf) - stderrKeep; over > 0 {
		t.buf = append(t.buf[:0], t.buf[over:]...)
		t.dropped = true
	}
	return len(p), nil
}

// text returns the end of stderr for an error message: redacted first, so that a secret the
// cut would split is not shown in part, then at most stderrShow bytes from a line start.
// When the buffer dropped the start of stderr, its first line is a fragment, which can
// begin with the end of a secret that redaction does not recognize: the fragment is
// dropped, or, when the buffer is a single line, the end of a secret it starts with.
func (t *stderrTail) text(secrets []string) string {
	t.mu.Lock()
	s, dropped := string(t.buf), t.dropped
	t.mu.Unlock()
	if dropped {
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			s = s[i+1:]
		} else {
			s = withoutSecretTail(s, secrets)
		}
	}
	return lastLines(validate.Clean(redact(s, secrets)), stderrShow)
}

// withoutSecretTail replaces the end of a secret that s starts with by [secret].
func withoutSecretTail(s string, secrets []string) string {
	cut := 0
	for _, secret := range secrets {
		secret = strings.TrimSpace(secret)
		for n := min(len(secret)-1, len(s)); n > cut; n-- {
			if strings.HasPrefix(s, secret[len(secret)-n:]) {
				cut = n
				break
			}
		}
	}
	if cut == 0 {
		return s
	}
	return validate.Redacted + s[cut:]
}

// lastLines returns the end of s, at most n bytes, starting at a line.
func lastLines(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[len(s)-n:]
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	return strings.TrimSpace(s)
}

// lineStream splits a byte stream into lines of at most maxStreamLine bytes.
//
// Copied from github.com/rumpl/harness (run.go), MIT License, Copyright (c) 2025 Djordje
// Lukic: Permission is hereby granted, free of charge, to any person obtaining a copy of this
// software and associated documentation files (the "Software"), to deal in the Software
// without restriction, including without limitation the rights to use, copy, modify, merge,
// publish, distribute, sublicense, and/or sell copies of the Software, and to permit persons
// to whom the Software is furnished to do so, subject to the following conditions: The above
// copyright notice and this permission notice shall be included in all copies or substantial
// portions of the Software. THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND,
// EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS
// FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT
// HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF
// CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE
// USE OR OTHER DEALINGS IN THE SOFTWARE.
type lineStream struct {
	pending []byte
	handle  func(string) error
}

func (s *lineStream) write(data []byte) error {
	for len(data) > 0 {
		line, rest, found := bytes.Cut(data, []byte{'\n'})
		if len(line) > maxStreamLine-len(s.pending) {
			return fmt.Errorf("stream line exceeds %d bytes", maxStreamLine)
		}
		s.pending = append(s.pending, line...)
		if !found {
			break
		}
		if err := s.handle(string(bytes.TrimSuffix(s.pending, []byte{'\r'}))); err != nil {
			return err
		}
		s.pending = s.pending[:0]
		data = rest
	}
	return nil
}

func (s *lineStream) finish() error {
	if len(s.pending) == 0 {
		return nil
	}
	return s.handle(string(bytes.TrimSuffix(s.pending, []byte{'\r'})))
}
