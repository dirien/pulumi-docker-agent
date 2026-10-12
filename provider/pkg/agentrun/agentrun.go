// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

// Package agentrun runs a docker-agent configuration in process, headless, with the
// docker-agent Go SDK (pkg/teamloader, pkg/runtime and pkg/session), the same code path
// `docker agent run --exec` uses, but without the CLI's user-level state:
//
//   - every run gets a private, empty docker-agent data, config and cache directory
//     (paths.SetRoot), so ~/.cagent, ~/.config/cagent (user settings, aliases, global
//     providers, the .env file, hook drop-ins, the credential helper) and ~/.cache/cagent
//     are never read or written. The directory is removed when the run ends;
//   - credentials and ${env.NAME} references resolve from Request.Env and, when
//     Request.InheritEnvironment is set, the provider process environment only.
//     Without it, the processes the configuration starts get a scrubbed environment
//     (see procenv.go);
//   - sessions are kept in memory, telemetry is off and the models.dev catalog is the
//     snapshot embedded in the docker-agent library (no network lookup);
//   - tool calls are approved by docker-agent's own rules for unattended runs
//     (see ToolApproval), without hook approvals unless allowed (see policy.go);
//   - errors, warnings and logs are redacted (see secrets.go);
//   - coding-harness agents run only with Request.AllowHarnessAgents, and only claude-code:
//     AgentRun runs the Claude Code CLI itself, in the run's working directory and
//     environment (see harness.go).
//
// docker-agent keeps its data, config and cache directories in process-global state, so
// runs in one process are serialized.
package agentrun

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/docker/docker-agent/pkg/agent"
	"github.com/docker/docker-agent/pkg/bootstrap"
	"github.com/docker/docker-agent/pkg/chat"
	"github.com/docker/docker-agent/pkg/codingharness"
	"github.com/docker/docker-agent/pkg/config"
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
	// references. It takes precedence over the inherited environment.
	Env map[string]string
	// InheritEnvironment also resolves variables from the process environment.
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
	// Log receives docker-agent's own WARN and ERROR log records of this run, redacted.
	// Lower levels are dropped. Nil discards them.
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
	// `harness: {type: claude-code}`, on Linux and macOS. They start the Claude Code CLI,
	// which runs its own tools without docker-agent's tool approval (see harness.go).
	// Without it, a configuration with a harness agent fails.
	AllowHarnessAgents bool
	// AnnounceHarness reports CheckConfig's warnings about running claude-code agents (no
	// tool approval, the credentials Claude Code would bill) through Warn when the run
	// starts. It is for runs whose configuration no preview checked: a `reference`, or a
	// run when the resource is deleted.
	AnnounceHarness bool
	// Warn receives warnings while the run is in progress, redacted: those of
	// AnnounceHarness, and, when the run fails, that Claude Code billed API credentials.
	// The warnings of a successful run are in Result.Warnings. Nil drops them.
	Warn func(msg string)
	// Start is called once the run holds the run slot (runs of one process are serialized),
	// before the agent starts, from the goroutine that called Run. When it returns an error,
	// Run returns that error, unchanged, without running the agent. Nil starts the run.
	Start func() error
}

// Usage is the token usage and cost of a run, including sub-agents.
type Usage struct {
	InputTokens  int64
	OutputTokens int64
	// Cost is in US dollars, as estimated by docker-agent from the models.dev catalog.
	// It is 0 for models without known pricing (for example custom providers).
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
}

// runSlot serializes runs: paths.SetRoot is process-global. Waiting for it honours
// cancellation.
var runSlot = make(chan struct{}, 1)

// stateRootPrefix names the per-run state directories.
const stateRootPrefix = "pulumi-docker-agent-state-"

// Run executes the request. It blocks while another run of this process is in progress.
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
	secrets := redactionSet(req)
	if req.AllowHarnessAgents {
		// A SIGINT from now on also refuses the CLIs this run would start.
		WatchShutdownSignals()
	}

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

	// docker-agent logs to slog: keep its records (which can quote API errors) off the
	// provider's stderr.
	names := newNameHider(req)
	ctx, stopLog := startRunLog(ctx, secrets, names, req.Log)
	defer stopLog()

	stateRoot, err := os.MkdirTemp("", stateRootPrefix)
	if err != nil {
		return nil, fmt.Errorf("creating docker-agent state directory: %w", err)
	}
	paths.SetRoot(stateRoot)
	defer func() {
		// Point docker-agent at an unused directory rather than the user's defaults, so
		// that nothing in this process (for example a goroutine a toolset left behind)
		// falls back to ~/.cagent after the run.
		paths.SetRoot(idleRoot())
		_ = os.RemoveAll(stateRoot)
		_ = os.RemoveAll(idleRoot())
	}()

	workingDir := req.WorkingDir
	if workingDir == "" {
		wd, err := os.MkdirTemp("", "pulumi-docker-agent-work-")
		if err != nil {
			return nil, fmt.Errorf("creating working directory: %w", err)
		}
		defer func() { _ = os.RemoveAll(wd) }()
		workingDir = wd
	} else {
		abs, err := filepath.Abs(workingDir)
		if err != nil {
			return nil, fmt.Errorf("resolving workingDir: %w", err)
		}
		if fi, err := os.Stat(abs); err != nil || !fi.IsDir() {
			return nil, fmt.Errorf("workingDir %q is not an existing directory", workingDir)
		}
		workingDir = abs
	}

	warn := func(msg string) {
		if req.Warn != nil {
			req.Warn(redact(names.hide(msg), secrets))
		}
	}
	prog := newProgress(req, secrets, names)
	info := &harnessInfo{}
	res, err := run(ctx, req, runSetup{approval, timeout, workingDir, secrets, info, warn}, prog)
	harnessWarnings := reportHarness(info, prog)
	if err != nil {
		for _, w := range harnessWarnings {
			warn(w)
		}
		return nil, errors.New(redact(names.hide(err.Error()), secrets))
	}
	res.Warnings = append(res.Warnings, harnessWarnings...)
	for i, w := range res.Warnings {
		res.Warnings[i] = redact(w, secrets)
	}
	res.OutputContainsSecret = containsSecret(res.Output, secrets)
	if res.OutputContainsSecret {
		res.Warnings = append(res.Warnings, "the answer contains the value of a secret (an `env` value, a secret input or "+
			"a credential of the environment); output and structuredOutput are stored as secrets")
	}
	prog.summary(res)
	return res, nil
}

func idleRoot() string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("pulumi-docker-agent-idle-%d", os.Getpid()))
}

// reportHarness reports what the Claude Code CLI of a run said about itself, as a progress
// record, and returns the warnings: that it billed something other than a Claude
// subscription, and that its answer replaced an empty result.
//
// apiKeySource none only says that Claude Code used no Anthropic API key. The record then
// names what the CLI's environment selects instead: a cloud provider (Bedrock, Vertex AI,
// Foundry), ANTHROPIC_AUTH_TOKEN, or a Claude login, and whether a gateway
// (ANTHROPIC_BASE_URL) got the requests.
func reportHarness(info *harnessInfo, prog *progress) []string {
	r := info.get()
	if !r.seen {
		return nil
	}
	var warnings []string
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

// runSetup is what Run prepared for run.
type runSetup struct {
	approval   ToolApproval
	timeout    time.Duration
	workingDir string
	secrets    []string
	harness    *harnessInfo
	warn       func(string)
}

func run(ctx context.Context, req Request, rs runSetup, prog *progress) (*Result, error) {
	approval, timeout, workingDir := rs.approval, rs.timeout, rs.workingDir
	// Remote MCP servers get a token store private to this run.
	ctx = mcp.WithOAuthTokenStore(ctx, mcp.NewInMemoryTokenStore())

	runConfig := &config.RuntimeConfig{
		Config:              config.Config{WorkingDir: workingDir},
		EnvProviderOverride: envProvider(req),
		// The catalog embedded in docker-agent: model aliases, context windows and
		// pricing without a network lookup or a cache file.
		ModelsDevStoreOverride: modelsdev.NewDatabaseStore(modelsdev.EmbeddedSnapshot()),
	}

	name := req.Name
	if name == "" {
		name = "agent"
	}
	source := config.NewBytesSource(name+".yaml", req.YAML)

	// Coding-harness agents fail with the messages CheckConfig gives in a preview. Errors of
	// the configuration itself are the loader's.
	harnessEnv := claudeEnv(req, workingDir)
	rs.harness.setNoKey(harnessEnv.noKeySource())
	if cfg, err := config.Load(ctx, source); err == nil {
		agents := harnessAgentsOf(cfg)
		if problems := agents.problems(req.AllowHarnessAgents, req.HideNames); len(problems) > 0 {
			return nil, errors.New(strings.Join(problems, "; "))
		}
		if req.AnnounceHarness {
			for _, w := range agents.runWarnings(harnessEnv, req.HideNames) {
				rs.warn(w)
			}
		}
	}
	// Harness agents run through AgentRun's own runner, never through a driver registered
	// globally with runtime.RegisterHarness.
	var harnessFactory harness.Factory
	if req.AllowHarnessAgents {
		harnessFactory = newHarnessFactory(harnessRun{dir: workingDir, env: harnessEnv, secrets: rs.secrets, info: rs.harness})
	}

	policy := &approvalPolicy{}
	loaded, err := teamloader.LoadWithConfig(ctx, source, runConfig, loaderOpts(req, policy)...)
	if err != nil {
		return nil, describeLoadError(err)
	}
	t := loaded.Team
	defer stopToolSets(ctx, t)

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
	defer func() { _ = rt.Close() }()

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

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

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
		return nil, fmt.Errorf("the agent run failed: %s", strings.Join(slices.Compact(col.errors), "; "))
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
		answer := strings.TrimSpace(res.Output)
		if a.HasHarness() {
			// A harness does not enforce the schema, and Claude Code tends to fence JSON.
			answer = stripCodeFence(answer)
		}
		var v any
		if err := json.Unmarshal([]byte(answer), &v); err != nil {
			return nil, fmt.Errorf("agent %q declares structured_output but its answer is not valid JSON: %w", res.AgentName, err)
		}
		res.StructuredOutput = v
		res.HasStructuredOutput = true
	}

	res.Usage = usageOf(sess)
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
// this covers what the loader still finds.
func describeLoadError(err error) error {
	var ue *config.UnsupportedError
	if errors.As(err, &ue) {
		for _, f := range ue.Features {
			if f.Name == string(config.FeatureHarness) {
				return fmt.Errorf("coding-harness agents (%s) run only with allowHarnessAgents: true: they start an "+
					"external coding agent CLI that runs its own tools without docker-agent's tool approval",
					strings.Join(f.Locations, ", "))
			}
		}
	}
	return fmt.Errorf("loading the agent configuration: %w", err)
}

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

func stopToolSets(ctx context.Context, t *team.Team) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
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
	warnings    []string
	stopReason  string
}

func newCollector(rootSession string) *collector {
	return &collector{rootSession: rootSession}
}

func (c *collector) observe(ev runtime.Event) {
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
