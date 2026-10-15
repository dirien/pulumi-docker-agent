// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package agentrun

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// The codex CLI of a coding-harness agent (harness_codex.go) gets an environment that AgentRun
// builds itself, as the Claude Code CLI does (procenv.go), in this order:
//
//  1. the provider's environment, or, without Request.InheritEnvironment, its system variables
//     plus codexKeptEnvVars when they are set: CODEX_HOME selects the login and config.toml,
//     the proxy and CA variables let Codex reach its backend from a corporate network, and the
//     desktop session variables let it read a login kept in the Linux keyring. None of them is
//     a credential. The cookies of the supervisors are kept too (descendants.go);
//  2. without the variables a parent Codex or Claude Code session sets for the processes it
//     starts, and without the trace context (parentSessionEnvVars), so the CLI starts a session
//     of its own, outside the parent's trace, also when `pulumi up` runs inside Codex or Claude
//     Code;
//  3. without an inherited RUST_LOG unless Request.Env sets it: at trace level Codex logs the
//     whole environment of every command it starts to stderr;
//  4. without an inherited CODEX_API_KEY when Request.Env sets CODEX_ACCESS_TOKEN: Codex
//     prefers the key, which would bill API usage instead of the account the token belongs
//     to;
//  5. Request.Env, which wins over everything else. An explicit CODEX_API_KEY, also an empty
//     one that blanks an inherited key, is passed through;
//  6. PWD, set to the working directory.
//
// Codex itself also loads $CODEX_HOME/.env (every variable but CODEX_*), which reaches the
// commands it runs: inheritEnvironment: false does not keep those away from codex.

// codexKeptEnvVars are kept for the codex CLI when the environment is not inherited.
var codexKeptEnvVars = []string{
	"CODEX_HOME", "CODEX_CA_CERTIFICATE",
	"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "no_proxy", "all_proxy",
	"SSL_CERT_FILE", "SSL_CERT_DIR",
	"DBUS_SESSION_BUS_ADDRESS", "XDG_RUNTIME_DIR",
}

// codexSessionEnvVars mark a process as started by a running Codex session, or carry that
// session's state: its thread, sandbox and permission profile, its package manager, its
// shell escalation socket and patch helpers, and the tokens of its internal servers.
var codexSessionEnvVars = []string{
	"CODEX_THREAD_ID", "CODEX_SESSION_ID", "CODEX_VERSION", "CODEX_PERMISSION_PROFILE", "CODEX_SANDBOX",
	"CODEX_SANDBOX_NETWORK_DISABLED", "CODEX_MANAGED_BY_NPM", "CODEX_MANAGED_BY_BUN", "CODEX_MANAGED_BY_PNPM",
	"CODEX_MANAGED_BY_VITE_PLUS", "CODEX_MANAGED_PACKAGE_ROOT", "CODEX_INTERNAL_ORIGINATOR_OVERRIDE",
	"CODEX_ESCALATE_SOCKET", "EXEC_WRAPPER", "CODEX_APPLY_PATCH_PRESERVE_LINE_ENDINGS",
	"CODEX_EXEC_SERVER_NOISE_AUTH_TOKEN", "NODE_REPL_AUTH_TOKEN", "CODEX_GUARDIAN_DECISIONS_API_KEY",
}

const (
	codexAPIKeyVar      = "CODEX_API_KEY"      //nolint:gosec // the name of a variable, not a credential
	codexAccessTokenVar = "CODEX_ACCESS_TOKEN" //nolint:gosec // the name of a variable, not a credential
	codexHomeVar        = "CODEX_HOME"
)

// codexWorkloadIdentityVars select Codex's workload identity login (OIDC federation).
var codexWorkloadIdentityVars = []string{"OPENAI_IDENTITY_TOKEN_FILE", "OPENAI_FEDERATION_RULE_ID"}

// codexEnvironment is the environment of a codex CLI.
type codexEnvironment struct {
	vars map[string]string
	// fromEnv marks the variables Request.Env sets.
	fromEnv map[string]bool
	// dropped lists the inherited credentials removed for an explicit CODEX_ACCESS_TOKEN.
	dropped []string
	// dir is the working directory, against which Codex resolves a relative CODEX_HOME; empty
	// in a preview.
	dir string
	// homeUnknown: in a preview, env may set CODEX_HOME to a value that is not known yet.
	homeUnknown bool
}

// codexEnv is the environment of the codex CLI of a run working in dir.
func codexEnv(req Request, dir string) codexEnvironment {
	return newCodexEnvironment(providerEnviron(), req.Env, req.InheritEnvironment, dir)
}

// newCodexEnvironment builds the environment of a codex CLI from the provider's environment
// (environ), Request.Env (env) and Request.InheritEnvironment (inherit). An empty dir sets no
// PWD.
func newCodexEnvironment(environ []string, env map[string]string, inherit bool, dir string) codexEnvironment {
	e := codexEnvironment{vars: map[string]string{}, fromEnv: map[string]bool{}, dir: dir}
	for _, kv := range environ {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || k == "" || strings.ContainsAny(k, "=\x00") {
			continue
		}
		if inherit || isSystemEnvVar(k) || slices.Contains(codexKeptEnvVars, k) || isCookieVar(k) {
			e.vars[k] = v
		}
	}
	for _, k := range parentSessionEnvVars() {
		delete(e.vars, k)
	}
	delete(e.vars, "RUST_LOG")
	if strings.TrimSpace(env[codexAccessTokenVar]) != "" {
		if v, ok := e.vars[codexAPIKeyVar]; ok {
			if _, explicit := env[codexAPIKeyVar]; !explicit && strings.TrimSpace(v) != "" {
				e.dropped = append(e.dropped, codexAPIKeyVar)
			}
			delete(e.vars, codexAPIKeyVar)
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

// list returns the environment as KEY=VALUE entries, sorted by name.
func (e codexEnvironment) list() []string { return envList(e.vars) }

// path returns the PATH of the environment.
func (e codexEnvironment) path() string { return e.vars["PATH"] }

// set reports whether a variable is set to a value Codex uses: Codex trims the values of its
// credential variables and ignores empty ones.
func (e codexEnvironment) set(name string) bool { return strings.TrimSpace(e.vars[name]) != "" }

// origin says where a variable of the environment comes from.
func (e codexEnvironment) origin(name string) string {
	if e.fromEnv[name] {
		return "from env"
	}
	return "inherited from the environment of the Pulumi CLI"
}

// remedy says how to keep a variable of the environment away from the CLI.
func (e codexEnvironment) remedy(name string) string {
	if e.fromEnv[name] {
		return "remove it from env to use the Codex login of the machine"
	}
	return fmt.Sprintf("set inheritEnvironment: false, or blank it with env: {%s: \"\"}, to use the Codex login of the "+
		"machine", name)
}

// codexHome is the CODEX_HOME a codex CLI uses.
type codexHome struct {
	// dir is CODEX_HOME, absolute, or ~/.codex when the variable is not set; "" when it is
	// relative and the working directory is not known (a preview).
	dir string
	// set reports that CODEX_HOME selects it; value is the variable's value then.
	set   bool
	value string
	// unknown: env sets CODEX_HOME to a value the preview does not know.
	unknown bool
}

// home returns the CODEX_HOME of the environment, as Codex resolves it: the variable when it
// is not empty (a relative value against the working directory, Codex's own), else
// $HOME/.codex.
func (e codexEnvironment) home() codexHome {
	if e.homeUnknown {
		return codexHome{unknown: true}
	}
	if v := e.vars[codexHomeVar]; v != "" {
		h := codexHome{set: true, value: v, dir: v}
		if !filepath.IsAbs(v) {
			h.dir = ""
			if e.dir != "" {
				h.dir = filepath.Join(e.dir, v)
			}
		}
		return h
	}
	home := e.vars["HOME"]
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	if home == "" {
		return codexHome{}
	}
	return codexHome{dir: filepath.Join(home, ".codex")}
}
