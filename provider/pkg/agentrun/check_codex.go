// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package agentrun

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/docker/docker-agent/pkg/codingharness"
)

// The checks of codex agents (CheckConfig in a preview, and Run before the first CLI starts):
//
//   - problems: a forced_login_method that would make Codex delete its stored login, and a
//     CODEX_HOME that does not exist or is no directory;
//   - warnings: what codex agents can do under the toolApproval in effect (W1), the
//     credentials Codex would bill (W2) and a missing login, a CODEX_HOME in the temporary
//     directory or given as a relative path, a machine where Codex's sandbox may not start
//     (under toolApproval deny), a delete prompt under deny (W7), and per agent an instruction
//     (W4) or structured_output (W5) the harness does not get or enforces, and a model the
//     harness inherits from the agent (W6).

// Where the sandbox heuristics of the preview look (Linux), and the temporary directory that
// CODEX_HOME must not be in; tests change them.
var (
	procSysDir       = "/proc/sys"
	containerMarkers = []string{"/.dockerenv", "/run/.containerenv"}
	initCgroupFile   = "/proc/1/cgroup"
	tempDirOf        = os.TempDir
)

// codex returns the codex agents.
func (h harnessAgents) codex() harnessAgents {
	return slices.DeleteFunc(slices.Clone(h), func(a harnessAgent) bool { return a.typ != codingharness.TypeCodex })
}

// supported returns the agents of the harness types AgentRun runs.
func (h harnessAgents) supported() harnessAgents {
	return slices.DeleteFunc(slices.Clone(h), func(a harnessAgent) bool { return !isHarnessSupported(a.typ) })
}

// codexCheckEnv is the environment the preview assumes for the codex CLI: the Pulumi CLI's
// environment (when inherited) and env, with the known value of CODEX_HOME.
func codexCheckEnv(opts CheckOptions) codexEnvironment {
	env := map[string]string{}
	for k, set := range opts.EnvKeys {
		v, known := opts.EnvValues[k]
		switch {
		case !set:
			env[k] = ""
		case k == codexHomeVar && known:
			env[k] = v
		default:
			env[k] = "set"
		}
	}
	e := newCodexEnvironment(os.Environ(), env, opts.InheritEnvironment, "")
	if _, known := opts.EnvValues[codexHomeVar]; opts.EnvUnknown || opts.EnvKeys[codexHomeVar] && !known {
		e.homeUnknown = true
	}
	return e
}

// codexChecks returns the problems and warnings of a configuration's codex agents for a
// preview.
func codexChecks(agents harnessAgents, opts CheckOptions) (problems, warnings []string) {
	cx := agents.codex()
	if len(cx) == 0 {
		return nil, nil
	}
	ce := codexCheckEnv(opts)
	auth := codexAuthOf(ce, false)
	if problems := codexProblems(auth, opts.EnvUnknown); len(problems) > 0 {
		return problems, nil
	}
	warnings = cx.codexRunWarnings(ce, auth, opts.ToolApprovalDeny, opts.EnvUnknown, opts.HideNames)
	warnings = append(warnings, codexHomeWarnings(auth.home)...)
	if opts.ToolApprovalDeny {
		if w := codexSandboxDoubt(); w != "" {
			warnings = append(warnings, w)
		}
		if opts.DeletePrompt {
			warnings = append(warnings, "deletePrompt runs codex in its workspace-write sandbox (toolApproval deny): its "+
				"commands can write only in workingDir and the temporary directories, without network, which may not be "+
				"enough to clean up; set toolApproval: allow only on an externally isolated machine")
		}
	}
	for _, a := range cx {
		warnings = append(warnings, a.codexAgentWarnings(opts.HideNames)...)
	}
	return nil, warnings
}

// codexProblems are the reasons a codex CLI must not start: the forced-login logout, and a
// CODEX_HOME that Codex refuses. skip skips them when env is not known (a preview).
func codexProblems(auth codexAuth, skip bool) []string {
	if skip {
		return nil
	}
	var out []string
	if auth.conflict != "" {
		out = append(out, auth.conflict)
	}
	if h := auth.home; h.set && h.dir != "" && !h.unknown {
		fi, err := os.Stat(h.dir)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			out = append(out, fmt.Sprintf("CODEX_HOME points to %s, which does not exist, so codex would not start: create "+
				"it and log in with CODEX_HOME=%s codex login, or remove CODEX_HOME", h.dir, h.dir))
		case err == nil && !fi.IsDir():
			out = append(out, fmt.Sprintf("CODEX_HOME points to %s, which is not a directory, so codex would not start",
				h.dir))
		}
	}
	return out
}

// codexHomeWarnings warn about a CODEX_HOME that Codex resolves against workingDir, and one in
// the temporary directory.
func codexHomeWarnings(h codexHome) []string {
	if h.unknown || !h.set {
		return nil
	}
	if h.dir == "" {
		return []string{fmt.Sprintf("CODEX_HOME (%s) is a relative path, which codex resolves against workingDir; use an "+
			"absolute path", h.value)}
	}
	tmp := evalSymlinks(tempDirOf())
	if d := evalSymlinks(h.dir); d == tmp || strings.HasPrefix(d, tmp+string(filepath.Separator)) {
		return []string{fmt.Sprintf("CODEX_HOME (%s) is in the temporary directory, where codex refuses to create its "+
			"helper commands (such as its sandbox launcher on Linux) and runs without them; use a directory outside %s",
			h.dir, tempDirOf())}
	}
	return nil
}

// codexRunWarnings are the warnings about running the codex agents with an environment: W1,
// W2 with the information that inherited credentials are dropped, and a missing login. Run
// reports them too, for a configuration no preview checked (Request.AnnounceHarness).
func (h harnessAgents) codexRunWarnings(ce codexEnvironment, auth codexAuth, deny, envUnknown, hideNames bool) []string {
	cx := h.codex()
	if len(cx) == 0 {
		return nil
	}
	tools := fmt.Sprintf("coding-harness agents (%s) run their own tools: the codex CLI runs shell commands, file edits, "+
		"MCP tools and web searches on the machine that runs pulumi up, in workingDir or a temporary directory, with its "+
		"user's Codex login, config.toml (MCP servers, profiles, rules, hooks) and AGENTS.md files, and without "+
		"docker-agent's permissions", cx.list(hideNames))
	if deny {
		tools += "; toolApproval deny runs codex's commands in its workspace-write sandbox, which limits writes and " +
			"network but not reads: they can write only in workingDir (not its .git, .codex or .agents), the temporary " +
			"directories and writable_roots of config.toml, and reach the network only when config.toml allows it, but " +
			"they can read every file the user can, and the model sees what they read; MCP servers, hooks and notify " +
			"programs (also those of a trusted project's .codex/) run outside the sandbox, and hosted web search stays on"
	} else {
		tools += "; toolApproval allow (also one not known yet) turns off Codex's sandbox too " +
			"(--dangerously-bypass-approvals-and-sandbox), so they can do everything the user can: use it only on an " +
			"externally isolated machine or in a container"
	}
	out := []string{tools}
	if envUnknown {
		return out
	}
	if auth.billing != "" {
		out = append(out, auth.billing)
	}
	for _, k := range ce.dropped {
		out = append(out, fmt.Sprintf("inherited %s is not passed to codex because env sets %s", k, codexAccessTokenVar))
	}
	if auth.missing {
		if strings.EqualFold(auth.cfg.credentialsStore, "ephemeral") {
			out = append(out, "the config.toml of codex sets cli_auth_credentials_store = \"ephemeral\", so codex keeps no "+
				"login: pass CODEX_API_KEY (bills API usage) or CODEX_ACCESS_TOKEN in env")
		} else {
			out = append(out, fmt.Sprintf("found no Codex login for the user that runs pulumi up (%s does not exist): run "+
				"codex login as that user (codex login --device-auth on a machine without a browser), preferably into a "+
				"CODEX_HOME of AgentRun's own (env: {CODEX_HOME: ...}), or pass CODEX_API_KEY in env (bills API usage); a "+
				"login in the OS keyring counts only when config.toml sets cli_auth_credentials_store", auth.authPath))
		}
	}
	return out
}

// codexAgentWarnings are W4, W5 and W6 for a codex agent.
func (a harnessAgent) codexAgentWarnings(hideNames bool) []string {
	var out []string
	if a.instruction {
		out = append(out, agentRef(a.name, "instruction", hideNames)+" is not sent to the coding harness: docker-agent "+
			"sends only the prompt (to a sub-agent: its task); put the instructions into the prompt or into an AGENTS.md "+
			"in workingDir")
	}
	if a.structuredOutput {
		if a.strict {
			out = append(out, agentRef(a.name, "structured_output", hideNames)+" is enforced by codex --output-schema in "+
				"OpenAI's strict mode: every property must be required and additionalProperties false, or the turn fails")
		} else {
			out = append(out, agentRef(a.name, "structured_output", hideNames)+" is not enforced for codex: AgentRun "+
				"decodes Codex's final message as JSON (a surrounding Markdown code fence is removed); set "+
				"structured_output.strict: true to have Codex enforce the schema, or ask for JSON in the prompt")
		}
	}
	if a.inheritsModel {
		model := fmt.Sprintf(" %q", a.model)
		if hideNames {
			model = ""
		}
		out = append(out, fmt.Sprintf("%s has no model, so docker-agent passes the agent's model%s to codex --model, which "+
			"takes Codex model names only; set harness.model (for example gpt-6.1-sol), or remove the agent's model to use "+
			"Codex's default", agentRef(a.name, "harness", hideNames), model))
	}
	return out
}

// codexSandboxDoubt warns, on Linux, when Codex's sandbox (bubblewrap in a user namespace)
// looks unlikely to start: user namespaces disabled or restricted, or a container. These are
// heuristics; the run checks the sandbox itself before the first codex turn.
func codexSandboxDoubt() string {
	if harnessOS != "linux" {
		return ""
	}
	read := func(name string) string {
		b, _ := os.ReadFile(filepath.Join(procSysDir, name))
		return strings.TrimSpace(string(b))
	}
	var why []string
	if read("user/max_user_namespaces") == "0" {
		why = append(why, "user.max_user_namespaces is 0")
	}
	if read("kernel/unprivileged_userns_clone") == "0" {
		why = append(why, "kernel.unprivileged_userns_clone is 0")
	}
	if read("kernel/apparmor_restrict_unprivileged_userns") == "1" {
		why = append(why, "AppArmor restricts unprivileged user namespaces")
	}
	if inContainer() {
		why = append(why, "the provider runs in a container")
	}
	if len(why) == 0 {
		return ""
	}
	return fmt.Sprintf("codex's sandbox may not start on this machine (%s): under toolApproval deny AgentRun checks it "+
		"before the first codex turn and fails the run if it cannot start; %s", strings.Join(why, "; "), codexSandboxAdvice)
}

// inContainer reports whether the provider seems to run in a container.
func inContainer() bool {
	for _, m := range containerMarkers {
		if _, err := os.Stat(m); err == nil {
			return true
		}
	}
	b, _ := os.ReadFile(initCgroupFile)
	return containsAny(string(b), "docker", "kubepods", "containerd", "libpod", "lxc")
}
