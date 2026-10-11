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
//   - errors, warnings and logs are redacted (see secrets.go).
//
// docker-agent keeps its data, config and cache directories in process-global state, so
// runs in one process are serialized.
package agentrun

import (
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
	"github.com/docker/docker-agent/pkg/config"
	"github.com/docker/docker-agent/pkg/config/sources"
	"github.com/docker/docker-agent/pkg/environment"
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

	select {
	case runSlot <- struct{}{}:
	case <-ctx.Done():
		return nil, fmt.Errorf("canceled while waiting for another agent run to finish: %w", ctx.Err())
	}
	defer func() { <-runSlot }()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// docker-agent logs to slog: keep its records (which can quote API errors) off the
	// provider's stderr.
	ctx, stopLog := startRunLog(ctx, secrets, req.Log)
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

	res, err := run(ctx, req, approval, timeout, workingDir)
	if err != nil {
		return nil, errors.New(redact(err.Error(), secrets))
	}
	for i, w := range res.Warnings {
		res.Warnings[i] = redact(w, secrets)
	}
	res.OutputContainsSecret = containsSecret(res.Output, secrets)
	if res.OutputContainsSecret {
		res.Warnings = append(res.Warnings, "the answer contains the value of a secret (an `env` value, a secret input or "+
			"a credential of the environment); output and structuredOutput are stored as secrets")
	}
	return res, nil
}

func idleRoot() string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("pulumi-docker-agent-idle-%d", os.Getpid()))
}

func run(ctx context.Context, req Request, approval ToolApproval, timeout time.Duration, workingDir string) (*Result, error) {
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
	for _, a := range teamAgents(t) {
		for _, w := range a.DrainWarnings() {
			res.Warnings = append(res.Warnings, fmt.Sprintf("agent %q: %s", a.Name(), w))
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
		// Coding-harness agents are rejected at load time; slash commands are not
		// resolved, so JavaScript command expressions are not needed either.
		runtime.WithHarnessFactory(nil),
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
		col.observe(ev)
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
		var v any
		if err := json.Unmarshal([]byte(strings.TrimSpace(res.Output)), &v); err != nil {
			return nil, fmt.Errorf("agent %q declares structured_output but its answer is not valid JSON: %w", res.AgentName, err)
		}
		res.StructuredOutput = v
		res.HasStructuredOutput = true
	}

	res.Usage = usageOf(sess)
	res.ToolCalls = col.toolNames()
	for _, r := range col.rejected {
		res.RejectedToolCalls = append(res.RejectedToolCalls, r.tool)
		res.Warnings = append(res.Warnings, rejectionWarning(r))
	}
	res.Warnings = append(res.Warnings, col.warnings...)
	return res, nil
}

// loaderOpts mirror docker-agent's loader defaults (pkg/teamloader/defaults.LoaderOpts:
// every model provider, toolset, agent source and optional feature) in strict mode,
// except coding-harness agents: they start an external coding agent CLI that executes
// its own tools outside docker-agent's approval flow. The defaults package is not
// imported because it links that harness driver. The toolset registry is the run's own
// (see toolsetCreators).
func loaderOpts(req Request, policy *approvalPolicy) []teamloader.Opt {
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
		teamloader.WithStrict(enabledFeatures...),
	}
}

var enabledFeatures = []config.Feature{
	config.FeatureCodeMode, config.FeatureDeferredTools, config.FeatureExternalAgents,
	config.FeatureAgentRouting, config.FeatureHooks, config.FeatureEvaluators,
	config.FeatureSkills, config.FeatureToon,
}

// CheckConfig reports problems that prevent a valid configuration from running in
// AgentRun (today: coding-harness agents), so they surface during a preview.
func CheckConfig(ctx context.Context, name string, yamlDoc []byte) []string {
	cfg, err := config.Load(ctx, config.NewBytesSource(name, yamlDoc))
	if err != nil {
		// Configuration errors are reported by the validator.
		return nil
	}
	var out []string
	if locs := config.Requires(cfg).Features[config.FeatureHarness]; len(locs) > 0 {
		out = append(out, harnessMessage(locs))
	}
	return out
}

func harnessMessage(locs []string) string {
	return fmt.Sprintf("coding-harness agents (%s) cannot run in AgentRun: they start an external coding agent CLI "+
		"that executes its own tools without docker-agent's tool approval", strings.Join(locs, ", "))
}

func describeLoadError(err error) error {
	var ue *config.UnsupportedError
	if errors.As(err, &ue) {
		for _, f := range ue.Features {
			if f.Name == string(config.FeatureHarness) {
				return errors.New(harnessMessage(f.Locations))
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
		if e.Result != nil && e.Result.IsError && isRejection(e.Response) {
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

func rejectionWarning(r rejection) string {
	if isGuardRejection(r.reason) {
		return fmt.Sprintf("agent %q: tool call %q was rejected: a hook of the configuration approved it, but "+
			"toolApproval %q does not accept hook approvals unless allowHookApprovals is set", r.agent, r.tool, ToolApprovalDeny)
	}
	if strings.Contains(r.reason, "requires user confirmation") {
		return fmt.Sprintf("agent %q: tool call %q was rejected because it needs approval and toolApproval is %q; "+
			"allow it with a `permissions` allow rule in the configuration, or set toolApproval to %q",
			r.agent, r.tool, ToolApprovalDeny, ToolApprovalAllow)
	}
	return fmt.Sprintf("agent %q: tool call %q was rejected: %s", r.agent, r.tool, r.reason)
}

// isRejection recognizes the tool errors docker-agent's approval flow produces.
func isRejection(response string) bool {
	return isGuardRejection(response) ||
		strings.Contains(response, "requires user confirmation but the session is non-interactive") ||
		strings.Contains(response, "The user rejected the tool call") ||
		(strings.HasPrefix(response, "Tool '") && strings.Contains(response, "' is denied by"))
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
