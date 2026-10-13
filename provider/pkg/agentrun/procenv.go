// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package agentrun

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"

	"github.com/docker/docker-agent/pkg/config"
	"github.com/docker/docker-agent/pkg/config/latest"
	"github.com/docker/docker-agent/pkg/environment"
	"github.com/docker/docker-agent/pkg/teamloader"
	"github.com/docker/docker-agent/pkg/teamloader/toolsets"
	"github.com/docker/docker-agent/pkg/toolinstall"
	"github.com/docker/docker-agent/pkg/tools"
	"github.com/docker/docker-agent/pkg/tools/builtin/backgroundjobs"
	"github.com/docker/docker-agent/pkg/tools/builtin/lsp"
	"github.com/docker/docker-agent/pkg/tools/builtin/shell"
	"github.com/docker/docker-agent/pkg/tools/lifecycle"
	"github.com/docker/docker-agent/pkg/tools/mcp"
	"github.com/docker/docker-agent/pkg/tools/workingdir"
)

// The processes docker-agent starts for a configuration inherit the environment of the
// process that runs it, a run child (childenv.go): docker-agent builds their environment from
// os.Environ(), which holds Request.Env there. AgentRun therefore builds the environment of
// the processes it can control from the provider's environment (providerEnviron), which
// Request.Env is not part of, plus what the configuration passes explicitly (a toolset's
// `env`, and Request.Env for command hooks):
//
//   - command hooks: runtime.WithEnv, with Request.Env;
//   - `shell`, `script` and `background_jobs` toolsets: with Request.InheritEnvironment,
//     AgentRun builds them as docker-agent does, with the provider's environment in place of
//     os.Environ() (inheritedToolset), so a variable that only Request.Env sets is unset for
//     their processes, as it was in process. Otherwise, and for the options only docker-agent's
//     constructor can set (shell's sudo_askpass, background_jobs' recall), docker-agent
//     appends the toolset's `env` after os.Environ() and the last value wins: each variable of
//     Request.Env is overridden with the provider's value or an empty value;
//   - stdio MCP servers (`command`): started with exactly the environment AgentRun builds;
//   - LSP servers (`command`): docker-agent puts os.Environ() first when it starts the server,
//     so a variable can only be overridden, as for `shell` without Request.InheritEnvironment:
//     a variable that only Request.Env sets is empty, not unset. Empty is the safe choice: its
//     value must not reach the server.
//
// With Request.InheritEnvironment they get the provider's environment. Without it, only the
// system variables below: every other variable is overridden with an empty value, or not
// passed at all to stdio MCP servers. The cookies of the supervisors (descendants.go) are kept
// in both cases.
//
// Not controllable: `docker:` MCP servers (the Docker MCP Gateway starts with the whole
// environment), filesystem `post_edit` commands, the `open_url` browser launcher and
// docker-agent's on-demand tool installation.

// systemEnvVars are passed to processes when the environment is not inherited.
var systemEnvVars = []string{
	"PATH", "HOME", "USER", "LOGNAME", "SHELL", "LANG", "LANGUAGE", "LC_ALL", "LC_CTYPE", "LC_MESSAGES",
	"TERM", "TMPDIR", "TEMP", "TMP", "TZ",
	// Windows
	"SYSTEMROOT", "SYSTEMDRIVE", "COMSPEC", "PATHEXT", "WINDIR", "USERPROFILE", "APPDATA", "LOCALAPPDATA",
	"PROGRAMDATA", "PROGRAMFILES",
}

func isSystemEnvVar(name string) bool {
	return slices.ContainsFunc(systemEnvVars, func(s string) bool { return strings.EqualFold(s, name) })
}

// systemEnv returns the system variables of the provider's environment, and the cookies.
func systemEnv() []string {
	var out []string
	for _, kv := range providerEnviron() {
		if k, _, ok := strings.Cut(kv, "="); ok && (isSystemEnvVar(k) || isCookieVar(k)) {
			out = append(out, kv)
		}
	}
	return out
}

// emptiedEnv maps every other variable of the provider's environment to "", and each variable
// of Request.Env to the provider's value of a system variable or a cookie, or to "".
func emptiedEnv(req Request) map[string]string {
	out := map[string]string{}
	for _, kv := range providerEnviron() {
		k, _, ok := strings.Cut(kv, "=")
		if !ok || k == "" || isSystemEnvVar(k) || isCookieVar(k) || strings.ContainsAny(k, "=\x00") {
			continue
		}
		out[k] = ""
	}
	maps.Copy(out, requestEnvOverrides(req.Env, false))
	return out
}

// envList returns a map as KEY=VALUE entries, sorted by name.
func envList(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for _, k := range sortedKeys(m) {
		out = append(out, k+"="+m[k])
	}
	return out
}

func emptiedEnvList(req Request) []string { return envList(emptiedEnv(req)) }

// hookEnv is the environment of command hooks: Request.Env over the provider's
// environment, or over its system variables only.
func hookEnv(req Request) []string {
	var env []string
	if req.InheritEnvironment {
		env = providerEnviron()
	} else {
		env = systemEnv()
	}
	env = slices.Clone(env)
	for _, k := range sortedKeys(req.Env) {
		env = append(env, k+"="+req.Env[k])
	}
	if env == nil {
		env = []string{}
	}
	return env
}

// The Claude Code CLI of a coding-harness agent (harness.go) gets an environment that
// AgentRun builds itself, in this order:
//
//  1. the provider's environment, or, without Request.InheritEnvironment, its system
//     variables plus the Claude configuration locations of claudeKeptEnvVars when they are
//     set. They select the login (the macOS keychain entry and ~/.claude/.credentials.json
//     depend on CLAUDE_CONFIG_DIR) and are not credentials. The cookies of the supervisors
//     are kept too (descendants.go);
//  2. without the variables a parent Claude Code session sets (claudeSessionEnvVars), so the
//     CLI starts a top-level session also when `pulumi up` runs inside Claude Code;
//  3. without an inherited ANTHROPIC_API_KEY and ANTHROPIC_AUTH_TOKEN when Request.Env sets
//     CLAUDE_CODE_OAUTH_TOKEN: Claude Code prefers them to the token in print mode, which
//     would silently bill API usage instead of the subscription the token belongs to;
//  4. Request.Env, which wins over everything else. An explicit ANTHROPIC_API_KEY, also an
//     empty one that blanks an inherited key, is passed through;
//  5. PWD, set to the working directory, because exec does not update it.
//
// Check (CheckConfig) computes its billing and root warnings from the same environment.

// claudeKeptEnvVars are kept for the CLI when the environment is not inherited.
var claudeKeptEnvVars = []string{
	"CLAUDE_CONFIG_DIR", "CLAUDE_SECURESTORAGE_CONFIG_DIR",
	"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME",
	"IS_SANDBOX",
}

// claudeSessionEnvVars mark a process as part of a running Claude Code session, or carry
// that session's state: Claude Code sets them for the processes it starts (its trace
// context, its executable and temporary directory, the skills it invoked).
var claudeSessionEnvVars = []string{
	"CLAUDECODE", "CLAUDE_CODE_SESSION_ID", "CLAUDE_CODE_CHILD_SESSION", "CLAUDE_CODE_SESSION_ATTENDED", "CLAUDE_PID",
	"CLAUDE_CODE_ENTRYPOINT", "CLAUDE_CODE_MESSAGING_SOCKET", "CLAUDE_CODE_MESSAGING_TOKEN", "AI_AGENT", "CLAUDE_EFFORT",
	"TRACEPARENT", "CLAUDE_CODE_EXECPATH", "CLAUDE_CODE_TMPDIR", "CLAUDE_CODE_INVOKED_SKILLS",
}

const claudeOAuthTokenVar = "CLAUDE_CODE_OAUTH_TOKEN" //nolint:gosec // the name of a variable, not a credential

// claudeAPICredentialVars bill API usage instead of a Claude subscription.
var claudeAPICredentialVars = []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN"}

// claudeRoutingVars send Claude Code to another provider when they are true.
var claudeRoutingVars = []struct{ name, target string }{
	{"CLAUDE_CODE_USE_BEDROCK", "Amazon Bedrock"},
	{"CLAUDE_CODE_USE_VERTEX", "Google Vertex AI"},
	{"CLAUDE_CODE_USE_FOUNDRY", "Microsoft Foundry"},
}

// claudeEnvironment is the environment of a Claude Code CLI.
type claudeEnvironment struct {
	vars map[string]string
	// fromEnv marks the variables Request.Env sets.
	fromEnv map[string]bool
	// dropped lists the inherited credentials removed for an explicit CLAUDE_CODE_OAUTH_TOKEN.
	dropped []string
	// withheldToken reports that the provider's environment has a CLAUDE_CODE_OAUTH_TOKEN that
	// the CLI does not get: inheritEnvironment is false and env does not set the variable.
	withheldToken bool
}

// withheldTokenNote says why the CLI does not use the Pulumi CLI's CLAUDE_CODE_OAUTH_TOKEN.
const withheldTokenNote = "inheritEnvironment: false did not pass the Pulumi CLI's " + claudeOAuthTokenVar

// claudeEnv is the environment of the Claude Code CLI of a run working in dir. In a run
// child without an inherited environment, the provider's CLAUDE_CODE_OAUTH_TOKEN is not in
// the child's environment: the parent reports it (Request.withheldToken).
func claudeEnv(req Request, dir string) claudeEnvironment {
	e := newClaudeEnvironment(providerEnviron(), req.Env, req.InheritEnvironment, dir)
	if _, set := req.Env[claudeOAuthTokenVar]; req.withheldToken && !req.InheritEnvironment && !set {
		e.withheldToken = true
	}
	return e
}

// newClaudeEnvironment builds the environment of a Claude Code CLI from the provider's
// environment (environ), Request.Env (env) and Request.InheritEnvironment (inherit). An
// empty dir sets no PWD.
func newClaudeEnvironment(environ []string, env map[string]string, inherit bool, dir string) claudeEnvironment {
	e := claudeEnvironment{vars: map[string]string{}, fromEnv: map[string]bool{}}
	for _, kv := range environ {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || k == "" || strings.ContainsAny(k, "=\x00") {
			continue
		}
		if inherit || isSystemEnvVar(k) || slices.Contains(claudeKeptEnvVars, k) || isCookieVar(k) {
			e.vars[k] = v
		}
	}
	for _, k := range claudeSessionEnvVars {
		delete(e.vars, k)
	}
	if _, set := env[claudeOAuthTokenVar]; !inherit && !set {
		for _, kv := range environ {
			if v, ok := strings.CutPrefix(kv, claudeOAuthTokenVar+"="); ok && v != "" {
				e.withheldToken = true
			}
		}
	}
	if env[claudeOAuthTokenVar] != "" {
		for _, k := range claudeAPICredentialVars {
			if v, ok := e.vars[k]; ok {
				if _, explicit := env[k]; !explicit && v != "" {
					e.dropped = append(e.dropped, k)
				}
				delete(e.vars, k)
			}
		}
	}
	for k, v := range env {
		e.vars[k] = v
		e.fromEnv[k] = true
	}
	if dir != "" {
		e.vars["PWD"] = dir
		delete(e.fromEnv, "PWD")
	}
	return e
}

// usesLoginInsteadOfToken reports that the CLI falls back to the Claude login of the machine
// although the Pulumi CLI has a CLAUDE_CODE_OAUTH_TOKEN, which inheritEnvironment: false keeps
// from it: no API key, auth token or cloud provider of its environment takes over.
func (e claudeEnvironment) usesLoginInsteadOfToken() bool {
	return e.noKeySource().withheldToken
}

// list returns the environment as KEY=VALUE entries, sorted by name.
func (e claudeEnvironment) list() []string {
	out := make([]string, 0, len(e.vars))
	for _, k := range sortedKeys(e.vars) {
		out = append(out, k+"="+e.vars[k])
	}
	return out
}

// origin says where a variable of the environment comes from.
func (e claudeEnvironment) origin(name string) string {
	if e.fromEnv[name] {
		return "from env"
	}
	return "inherited from the environment of the Pulumi CLI"
}

// remedy says how to keep a variable of the environment away from the CLI.
func (e claudeEnvironment) remedy(name string) string {
	if e.fromEnv[name] {
		return "remove it from env to use the Claude login of the machine or CLAUDE_CODE_OAUTH_TOKEN"
	}
	return fmt.Sprintf("set inheritEnvironment: false, or blank it with env: {%s: \"\"}", name)
}

// billingWarnings explains the variables that make Claude Code use something other than a
// Claude subscription login, without their values.
func (e claudeEnvironment) billingWarnings() []string {
	var out []string
	for _, k := range claudeAPICredentialVars {
		if e.vars[k] == "" {
			continue
		}
		remedy := e.remedy(k)
		if !e.fromEnv[k] {
			remedy = "pass " + claudeOAuthTokenVar + " in env, " + remedy
		}
		out = append(out, fmt.Sprintf("claude-code would use %s (%s) instead of a Claude subscription login and bill API "+
			"usage to it; %s", k, e.origin(k), remedy))
	}
	for _, r := range claudeRoutingVars {
		if isTrue(e.vars[r.name]) {
			out = append(out, fmt.Sprintf("%s (%s) routes Claude Code to %s instead of a Claude subscription login; %s",
				r.name, e.origin(r.name), r.target, e.remedy(r.name)))
		}
	}
	if e.vars["ANTHROPIC_BASE_URL"] != "" {
		out = append(out, fmt.Sprintf("ANTHROPIC_BASE_URL (%s) sends Claude Code's requests to ANTHROPIC_BASE_URL; %s",
			e.origin("ANTHROPIC_BASE_URL"), e.remedy("ANTHROPIC_BASE_URL")))
	}
	for _, k := range e.dropped {
		out = append(out, fmt.Sprintf("inherited %s is not passed to claude because env sets %s", k, claudeOAuthTokenVar))
	}
	if e.usesLoginInsteadOfToken() {
		out = append(out, claudeOAuthTokenVar+" is set in the environment of the Pulumi CLI, but inheritEnvironment: false "+
			"keeps it from the claude CLI, which then uses the Claude login of this machine; pass the token in env (for "+
			"example from secret config) to use it, or set it to \"\" in env to use the login")
	}
	return out
}

// noKeySource is what Claude Code bills when it reports apiKeySource none, which only says
// that it uses no Anthropic API key: a cloud provider, ANTHROPIC_AUTH_TOKEN, or a Claude
// login (CLAUDE_CODE_OAUTH_TOKEN or the machine's), as the CLI's environment selects.
type noKeySource struct {
	// what names it, for example "Amazon Bedrock", and via the variable that selects a
	// cloud provider.
	what, via string
	// billed says that it is not a Claude subscription, and what is billed.
	billed string
	// gateway is set when ANTHROPIC_BASE_URL sends the requests elsewhere.
	gateway bool
	// withheldToken: the login is used because inheritEnvironment: false did not pass the
	// Pulumi CLI's CLAUDE_CODE_OAUTH_TOKEN.
	withheldToken bool
}

const claudeLogin = "the Claude login of this machine"

// noKeySource returns what the environment selects when Claude Code reports apiKeySource
// none, in the order Claude Code checks: a cloud provider, then ANTHROPIC_AUTH_TOKEN.
func (e claudeEnvironment) noKeySource() noKeySource {
	n := noKeySource{gateway: e.vars["ANTHROPIC_BASE_URL"] != ""}
	switch {
	case e.routing() != nil:
		r := e.routing()
		n.what, n.via, n.billed = r.target, r.name, "usage to that cloud account"
	case e.vars["ANTHROPIC_AUTH_TOKEN"] != "":
		n.what, n.billed = "ANTHROPIC_AUTH_TOKEN", "API usage"
	case e.vars[claudeOAuthTokenVar] != "":
		n.what = claudeOAuthTokenVar
	default:
		n.what = claudeLogin
		n.withheldToken = e.withheldToken && e.vars["ANTHROPIC_API_KEY"] == ""
	}
	return n
}

// isFlagEnvVar reports whether the value of a variable, not only whether it is set, decides
// how Claude Code runs: the routing variables and IS_SANDBOX.
func isFlagEnvVar(name string) bool {
	return name == "IS_SANDBOX" || slices.ContainsFunc(claudeRoutingVars, func(r struct{ name, target string }) bool {
		return r.name == name
	})
}

// routing returns the cloud provider a routing variable selects, if one does.
func (e claudeEnvironment) routing() *struct{ name, target string } {
	for i, r := range claudeRoutingVars {
		if isTrue(e.vars[r.name]) {
			return &claudeRoutingVars[i]
		}
	}
	return nil
}

// isTrue reports whether a flag variable is set to a true value, as Claude Code reads it.
func isTrue(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "0", "false", "no", "off":
		return false
	}
	return true
}

// sandboxed reports whether IS_SANDBOX=1 lets Claude Code skip permissions as root.
func (e claudeEnvironment) sandboxed() bool { return e.vars["IS_SANDBOX"] == "1" }

// path returns the PATH of the environment.
func (e claudeEnvironment) path() string { return e.vars["PATH"] }

// toolsetCreators returns docker-agent's toolset registry for one run: every toolset is
// guarded by the run's approval policy, and the process-starting toolsets get the environment
// described at the top of this file.
func toolsetCreators(req Request, policy *approvalPolicy) map[string]teamloader.ToolsetCreator {
	creators := toolsets.DefaultToolsetCreators()
	for _, typ := range []string{"shell", "script", "background_jobs"} {
		if c, ok := creators[typ]; ok {
			creators[typ] = withToolEnv(c, req, typ)
		}
	}
	if c, ok := creators["mcp"]; ok {
		creators["mcp"] = mcpWithEnv(c, req)
	}
	if _, ok := creators["lsp"]; ok {
		creators["lsp"] = lspWithEnv(req)
	}
	for typ, c := range creators {
		creators[typ] = guarded(c, policy)
	}
	return creators
}

func guarded(create teamloader.ToolsetCreator, policy *approvalPolicy) teamloader.ToolsetCreator {
	return func(ctx context.Context, ts latest.Toolset, parentDir string, rc *config.RuntimeConfig, name string) (tools.ToolSet, error) {
		set, err := create(ctx, ts, parentDir, rc, name)
		if err != nil || set == nil {
			return set, err
		}
		return guard(set, policy), nil
	}
}

// toolEnvOverrides are the variables a `shell`, `script` or `background_jobs` toolset gets
// through its own `env` on top of the process environment: Request.Env replaced by the
// provider's values or blanked, and without Request.InheritEnvironment every non-system
// variable blanked. docker-agent expands the toolset's `env` (`$NAME`), so a provider's value
// with a `$` is blanked too.
func toolEnvOverrides(req Request) map[string]string {
	var env map[string]string
	if req.InheritEnvironment {
		env = requestEnvOverrides(req.Env, true)
	} else {
		env = emptiedEnv(req)
	}
	for k, v := range env {
		if strings.Contains(v, "$") {
			env[k] = ""
		}
	}
	return env
}

// withToolEnv builds a `shell`, `script` or `background_jobs` toolset (typ) with the
// provider's environment when Request.InheritEnvironment is set (inheritedToolset). Otherwise
// it overrides variables through the toolset's own `env`, which docker-agent appends after
// os.Environ() (see toolEnvOverrides). The toolset's `env` still wins.
func withToolEnv(create teamloader.ToolsetCreator, req Request, typ string) teamloader.ToolsetCreator {
	return func(ctx context.Context, ts latest.Toolset, parentDir string, rc *config.RuntimeConfig, name string) (tools.ToolSet, error) {
		if req.InheritEnvironment {
			if set, ok, err := inheritedToolset(ctx, typ, ts, rc); ok {
				return set, err
			}
		}
		env := toolEnvOverrides(req)
		if len(env) == 0 {
			return create(ctx, ts, parentDir, rc, name)
		}
		maps.Copy(env, ts.Env)
		ts.Env = env
		return create(ctx, ts, parentDir, rc, name)
	}
}

// inheritedToolset builds a `shell`, `script` or `background_jobs` toolset as docker-agent's
// CreateToolSet and CreateScriptToolSet do (keep them in step when docker-agent is upgraded),
// with providerEnviron() in place of os.Environ(): Request.Env then reaches none of its
// processes, and a variable that only Request.Env sets is unset for them, not empty. ok is
// false for an option that only docker-agent's constructor can set (shell's sudo_askpass,
// background_jobs' recall): the caller then overrides the variables instead.
func inheritedToolset(ctx context.Context, typ string, ts latest.Toolset, rc *config.RuntimeConfig,
) (set tools.ToolSet, ok bool, err error) {
	what := "toolset's"
	switch {
	case typ == "shell" && (ts.SudoAskpass == nil || !*ts.SudoAskpass):
	case typ == "background_jobs" && (ts.Recall == nil || !*ts.Recall):
	case typ == "script":
		if len(ts.Shell) == 0 {
			return nil, true, errors.New("shell is required for script toolset")
		}
		what = "tool's"
	default:
		return nil, false, nil
	}
	env, err := environment.ExpandAll(ctx, environment.ToValues(ts.Env), rc.EnvProvider())
	if err != nil {
		return nil, true, fmt.Errorf("failed to expand the %s environment variables: %w", what, err)
	}
	// The toolset's `env` wins on a key collision, as in docker-agent (exec.Cmd keeps the last).
	env = append(providerEnviron(), env...)
	switch typ {
	case "shell":
		return shell.New(env, rc), true, nil
	case "background_jobs":
		return backgroundjobs.New(env, rc), true, nil
	}
	script, err := shell.NewScript(ts.Shell, env)
	if err != nil {
		return nil, true, err
	}
	return script, true, nil
}

// mcpWithEnv starts stdio MCP servers with the environment docker-agent's mcp.CreateToolSet
// gives them, with the provider's environment instead of the process's: the toolset's `env`
// followed by the provider's environment, or without Request.InheritEnvironment the system
// variables followed by the toolset's `env`. Other MCP toolsets (remote servers, `docker:`
// references) are created by docker-agent.
func mcpWithEnv(create teamloader.ToolsetCreator, req Request) teamloader.ToolsetCreator {
	return func(ctx context.Context, ts latest.Toolset, parentDir string, rc *config.RuntimeConfig, name string) (tools.ToolSet, error) {
		if ts.Ref != "" || ts.Command == "" {
			return create(ctx, ts, parentDir, rc, name)
		}
		cwd := workingdir.Resolve(ts.WorkingDir, rc.WorkingDir)
		if ts.WorkingDir != "" {
			if err := workingdir.CheckDirExists(cwd, "mcp"); err != nil {
				return nil, err
			}
		}
		command, err := toolinstall.EnsureCommand(ctx, ts.Command, ts.Version)
		if err != nil {
			slog.WarnContext(ctx, "MCP command not yet available, will retry", "command", ts.Command, "error", err)
			command = ts.Command
		}
		env, err := environment.ExpandAll(ctx, environment.ToValues(ts.Env), rc.EnvProvider())
		if err != nil {
			return nil, fmt.Errorf("failed to expand the tool's environment variables: %w", err)
		}
		if req.InheritEnvironment {
			env = append(env, providerEnviron()...)
		} else {
			env = append(systemEnv(), env...)
		}
		env = toolinstall.PrependBinDirToEnv(env)
		return mcp.NewToolsetCommand(ts.Name, command, ts.Args, env, cwd, lifecycle.PolicyFromConfig(ts.Name, ts.Lifecycle)), nil
	}
}

// lspWithEnv mirrors docker-agent's lsp.CreateToolSet. docker-agent starts the server with
// os.Environ() followed by this environment: with Request.InheritEnvironment, Request.Env
// blanked, the toolset's `env` and the provider's environment (as docker-agent orders them);
// without it, the non-system variables emptied, the system variables and the toolset's `env`.
func lspWithEnv(req Request) teamloader.ToolsetCreator {
	return func(ctx context.Context, ts latest.Toolset, _ string, rc *config.RuntimeConfig, _ string) (tools.ToolSet, error) {
		command, err := toolinstall.EnsureCommand(ctx, ts.Command, ts.Version)
		if err != nil {
			return nil, fmt.Errorf("resolving command %q: %w", ts.Command, err)
		}
		env, err := environment.ExpandAll(ctx, environment.ToValues(ts.Env), rc.EnvProvider())
		if err != nil {
			return nil, fmt.Errorf("failed to expand the tool's environment variables: %w", err)
		}
		if req.InheritEnvironment {
			env = slices.Concat(envList(requestEnvOverrides(req.Env, false)), env, providerEnviron())
		} else {
			env = slices.Concat(emptiedEnvList(req), systemEnv(), env)
		}
		env = toolinstall.PrependBinDirToEnv(env)
		cwd := workingdir.Resolve(ts.WorkingDir, rc.WorkingDir)
		if ts.WorkingDir != "" {
			if err := workingdir.CheckDirExists(cwd, "lsp"); err != nil {
				return nil, err
			}
		}
		tool := lsp.New(command, ts.Args, env, cwd, lifecycle.PolicyFromConfig(ts.Name, ts.Lifecycle))
		if len(ts.FileTypes) > 0 {
			tool.SetFileTypes(ts.FileTypes)
		}
		return tool, nil
	}
}
