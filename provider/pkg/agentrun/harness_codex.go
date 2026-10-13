// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package agentrun

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/docker/docker-agent/pkg/codingharness"
	"github.com/docker/docker-agent/pkg/config/latest"
	"github.com/docker/docker-agent/pkg/harness"
	"github.com/docker/docker-agent/pkg/modelsdev"
	"github.com/docker/docker-agent/pkg/team"
)

// Codex agents (`harness: {type: codex}`) hand their turn to the Codex CLI. docker-agent's
// driver (rumpl/harness/codex) passes the prompt as an argument, always bypasses Codex's
// sandbox, reads only three of Codex's item types, counts cached input twice and joins every
// message into the answer. AgentRun therefore builds the arguments itself and reads Codex's
// JSONL events (codex exec --json, Codex 0.162) with its own adapter:
//
//	codex exec --json --color=never --skip-git-repo-check <sandbox> [--model=<m>]
//	           [--output-schema=<file>] --output-last-message=<file> [resume <thread>] -
//
// with the prompt on stdin (`-`), and every flag before `resume`, whose own flags are fewer.
// <sandbox> follows toolApproval: deny runs model-generated commands in Codex's workspace-write
// sandbox without approvals (--sandbox=workspace-write --config=approval_policy="never"), allow
// turns the sandbox off (--dangerously-bypass-approvals-and-sandbox), as docker-agent does.
// The user's config.toml and AGENTS.md files apply.
//
// The CLI runs like the Claude Code CLI (harness.go, harness_unix.go): in the run's working
// directory, with the environment of codexEnv (codexenv.go), in a process group of its own,
// with the processes it starts recorded and stopped when it ends. Codex runs every shell
// command in a session of its own too. A stop sends SIGINT to the CLI first, which makes Codex
// interrupt its turn and end its commands and MCP servers itself; its group and the recorded
// processes get SIGTERM interruptGrace later (supervision.interruptFirst). The stdio MCP
// servers Codex starts in groups of their own are stopped without counting as left running
// (supervision.ownGroups).
//
// Before the first codex turn of a run, AgentRun runs `codex --version` (a version other than
// the tested one is reported), and under toolApproval deny `codex sandbox -- true`: when
// Codex's sandbox cannot start on the machine, every command would fail while the model still
// completes its turn, so the run fails before any model call instead.
//
// Of Codex's events, the runner forwards reasoning, the tool calls (command_execution as
// Bash, file_change as apply_patch, mcp_tool_call as mcp__<server>__<tool>, web_search, the
// collab tools) and, after the CLI exited, one result: the final message (from the
// --output-last-message file, else the last agent_message of the completed turn) with the
// turn's usage. Codex's usage is the thread's running total and includes cached input; the
// runner reports the turn's share with cached input apart. Agent messages before the final one
// are commentary and not forwarded.

// codexTestedVersion is the Codex release (major.minor) AgentRun was tested with.
const codexTestedVersion = "0.162"

var errCodexNotFound = errors.New("the codex harness needs the codex CLI, which was not found on PATH: install Codex " +
	"(npm i -g @openai/codex, or brew install codex) on the machine that runs pulumi up, or set PATH in env")

// Durations of the codex runner. Variables, so that tests can shorten them.
var (
	// interruptGrace is how long a CLI that stops gracefully on SIGINT (codex) has before its
	// group and the processes started under it get SIGTERM (harness_unix.go).
	interruptGrace = 2 * time.Second
	// codexProbeTimeout bounds each of the checks before the first codex turn.
	codexProbeTimeout = 30 * time.Second
)

// statusFailed is the status of a Codex item that failed.
const statusFailed = "failed"

// Limits of what the codex runner forwards.
const (
	maxToolOutput   = 64 << 10
	maxItemWarnings = 5
	maxItemWarning  = 300
)

// codexSupervision is the supervision of a codex CLI.
func codexSupervision(debug func(string)) supervision {
	return supervision{name: "codex", kind: "codex CLI", cookieVar: runCookieVar, interruptFirst: true, ownGroups: true,
		debug: debug}
}

// codexRun is what the codex CLIs of one run share.
type codexRun struct {
	env codexEnvironment
	// deny runs the CLI in Codex's workspace-write sandbox (toolApproval deny).
	deny  bool
	info  *harnessInfo
	debug func(string)

	mu   sync.Mutex
	auth codexAuth
	// schemas are the structured outputs of the codex agents, by their harness configuration
	// (the pointer the runtime passes to the factory).
	schemas map[*latest.HarnessConfig]*latest.StructuredOutput
	// checked: the checks before the first turn ran; checkErr is their outcome.
	checked  bool
	checkErr error
	version  string
	// totals are the usage totals Codex reported last, by thread.
	totals map[string]codexUsage
}

// newCodexRun prepares the codex agents of a run working in dir. It reads the login of the
// codex CLI's CODEX_HOME, whose token values are secrets of the run from now on.
func newCodexRun(req Request, dir string, approval ToolApproval, info *harnessInfo, debug func(string)) *codexRun {
	r := &codexRun{
		env: codexEnv(req, dir), deny: approval != ToolApprovalAllow, info: info, debug: debug,
		schemas: map[*latest.HarnessConfig]*latest.StructuredOutput{}, totals: map[string]codexUsage{},
	}
	r.auth = codexAuthOf(r.env, true)
	info.addSecrets(r.auth.secrets...)
	return r
}

// bind records the structured outputs of the team's codex agents.
func (r *codexRun) bind(t *team.Team) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, a := range teamAgents(t) {
		if a.HasHarness() && a.Harness().Type == codingharness.TypeCodex {
			r.schemas[a.Harness()] = a.StructuredOutput()
		}
	}
}

func (r *codexRun) schema(cfg *latest.HarnessConfig) *latest.StructuredOutput {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.schemas[cfg]
}

func (r *codexRun) logf(format string, args ...any) {
	if r.debug != nil {
		r.debug(fmt.Sprintf(format, args...))
	}
}

// refreshSecrets adds the tokens of the login that Codex wrote since they were read.
func (r *codexRun) refreshSecrets() {
	r.mu.Lock()
	added := r.auth.refreshSecrets()
	r.mu.Unlock()
	r.info.addSecrets(added...)
}

// sandboxName is the sandbox the CLI runs commands in, for the info line.
func (r *codexRun) sandboxName() string {
	if r.deny {
		return "workspace-write"
	}
	return "off"
}

// codexHarness runs one turn of a codex agent.
type codexHarness struct {
	run harnessRun
	cfg *latest.HarnessConfig
}

func newCodexHarness(h harnessRun, cfg *latest.HarnessConfig) (harness.Provider, error) {
	if h.codex == nil {
		return nil, errors.New("the codex agents of this run were not prepared")
	}
	return &codexHarness{run: h, cfg: cfg}, nil
}

func (c *codexHarness) Name() string { return codingharness.TypeCodex }

func (c *codexHarness) Run(ctx context.Context, prompt string, handle func(harness.Event)) error {
	return c.exec(ctx, "", prompt, handle)
}

func (c *codexHarness) Resume(ctx context.Context, sessionID, prompt string, handle func(harness.Event)) error {
	if sessionID == "" {
		return errors.New("resume: session ID is empty")
	}
	return c.exec(ctx, sessionID, prompt, handle)
}

// model is the model the CLI uses: harness.model (or the agent's model, which docker-agent
// passes when harness.model is empty), else the model of config.toml, else "".
func (c *codexHarness) model() string {
	if c.cfg.Model != "" {
		return c.cfg.Model
	}
	return c.run.codex.auth.cfg.model
}

func (c *codexHarness) exec(ctx context.Context, thread, prompt string, handle func(harness.Event)) error {
	r := c.run.codex
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := lookPathIn("codex", r.env.path()); err != nil {
		return err
	}
	if err := r.preflight(ctx, c.run); err != nil {
		return err
	}
	// The final message and the schema go to files of the run's own, not to the working
	// directory.
	tmp, err := os.MkdirTemp("", "pulumi-docker-agent-codex-")
	if err != nil {
		return fmt.Errorf("creating a directory for codex's answer: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	inv := codexInvocation{deny: r.deny, model: c.cfg.Model, lastMessage: filepath.Join(tmp, "last-message.txt"),
		resume: thread}
	if so := r.schema(c.cfg); so != nil && so.Strict && len(so.Schema) > 0 {
		b, err := json.Marshal(so.Schema)
		if err != nil {
			return fmt.Errorf("encoding the structured_output schema for codex: %w", err)
		}
		inv.schema = filepath.Join(tmp, "output-schema.json")
		if err := os.WriteFile(inv.schema, b, 0o600); err != nil {
			return fmt.Errorf("writing the structured_output schema for codex: %w", err)
		}
	}
	r.refreshSecrets()
	s := &codexStream{run: r, h: c.run, handle: handle, lastMessageFile: inv.lastMessage, model: c.model()}
	return runCLI(ctx, cliRun{
		name: "codex", args: codexArgs(inv), dir: c.run.dir, env: r.env.list(), path: r.env.path(), prompt: prompt,
		start:  func(cmd *exec.Cmd) (*harnessProcess, error) { return startCodex(cmd, c.run.debug) },
		read:   s.read,
		finish: s.finish,
	})
}

// codexInvocation is what codexArgs puts into the arguments of a turn.
type codexInvocation struct {
	deny                       bool
	model, schema, lastMessage string
	resume                     string // the thread to resume, "" for a new one
}

// codexArgs returns the arguments of `codex exec` (without the executable). Values are single
// --flag=value arguments, so that none can be taken for an option, and every flag comes before
// `resume`, which accepts fewer of them.
func codexArgs(inv codexInvocation) []string {
	args := []string{"exec", "--json", "--color=never", "--skip-git-repo-check"}
	if inv.deny {
		args = append(args, "--sandbox=workspace-write", `--config=approval_policy="never"`)
	} else {
		args = append(args, "--dangerously-bypass-approvals-and-sandbox")
	}
	if inv.model != "" {
		args = append(args, "--model="+inv.model)
	}
	if inv.schema != "" {
		args = append(args, "--output-schema="+inv.schema)
	}
	if inv.lastMessage != "" {
		args = append(args, "--output-last-message="+inv.lastMessage)
	}
	if inv.resume != "" {
		args = append(args, "resume", inv.resume)
	}
	return append(args, "-")
}

// preflight runs the checks before the first codex turn of a run, once: the version of the
// CLI, and under toolApproval deny whether Codex's sandbox can start.
func (r *codexRun) preflight(ctx context.Context, h harnessRun) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.checked {
		return r.checkErr
	}
	if r.auth.conflict != "" {
		return errors.New(r.auth.conflict)
	}
	res, err := r.probe(ctx, h, "--version")
	if ctx.Err() != nil {
		return ctx.Err()
	}
	r.version = parseCodexVersion(res.stdout)
	switch {
	case errors.Is(err, errShuttingDown):
		return err
	case err != nil || res.exit != 0 || r.version == "":
		r.logf("codex --version did not report a version (exit status %d, error %v): %s", res.exit, err,
			firstLine(res.stderr))
	}
	if r.deny {
		r.checkErr = r.probeSandbox(ctx, h)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(r.checkErr, errShuttingDown) {
			return r.checkErr
		}
	}
	r.checked = true
	return r.checkErr
}

// codexProbe is the outcome of a check's process.
type codexProbe struct {
	stdout, stderr string
	exit           int
}

// probe runs a codex command of a check with the CLI's environment, in the working
// directory, supervised like the CLI, for at most codexProbeTimeout.
func (r *codexRun) probe(ctx context.Context, h harnessRun, args ...string) (codexProbe, error) {
	return r.probeCommand(ctx, h, "codex", args...)
}

func (r *codexRun) probeCommand(ctx context.Context, h harnessRun, name string, args ...string) (codexProbe, error) {
	ctx, cancel := context.WithTimeout(ctx, codexProbeTimeout)
	defer cancel()
	var res codexProbe
	var out strings.Builder
	canceled := false
	collect := func(line string) error {
		if out.Len() < 4<<10 {
			out.WriteString(line + "\n")
		}
		return nil
	}
	err := runCLI(ctx, cliRun{
		name: name, args: args, dir: h.dir, env: r.env.list(), path: r.env.path(),
		start: func(cmd *exec.Cmd) (*harnessProcess, error) { return startCodex(cmd, h.debug) },
		read: func(ctx context.Context, stdout *os.File, proc *harnessProcess) error {
			return readStream(ctx, stdout, proc, collect, &canceled)
		},
		finish: func(ctx context.Context, proc *harnessProcess, streamErr error, tail *stderrTail) error {
			res.stderr = tail.text(slices.Concat(h.secrets, r.info.extraSecrets()))
			switch {
			case ctx.Err() != nil:
				return fmt.Errorf("%s %s did not finish within %s", name, strings.Join(args, " "), codexProbeTimeout)
			case streamErr != nil:
				return streamErr
			case proc.state == nil:
				return proc.waitErr
			}
			res.exit = proc.state.ExitCode()
			return nil
		},
	})
	res.stdout = out.String()
	return res, err
}

var codexVersionPattern = regexp.MustCompile(`\b(\d+)\.(\d+)(\.\d+)?(-[0-9A-Za-z.-]+)?\b`)

// parseCodexVersion returns the version `codex --version` prints ("codex-cli 0.162.1").
func parseCodexVersion(out string) string {
	return codexVersionPattern.FindString(firstLine(out))
}

// codexVersionWarning says that the installed codex is not the release AgentRun was tested
// with; "" when it is, or when the version is unknown.
func codexVersionWarning(version string) string {
	m := codexVersionPattern.FindStringSubmatch(version)
	if m == nil || m[1]+"."+m[2] == codexTestedVersion {
		return ""
	}
	return fmt.Sprintf("codex %s is installed, but AgentRun was tested with Codex %s, and Codex changes the arguments "+
		"and events of codex exec often: if codex agents misbehave, install Codex %s or a pulumi-docker-agent release "+
		"tested with yours", version, codexTestedVersion, codexTestedVersion)
}

// probeSandbox checks whether Codex's sandbox can start: `codex sandbox -- true` runs `true`
// in the sandbox Codex would run the commands of a turn in (Seatbelt on macOS, bubblewrap on
// Linux), as workspace-write. When the installed codex has no such command, Linux falls back
// to Codex's own probe of the system's bubblewrap.
func (r *codexRun) probeSandbox(ctx context.Context, h harnessRun) error {
	res, err := r.probe(ctx, h, `--config=sandbox_mode="workspace-write"`, "sandbox", "--", "true")
	switch {
	case ctx.Err() != nil:
		return ctx.Err()
	case errors.Is(err, errShuttingDown):
		return err
	case err != nil:
		return codexSandboxError(err.Error())
	case res.exit == 0:
		return nil
	case codexCLIDrift(res.stderr):
		r.logf("codex sandbox is not available in this codex (%s); checking bubblewrap instead", firstLine(res.stderr))
		return r.probeBwrap(ctx, h)
	}
	return codexSandboxError(cmp.Or(firstLine(res.stderr), fmt.Sprintf("codex sandbox exited with status %d", res.exit)))
}

// probeBwrap runs Codex's own probe of the system's bubblewrap on Linux: a user and network
// namespace with the root read-only. Without bwrap on PATH (Codex may bring its own), or on
// macOS, it checks nothing.
func (r *codexRun) probeBwrap(ctx context.Context, h harnessRun) error {
	if harnessOS != "linux" {
		return nil
	}
	if _, err := lookPathIn("bwrap", r.env.path()); err != nil {
		r.logf("bubblewrap is not on PATH: codex's sandbox was not checked")
		return nil
	}
	res, err := r.probeCommand(ctx, h, "bwrap", "--unshare-user", "--unshare-net", "--ro-bind", "/", "/", "true")
	switch {
	case ctx.Err() != nil:
		return ctx.Err()
	case errors.Is(err, errShuttingDown):
		return err
	case err != nil:
		return codexSandboxError(err.Error())
	case res.exit != 0:
		return codexSandboxError(cmp.Or(firstLine(res.stderr), fmt.Sprintf("bwrap exited with status %d", res.exit)))
	}
	return nil
}

// codexSandboxAdvice says what to do when Codex's sandbox cannot start.
const codexSandboxAdvice = "enable unprivileged user namespaces or install bubblewrap (Linux), do not run pulumi up " +
	"inside another sandbox such as a Claude Code or Codex session (macOS), or set toolApproval: allow, but only on an " +
	"externally isolated machine or in a container"

// codexSandboxError is the error of a run whose codex sandbox cannot start.
func codexSandboxError(detail string) error {
	return fmt.Errorf("codex's sandbox cannot start on this machine (%s): with toolApproval deny codex runs the model's "+
		"commands in its workspace-write sandbox, so none of them could run; %s", truncate(detail, maxReason), codexSandboxAdvice)
}

// firstLine returns the first line of s that is not empty, trimmed.
func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			return l
		}
	}
	return ""
}

// codexUsage is the usage of a turn.completed event: the thread's running total.
type codexUsage struct {
	InputTokens           int64 `json:"input_tokens"`
	CachedInputTokens     int64 `json:"cached_input_tokens"`
	CacheWriteInputTokens int64 `json:"cache_write_input_tokens"`
	OutputTokens          int64 `json:"output_tokens"`
	ReasoningOutputTokens int64 `json:"reasoning_output_tokens"`
}

func (u codexUsage) zero() bool { return u == codexUsage{} }

// turnUsage converts a thread's usage total to the usage of the turn: the difference to the
// last total of the thread, with cached input apart from fresh input (Codex's input_tokens
// includes it). A turn without usage updates reports zeros, which does not lower the total.
func (r *codexRun) turnUsage(thread string, total *codexUsage) *harness.Usage {
	if total == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if total.zero() {
		return &harness.Usage{}
	}
	prev := r.totals[thread]
	r.totals[thread] = *total
	d := codexUsage{
		InputTokens:           max(total.InputTokens-prev.InputTokens, 0),
		CachedInputTokens:     max(total.CachedInputTokens-prev.CachedInputTokens, 0),
		CacheWriteInputTokens: max(total.CacheWriteInputTokens-prev.CacheWriteInputTokens, 0),
		OutputTokens:          max(total.OutputTokens-prev.OutputTokens, 0),
	}
	return &harness.Usage{
		InputTokens:              int(max(d.InputTokens-d.CachedInputTokens-d.CacheWriteInputTokens, 0)),
		CacheReadInputTokens:     int(d.CachedInputTokens),
		CacheCreationInputTokens: int(d.CacheWriteInputTokens),
		OutputTokens:             int(d.OutputTokens),
	}
}

// codexCost estimates what a turn's usage would cost at the API prices of the embedded
// models.dev catalog (openai/<model>, base rates, adjusted for config.toml's service_tier).
// Codex reports no cost, and a ChatGPT plan bills none per token. known is false when the
// model is not in the catalog.
func codexCost(model, tier string, u *harness.Usage) (cost float64, known bool) {
	if u == nil {
		return 0, true
	}
	m := strings.TrimPrefix(model, "openai/")
	if m == "" {
		return 0, false
	}
	mdl, ok := modelsdev.EmbeddedSnapshot().Providers["openai"].Models[m]
	if !ok || mdl.Cost == nil {
		return 0, false
	}
	c := mdl.Cost
	if tier != "" {
		c = c.ForServiceTier(modelsdev.NewID("openai", m), tier)
	}
	cacheRead, cacheWrite := cmp.Or(c.CacheRead, c.Input), cmp.Or(c.CacheWrite, c.Input)
	return (float64(u.InputTokens)*c.Input + float64(u.CacheReadInputTokens)*cacheRead +
		float64(u.CacheCreationInputTokens)*cacheWrite + float64(u.OutputTokens)*c.Output) / 1e6, true
}

// codexStream turns the CLI's JSONL events into docker-agent harness events.
type codexStream struct {
	run    *codexRun
	h      harnessRun
	handle func(harness.Event)
	// lastMessageFile is where Codex writes its final message (--output-last-message).
	lastMessageFile string
	// model is the model of the turn, for the cost estimate ("" when unknown).
	model string

	// canceled stops the events: after a cancellation the output is read and discarded.
	canceled bool
	threadID string
	// completed and failed: the last turn ended with turn.completed or turn.failed.
	completed, failed bool
	usage             *codexUsage
	lastMessage       string
	// errorText is the message of turn.failed; lastError that of the last error event.
	errorText, lastError string
	// started are the tool calls that were reported, by item ID.
	started map[string]bool
	// warnings are the messages of Codex's error items (non-fatal).
	warnings                           []string
	sandboxFailures, declined, spawned int
	notJSON                            int
}

// read reads the CLI's output (see readStream).
func (s *codexStream) read(ctx context.Context, stdout *os.File, proc *harnessProcess) error {
	return readStream(ctx, stdout, proc, s.line, &s.canceled)
}

// codexEvent is a line of `codex exec --json`.
type codexEvent struct {
	Type     string          `json:"type"`
	ThreadID string          `json:"thread_id"`
	Usage    *codexUsage     `json:"usage"`
	Error    json.RawMessage `json:"error"`
	Message  string          `json:"message"`
	Item     json.RawMessage `json:"item"`
}

// codexItem is the item of an item.started, item.updated or item.completed event, with the
// fields of every item type.
type codexItem struct {
	// ID is the item's ID (item_N). A web_search item has a second id, the search's own,
	// which encoding/json keeps as the last one: started and completed have the same.
	ID     string `json:"id"`
	Type   string `json:"type"`
	Status string `json:"status"`
	// agent_message, reasoning
	Text string `json:"text"`
	// command_execution
	Command          string `json:"command"`
	AggregatedOutput string `json:"aggregated_output"`
	ExitCode         *int   `json:"exit_code"`
	// file_change
	Changes []struct {
		Path string `json:"path"`
		Kind string `json:"kind"`
	} `json:"changes"`
	// mcp_tool_call (Tool also of collab_tool_call)
	Server    string          `json:"server"`
	Tool      string          `json:"tool"`
	Arguments json.RawMessage `json:"arguments"`
	Result    *struct {
		Content           []json.RawMessage `json:"content"`
		StructuredContent json.RawMessage   `json:"structured_content"`
	} `json:"result"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
	// web_search
	Query   string          `json:"query"`
	Action  json.RawMessage `json:"action"`
	Results []any           `json:"results"`
	// collab_tool_call
	Prompt            *string  `json:"prompt"`
	ReceiverThreadIDs []string `json:"receiver_thread_ids"`
	// error
	Message string `json:"message"`
}

// line handles one line of the CLI's output. A line that is not a JSON event is skipped (a
// notify program can write to stdout); turn.completed and turn.failed decide the outcome.
func (s *codexStream) line(line string) error {
	if s.canceled {
		return nil
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}
	var ev codexEvent
	if json.Unmarshal([]byte(line), &ev) != nil || ev.Type == "" {
		s.notJSON++
		if s.notJSON == 1 {
			// Never the line itself: it can hold anything.
			s.run.logf("codex wrote a line to stdout that is not a JSON event (%d bytes); such lines are skipped", len(line))
		}
		return nil
	}
	switch ev.Type {
	case "thread.started":
		if ev.ThreadID != "" {
			s.threadID = ev.ThreadID
			s.handle(harness.Event{Type: harness.EventSessionID, SessionID: ev.ThreadID})
		}
	case "turn.started":
		s.completed, s.failed, s.lastMessage = false, false, ""
	case "turn.completed":
		s.completed, s.usage = true, ev.Usage
	case "turn.failed":
		s.failed = true
		var e struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(ev.Error, &e) == nil {
			s.errorText = e.Message
		}
	case "error":
		// Also a transient one ("Reconnecting... 1/5"): only the outcome decides.
		s.lastError = ev.Message
		s.scanSandbox(ev.Message)
	case "item.started", "item.updated", "item.completed":
		var it codexItem
		if len(ev.Item) > 0 && json.Unmarshal(ev.Item, &it) == nil {
			s.item(ev.Type == "item.completed", ev.Type == "item.started", it)
		}
	}
	return nil
}

// item handles a thread item.
func (s *codexStream) item(completed, started bool, it codexItem) {
	switch it.Type {
	case "agent_message":
		if completed {
			s.lastMessage = it.Text
		}
	case "reasoning":
		if completed && strings.TrimSpace(it.Text) != "" {
			s.handle(harness.Event{Type: harness.EventReasoning, Reasoning: it.Text})
		}
	case "command_execution":
		args := jsonArgs(map[string]any{"command": cutString(it.Command, maxToolArguments)})
		if started || completed {
			s.call(it.ID, "Bash", args)
		}
		if completed {
			out := it.AggregatedOutput
			if it.Status == "declined" {
				s.declined++
				if strings.TrimSpace(out) == "" {
					out = "codex declined to run the command: it needs an approval that codex exec does not ask for"
				}
			}
			s.scanSandbox(out)
			s.toolResult(it.ID, "Bash", out,
				it.Status == statusFailed || it.Status == "declined" || it.ExitCode != nil && *it.ExitCode != 0)
		}
	case "file_change":
		if completed {
			s.fileChange(it)
		}
	case "mcp_tool_call":
		name := "mcp__" + it.Server + "__" + it.Tool
		if started || completed {
			s.call(it.ID, name, compactJSON(it.Arguments, "{}"))
		}
		if completed {
			out, failed := mcpOutput(it)
			s.toolResult(it.ID, name, out, failed || it.Status == statusFailed)
		}
	case "web_search":
		if completed {
			s.webSearch(it)
		}
	case "collab_tool_call":
		if it.Tool == "" {
			return
		}
		args := map[string]any{}
		if it.Prompt != nil {
			args["prompt"] = cutString(*it.Prompt, maxToolArguments)
		}
		if len(it.ReceiverThreadIDs) > 0 {
			args["receiver_thread_ids"] = it.ReceiverThreadIDs
		}
		if started || completed {
			s.call(it.ID, it.Tool, jsonArgs(args))
		}
		if completed {
			if it.Tool == "spawn_agent" && it.Status != statusFailed {
				s.spawned++
			}
			s.toolResult(it.ID, it.Tool, it.Status, it.Status == statusFailed)
		}
	case "error":
		if completed {
			s.itemError(it.Message)
		}
	}
}

// call reports a tool call once.
func (s *codexStream) call(id, name, args string) {
	if s.started == nil {
		s.started = map[string]bool{}
	}
	if s.started[id] {
		return
	}
	s.started[id] = true
	// A tool's output can show a token Codex wrote meanwhile (`cat auth.json`).
	s.run.refreshSecrets()
	s.handle(harness.Event{Type: harness.EventToolCall, ToolID: id, ToolName: name, ToolArgs: args})
}

func (s *codexStream) toolResult(id, name, output string, failed bool) {
	s.run.refreshSecrets()
	s.handle(harness.Event{Type: harness.EventToolResult, ToolID: id, ToolName: name,
		ToolOutput: cutString(output, maxToolOutput), ToolError: failed})
}

// fileChange reports a patch Codex applied as a call of apply_patch, with the paths relative to
// the working directory when they are in it.
func (s *codexStream) fileChange(it codexItem) {
	changes := make([]map[string]string, 0, len(it.Changes))
	var done []string
	for _, c := range it.Changes {
		p := s.relPath(c.Path)
		changes = append(changes, map[string]string{"path": p, "kind": c.Kind})
		verb := map[string]string{"add": "added", "delete": "deleted", "update": "updated"}[c.Kind]
		done = append(done, cmp.Or(verb, c.Kind)+" "+p)
	}
	s.call(it.ID, "apply_patch", jsonArgs(map[string]any{"changes": changes}))
	out := strings.Join(done, ", ")
	if it.Status == statusFailed {
		out = strings.TrimSpace("the patch failed: " + out)
	}
	s.toolResult(it.ID, "apply_patch", out, it.Status == statusFailed)
}

// relPath returns a path of a file change relative to the working directory, when it is in it.
func (s *codexStream) relPath(p string) string {
	dir := s.h.dir
	if dir == "" || !filepath.IsAbs(p) {
		return p
	}
	for _, d := range []string{dir, evalSymlinks(dir)} {
		if rel, err := filepath.Rel(d, p); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return rel
		}
	}
	return p
}

func evalSymlinks(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

// webSearch reports a web search: its query and action as the arguments, what it did as the
// output.
func (s *codexStream) webSearch(it codexItem) {
	var action struct {
		Type    string   `json:"type"`
		Query   string   `json:"query"`
		Queries []string `json:"queries"`
		URL     string   `json:"url"`
		Pattern string   `json:"pattern"`
	}
	_ = json.Unmarshal(it.Action, &action)
	args := map[string]any{"query": it.Query}
	if len(it.Action) > 0 && string(it.Action) != "null" {
		args["action"] = json.RawMessage(compactJSON(it.Action, "null"))
	}
	var out string
	switch action.Type {
	case "open_page":
		out = "opened " + action.URL
	case "find_in_page":
		out = fmt.Sprintf("searched %s for %q", action.URL, action.Pattern)
	default:
		out = "searched for " + cmp.Or(action.Query, it.Query, strings.Join(action.Queries, ", "))
	}
	if len(it.Results) > 0 {
		out += fmt.Sprintf(" (%d results)", len(it.Results))
	}
	s.call(it.ID, "web_search", jsonArgs(args))
	s.toolResult(it.ID, "web_search", strings.TrimSpace(out), false)
}

// mcpOutput is the output of an MCP tool call: its error, its text content, or its structured
// content.
func mcpOutput(it codexItem) (string, bool) {
	if it.Error != nil && strings.TrimSpace(it.Error.Message) != "" {
		return it.Error.Message, true
	}
	if it.Result == nil {
		return "", false
	}
	var texts []string
	for _, c := range it.Result.Content {
		var block struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(c, &block) == nil && block.Type == "text" {
			texts = append(texts, block.Text)
		}
	}
	if len(texts) > 0 {
		return strings.Join(texts, "\n"), false
	}
	return compactJSON(it.Result.StructuredContent, ""), false
}

// codexSandboxSignatures are what bubblewrap, Seatbelt and Codex write when the sandbox of a
// command cannot start.
var codexSandboxSignatures = []string{
	"No permissions to create a new namespace", "No permissions to create new namespace",
	"setting up uid map: Permission denied", "loopback: Failed RTM_NEWADDR", "loopback: Failed RTM_NEWLINK",
	"Creating new namespace failed", "sandbox-exec: sandbox_apply", "codex-linux-sandbox was required",
	"Landlock was not able", "seccomp setup error",
}

// scanSandbox counts a command output or error that shows that the sandbox could not start.
func (s *codexStream) scanSandbox(text string) {
	if containsAny(text, codexSandboxSignatures...) || strings.Contains(text, "sandbox error:") {
		s.sandboxFailures++
	}
}

// itemError keeps a non-fatal error of Codex (a config warning, a deprecation notice, a model
// reroute), at most maxItemWarnings different ones.
func (s *codexStream) itemError(msg string) {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return
	}
	s.scanSandbox(msg)
	if strings.Contains(msg, "event stream lagged") {
		s.run.logf("codex dropped events of its stream (%s): the answer comes from --output-last-message", msg)
	}
	msg = truncate(msg, maxItemWarning)
	if len(s.warnings) < maxItemWarnings && !slices.Contains(s.warnings, msg) {
		s.warnings = append(s.warnings, msg)
	}
}

// finish maps the end of the CLI to the turn's outcome: an error, or the result event with the
// final message and the turn's usage.
func (s *codexStream) finish(ctx context.Context, proc *harnessProcess, streamErr error, tail *stderrTail) error {
	r := s.run
	// Codex may have rotated the login's tokens during the turn.
	r.refreshSecrets()
	stderr := tail.text(slices.Concat(s.h.secrets, r.info.extraSecrets()))
	usage := r.turnUsage(s.threadID, s.usage)
	if !s.completed || s.failed {
		usage = nil
	}
	s.report(proc, usage)
	if err := s.result(ctx, proc, streamErr, stderr); err != nil {
		return err
	}
	answer := s.lastMessage
	// The file has the final message also when the stream dropped events.
	if b, err := os.ReadFile(s.lastMessageFile); err == nil && strings.TrimSpace(string(b)) != "" {
		answer = string(b)
	}
	_ = os.Remove(s.lastMessageFile)
	if strings.TrimSpace(answer) == "" {
		return errors.New("codex completed its turn without a final message")
	}
	s.handle(harness.Event{Type: harness.EventResult, Result: answer, Usage: usage})
	return nil
}

// report records what the CLI did for the run's info line and warnings.
func (s *codexStream) report(proc *harnessProcess, usage *harness.Usage) {
	r := s.run
	cost, known := codexCost(s.model, r.auth.cfg.serviceTier, usage)
	tokens := usage != nil && usage.InputTokens+usage.OutputTokens+usage.CacheReadInputTokens > 0
	r.info.updateCodex(func(c *codexReport) {
		c.seen = true
		c.version, c.model, c.sandbox, c.source, c.plan = r.version, s.model, r.sandboxName(), r.auth.source, r.auth.plan
		c.home, c.billed = r.auth.home.dir, r.auth.billed
		if s.threadID != "" {
			c.thread = s.threadID
		}
		c.spawned += s.spawned
		c.sandboxFailures += s.sandboxFailures
		c.declined += s.declined
		c.leftRunning += proc.leftRunning
		c.cost += cost
		c.costUnknown = c.costUnknown || !known && tokens
		for _, w := range s.warnings {
			if len(c.warnings) < maxItemWarnings && !slices.Contains(c.warnings, w) {
				c.warnings = append(c.warnings, w)
			}
		}
	})
}

// result maps the end of a CLI run to the error docker-agent reports (as "harness codex
// failed: ..."), with a hint for the usual causes.
func (s *codexStream) result(ctx context.Context, proc *harnessProcess, streamErr error, stderr string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if streamErr != nil {
		return fmt.Errorf("reading the output of codex: %w", streamErr)
	}
	state := proc.state
	if state == nil {
		return fmt.Errorf("waiting for codex: %w", proc.waitErr)
	}
	if (!state.Success() || !s.completed) && proc.stoppedForShutdown() {
		return errors.New("codex was stopped because Pulumi is stopping the provider (SIGINT)")
	}
	hint := codexHint(s.errorText+"\n"+s.lastError, stderr)
	detail := firstNonEmpty(s.lastError, stderr)
	switch {
	case s.failed:
		return errors.New("codex reported an error: " + firstNonEmpty(s.errorText, s.lastError, "turn failed") + hint)
	case !state.Success():
		msg := "codex exited with status " + fmt.Sprint(state.ExitCode())
		if state.ExitCode() < 0 {
			msg = "codex was stopped (" + state.String() + ")"
		}
		if s.completed {
			msg += " after completing its turn"
		}
		if detail != "" {
			msg += ": " + detail
		}
		return errors.New(msg + hint)
	case !s.completed:
		msg := "codex ended without completing its turn"
		if detail != "" {
			msg += ": " + detail
		}
		return errors.New(msg + hint)
	}
	return nil
}

// Hints for the usual causes of a failing codex CLI.
const (
	codexAuthHint = "Codex could not authenticate: log in with codex login as the user that runs pulumi up (codex login " +
		"--device-auth on a machine without a browser), preferably into a CODEX_HOME of AgentRun's own, or pass " +
		"CODEX_API_KEY in env (bills API usage)"
	codexContentionHint = "another Codex process (for example your interactive session) refreshed the login at the " +
		"same time, or auth.json was copied from another CODEX_HOME: retry, and give AgentRun a CODEX_HOME of its own with " +
		"its own codex login (never a copy of auth.json)"
	codexUsageLimitHint = "the Codex usage limit of the ChatGPT plan is reached: wait until it resets (see the message), " +
		"buy credits, or pass CODEX_API_KEY in env (bills API usage)"
	codexPlanHint  = "the ChatGPT plan of this login does not include Codex"
	codexQuotaHint = "the OpenAI account of the API key (CODEX_API_KEY or an API-key login) has no quota left"
	codexModelHint = "set harness.model to a model your Codex login offers, or leave harness.model and the agent's " +
		"model empty for Codex's default"
	codexForcedLoginHint = "codex logged out because forced_login_method of its config.toml does not allow the " +
		"credential it got: log in again with the required method, and pass no credential of the other kind in env"
	codexMCPHint = "a required MCP server of codex's config.toml did not start: with inheritEnvironment: false it gets " +
		"only the variables of the base environment and env; fix it, or make it optional"
	codexDriftHint = "the installed codex CLI does not accept AgentRun's arguments: AgentRun was tested with Codex " +
		codexTestedVersion + "; install that release, or a pulumi-docker-agent release tested with yours"
	codexHomeHint = "set CODEX_HOME to an existing directory, or remove it to use ~/.codex"
)

// codexCLIDrift reports stderr of a codex that does not know an argument or a command.
func codexCLIDrift(stderr string) bool {
	return containsAny(stderr, "unexpected argument", "unrecognized subcommand", "unrecognized option",
		"unknown option", "Found argument")
}

// codexHint returns at most one hint, to append to an error, from the turn's error messages
// (text) and stderr.
func codexHint(text, stderr string) string {
	all := text + "\n" + stderr
	lower := strings.ToLower(all)
	hint := ""
	switch {
	case strings.Contains(lower, "refresh token was already used"):
		hint = codexContentionHint
	case strings.Contains(all, "login is required, but") && strings.Contains(all, "Logging out"):
		hint = codexForcedLoginHint
	case strings.Contains(lower, "hit your usage limit"):
		hint = codexUsageLimitHint
	case strings.Contains(all, "To use Codex with your ChatGPT plan"):
		hint = codexPlanHint
	case strings.Contains(all, "Quota exceeded"):
		hint = codexQuotaHint
	case containsAny(lower, "401 unauthorized", "status 401", "403 forbidden", "status 403", "refresh token",
		"sign in again", "not logged in", "please log in", "run codex login"):
		hint = codexAuthHint
	case strings.Contains(lower, "model") && containsAny(lower, "model_not_found", "does not exist", "not supported",
		"unknown model", "invalid model"):
		hint = codexModelHint
	case strings.Contains(lower, "mcp") && containsAny(lower, "required", "failed to start", "initialize"):
		hint = codexMCPHint
	case strings.Contains(all, "CODEX_HOME points to"):
		hint = codexHomeHint
	case codexCLIDrift(stderr):
		hint = codexDriftHint
	case containsAny(all, codexSandboxSignatures...) || strings.Contains(all, "sandbox error:"):
		hint = "codex's sandbox could not start: " + codexSandboxAdvice
	}
	if hint == "" {
		return ""
	}
	return "; " + hint
}

// jsonArgs encodes tool arguments as compact JSON with sorted keys.
func jsonArgs(v map[string]any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// compactJSON returns raw JSON compacted, or empty when it is empty or null.
func compactJSON(raw json.RawMessage, empty string) string {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" {
		return empty
	}
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return empty
	}
	b, err := json.Marshal(v)
	if err != nil {
		return empty
	}
	return cutString(string(b), maxToolArguments)
}

// cutString shortens s to at most n bytes, at a rune boundary.
func cutString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + "..."
}

// codexReport is what the codex CLIs of a run reported, for its info line and warnings.
type codexReport struct {
	seen                                                bool
	version, model, sandbox, source, plan, home, billed string
	thread                                              string
	spawned, sandboxFailures, declined, leftRunning     int
	warnings                                            []string
	cost                                                float64
	costUnknown                                         bool
}

func (h *harnessInfo) updateCodex(f func(*codexReport)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	f(&h.codex)
}

func (h *harnessInfo) codexSummary() codexReport {
	h.mu.Lock()
	defer h.mu.Unlock()
	c := h.codex
	c.warnings = slices.Clone(c.warnings)
	return c
}

// codexCost is the cost estimate of the run's codex turns.
func (h *harnessInfo) codexCost() float64 {
	if h == nil {
		return 0
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.codex.cost
}

// addSecrets adds values found during the run to the run's redaction set.
func (h *harnessInfo) addSecrets(values ...string) {
	if h == nil || len(values) == 0 {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, v := range values {
		if !slices.Contains(h.secrets, v) {
			h.secrets = append(h.secrets, v)
		}
	}
}

// extraSecrets returns the values found during the run that are secret.
func (h *harnessInfo) extraSecrets() []string {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.secrets)
}

// reportCodex reports what the codex CLIs of a run did, as a progress record, and returns the
// warnings: what they billed when it is not a ChatGPT plan, a codex release other than the
// tested one, commands whose sandbox could not start or that Codex declined, Codex's own
// warnings, and the processes they left running.
func reportCodex(info *harnessInfo, prog *progress) []string {
	r := info.codexSummary()
	var warnings []string
	if r.leftRunning > 0 {
		warnings = append(warnings, leftRunningWarningOf(codingharness.TypeCodex, r.leftRunning))
	}
	if !r.seen {
		return warnings
	}
	if prog.at(LoggingStatus) {
		model := "its default model"
		if r.model != "" {
			model = prog.name(r.model, "a model")
		}
		line := fmt.Sprintf("codex %s (%s), sandbox %s, auth %s, CODEX_HOME %s", cmp.Or(r.version, "of unknown version"),
			model, r.sandbox, r.source, cmp.Or(r.home, "unknown"))
		if r.thread != "" {
			line += ", thread " + r.thread
		}
		if r.spawned > 0 {
			line += fmt.Sprintf(", %d spawned agent(s) whose tokens and tool calls are not counted", r.spawned)
		}
		switch {
		case r.costUnknown:
			line += ", cost unknown (Codex reports none, and the model is not in the catalog)"
		case r.cost > 0 && r.plan != "":
			line += ", cost an API-price estimate (a ChatGPT plan bills no per-token cost)"
		case r.cost > 0:
			line += ", cost an API-price estimate"
		}
		prog.record("%s", line)
	}
	if r.billed != "" {
		warnings = append(warnings, r.billed)
	}
	if w := codexVersionWarning(r.version); w != "" {
		warnings = append(warnings, w)
	}
	if r.sandboxFailures > 0 {
		warnings = append(warnings, fmt.Sprintf("%d codex command(s) or error(s) show that codex's sandbox could not "+
			"start: %s", r.sandboxFailures, codexSandboxAdvice))
	}
	if r.declined > 0 {
		warnings = append(warnings, fmt.Sprintf("codex declined %d command(s) that needed an approval: codex exec does not "+
			"ask for approvals, and toolApproval deny runs it with approval_policy never", r.declined))
	}
	if !prog.hideNames {
		for _, w := range r.warnings {
			warnings = append(warnings, "codex: "+w)
		}
	}
	return warnings
}
