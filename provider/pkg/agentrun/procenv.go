// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package agentrun

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/docker/docker-agent/pkg/config"
	"github.com/docker/docker-agent/pkg/config/latest"
	"github.com/docker/docker-agent/pkg/environment"
	"github.com/docker/docker-agent/pkg/teamloader"
	"github.com/docker/docker-agent/pkg/teamloader/toolsets"
	"github.com/docker/docker-agent/pkg/toolinstall"
	"github.com/docker/docker-agent/pkg/tools"
	"github.com/docker/docker-agent/pkg/tools/builtin/lsp"
	"github.com/docker/docker-agent/pkg/tools/lifecycle"
	"github.com/docker/docker-agent/pkg/tools/mcp"
	"github.com/docker/docker-agent/pkg/tools/workingdir"
)

// The processes docker-agent starts for a configuration inherit the provider's whole
// environment: docker-agent builds their environment from os.Environ(). With
// Request.InheritEnvironment false, AgentRun instead gives the processes it can control
// only the system variables below, plus what the configuration passes explicitly
// (a toolset's `env`, and Request.Env for command hooks):
//
//   - command hooks: runtime.WithEnv;
//   - `shell`, `script` and `background_jobs` toolsets: docker-agent appends the toolset's
//     `env` after os.Environ() and the last value wins, so every other variable is
//     overridden with an empty value;
//   - stdio MCP servers (`command`): started with exactly that environment;
//   - LSP servers (`command`): like `shell`, other variables are emptied.
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

// systemEnv returns the system variables of the provider's environment.
func systemEnv() []string {
	var out []string
	for _, kv := range os.Environ() {
		if k, _, ok := strings.Cut(kv, "="); ok && isSystemEnvVar(k) {
			out = append(out, kv)
		}
	}
	return out
}

// emptiedEnv maps every other variable of the provider's environment to "".
func emptiedEnv() map[string]string {
	out := map[string]string{}
	for _, kv := range os.Environ() {
		k, _, ok := strings.Cut(kv, "=")
		if !ok || k == "" || isSystemEnvVar(k) || strings.ContainsAny(k, "=\x00") {
			continue
		}
		out[k] = ""
	}
	return out
}

func emptiedEnvList() []string {
	m := emptiedEnv()
	out := make([]string, 0, len(m))
	for _, k := range sortedKeys(m) {
		out = append(out, k+"=")
	}
	return out
}

// hookEnv is the environment of command hooks: Request.Env over the provider's
// environment, or over its system variables only.
func hookEnv(req Request) []string {
	var env []string
	if req.InheritEnvironment {
		env = os.Environ()
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

// toolsetCreators returns docker-agent's toolset registry for one run: every toolset is
// guarded by the run's approval policy, and without an inherited environment the
// process-starting toolsets get a scrubbed environment.
func toolsetCreators(req Request, policy *approvalPolicy) map[string]teamloader.ToolsetCreator {
	creators := toolsets.DefaultToolsetCreators()
	if !req.InheritEnvironment {
		for _, typ := range []string{"shell", "script", "background_jobs"} {
			if c, ok := creators[typ]; ok {
				creators[typ] = withEmptiedEnv(c)
			}
		}
		if c, ok := creators["mcp"]; ok {
			creators["mcp"] = mcpWithSystemEnv(c)
		}
		if _, ok := creators["lsp"]; ok {
			creators["lsp"] = lspWithEmptiedEnv
		}
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

// withEmptiedEnv overrides every non-system variable with "" through the toolset's own
// `env`, which docker-agent appends after os.Environ(). The toolset's `env` still wins.
func withEmptiedEnv(create teamloader.ToolsetCreator) teamloader.ToolsetCreator {
	return func(ctx context.Context, ts latest.Toolset, parentDir string, rc *config.RuntimeConfig, name string) (tools.ToolSet, error) {
		env := emptiedEnv()
		maps.Copy(env, ts.Env)
		ts.Env = env
		return create(ctx, ts, parentDir, rc, name)
	}
}

// mcpWithSystemEnv starts stdio MCP servers with the system variables and the toolset's
// `env`, mirroring docker-agent's mcp.CreateToolSet. Other MCP toolsets (remote servers,
// `docker:` references) are created by docker-agent.
func mcpWithSystemEnv(create teamloader.ToolsetCreator) teamloader.ToolsetCreator {
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
		env = toolinstall.PrependBinDirToEnv(append(systemEnv(), env...))
		return mcp.NewToolsetCommand(ts.Name, command, ts.Args, env, cwd, lifecycle.PolicyFromConfig(ts.Name, ts.Lifecycle)), nil
	}
}

// lspWithEmptiedEnv mirrors docker-agent's lsp.CreateToolSet. docker-agent starts the
// server with os.Environ() followed by this environment, so the non-system variables are
// emptied here.
func lspWithEmptiedEnv(ctx context.Context, ts latest.Toolset, _ string, rc *config.RuntimeConfig, _ string) (tools.ToolSet, error) {
	command, err := toolinstall.EnsureCommand(ctx, ts.Command, ts.Version)
	if err != nil {
		return nil, fmt.Errorf("resolving command %q: %w", ts.Command, err)
	}
	env, err := environment.ExpandAll(ctx, environment.ToValues(ts.Env), rc.EnvProvider())
	if err != nil {
		return nil, fmt.Errorf("failed to expand the tool's environment variables: %w", err)
	}
	env = toolinstall.PrependBinDirToEnv(slices.Concat(emptiedEnvList(), systemEnv(), env))
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
