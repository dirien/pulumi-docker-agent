// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

// Package agentrun runs a docker-agent configuration headless, with the docker-agent Go SDK
// (pkg/teamloader, pkg/runtime and pkg/session), the same code path `docker agent run --exec`
// uses, in a child process of the provider (a run child, see child.go), but without the CLI's
// user-level state:
//
//   - every run gets a private, empty docker-agent data, config and cache directory
//     (paths.SetRoot), so ~/.cagent, ~/.config/cagent (user settings, aliases, global
//     providers, the .env file, hook drop-ins, the credential helper) and ~/.cache/cagent
//     are never read or written. The directory is removed when the run ends. The one
//     exception is the ChatGPT login of docker agent setup (chatgpt-auth.json), which a
//     configuration that names a chatgpt model uses: it is copied in, and a login the run
//     refreshed is written back (chatgpt.go);
//   - credentials and ${env.NAME} references resolve from Request.Env and, when
//     Request.InheritEnvironment is set, the provider process environment only (and
//     CHATGPT_OAUTH_TOKEN from that ChatGPT login). DOCKER_AGENT_DEFAULT_MODEL selects model
//     auto's model, as with docker agent run; a Docker models gateway is never used. The model
//     SDKs, which read the process environment themselves, see the same: the run child's
//     environment is Request.Env over the provider's environment, or over a base environment
//     without credentials (see childenv.go). The processes the configuration starts do not
//     get Request.Env, and without Request.InheritEnvironment they get a scrubbed
//     environment (see procenv.go);
//   - sessions are kept in memory, telemetry is off and the models.dev catalog is the
//     snapshot embedded in the docker-agent library (no network lookup);
//   - tool calls are approved by docker-agent's own rules for unattended runs
//     (see ToolApproval), without hook approvals unless allowed (see policy.go);
//   - errors, warnings and logs are redacted (see secrets.go);
//   - coding-harness agents run only with Request.AllowHarnessAgents, and only claude-code
//     and codex: AgentRun runs the Claude Code or Codex CLI itself, in the run's working
//     directory and environment (see harness.go and harness_codex.go).
//
// Runs of one provider process are serialized.
package agentrun

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/docker/docker-agent/pkg/agent"
	"github.com/docker/docker-agent/pkg/bootstrap"
	"github.com/docker/docker-agent/pkg/chat"
	"github.com/docker/docker-agent/pkg/chatgpt"
	"github.com/docker/docker-agent/pkg/codingharness"
	"github.com/docker/docker-agent/pkg/config"
	"github.com/docker/docker-agent/pkg/config/latest"
	"github.com/docker/docker-agent/pkg/config/sources"
	"github.com/docker/docker-agent/pkg/environment"
	"github.com/docker/docker-agent/pkg/harness"
	"github.com/docker/docker-agent/pkg/js"
	"github.com/docker/docker-agent/pkg/model/provider/providers"
	"github.com/docker/docker-agent/pkg/modelsdev"
	"github.com/docker/docker-agent/pkg/paths"
	"github.com/docker/docker-agent/pkg/runtime"
	"github.com/docker/docker-agent/pkg/session"
	"github.com/docker/docker-agent/pkg/team"
	"github.com/docker/docker-agent/pkg/teamloader"
	"github.com/docker/docker-agent/pkg/tools"
	"github.com/docker/docker-agent/pkg/tools/builtin/deferred"
	"github.com/docker/docker-agent/pkg/tools/builtin/structuredoutput"
	"github.com/docker/docker-agent/pkg/tools/codemode"
	"github.com/docker/docker-agent/pkg/tools/mcp"
	"github.com/docker/docker-agent/pkg/tools/toon"
)

// ToolApproval decides what happens to tool calls that docker-agent would ask the user
// to confirm.
type ToolApproval string

const (
	// ToolApprovalDeny runs only the tool calls docker-agent approves without asking:
	// tools annotated read-only, calls an `allow` rule of the configuration's
	// `permissions` matches, calls the configuration's own `safety` mode (strict,
	// balanced or restricted) approves, and runtime-managed calls (transfer_task,
	// handoff, structured output). Every other call is rejected and the model is told
	// the tool needs a confirmation an unattended run cannot give. A `safety:
	// autonomous` in the configuration is ignored, and so are approvals from hooks
	// unless Request.AllowHookApprovals is set.
	ToolApprovalDeny ToolApproval = "deny"
	// ToolApprovalAllow approves every tool call, like `docker agent run --yolo`. `deny`
	// rules of the configuration's `permissions` still apply.
	ToolApprovalAllow ToolApproval = "allow"
)

// DefaultTimeout bounds a run when Request.Timeout is zero.
const DefaultTimeout = 300 * time.Second

// Request describes one run.
type Request struct {
	// Name labels the configuration in docker-agent messages (for example the Pulumi
	// resource name).
	Name string
	// YAML is the docker-agent configuration.
	YAML []byte
	// Agent is the agent to run. Empty means the default agent: `root`, or else the
	// first agent.
	Agent string
	// Prompt is the user message.
	Prompt string
	// Env holds environment variables for model credentials and ${env.NAME}
	// references. It takes precedence over the inherited environment. The run child sets
	// them in its process environment, where the model SDKs read them (see childenv.go).
	Env map[string]string
	// InheritEnvironment also resolves variables from the process environment, and gives
	// the run child the provider's whole environment instead of a base environment.
	InheritEnvironment bool
	// WorkingDir is the agent's working directory. Empty means a fresh temporary
	// directory that is removed after the run.
	WorkingDir string
	// ToolApproval decides what happens to tool calls that need approval. Empty means
	// ToolApprovalDeny.
	ToolApproval ToolApproval
	// AllowHookApprovals lets the configuration's hooks (pre_tool_use and
	// permission_request) approve tool calls under ToolApprovalDeny, as they do in
	// docker-agent. By default AgentRun ignores their approvals (see policy.go).
	AllowHookApprovals bool
	// Timeout bounds the run (not the time spent waiting for another run to finish).
	// Zero means DefaultTimeout.
	Timeout time.Duration
	// Secrets are values that must never appear in errors, warnings or logs, in addition
	// to the values of Env and the credentials of the provider's environment (see
	// secrets.go).
	Secrets []string
	// Log receives docker-agent's own WARN and ERROR log records of this run, redacted
	// (lower levels are dropped), and the runner's DEBUG records: problems tracking the
	// processes of the run child and of a Claude Code CLI, and what the run child wrote to
	// its stdout and stderr (see child.go). It is called from the goroutine that called Run.
	// Nil discards them.
	Log func(level slog.Level, msg string)
	// Progress receives the run's progress messages, redacted, as selected by Logging. It is
	// called from the goroutine that called Run. Nil reports nothing.
	Progress func(Progress)
	// Logging selects the progress messages. Empty means LoggingStatus.
	Logging Logging
	// HideNames keeps the names of agents, tools and models out of the progress messages,
	// warnings, errors and logs (for a secret configuration), and the tool calls' arguments
	// and results out of the progress messages.
	HideNames bool
	// AllowHarnessAgents lets the configuration's coding-harness agents run: agents with
	// `harness: {type: claude-code}` or `harness: {type: codex}`, on Linux and macOS. They
	// start the Claude Code or Codex CLI, which runs its own tools without docker-agent's tool
	// approval (see harness.go; for codex, ToolApproval selects Codex's sandbox, see
	// harness_codex.go). Without it, a configuration with a harness agent fails.
	AllowHarnessAgents bool
	// AnnounceHarness reports CheckConfig's warnings about running claude-code agents (no
	// tool approval, the credentials Claude Code would bill) and CheckModels' warnings about
	// the models (missing credentials, variables of the environment that do or no longer reach
	// the model SDKs) through Warn when the run starts. It is for runs whose configuration no
	// preview checked: a `reference`, or a run when the resource is deleted.
	AnnounceHarness bool
	// Warn receives warnings while the run is in progress, redacted: those of
	// AnnounceHarness, and, when the run fails, that Claude Code billed API credentials and
	// that the run left processes running. The warnings of a successful run are in
	// Result.Warnings. It is called from the goroutine that called Run. Nil drops them.
	Warn func(msg string)
	// Start is called once the run holds the run slot (runs of one process are serialized),
	// before the agent starts, from the goroutine that called Run. When it returns an error,
	// Run returns that error, unchanged, without running the agent. Nil starts the run.
	Start func() error

	// withheldToken is set in a run child: the provider's environment has a
	// CLAUDE_CODE_OAUTH_TOKEN that the child does not inherit (procenv.go).
	withheldToken bool
	// chatgptLogin is set in a run child: the ChatGPT login of docker agent setup the run may use
	// (chatgpt.go).
	chatgptLogin string
	// test is what a test makes the run child do (childtest.go).
	test *childFaults
}

// Usage is the token usage and cost of a run, including sub-agents.
type Usage struct {
	InputTokens  int64
	OutputTokens int64
	// Cost is in US dollars, as estimated by docker-agent from the models.dev catalog, for the
	// usage billed per token (API keys, cloud accounts, custom endpoints with a `cost` table). It
	// is 0 for models without known pricing (for example custom providers), and for the usage a
	// subscription plan pays for: a chatgpt model, Claude Code with a Claude subscription login
	// or CLAUDE_CODE_OAUTH_TOKEN, Codex with a ChatGPT login (the run's summary names the plan,
	// with the usage's price at API rates where one is known).
	Cost float64
}

// Result is the outcome of a successful run.
type Result struct {
	// Output is the final answer of the agent that ended the conversation.
	Output string
	// StructuredOutput is Output decoded as JSON when that agent declares
	// `structured_output`, else nil.
	StructuredOutput any
	// HasStructuredOutput reports whether StructuredOutput is set (a JSON null answer
	// decodes to nil).
	HasStructuredOutput bool
	// AgentName is the agent that produced Output: the selected agent, or the agent
	// the conversation was handed off to.
	AgentName string
	Usage     Usage
	// ToolCalls are the names of the tools that ran, in order, across the agent and
	// the sub-agents it transferred tasks to. Rejected calls are not included.
	ToolCalls []string
	// RejectedToolCalls are the names of the tool calls that were not approved.
	RejectedToolCalls []string
	// Warnings are docker-agent warnings worth surfacing (load-time warnings, model
	// warnings, rejected tool calls).
	Warnings []string
	// OutputContainsSecret reports that Output contains a value of the redaction set
	// (for example an `env` value the model repeated), so it must be stored as a secret.
	OutputContainsSecret bool

	// costNote names the subscription plans that pay for usage Usage.Cost does not count, for
	// the summary ("billed to the ChatGPT plan"; billingNote).
	costNote string
}

// runSlot serializes runs. Waiting for it honours cancellation.
var runSlot = make(chan struct{}, 1)

// stateRootPrefix names the per-run state directories.
const stateRootPrefix = "pulumi-docker-agent-state-"

// Run executes the request in a run child (child.go). It blocks while another run of this
// process is in progress. The process must have called MaybeRunChild first.
func Run(ctx context.Context, req Request) (*Result, error) {
	if strings.TrimSpace(req.Prompt) == "" {
		return nil, errors.New("prompt must not be empty")
	}
	if len(req.YAML) == 0 {
		return nil, errors.New("the agent configuration is empty")
	}
	approval := req.ToolApproval
	if approval == "" {
		approval = ToolApprovalDeny
	}
	if approval != ToolApprovalDeny && approval != ToolApprovalAllow {
		return nil, fmt.Errorf("invalid toolApproval %q: must be %q or %q", approval, ToolApprovalDeny, ToolApprovalAllow)
	}
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	switch {
	case inChild.Load():
		return nil, errors.New("an agent run cannot start another agent run")
	case !childEntry.Load():
		return nil, errors.New("this program cannot run agents: its main function (or TestMain) must call " +
			"agentrun.MaybeRunChild first")
	}
	secrets := redactionSet(req)
	// A SIGINT from now on also stops the run, and refuses the next.
	WatchShutdownSignals()

	select {
	case runSlot <- struct{}{}:
	case <-ctx.Done():
		return nil, fmt.Errorf("canceled while waiting for another agent run to finish: %w", ctx.Err())
	}
	defer func() { <-runSlot }()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if req.Start != nil {
		if err := req.Start(); err != nil {
			return nil, err
		}
	}
	return runInChild(ctx, req, approval, timeout, secrets)
}

// runRequest runs the request in the run child (child.go), in the state root and the working
// directory the parent created. It calls ended with the result or the error as soon as the
// conversation ended, and returns once the runtime and the toolsets were stopped. The parent
// redacts what ended gets again, adds its own check whether the answer is secret, and reports
// the summary. timed is called when the run's timeout starts (before the configuration is
// loaded: loading can pull a model).
func runRequest(ctx context.Context, req Request, stateRoot string, timed func(), ended func(*Result, error)) {
	secrets := redactionSet(req)
	// docker-agent logs to slog: keep its records (which can quote API errors) off stderr.
	names := newNameHider(req)
	ctx, stopLog := startRunLog(ctx, secrets, names, req.Log)
	defer stopLog()
	paths.SetRoot(stateRoot)

	// Redacted first, then without the names: hiding a name inside a secret would split it.
	warn := func(msg string) {
		if req.Warn != nil {
			req.Warn(names.hide(redact(msg, secrets)))
		}
	}
	prog := newProgress(req, secrets, names)
	info := &harnessInfo{}
	prog.extra = info.extraSecrets
	debug := func(msg string) {
		if req.Log != nil {
			req.Log(slog.LevelDebug, names.hide(redact(msg, secrets)))
		}
	}
	// The ChatGPT login the parent planned for, which the run copies when it asks for it.
	var login *chatgptSource
	if req.chatgptLogin != "" {
		login = newChatGPTSource(req.chatgptLogin, stateRoot, debug, info.addSecrets)
	}
	// What run started is stopped after ended (and before stopLog: the teardown's records still
	// reach the run's log).
	var td teardown
	defer td.run()
	res, err := run(ctx, req, runSetup{req.ToolApproval, req.Timeout, req.WorkingDir, secrets, info, warn, debug, timed,
		&td, login}, prog)
	// The secrets found during the run (the tokens of a codex login, a refreshed ChatGPT login)
	// are secrets of its result.
	secrets = append(secrets, info.extraSecrets()...)
	harnessWarnings := reportHarness(info, prog)
	if err != nil {
		for _, w := range harnessWarnings {
			warn(w)
		}
		text := err.Error()
		rf, failed := errors.AsType[*runFailure](err)
		// The ChatGPT login's hints concern the agents' own models, not a coding-harness CLI.
		if !failed {
			text = joinHints(text, nonEmpty(login.errorHint(text, "")))
		} else if rf.inProcess != "" {
			text = joinHints(text, nonEmpty(login.errorHint(rf.inProcess, rf.chatgpt)))
		}
		msg := names.hide(redact(text, secrets))
		switch mce, ok := errors.AsType[*MissingCredentialsError](err); {
		case ok:
			ended(nil, &MissingCredentialsError{Variables: mce.Variables, msg: msg})
		case failed:
			ended(nil, &runFailure{msg: msg, inProcess: names.hide(redact(rf.inProcess, secrets))})
		default:
			ended(nil, errors.New(msg))
		}
		return
	}
	res.Warnings = append(res.Warnings, harnessWarnings...)
	for i, w := range res.Warnings {
		res.Warnings[i] = redact(w, secrets)
	}
	// The parent adds its own finding (the provider's credentials) and the warning.
	res.OutputContainsSecret = containsSecret(res.Output, secrets)
	ended(res, nil)
}

// reportHarness reports what the coding-harness CLIs of a run said about themselves, as
// progress records, and returns the warnings (reportClaude, reportCodex).
func reportHarness(info *harnessInfo, prog *progress) []string {
	return append(reportClaude(info, prog), reportCodex(info, prog)...)
}

// reportClaude reports what the Claude Code CLI of a run said about itself, as a progress
// record, and returns the warnings: that it billed something other than a Claude
// subscription, that its answer replaced an empty result, and that it left processes
// running, which the runner stopped.
//
// apiKeySource none only says that Claude Code used no Anthropic API key. The record then
// names what the CLI's environment selects instead: a cloud provider (Bedrock, Vertex AI,
// Foundry), ANTHROPIC_AUTH_TOKEN, or a Claude login, and whether a gateway
// (ANTHROPIC_BASE_URL) got the requests.
func reportClaude(info *harnessInfo, prog *progress) []string {
	r := info.get()
	var warnings []string
	if r.leftRunning > 0 {
		warnings = append(warnings, leftRunningWarning(r.leftRunning))
	}
	if !r.seen {
		return warnings
	}
	source, billed := cmp.Or(r.apiKeySource, "unknown"), ""
	switch r.apiKeySource {
	case "", "unknown":
	case "none":
		n := r.noKey
		used := n.what
		source = "none (" + n.what
		if n.via != "" {
			source, used = source+", "+n.via, used+" ("+n.via+")"
		}
		if n.gateway {
			source += ", through ANTHROPIC_BASE_URL"
		}
		if n.withheldToken {
			source += "; " + withheldTokenNote
		}
		source += ")"
		if n.billed != "" {
			billed = fmt.Sprintf("%s used %s, which bills %s, not a Claude subscription", codingharness.TypeClaudeCode,
				used, n.billed)
		}
	default:
		billed = fmt.Sprintf("%s authenticated with %s, which bills API usage, not a Claude subscription",
			codingharness.TypeClaudeCode, r.apiKeySource)
	}
	if prog.at(LoggingStatus) {
		prog.record("%s %s (%s), apiKeySource %s", codingharness.TypeClaudeCode, r.version, prog.name(r.model, "a model"),
			source)
	}
	if billed != "" {
		warnings = append(warnings, billed)
	}
	if r.emptyResults > 0 {
		warnings = append(warnings, codingharness.TypeClaudeCode+" returned an empty final result, so the agent's "+
			"answer is the text that Claude Code wrote during the run instead")
	}
	return warnings
}

// leftRunningWarning says that the Claude Code CLIs of a run left n processes running when
// they exited. It names no command: a command line can contain a secret.
func leftRunningWarning(n int) string { return leftRunningWarningOf(codingharness.TypeClaudeCode, n) }

// leftRunningWarningOf says that the CLIs of a harness type left n processes running.
func leftRunningWarningOf(typ string, n int) string {
	what, them := "1 process", "it"
	if n > 1 {
		what, them = fmt.Sprintf("%d processes", n), "them"
	}
	return fmt.Sprintf("%s left %s running when it exited (for example a command it ran in the background); "+
		"AgentRun stopped %s", typ, what, them)
}

// runSetup is what Run prepared for run.
type runSetup struct {
	approval   ToolApproval
	timeout    time.Duration
	workingDir string
	secrets    []string
	harness    *harnessInfo
	warn       func(string)
	// debug receives the runner's own debug messages, redacted.
	debug func(string)
	// timed is called when the timeout starts; nil does nothing.
	timed func()
	// teardown gets what run stops after the conversation; nil stops it when run returns.
	teardown *teardown
	// login serves the ChatGPT login of docker agent setup (chatgpt.go); nil when the run uses
	// none.
	login *chatgptSource
}

// runFailure is the error of a run whose conversation failed. inProcess is what the agents'
// own models reported, "" when only coding-harness CLIs failed (docker-agent's "harness <type>
// failed: ..."): the hints about the models' credentials and endpoints concern only that part.
// chatgpt is what the agents with a chatgpt model reported.
type runFailure struct{ msg, inProcess, chatgpt string }

func (e *runFailure) Error() string { return e.msg }

// harnessError matches docker-agent's error of a coding-harness CLI (runtime/harness.go).
var harnessError = regexp.MustCompile(`^harness [A-Za-z0-9_-]+ failed: `)

// nonEmpty returns s as a list of at most one hint.
func nonEmpty(s string) []string {
	if s == "" {
		return nil
	}
	return []string{s}
}

// teardown is what a run stops after its conversation, in the reverse order of add: the
// runtime, the toolsets, the run's timeout. A run child sends its result before it.
type teardown struct{ steps []func() }

func (t *teardown) add(step func()) { t.steps = append(t.steps, step) }

func (t *teardown) run() {
	for i := len(t.steps) - 1; i >= 0; i-- {
		t.steps[i]()
	}
	t.steps = nil
}

func run(ctx context.Context, req Request, rs runSetup, prog *progress) (*Result, error) {
	approval, timeout, workingDir := rs.approval, rs.timeout, rs.workingDir
	if rs.teardown == nil {
		rs.teardown = &teardown{}
		defer rs.teardown.run()
	}
	// Remote MCP servers get a token store private to this run.
	ctx = mcp.WithOAuthTokenStore(ctx, mcp.NewInMemoryTokenStore())

	name := req.Name
	if name == "" {
		name = "agent"
	}
	source := config.NewBytesSource(name+".yaml", req.YAML)

	// Coding-harness agents fail with the messages CheckConfig gives in a preview. Errors of
	// the configuration itself are the loader's.
	harnessEnv := claudeEnv(req, workingDir)
	rs.harness.setNoKey(harnessEnv.noKeySource())
	var codex *codexRun
	// cfg describes the run's models (runinfo.go); nil when the configuration does not load.
	cfg, err := config.Load(ctx, source)
	if err == nil {
		agents := harnessAgentsOf(cfg)
		if problems := agents.problems(req.AllowHarnessAgents, req.HideNames); len(problems) > 0 {
			return nil, errors.New(strings.Join(problems, "; "))
		}
		if len(agents.codex()) > 0 {
			codex = newCodexRun(req, workingDir, approval, rs.harness, rs.debug)
			if problems := codexProblems(codex.auth, false); len(problems) > 0 {
				return nil, errors.New(strings.Join(problems, "; "))
			}
		}
		if req.AnnounceHarness {
			warnings := agents.runWarnings(harnessEnv, req.HideNames)
			if codex != nil {
				warnings = append(warnings, agents.codexRunWarnings(codex.env, codex.auth, codex.deny, false, req.HideNames)...)
			}
			for _, w := range warnings {
				rs.warn(w)
			}
		}
	} else {
		cfg = nil
	}

	// A run that may use a chatgpt model also resolves CHATGPT_OAUTH_TOKEN from the ChatGPT login
	// of docker agent setup, which it copies into its config directory when it asks for it
	// (chatgpt.go).
	env := envProvider(req)
	if rs.login != nil {
		env = environment.NewMultiProvider(env, rs.login)
	}
	runConfig := &config.RuntimeConfig{
		Config:              config.Config{WorkingDir: workingDir},
		EnvProviderOverride: env,
		// The catalog embedded in docker-agent: model aliases, context windows and
		// pricing without a network lookup or a cache file.
		ModelsDevStoreOverride: modelsdev.NewDatabaseStore(modelsdev.EmbeddedSnapshot()),
	}
	// model auto honours DOCKER_AGENT_DEFAULT_MODEL (or CAGENT_DEFAULT_MODEL) of the run's
	// environment, as docker agent run does.
	runConfig.DefaultModel, _ = defaultModel(ctx, env)
	// Harness agents run through AgentRun's own runner, never through a driver registered
	// globally with runtime.RegisterHarness.
	var harnessFactory harness.Factory
	if req.AllowHarnessAgents {
		harnessFactory = newHarnessFactory(harnessRun{dir: workingDir, env: harnessEnv, secrets: rs.secrets, info: rs.harness,
			debug: rs.debug, codex: codex})
	}

	// timeoutSeconds bounds the run from here: loading the configuration (which pulls a missing
	// Docker Model Runner model and starts MCP servers) and the conversation. Torn down last, the
	// timeout is canceled only after the toolsets stopped: processes bound to it would be killed.
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	rs.teardown.add(cancel)
	if rs.timed != nil {
		rs.timed()
	}
	pulls := &pullOutput{debug: rs.debug, started: func(model string) {
		if prog.at(LoggingStatus) {
			prog.record("pulling the Docker Model Runner model %s (within timeoutSeconds)", prog.name(model, "a model"))
		}
	}}
	runCtx = withModelRunner(runCtx, pulls, rs.debug)

	policy := &approvalPolicy{}
	loaded, err := teamloader.LoadWithConfig(runCtx, source, runConfig, loaderOpts(req, policy)...)
	if err != nil {
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
			if model := pulls.pulling(); model != "" {
				return nil, pullTimeoutError(model, timeout)
			}
			return nil, fmt.Errorf("the agent did not finish within %s (timeoutSeconds)", timeout)
		}
		return nil, describeLoadError(ctx, err, req, cfg, env)
	}
	t := loaded.Team
	rs.teardown.add(func() { stopToolSets(ctx, t) })
	codex.bind(t)

	selected, err := t.AgentOrDefault(req.Agent)
	if err != nil {
		return nil, err
	}
	mode := safetyPolicy(approval, selected, t)
	policy.bind(approval == ToolApprovalDeny && !req.AllowHookApprovals, mode, t.Permissions())

	res := &Result{}
	names := prog.names
	for _, a := range teamAgents(t) {
		names.add(a.Name())
	}
	for _, a := range teamAgents(t) {
		for _, w := range a.DrainWarnings() {
			if names != nil {
				res.Warnings = append(res.Warnings, "an agent: "+names.hide(w))
			} else {
				res.Warnings = append(res.Warnings, fmt.Sprintf("agent %q: %s", a.Name(), w))
			}
		}
	}

	// The credential line: the models the agents can use and where their credentials come from.
	var models runModels
	if cfg != nil {
		models = newRunModels(ctx, cfg, req, t)
		if line := models.line(req.HideNames); line != "" && prog.at(LoggingStatus) {
			prog.record("%s", line)
		}
	}

	opts := bootstrap.RuntimeOpts(loaded, runConfig)
	opts = append(opts,
		runtime.WithSessionStore(session.NewInMemorySessionStore()),
		runtime.WithCurrentAgent(selected.Name()),
		runtime.WithWorkingDir(workingDir),
		runtime.WithEnv(hookEnv(req)),
		// Headless: elicitations (MCP OAuth, forms) are declined instead of waiting.
		runtime.WithNonInteractive(true),
		// Nil unless harness agents are allowed. Slash commands are not resolved, so
		// JavaScript command expressions are not needed.
		runtime.WithHarnessFactory(harnessFactory),
		runtime.WithCommandEvaluatorFactory(nil),
	)
	rt, err := runtime.New(ctx, t, opts...)
	if err != nil {
		return nil, fmt.Errorf("creating the docker-agent runtime: %w", err)
	}
	rs.teardown.add(func() { _ = rt.Close() })

	sess := session.New(
		session.WithTitle("Pulumi AgentRun"),
		session.WithMaxIterations(selected.MaxIterations()),
		session.WithMaxConsecutiveToolCalls(selected.MaxConsecutiveToolCalls()),
		session.WithMaxOldToolCallTokens(selected.MaxOldToolCallTokens()),
		session.WithMaxToolResultTokens(selected.MaxToolResultTokens()),
		session.WithToolsApproved(approval == ToolApprovalAllow),
		session.WithSafetyPolicy(mode),
		// Non-interactive: a call that still needs confirmation is denied with a
		// tool error the model sees, and max_iterations stops the run instead of
		// asking whether to continue.
		session.WithNonInteractive(true),
		session.WithWorkingDir(workingDir),
		session.WithUserMessage(req.Prompt),
	)

	col := newCollector(sess.ID)
	for ev := range rt.RunStream(runCtx, sess) {
		names.observe(ev)
		col.observe(ev)
		prog.observe(ev)
		switch e := ev.(type) {
		case *runtime.ToolCallConfirmationEvent:
			// Not expected for a non-interactive session; never leave the runtime
			// waiting.
			rt.Resume(runCtx, runtime.ResumeReject("this agent runs unattended and cannot confirm tool calls"))
		case *runtime.MaxIterationsReachedEvent:
			rt.Resume(runCtx, runtime.ResumeReject(""))
		case *runtime.ElicitationRequestEvent:
			_ = rt.ResumeElicitation(runCtx, tools.ElicitationActionDecline, nil, e.ElicitationID)
		}
	}

	if errors.Is(runCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
		return nil, fmt.Errorf("the agent did not finish within %s (timeoutSeconds)", timeout)
	}
	if err := ctx.Err(); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, errors.New("the operation's time limit (customTimeouts) ran out before the agent finished")
		}
		return nil, fmt.Errorf("the agent run was canceled: %w", err)
	}
	if len(col.errors) > 0 {
		errs := slices.Compact(col.errors)
		msg := strings.Join(errs, "; ")
		// The errors of the agents' own models, without those of coding-harness CLIs.
		inProcess := strings.Join(slices.DeleteFunc(slices.Clone(errs), harnessError.MatchString), "; ")
		if line := models.line(req.HideNames); line != "" && isAuthFailure(inProcess) {
			// Which credentials the rejected requests carried (never their values).
			msg += "; the run's " + line
		}
		return nil, &runFailure{msg: "the agent run failed: " + msg, inProcess: inProcess,
			chatgpt: strings.Join(col.errorsOf(chatgptAgents(t)), "; ")}
	}
	if col.stopReason != "" && col.stopReason != runtime.TurnEndReasonNormal {
		return nil, fmt.Errorf("the agent stopped without a final answer (%s)", strings.ReplaceAll(col.stopReason, "_", " "))
	}

	final, ok := lastAnswer(sess)
	if !ok {
		return nil, errors.New("the agent produced no final answer")
	}
	if final.Message.FinishReason == chat.FinishReasonLength {
		return nil, errors.New("the agent's answer was truncated by the model's token limit (max_tokens)")
	}
	res.Output = chat.VisibleAssistantContent(final.Message.Content)
	res.AgentName = final.AgentName
	if res.AgentName == "" {
		res.AgentName = rt.CurrentAgentName(ctx)
	}

	if a, err := t.Agent(res.AgentName); err == nil && a.StructuredOutput() != nil {
		// One surrounding Markdown code fence is removed for every agent: harnesses, providers
		// without native structured output (Amazon Bedrock) and servers that ignore the schema
		// tend to fence JSON. Valid JSON never starts with a backtick, so no answer that decodes
		// changes. Output stays as the agent wrote it.
		answer := stripCodeFence(strings.TrimSpace(res.Output))
		var v any
		if err := json.Unmarshal([]byte(answer), &v); err != nil {
			hint := ""
			if !a.HasHarness() && !a.StructuredOutput().ToolMode() {
				hint = "; set structured_output.mode: tool to have docker-agent enforce the schema"
			}
			return nil, fmt.Errorf("agent %q declares structured_output but its answer is not valid JSON: %w%s", res.AgentName,
				err, hint)
		}
		res.StructuredOutput = v
		res.HasStructuredOutput = true
	}

	res.Usage = usageOf(sess)
	// Only usage billed per token counts. Codex reports no cost: its turns with a login billed
	// per token add an estimate at API prices (harness_codex.go). A plan's usage (chatgpt models,
	// a Claude subscription, a ChatGPT login of Codex) is named in the summary instead.
	res.Usage.Cost += rs.harness.codexCost()
	res.costNote = billingNote(chatgptAnswered(cfg, sess), rs.harness.plans())
	if w := rs.login.unusedWarning(models.line(req.HideNames)); w != "" {
		res.Warnings = append(res.Warnings, w)
	}
	res.ToolCalls = col.toolNames()
	for _, r := range col.rejected {
		res.RejectedToolCalls = append(res.RejectedToolCalls, r.tool)
		res.Warnings = append(res.Warnings, rejectionWarning(r, names))
	}
	for _, w := range col.warnings {
		res.Warnings = append(res.Warnings, names.hide(w))
	}
	return res, nil
}

// loaderOpts mirror docker-agent's loader defaults (pkg/teamloader/defaults.LoaderOpts:
// every model provider, toolset, agent source and optional feature) in strict mode,
// except coding-harness agents unless Request.AllowHarnessAgents is set: they start an
// external coding agent CLI that executes its own tools outside docker-agent's approval
// flow. The defaults package is not imported because it registers docker-agent's own
// harness driver globally. The toolset registry is the run's own (see toolsetCreators).
func loaderOpts(req Request, policy *approvalPolicy) []teamloader.Opt {
	features := enabledFeatures
	if req.AllowHarnessAgents {
		features = append(slices.Clone(enabledFeatures), config.FeatureHarness)
	}
	return []teamloader.Opt{
		teamloader.WithToolsetRegistry(teamloader.NewToolsetRegistry(toolsetCreators(req, policy))),
		teamloader.WithProviderRegistry(providers.NewDefaultRegistry()),
		teamloader.WithSourceResolver(func(ref string, env environment.Provider) (config.Source, error) {
			return sources.Resolve(ref, env)
		}),
		teamloader.WithExpander(js.NewJsExpander),
		teamloader.WithCodeMode(codemode.Wrap),
		teamloader.WithToon(toon.Wrap),
		teamloader.WithDeferredTools(deferred.New),
		teamloader.WithStrict(features...),
	}
}

var enabledFeatures = []config.Feature{
	config.FeatureCodeMode, config.FeatureDeferredTools, config.FeatureExternalAgents,
	config.FeatureAgentRouting, config.FeatureHooks, config.FeatureEvaluators,
	config.FeatureSkills, config.FeatureToon,
}

// describeLoadError explains a configuration the loader rejected. Run reports coding-harness
// agents that cannot run before it loads the configuration (see harnessAgents.problems);
// this covers what the loader still finds. cfg is the loaded configuration (nil when it did
// not load), env the run's environment chain.
func describeLoadError(ctx context.Context, err error, req Request, cfg *latest.Config, env environment.Provider) error {
	var ue *config.UnsupportedError
	if errors.As(err, &ue) {
		for _, f := range ue.Features {
			if f.Name == string(config.FeatureHarness) {
				return fmt.Errorf("coding-harness agents (%s) run only with allowHarnessAgents: true: they start an "+
					"external coding agent CLI that runs its own tools without docker-agent's tool approval",
					strings.Join(f.Locations, ", "))
			}
		}
		if len(ue.Providers) > 0 {
			return fmt.Errorf("loading the agent configuration: %w; %s", err, UserProvidersHint)
		}
	}
	// docker-agent's advice for missing credentials is for its CLI (export, --env-from-file, its
	// env file), which AgentRun does not read.
	if e := describeMissingCredentials(err, req.InheritEnvironment); e != nil {
		return e
	}
	if e := describeAutoError(ctx, err, req, cfg, env); e != nil {
		return e
	}
	return fmt.Errorf("loading the agent configuration: %w", err)
}

// UserProvidersHint explains a model provider that is neither docker-agent's nor declared in the
// configuration.
const UserProvidersHint = "AgentRun does not use user-level providers (from docker agent setup, or UserSettings " +
	"providers): declare the provider under `providers` in the configuration"

// safetyPolicy is the session's docker-agent safety mode. `allow` is autonomous (like
// --yolo). `deny` honours the strict, balanced and restricted modes the configuration
// declares for the agent or the runtime, and otherwise keeps docker-agent's default
// mode, in which only read-only tools run without confirmation.
func safetyPolicy(approval ToolApproval, a *agent.Agent, t *team.Team) session.SafetyPolicy {
	if approval == ToolApprovalAllow {
		return session.SafetyPolicyAutonomous
	}
	declared := session.SafetyPolicy(a.Safety())
	if declared == "" {
		declared = session.SafetyPolicy(t.RuntimeSafety())
	}
	switch p := declared.Normalize(); p {
	case session.SafetyPolicyStrict, session.SafetyPolicyBalanced, session.SafetyPolicyRestricted:
		return p
	default:
		return ""
	}
}

// envProvider resolves model credentials and ${env.NAME} references: Request.Env first,
// then (optionally) the process environment. docker-agent's other sources (its .env
// file, credential helper, Docker Desktop, 1Password references) are not used.
func envProvider(req Request) environment.Provider {
	vars := environment.NewMapEnvProvider(req.Env)
	if !req.InheritEnvironment {
		return vars
	}
	return environment.NewMultiProvider(vars, environment.NewOsEnvProvider())
}

func teamAgents(t *team.Team) []*agent.Agent {
	var out []*agent.Agent
	for _, n := range t.AgentNames() {
		if a, err := t.Agent(n); err == nil {
			out = append(out, a)
		}
	}
	return out
}

// toolsetStopTimeout bounds stopping a run's toolsets; a variable, so that tests can shorten
// it.
var toolsetStopTimeout = 30 * time.Second

func stopToolSets(ctx context.Context, t *team.Team) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), toolsetStopTimeout)
	defer cancel()
	_ = t.StopToolSets(ctx)
}

// lastAnswer returns the conversation's final assistant message: the last visible
// assistant message of the root session that carries no tool calls. Messages of
// sub-agents that ran a transferred task live in sub-sessions; after a handoff the
// receiving agent writes to the root session.
func lastAnswer(sess *session.Session) (session.Message, bool) {
	msgs := sess.OwnMessages()
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if m.Message.Role != chat.MessageRoleAssistant || m.Implicit {
			continue
		}
		if len(m.Message.ToolCalls) > 0 || strings.TrimSpace(chat.VisibleAssistantContent(m.Message.Content)) == "" {
			return session.Message{}, false
		}
		return m, true
	}
	return session.Message{}, false
}

func usageOf(sess *session.Session) Usage {
	var u Usage
	for _, m := range sess.GetAllMessages() {
		if m.Message.Role != chat.MessageRoleAssistant || m.Message.Usage == nil {
			continue
		}
		u.InputTokens += m.Message.Usage.PromptTokens()
		u.OutputTokens += m.Message.Usage.OutputTokens
	}
	u.Cost = sess.TotalCost()
	return u
}

// collector gathers what the event stream tells about a run.
type collector struct {
	rootSession string
	toolCalls   []toolCall
	rejected    []rejection
	errors      []string
	// errorAgents are the agents of errors: the agent that last ran in the error's session.
	errorAgents  []string
	sessionAgent map[string]string
	warnings     []string
	stopReason   string
}

// errorsOf returns the errors of the agents of names, without those of coding-harness CLIs.
func (c *collector) errorsOf(names []string) []string {
	var out []string
	for i, e := range c.errors {
		if slices.Contains(names, c.errorAgents[i]) && !harnessError.MatchString(e) && !slices.Contains(out, e) {
			out = append(out, e)
		}
	}
	return out
}

// chatgptAgents returns the agents of a team that have a chatgpt model (also as a fallback).
func chatgptAgents(t *team.Team) []string {
	var out []string
	for _, a := range teamAgents(t) {
		for _, m := range slices.Concat(a.ConfiguredModels(), a.FallbackModels()) {
			if m.BaseConfig().ModelConfig.Provider == chatgpt.ProviderName {
				out = append(out, a.Name())
				break
			}
		}
	}
	return out
}

func newCollector(rootSession string) *collector {
	return &collector{rootSession: rootSession, sessionAgent: map[string]string{}}
}

func (c *collector) observe(ev runtime.Event) {
	if a, ok := ev.(interface{ GetAgentName() string }); ok && a.GetAgentName() != "" {
		if s, ok := ev.(interface{ GetSessionID() string }); ok {
			c.sessionAgent[cmp.Or(s.GetSessionID(), c.rootSession)] = a.GetAgentName()
		}
	}
	switch e := ev.(type) {
	case *runtime.ToolCallEvent:
		// Emitted when a tool actually runs (after approval).
		if e.ToolCall.Function.Name != structuredoutput.ToolName {
			c.toolCalls = append(c.toolCalls, toolCall{id: e.ToolCall.ID, name: e.ToolCall.Function.Name})
		}
	case *runtime.ToolCallResponseEvent:
		// The approval flow never sees the calls a harness reports: their results only quote
		// what the harness's tools printed.
		if e.ToolDefinition.Category != harnessToolCategory && e.Result != nil && e.Result.IsError &&
			isRejection(e.Response) {
			c.rejected = append(c.rejected, rejection{tool: e.ToolDefinition.Name, agent: e.AgentName, reason: e.Response})
			// The approval guard rejects a call after docker-agent announced it.
			c.toolCalls = slices.DeleteFunc(c.toolCalls, func(tc toolCall) bool { return tc.id != "" && tc.id == e.ToolCallID })
		}
	case *runtime.ErrorEvent:
		if e.Error != "" {
			c.errors = append(c.errors, e.Error)
			c.errorAgents = append(c.errorAgents, c.sessionAgent[cmp.Or(e.SessionID, c.rootSession)])
		}
	case *runtime.WarningEvent:
		if e.Message != "" {
			c.warnings = append(c.warnings, e.Message)
		}
	case *runtime.StreamStoppedEvent:
		if e.SessionID == "" || e.SessionID == c.rootSession {
			c.stopReason = e.Reason
		}
	}
}

// toolCall is a tool call that ran.
type toolCall struct {
	id, name string
}

func (c *collector) toolNames() []string {
	out := make([]string, 0, len(c.toolCalls))
	for _, tc := range c.toolCalls {
		out = append(out, tc.name)
	}
	return out
}

// rejection is a tool call the approval flow did not let run.
type rejection struct {
	tool, agent, reason string
}

// rejectionWarning explains a rejected tool call. When names are hidden, it names neither
// the agent nor the tool, and docker-agent's reason only without the names.
func rejectionWarning(r rejection, names *nameHider) string {
	call := fmt.Sprintf("agent %q: tool call %q", r.agent, r.tool)
	if names != nil {
		call = "an agent: a tool call"
	}
	if isGuardRejection(r.reason) {
		return fmt.Sprintf("%s was rejected: a hook of the configuration approved it, but toolApproval %q does not "+
			"accept hook approvals unless allowHookApprovals is set", call, ToolApprovalDeny)
	}
	if strings.Contains(r.reason, "requires user confirmation") {
		return fmt.Sprintf("%s was rejected because it needs approval and toolApproval is %q; allow it with a "+
			"`permissions` allow rule in the configuration, or set toolApproval to %q", call, ToolApprovalDeny,
			ToolApprovalAllow)
	}
	return fmt.Sprintf("%s was rejected: %s", call, names.hide(r.reason))
}

// isRejection recognizes the tool errors docker-agent's approval flow produces.
func isRejection(response string) bool {
	return isGuardRejection(response) ||
		strings.Contains(response, "requires user confirmation but the session is non-interactive") ||
		strings.Contains(response, "The user rejected the tool call") ||
		(strings.HasPrefix(response, "Tool '") && strings.Contains(response, "' is denied by"))
}

// stripCodeFence removes one Markdown code fence around s (for example ```json ... ```).
func stripCodeFence(s string) string {
	if len(s) < 6 || !strings.HasPrefix(s, "```") || !strings.HasSuffix(s, "```") {
		return s
	}
	body := strings.TrimSuffix(s, "```")
	nl := strings.IndexByte(body, '\n')
	if nl < 0 {
		return s
	}
	return strings.TrimSpace(body[nl+1:])
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
