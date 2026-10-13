// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package agentrun

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"sync/atomic"
)

// The environment of a run child (child.go). The model SDKs that docker-agent builds read
// their variables (API keys, base URLs, AWS and Google credentials, headers) from the process
// environment with os.Getenv, not from docker-agent's environment provider. A run child is a
// process of its own, so its environment can be exactly what AgentRun promises:
//
//   - the parent starts the child with a base environment only: with
//     Request.InheritEnvironment the provider's whole environment, without it the variables of
//     baseEnvVars that the provider's environment sets. Request.Env is never part of it, so it
//     never shows in /proc/<pid>/environ (Linux) or KERN_PROCARGS2 (macOS), which other
//     processes of the same user can read: builds are CGO_ENABLED=0, so os.Setenv changes the
//     Go runtime's copy of the environment only;
//   - the child keeps a copy of that environment (providerEnviron), then sets every value of
//     Request.Env, which it received over stdin, with os.Setenv before docker-agent reads
//     anything: the SDKs see Request.Env over the base environment;
//   - everything that means "the provider's environment" for the processes the configuration
//     starts uses the copy (procenv.go), so those processes see what they saw when runs were
//     in process: Request.Env reaches command hooks, the Claude Code CLI and, through their own
//     `env`, toolsets, but not shell, script, background_jobs, LSP or stdio MCP processes.
//     docker-agent starts some processes with its own environment, which AgentRun cannot
//     change: `docker:` MCP servers (the Docker MCP Gateway), filesystem `post_edit` commands,
//     the `open_url` browser launcher and the on-demand tool installation. They see the
//     child's environment, Request.Env included.

// baseEnvVars are the variables of the provider's environment that a run child gets without
// Request.InheritEnvironment, besides systemEnvVars. They are no credentials, and the child's
// own code (docker-agent, the Go runtime) needs them. They are not system variables: the
// processes AgentRun controls still get them emptied (procenv.go).
var baseEnvVars = slices.Concat(
	// Claude Code's configuration locations (they select the login), and its root opt-in.
	claudeKeptEnvVars,
	// Codex's configuration location (it selects the login) and its CA certificate.
	[]string{"CODEX_HOME", "CODEX_CA_CERTIFICATE"},
	// The network: proxies (Go and docker-agent read both cases) and CA certificates.
	[]string{
		"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "no_proxy", "all_proxy",
		"SSL_CERT_FILE", "SSL_CERT_DIR",
	},
	// The Docker connection: Docker Model Runner, `docker:` MCP servers, OCI sub-agent references.
	[]string{"DOCKER_HOST", "DOCKER_CONTEXT", "DOCKER_CONFIG", "DOCKER_CERT_PATH", "DOCKER_TLS_VERIFY", "DOCKER_TLS"},
	// The provider's policy for docker-agent: tool installation (an opt-out must hold), the
	// tools directory, the Docker Desktop proxy, the skills kit, telemetry and the sandbox
	// detection (SQLite journal mode).
	[]string{
		"DOCKER_AGENT_AUTO_INSTALL", "DOCKER_AGENT_TOOLS_DIR", "DOCKER_AGENT_DISABLE_DESKTOP_PROXY",
		"DOCKER_AGENT_KIT_DIR", "TELEMETRY_ENABLED", "TELEMETRY_TAGS", "SANDBOX_VM_ID",
	},
	// The Go runtime of the child (for example a TLS workaround in GODEBUG, a memory limit).
	[]string{"GODEBUG", "GOMAXPROCS", "GOMEMLIMIT", "GOGC"},
	// The Go module settings of the tool installation, which runs `go install` with the child's
	// environment (a corporate module proxy, private modules, the toolchain and the caches).
	// GITHUB_TOKEN and GH_TOKEN, which it reads for GitHub's API, are credentials: they come
	// from env.
	[]string{
		"GOPROXY", "GOFLAGS", "GONOSUMDB", "GONOPROXY", "GOPRIVATE", "GOSUMDB", "GOINSECURE", "GOTOOLCHAIN", "GOENV",
		"GOPATH", "GOMODCACHE", "GOCACHE", "GOROOT", "GOAUTH", "GOVCS", "GOTMPDIR",
	},
	// The desktop session: Docker credential helpers (secretservice).
	[]string{"DBUS_SESSION_BUS_ADDRESS", "XDG_RUNTIME_DIR"},
)

// isBaseEnvVar reports whether a run child gets the variable without
// Request.InheritEnvironment.
func isBaseEnvVar(name string) bool {
	return isSystemEnvVar(name) || slices.ContainsFunc(baseEnvVars, func(s string) bool { return strings.EqualFold(s, name) })
}

// childExecEnv returns the environment a run child starts with, from the provider's
// environment: all of it with inherit, else its base variables. A GOTRACEBACK that makes the
// Go runtime dump core (crash, wer) is dropped: a dump would hold Request.Env, the prompt and
// the answer. The parent adds the child's cookie.
func childExecEnv(environ []string, inherit bool) []string {
	out := make([]string, 0, len(environ))
	for _, kv := range environ {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || k == "" {
			continue
		}
		if strings.EqualFold(k, "GOTRACEBACK") && slices.Contains([]string{"crash", "wer"}, strings.ToLower(v)) {
			continue
		}
		if inherit || isBaseEnvVar(k) {
			out = append(out, kv)
		}
	}
	return out
}

// childEnviron is the environment a run child started with, before Request.Env was set; nil
// outside a run child.
var childEnviron atomic.Pointer[[]string]

// providerEnviron returns the provider's environment, as the processes a configuration starts
// see it: in a run child the environment it started with (Request.Env is not in it), else the
// process environment.
func providerEnviron() []string {
	if e := childEnviron.Load(); e != nil {
		return slices.Clone(*e)
	}
	return os.Environ()
}

// providerEnvMap returns providerEnviron as a map.
func providerEnvMap() map[string]string {
	out := map[string]string{}
	for _, kv := range providerEnviron() {
		if k, v, ok := strings.Cut(kv, "="); ok && k != "" {
			out[k] = v
		}
	}
	return out
}

// applyRequestEnv keeps a copy of the process environment as the provider's environment and
// sets every variable of env in the process environment, for the model SDKs. A name that
// cannot be a variable (empty, or with "=" or NUL) or a value with NUL is not set, and debug
// says which name; docker-agent still resolves it from Request.Env.
func applyRequestEnv(env map[string]string, debug func(string)) {
	environ := os.Environ()
	childEnviron.Store(&environ)
	for _, k := range sortedKeys(env) {
		if k == "" || strings.ContainsAny(k, "=\x00") || strings.ContainsRune(env[k], 0) {
			debug(fmt.Sprintf("env %q is not set in the environment of the agent run: the name or the value is not "+
				"valid for an environment variable", k))
			continue
		}
		if err := os.Setenv(k, env[k]); err != nil {
			debug(fmt.Sprintf("env %q is not set in the environment of the agent run: %v", k, err))
		}
	}
}

// requestEnvOverrides returns, for each variable of Request.Env, the value that the processes
// AgentRun controls get instead of it: the provider's value when inherit is set, or when the
// variable is kept for them anyway (a system variable or a cookie), else "". In a run child
// the process environment holds Request.Env, which these processes would otherwise inherit.
func requestEnvOverrides(env map[string]string, inherit bool) map[string]string {
	if len(env) == 0 {
		return map[string]string{}
	}
	provider := providerEnvMap()
	out := make(map[string]string, len(env))
	for k := range env {
		v, ok := provider[k]
		if ok && (inherit || isSystemEnvVar(k) || isCookieVar(k)) {
			out[k] = v
		} else {
			out[k] = ""
		}
	}
	return out
}
