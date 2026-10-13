// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

//go:build linux || darwin

package agentrun

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	goruntime "runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/docker/docker-agent/pkg/config/latest"
	"github.com/docker/docker-agent/pkg/harness"
	baseharness "github.com/rumpl/harness"
	"github.com/rumpl/harness/claudecode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dirien/pulumi-docker-agent/provider/pkg/agentrun/fakeopenai"
)

// The coding-harness tests run the fake Claude Code CLI of testdata/fake-claude (see its
// README), never the real one: PATH holds the fake's directory and the system directories
// only, and every test checks that `claude` resolves to the fake. The fake's mode and log
// come from the test's environment, because AgentRun redacts the values of Request.Env.

const claudeAgent = `agents:
  root:
    description: Writes notes with Claude Code
    harness:
      type: claude-code
      model: sonnet
      effort: high
`

const fakeOAuthToken = "sk-ant-oat01-fake-oauth-token-0123456789"

// fakeCLI is a copy of the fake claude and the files it writes.
type fakeCLI struct {
	dir, log, pidfile string
}

// useFakeClaude copies the fake CLI into a directory of the test's own, puts it first on
// PATH and points its log and pidfile into another one.
func useFakeClaude(t *testing.T) fakeCLI {
	t.Helper()
	src := filepath.Join("testdata", "fake-claude")
	dir := t.TempDir()
	entries, err := os.ReadDir(src)
	require.NoError(t, err)
	for _, e := range entries {
		if e.Name() != "claude" && filepath.Ext(e.Name()) != ".jsonl" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(src, e.Name()))
		require.NoError(t, err)
		mode := os.FileMode(0o644)
		if e.Name() == "claude" {
			mode = 0o755
		}
		require.NoError(t, os.WriteFile(filepath.Join(dir, e.Name()), b, mode))
	}
	path := strings.Join([]string{dir, "/usr/bin", "/bin"}, string(os.PathListSeparator))
	t.Setenv("PATH", path)
	bin, err := lookPathIn("claude", path)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(dir, "claude"), bin, "claude must resolve to the fake")

	state := t.TempDir()
	f := fakeCLI{dir: dir, log: filepath.Join(state, "claude.log"), pidfile: filepath.Join(state, "claude.pid")}
	t.Setenv("FAKE_CLAUDE_LOG", f.log)
	t.Setenv("FAKE_CLAUDE_PIDFILE", f.pidfile)
	// Variables of the developer's environment that change what the fake or the warnings see.
	for _, k := range []string{"FAKE_CLAUDE_MODE", "FAKE_CLAUDE_STDERR", "FAKE_CLAUDE_KEY_SOURCE", "FAKE_CLAUDE_IGNORE_TERM",
		"FAKE_CLAUDE_TRAP_TERM", "FAKE_MARKER",
		claudeOAuthTokenVar, "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_BASE_URL", "CLAUDE_CODE_USE_BEDROCK",
		"CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_FOUNDRY"} {
		t.Setenv(k, "")
	}
	return f
}

// request is a run of the claude-code agent that inherits the test's environment.
func (f fakeCLI) request(prompt string) Request {
	return Request{
		Name: "test", YAML: []byte(claudeAgent), Prompt: prompt,
		InheritEnvironment: true, AllowHarnessAgents: true,
	}
}

// invocation is one entry of the fake's log: the values of each key, in order.
type invocation map[string][]string

func (i invocation) get(key string) string {
	if v := i[key]; len(v) > 0 {
		return v[0]
	}
	return ""
}

// readInvocations parses the fake's log; it is nil when the fake never ran.
func (f fakeCLI) readInvocations() []invocation {
	b, err := os.ReadFile(f.log)
	if err != nil {
		return nil
	}
	var out []invocation
	cur := invocation{}
	inStdin := false
	for _, line := range strings.Split(strings.TrimSuffix(string(b), "\n"), "\n") {
		switch {
		case line == "--- invocation":
			cur, inStdin = invocation{}, false
			out = append(out, cur)
		case line == "term":
			cur["term"] = append(cur["term"], line)
		case inStdin:
			cur["stdin"][0] += "\n" + line
		default:
			k, v, _ := strings.Cut(line, "=")
			cur[k] = append(cur[k], v)
			inStdin = k == "stdin"
		}
	}
	return out
}

// invocations returns the fake's log and fails the test when the fake never ran.
func (f fakeCLI) invocations(t *testing.T) []invocation {
	t.Helper()
	inv := f.readInvocations()
	require.NotEmpty(t, inv, "the fake claude did not run (no log at %s)", f.log)
	return inv
}

// waitPID waits for a pidfile and returns its first PID.
func waitPID(t *testing.T, file string) int {
	t.Helper()
	return waitPIDs(t, file, 1)[0]
}

// waitPIDs waits until a pidfile has n PIDs, one per line, and returns them.
func waitPIDs(t *testing.T, file string, n int) []int {
	t.Helper()
	var pids []int
	require.Eventually(t, func() bool {
		pids = pids[:0]
		b, err := os.ReadFile(file)
		if err != nil {
			return false
		}
		for _, f := range strings.Fields(string(b)) {
			pid, err := strconv.Atoi(f)
			if err != nil || pid <= 0 {
				return false
			}
			pids = append(pids, pid)
		}
		return len(pids) >= n
	}, 10*time.Second, 10*time.Millisecond, "no %d PIDs in %s", n, file)
	return pids
}

// killAllAtCleanup kills processes the test started indirectly, should they still run.
func killAllAtCleanup(t *testing.T, pids []int) {
	for _, pid := range pids {
		killAtCleanup(t, pid)
	}
}

// allGone reports whether none of the processes runs any more.
func allGone(pids []int) bool {
	return !slices.ContainsFunc(pids, func(pid int) bool { return !gone(pid) })
}

// running returns the processes that still run.
func running(pids []int) []int {
	return slices.DeleteFunc(slices.Clone(pids), gone)
}

// gone reports whether a process no longer runs: it does not exist or is a zombie (kill -0
// succeeds on a zombie).
func gone(pid int) bool {
	if goruntime.GOOS == "linux" {
		b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if err != nil {
			return true
		}
		s := string(b)
		fields := strings.Fields(s[strings.LastIndexByte(s, ')')+1:])
		return len(fields) == 0 || fields[0] == "Z" || fields[0] == "X"
	}
	out, _ := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
	st := strings.TrimSpace(string(out))
	return st == "" || strings.HasPrefix(st, "Z")
}

// killAtCleanup kills a process the test started indirectly, should it still run.
func killAtCleanup(t *testing.T, pid int) {
	t.Cleanup(func() {
		if !gone(pid) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	})
}

// shortGrace shortens the time a cancelled CLI gets before its group is killed.
func shortGrace(t *testing.T, d time.Duration) {
	prev := harnessGrace
	harnessGrace = d
	t.Cleanup(func() { harnessGrace = prev })
}

// records collects a run's persistent progress messages and its status messages.
type records struct{ persistent, status []string }

func (r *records) progress(p Progress) {
	if p.Persistent {
		r.persistent = append(r.persistent, p.Message)
	} else {
		r.status = append(r.status, p.Message)
	}
}

func TestFakeClaudeStreamMatchesTheParser(t *testing.T) {
	type ev struct {
		typ          baseharness.EventType
		tool, output string
	}
	expected := map[string][]ev{
		"session.jsonl": {{typ: "session_id"}, {typ: "text"}, {typ: "tool_call_start", tool: "Write"},
			{typ: "tool_call_delta", tool: "Write"}, {typ: "tool_call_delta", tool: "Write"}, {typ: "tool_call", tool: "Write"},
			{typ: "tool_result", tool: "Write", output: "File created successfully at: NOTES.md"}, {typ: "text"},
			{typ: "result", output: "Done: NOTES.md lists the changes."}},
		"session-token.jsonl": {{typ: "session_id"}, {typ: "tool_call_start", tool: "Bash"}, {typ: "tool_call_delta", tool: "Bash"},
			{typ: "tool_call", tool: "Bash"}, {typ: "tool_result", tool: "Bash", output: "__TOKEN__"}, {typ: "text"},
			{typ: "result", output: "The token is __TOKEN__."}},
		"session-json.jsonl": {{typ: "session_id"}, {typ: "text"},
			{typ: "result", output: "```json\n{\"risk\":\"low\",\"summary\":\"fake review\"}\n```"}},
		"session-error.jsonl": {{typ: "session_id"},
			{typ: "result", output: "Failed to authenticate: OAuth session expired and could not be refreshed"}},
		// The driver drops a result without a `result` string: the runner reads `errors` itself.
		"session-error-subtype.jsonl": {{typ: "session_id"}},
		"session-contention.jsonl": {{typ: "session_id"}, {typ: "result", output: "Failed to refresh OAuth token: another " +
			"Claude Code process is refreshing it or exited mid-refresh. This is usually transient; retry in a minute, and " +
			"if it persists close other Claude Code processes or sign in again"}},
		"session-no-result.jsonl": {{typ: "session_id"}, {typ: "text"}},
		"session-tool-error.jsonl": {{typ: "session_id"}, {typ: "tool_call_start", tool: "Bash"}, {typ: "tool_call_delta", tool: "Bash"},
			{typ: "tool_call", tool: "Bash"}, {typ: "tool_result", tool: "Bash", output: "Tool 'shell' is denied by permissions configuration."},
			{typ: "result", output: "The nested agent could not use its shell."}},
		// Claude Code reports success with an empty result when its last message does not end
		// with text, and when it stopped for a deferred tool call.
		"session-empty-result.jsonl": {{typ: "session_id"}, {typ: "text"}, {typ: "tool_call_start", tool: "Write"},
			{typ: "tool_call_delta", tool: "Write"}, {typ: "tool_call_delta", tool: "Write"}, {typ: "tool_call", tool: "Write"},
			{typ: "tool_result", tool: "Write", output: "File created successfully at: NOTES.md"}, {typ: "text"},
			{typ: "result", output: ""}},
		"session-empty.jsonl":    {{typ: "session_id"}, {typ: "result", output: ""}},
		"session-deferred.jsonl": {{typ: "session_id"}, {typ: "text"}, {typ: "result", output: ""}},
	}
	files, err := filepath.Glob(filepath.Join("testdata", "fake-claude", "*.jsonl"))
	require.NoError(t, err)
	require.Len(t, files, len(expected), "every fixture has expectations")
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			want, ok := expected[filepath.Base(file)]
			require.True(t, ok)
			b, err := os.ReadFile(file)
			require.NoError(t, err)
			lines := strings.Split(strings.TrimSpace(string(b)), "\n")
			assert.Contains(t, lines[0], `"subtype":"init"`)
			assert.Contains(t, lines[0], `"apiKeySource":"none","claude_code_version":"2.1.283"`)
			p := claudecode.New("")
			var got []ev
			var usage *baseharness.Usage
			for _, line := range lines {
				require.True(t, strings.HasPrefix(line, "{"), line)
				for _, e := range p.ParseStreamLine(line) {
					out := e.ToolOutput
					if e.Type == baseharness.EventResult {
						out, usage = e.Result, e.Usage
					}
					got = append(got, ev{typ: e.Type, tool: e.ToolName, output: out})
					if e.Type == baseharness.EventToolCall {
						assert.Empty(t, e.ToolArgs, "the driver reports streamed calls without arguments")
					}
				}
			}
			assert.Equal(t, want, got)
			if filepath.Base(file) == "session.jsonl" {
				require.NotNil(t, usage)
				assert.Equal(t, baseharness.Usage{InputTokens: 40, OutputTokens: 37, CacheReadInputTokens: 15000,
					TotalCostUSD: 0.0123, NumTurns: 2, DurationMS: 4321}, *usage)
			}
		})
	}

	// The argument surgery depends on the driver's argument order.
	p, ok := claudecode.New("sonnet", claudecode.WithEffort(claudecode.EffortHigh)).(baseharness.ResumableCommandProvider)
	require.True(t, ok)
	for _, args := range [][]string{p.PrintArgs("x"), p.ResumeArgs("session", "x")} {
		require.GreaterOrEqual(t, len(args), 3)
		assert.Equal(t, []string{"-p", "x"}, args[len(args)-2:])
		cut, err := stdinArgs(args, "x")
		require.NoError(t, err)
		assert.Equal(t, args[:len(args)-1], cut)
		assert.NotContains(t, cut, "x")
	}
	_, err = stdinArgs([]string{"claude", "--print", "x"}, "x")
	assert.ErrorIs(t, err, errUnexpectedArgs)
	_, err = stdinArgs([]string{"claude", "-p", "other"}, "x")
	assert.ErrorIs(t, err, errUnexpectedArgs)
}

func TestHarnessClaudeCode(t *testing.T) {
	f := useFakeClaude(t)
	var rec records
	req := f.request("Write NOTES.md")
	req.Progress = rec.progress
	res, err := Run(t.Context(), req)
	require.NoError(t, err)
	assert.Equal(t, "Done: NOTES.md lists the changes.", res.Output, "only Claude Code's final result")
	assert.Equal(t, "root", res.AgentName)
	assert.Equal(t, []string{"Write"}, res.ToolCalls)
	assert.Empty(t, res.RejectedToolCalls)
	assert.Equal(t, Usage{InputTokens: 15040, OutputTokens: 37, Cost: 0.0123}, res.Usage)
	assert.False(t, res.HasStructuredOutput)
	assert.Empty(t, res.Warnings)

	inv := f.invocations(t)
	require.Len(t, inv, 1)
	args := inv[0]["arg"]
	assert.Equal(t, strconv.Itoa(len(args)), inv[0].get("argc"))
	assert.Equal(t, "-p", args[len(args)-1], "the prompt is not an argument")
	assert.NotContains(t, args, "Write NOTES.md")
	assert.Equal(t, "Write NOTES.md", inv[0].get("stdin"))
	joined := strings.Join(args, " ")
	for _, want := range []string{"--print", "--dangerously-skip-permissions", "--output-format stream-json",
		"--model sonnet", "--effort high"} {
		assert.Contains(t, joined, want)
	}
	cwd := inv[0].get("cwd")
	assert.Contains(t, filepath.Base(cwd), "pulumi-docker-agent-work-")
	assert.Equal(t, cwd, inv[0].get("pwd_env"), "PWD is the working directory")
	assert.NoDirExists(t, cwd, "the temporary working directory is removed")
	assert.Equal(t, os.Getenv("HOME"), inv[0].get("home"))
	assert.Equal(t, "set", inv[0].get("cookie"), "the CLI gets the run's cookie")
	assert.Contains(t, rec.persistent, "claude-code 2.1.283 (claude-sonnet-4-6), apiKeySource none (the Claude login of "+
		"this machine)")
	assert.Contains(t, rec.persistent[len(rec.persistent)-1], "agent run finished")
}

func TestHarnessEmptyResult(t *testing.T) {
	f := useFakeClaude(t)
	t.Run("the streamed text is the answer", func(t *testing.T) {
		t.Setenv("FAKE_CLAUDE_MODE", "empty-result")
		var rec records
		req := f.request("Write NOTES.md")
		req.Logging = LoggingFull
		req.Progress = rec.progress
		res, err := Run(t.Context(), req)
		require.NoError(t, err)
		assert.Equal(t, "I'll write the notes.\n\nDone: NOTES.md lists the changes.", res.Output,
			"the text of every turn, in order")
		assert.Equal(t, []string{"Write"}, res.ToolCalls)
		assert.Contains(t, res.Warnings, "claude-code returned an empty final result, so the agent's answer is the text "+
			"that Claude Code wrote during the run instead")
	})
	t.Run("as a sub-agent", func(t *testing.T) {
		t.Setenv("FAKE_CLAUDE_MODE", "empty-result")
		srv := fakeopenai.New(func(r fakeopenai.Request) fakeopenai.Reply {
			for _, m := range r.Messages {
				if m.Role == "assistant" {
					return fakeopenai.Reply{Content: "The coder said: " + toolResult(r)}
				}
			}
			return fakeopenai.Reply{ToolCalls: []fakeopenai.ToolCall{{ID: "c1", Name: "transfer_task",
				Arguments: `{"agent":"coder","task":"Write NOTES.md","expected_output":"a summary"}`}}}
		})
		defer srv.Close()
		req := request(srv, "agents:\n  root:\n    model: m\n    instruction: You coordinate.\n    sub_agents: [coder]\n"+
			"  coder:\n    description: Writes notes\n    harness:\n      type: claude-code\n      model: sonnet\n", "Write the notes")
		req.AllowHarnessAgents, req.InheritEnvironment = true, true
		res, err := Run(t.Context(), req)
		require.NoError(t, err)
		assert.Equal(t, "The coder said: I'll write the notes.\n\nDone: NOTES.md lists the changes.", res.Output)
	})
	for _, c := range []struct{ mode, want string }{
		{"empty", "harness claude-code failed: claude returned an empty result and no text"},
		{"deferred", "harness claude-code failed: claude stopped for a deferred call of its AskUserQuestion tool, which " +
			"needs an interactive session"},
	} {
		t.Run(c.mode, func(t *testing.T) {
			t.Setenv("FAKE_CLAUDE_MODE", c.mode)
			_, err := Run(t.Context(), f.request("Write NOTES.md"))
			require.Error(t, err)
			assert.Contains(t, err.Error(), c.want)
		})
	}
}

func TestHarnessLargePrompt(t *testing.T) {
	f := useFakeClaude(t)
	prompt := strings.Repeat("A long prompt line. ", 200<<10/20)
	res, err := Run(t.Context(), f.request(prompt))
	require.NoError(t, err, "a 200 KiB prompt works because it goes to stdin")
	assert.Equal(t, "Done: NOTES.md lists the changes.", res.Output)
	assert.Len(t, f.invocations(t)[0].get("stdin"), len(strings.TrimSpace(prompt)))
}

func TestHarnessWorkingDir(t *testing.T) {
	f := useFakeClaude(t)
	dir := t.TempDir()
	req := f.request("Write NOTES.md")
	req.WorkingDir = dir
	_, err := Run(t.Context(), req)
	require.NoError(t, err)
	notes, err := os.ReadFile(filepath.Join(dir, "NOTES.md"))
	require.NoError(t, err, "the CLI runs in workingDir")
	assert.Equal(t, "Write NOTES.md\n", string(notes))
	assert.Equal(t, dir, f.invocations(t)[0].get("cwd"))
	assert.Equal(t, dir, f.invocations(t)[0].get("pwd_env"))

	req.Prompt = "Delete NOTES.md"
	_, err = Run(t.Context(), req)
	require.NoError(t, err)
	assert.NoFileExists(t, filepath.Join(dir, "NOTES.md"))
	assert.DirExists(t, dir, "a given working directory is kept")
}

func TestHarnessEnvironment(t *testing.T) {
	f := useFakeClaude(t)
	configDir := t.TempDir()
	t.Setenv("FAKE_MARKER", "from-the-provider")
	t.Setenv("CLAUDECODE", "1")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "the-parent-session")
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)
	t.Setenv("IS_SANDBOX", "1")
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-api03-inherited-key-0123456789")
	last := func() invocation {
		inv := f.invocations(t)
		return inv[len(inv)-1]
	}

	t.Run("inherited", func(t *testing.T) {
		_, err := Run(t.Context(), f.request("hi"))
		require.NoError(t, err)
		inv := last()
		assert.Equal(t, "from-the-provider", inv.get("marker"))
		assert.Equal(t, "unset", inv.get("claudecode"), "the CLI starts a top-level session")
		assert.Equal(t, "set", inv.get("api_key"))
		assert.Equal(t, "unset", inv.get("oauth_token"))
		assert.Equal(t, configDir, inv.get("config_dir"))
	})
	t.Run("not inherited", func(t *testing.T) {
		req := f.request("hi")
		req.InheritEnvironment = false
		req.Env = map[string]string{"FAKE_CLAUDE_LOG": f.log}
		_, err := Run(t.Context(), req)
		require.NoError(t, err)
		inv := last()
		assert.Empty(t, inv.get("marker"), "the provider's environment does not reach the CLI")
		assert.Equal(t, "unset", inv.get("api_key"))
		assert.Equal(t, "unset", inv.get("claudecode"))
		assert.Equal(t, configDir, inv.get("config_dir"), "the Claude configuration location is kept")
		assert.Equal(t, "1", inv.get("is_sandbox"))
		assert.Equal(t, os.Getenv("HOME"), inv.get("home"))
	})
	t.Run("an explicit token drops the inherited API key", func(t *testing.T) {
		req := f.request("hi")
		req.Env = map[string]string{claudeOAuthTokenVar: fakeOAuthToken}
		_, err := Run(t.Context(), req)
		require.NoError(t, err)
		inv := last()
		assert.Equal(t, "set", inv.get("oauth_token"))
		assert.Equal(t, "unset", inv.get("api_key"))
	})
	t.Run("an explicit API key is kept", func(t *testing.T) {
		req := f.request("hi")
		req.Env = map[string]string{claudeOAuthTokenVar: fakeOAuthToken, "ANTHROPIC_API_KEY": "sk-ant-api03-explicit-key-0123"}
		_, err := Run(t.Context(), req)
		require.NoError(t, err)
		assert.Equal(t, "set", last().get("api_key"))
	})
}

func TestClaudeEnvironment(t *testing.T) {
	environ := []string{"PATH=/bin", "HOME=/home/u", "SECRET_THING=x", "CLAUDECODE=1", "AI_AGENT=claude", "XDG_CONFIG_HOME=/x",
		"ANTHROPIC_API_KEY=inherited", "ANTHROPIC_AUTH_TOKEN=inherited-too", "PWD=/somewhere/else", "=C:=C:\\"}

	e := newClaudeEnvironment(environ, map[string]string{"A": "1"}, true, "/work")
	assert.Equal(t, "x", e.vars["SECRET_THING"])
	assert.NotContains(t, e.vars, "CLAUDECODE")
	assert.NotContains(t, e.vars, "AI_AGENT")
	assert.Equal(t, "/work", e.vars["PWD"])
	assert.Equal(t, "1", e.vars["A"])
	assert.Empty(t, e.dropped)
	assert.Contains(t, e.list(), "PWD=/work")

	e = newClaudeEnvironment(environ, nil, false, "/work")
	assert.Equal(t, map[string]string{"PATH": "/bin", "HOME": "/home/u", "XDG_CONFIG_HOME": "/x", "PWD": "/work"}, e.vars)

	e = newClaudeEnvironment(environ, map[string]string{claudeOAuthTokenVar: "tok"}, true, "")
	assert.NotContains(t, e.vars, "ANTHROPIC_API_KEY")
	assert.NotContains(t, e.vars, "ANTHROPIC_AUTH_TOKEN")
	assert.Equal(t, []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN"}, e.dropped)
	assert.Equal(t, "/somewhere/else", e.vars["PWD"], "no dir, no PWD override")

	e = newClaudeEnvironment(environ, map[string]string{claudeOAuthTokenVar: "tok", "ANTHROPIC_API_KEY": ""}, true, "")
	assert.Equal(t, "", e.vars["ANTHROPIC_API_KEY"], "an explicit value wins, also a blank one")
	assert.Equal(t, []string{"ANTHROPIC_AUTH_TOKEN"}, e.dropped)

	// An empty OAuth token does not drop anything.
	e = newClaudeEnvironment(environ, map[string]string{claudeOAuthTokenVar: ""}, true, "")
	assert.Equal(t, "inherited", e.vars["ANTHROPIC_API_KEY"])

	// An empty inherited credential is removed, but it was not a credential to drop.
	e = newClaudeEnvironment([]string{"ANTHROPIC_API_KEY=", "ANTHROPIC_AUTH_TOKEN=inherited"},
		map[string]string{claudeOAuthTokenVar: "tok"}, true, "")
	assert.NotContains(t, e.vars, "ANTHROPIC_API_KEY")
	assert.Equal(t, []string{"ANTHROPIC_AUTH_TOKEN"}, e.dropped)
	assert.Equal(t, []string{"inherited ANTHROPIC_AUTH_TOKEN is not passed to claude because env sets " +
		"CLAUDE_CODE_OAUTH_TOKEN"}, e.billingWarnings())

	// What a parent Claude Code session sets for its processes, but not the ordinary variables
	// it also sets.
	e = newClaudeEnvironment([]string{"TRACEPARENT=00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01",
		"CLAUDE_CODE_EXECPATH=/opt/claude/bin/claude", "CLAUDE_CODE_TMPDIR=/tmp/claude-1000",
		"CLAUDE_CODE_INVOKED_SKILLS=review", "TMPDIR=/tmp/t", "SHELL=/bin/zsh", "GIT_EDITOR=true"}, nil, true, "")
	assert.Equal(t, map[string]string{"TMPDIR": "/tmp/t", "SHELL": "/bin/zsh", "GIT_EDITOR": "true"}, e.vars)
}

// TestHarnessWithheldToken runs claude with inheritEnvironment: false while the Pulumi CLI has
// a CLAUDE_CODE_OAUTH_TOKEN: the CLI uses the machine's login, and the record and an
// authentication error say why.
func TestHarnessWithheldToken(t *testing.T) {
	f := useFakeClaude(t)
	t.Setenv(claudeOAuthTokenVar, fakeOAuthToken)
	const note = "inheritEnvironment: false did not pass the Pulumi CLI's CLAUDE_CODE_OAUTH_TOKEN"
	run := func(mode string) (*records, error) {
		var rec records
		req := f.request("Write NOTES.md")
		req.InheritEnvironment = false
		req.Env = map[string]string{"FAKE_CLAUDE_LOG": f.log, "FAKE_CLAUDE_MODE": mode}
		req.Progress = rec.progress
		_, err := Run(t.Context(), req)
		return &rec, err
	}
	rec, err := run("ok")
	require.NoError(t, err)
	inv := f.invocations(t)
	assert.Equal(t, "unset", inv[len(inv)-1].get("oauth_token"))
	assert.Contains(t, rec.persistent, "claude-code 2.1.283 (claude-sonnet-4-6), apiKeySource none (the Claude login of "+
		"this machine; "+note+")")

	_, err = run("error")
	require.Error(t, err)
	assert.Contains(t, err.Error(), authHint+" ("+note+")")
	assert.NotContains(t, err.Error(), fakeOAuthToken)
	_, err = run("contention")
	require.Error(t, err)
	assert.Contains(t, err.Error(), contentionHint+" ("+note+")", "the token avoids a login refreshed elsewhere too")

	// Without the token in the Pulumi CLI's environment, nothing is said.
	t.Setenv(claudeOAuthTokenVar, "")
	rec, err = run("ok")
	require.NoError(t, err)
	assert.Contains(t, rec.persistent, "claude-code 2.1.283 (claude-sonnet-4-6), apiKeySource none (the Claude login of "+
		"this machine)")
	_, err = run("error")
	require.Error(t, err)
	assert.NotContains(t, err.Error(), note)
}

func TestHarnessNoKeySource(t *testing.T) {
	f := useFakeClaude(t)
	const authToken = "an-explicit-auth-token-0123456789"
	for _, c := range []struct {
		name          string
		env           map[string]string
		record, warns string
	}{
		{"login", nil, "apiKeySource none (the Claude login of this machine)", ""},
		{"OAuth token", map[string]string{claudeOAuthTokenVar: fakeOAuthToken}, "apiKeySource none (CLAUDE_CODE_OAUTH_TOKEN)", ""},
		{"auth token", map[string]string{"ANTHROPIC_AUTH_TOKEN": authToken}, "apiKeySource none (ANTHROPIC_AUTH_TOKEN)",
			"claude-code used ANTHROPIC_AUTH_TOKEN, which bills API usage, not a Claude subscription"},
		{"Bedrock", map[string]string{"CLAUDE_CODE_USE_BEDROCK": "1", "ANTHROPIC_AUTH_TOKEN": authToken},
			"apiKeySource none (Amazon Bedrock, CLAUDE_CODE_USE_BEDROCK)",
			"claude-code used Amazon Bedrock (CLAUDE_CODE_USE_BEDROCK), which bills usage to that cloud account, not a " +
				"Claude subscription"},
		{"gateway", map[string]string{"ANTHROPIC_BASE_URL": "https://gateway.example.com"},
			"apiKeySource none (the Claude login of this machine, through ANTHROPIC_BASE_URL)", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			var rec records
			req := f.request("Write NOTES.md")
			req.Env = c.env
			req.Progress = rec.progress
			res, err := Run(t.Context(), req)
			require.NoError(t, err)
			assert.Contains(t, rec.persistent, "claude-code 2.1.283 (claude-sonnet-4-6), "+c.record)
			if c.warns == "" {
				assert.Empty(t, res.Warnings)
			} else {
				assert.Equal(t, []string{c.warns}, res.Warnings)
			}
		})
	}
}

func TestHarnessPathLookup(t *testing.T) {
	bin := t.TempDir()
	exe := filepath.Join(bin, "claude")
	require.NoError(t, os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755)) //nolint:gosec // an executable test file
	plain := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(plain, "claude"), []byte("not executable"), 0o600))
	dirOnly := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dirOnly, "claude"), 0o700))

	// A claude in the current directory and in a relative directory: never found.
	t.Chdir(bin)
	require.NoError(t, os.Mkdir("rel", 0o700))
	require.NoError(t, os.WriteFile(filepath.Join("rel", "claude"), []byte("#!/bin/sh\n"), 0o755)) //nolint:gosec // as above
	for _, path := range []string{"", ".", "::", "rel", ".:rel:" + plain + ":" + dirOnly} {
		_, err := lookPathIn("claude", path)
		assert.ErrorIs(t, err, errClaudeNotFound, "PATH %q", path)
	}
	got, err := lookPathIn("claude", ".::rel:"+plain+":"+bin)
	require.NoError(t, err)
	assert.Equal(t, exe, got)

	// Through a run: PATH without claude.
	t.Setenv("PATH", t.TempDir())
	_, err = Run(t.Context(), Request{YAML: []byte(claudeAgent), Prompt: "hi", AllowHarnessAgents: true, InheritEnvironment: true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the claude-code harness needs the claude CLI, which was not found on PATH: install "+
		"Claude Code on the machine that runs pulumi up, or set PATH in env")
}

func TestHarnessErrorMapping(t *testing.T) {
	f := useFakeClaude(t)
	cases := []struct {
		mode string
		want []string
	}{
		{"error", []string{"the agent run failed: harness claude-code failed: claude exited with status 1: Failed to " +
			"authenticate: OAuth session expired and could not be refreshed; " + authHint}},
		{"error-subtype", []string{"claude exited with status 1: Failed to authenticate: OAuth session expired and could " +
			"not be refreshed; " + authHint}},
		{"contention", []string{"another Claude Code process is refreshing it", "; " + contentionHint}},
		{"root", []string{"claude exited with status 1: ", "--dangerously-skip-permissions cannot be used with root/sudo " +
			"privileges for security reasons; " + rootHint}},
		{"no-result", []string{"claude ended without a result"}},
	}
	for _, c := range cases {
		t.Run(c.mode, func(t *testing.T) {
			t.Setenv("FAKE_CLAUDE_MODE", c.mode)
			// The token reaches stderr in every mode; mode error also writes it there itself.
			t.Setenv("FAKE_CLAUDE_STDERR", "the token is "+fakeOAuthToken)
			req := f.request("Write NOTES.md")
			req.Env = map[string]string{claudeOAuthTokenVar: fakeOAuthToken}
			var warnings []string
			req.Warn = func(m string) { warnings = append(warnings, m) }
			_, err := Run(t.Context(), req)
			require.Error(t, err)
			for _, want := range c.want {
				assert.Contains(t, err.Error(), want)
			}
			assert.NotContains(t, err.Error(), fakeOAuthToken)
			assert.NotContains(t, err.Error(), fakeOAuthToken[len(fakeOAuthToken)-12:])
			if c.mode == "root" {
				assert.Contains(t, err.Error(), "the token is [secret]", "stderr is shown, redacted")
			}
			assert.Empty(t, warnings)
		})
	}
}

func TestHarnessStderrRedactedBeforeTruncation(t *testing.T) {
	secret := "sk-ant-oat01-" + strings.Repeat("s", 40)
	// One long line, so that the cut at stderrShow bytes from the end falls inside the secret
	// and no line start is there to skip to.
	tail := &stderrTail{}
	_, _ = tail.Write([]byte(strings.Repeat("a", 3000) + secret + strings.Repeat("b", stderrShow-20)))
	got := tail.text([]string{secret})
	assert.Contains(t, got, "[secret]")
	assert.NotContains(t, got, secret[20:], "no part of the secret is shown")
	assert.LessOrEqual(t, len(got), stderrShow)

	// The buffer keeps the end of stderr only, and the text starts at a line.
	tail = &stderrTail{}
	for i := range 2000 {
		_, _ = fmt.Fprintf(tail, "line %d\n", i)
	}
	assert.LessOrEqual(t, len(tail.buf), stderrKeep)
	got = tail.text(nil)
	assert.True(t, strings.HasPrefix(got, "line "), got[:20])
	assert.True(t, strings.HasSuffix(got, "line 1999"))
	assert.LessOrEqual(t, len(got), stderrShow)

	// The buffer cut can split a secret at its start. Repeated long secrets redact the 16 KiB
	// buffer to less than stderrShow, so its first line would be shown.
	var b strings.Builder
	b.WriteString("sk-ant-oat01-")
	for i := 0; b.Len() < 5000; i++ {
		fmt.Fprintf(&b, "%05d", i)
	}
	long := b.String()
	for _, sep := range []string{"", "\n"} {
		tail = &stderrTail{}
		for range 4 {
			_, _ = tail.Write([]byte(long + sep))
		}
		_, _ = tail.Write([]byte("claude: fatal error" + sep))
		got = tail.text([]string{long})
		assert.Contains(t, got, "claude: fatal error", "separator %q", sep)
		assert.NotContains(t, got, long[len(long)-100:], "no part of the cut secret is shown (separator %q): %.80s", sep, got)
	}
}

func TestHarnessTimeoutStopsTheGroup(t *testing.T) {
	f := useFakeClaude(t)
	shortGrace(t, time.Second)
	t.Setenv("FAKE_CLAUDE_MODE", "term-trap")
	req := f.request("Write NOTES.md")
	req.Timeout = 2 * time.Second
	start := time.Now()
	_, err := Run(t.Context(), req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the agent did not finish within 2s (timeoutSeconds)")
	assert.Less(t, time.Since(start), 10*time.Second)

	child := waitPID(t, f.pidfile)
	killAtCleanup(t, child)
	inv := f.invocations(t)[0]
	leader, err := strconv.Atoi(inv.get("pid"))
	require.NoError(t, err)
	assert.NotEmpty(t, inv["term"], "the CLI got SIGTERM before SIGKILL")
	assert.Eventually(t, func() bool { return gone(leader) && gone(child) }, 3*time.Second, 20*time.Millisecond,
		"the CLI stopped its detached child when it got SIGTERM")
}

func TestHarnessTimeoutKillsAfterGrace(t *testing.T) {
	f := useFakeClaude(t)
	shortGrace(t, time.Second)
	t.Setenv("FAKE_CLAUDE_MODE", "hang")
	req := f.request("Write NOTES.md")
	req.Timeout = time.Second
	start := time.Now()
	_, err := Run(t.Context(), req)
	took := time.Since(start)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "did not finish within 1s")
	assert.GreaterOrEqual(t, took, 2*time.Second-100*time.Millisecond, "the CLI ignores SIGTERM: killed after the grace")
	assert.Less(t, took, 10*time.Second)

	child := waitPID(t, f.pidfile)
	killAtCleanup(t, child)
	leader, err := strconv.Atoi(f.invocations(t)[0].get("pid"))
	require.NoError(t, err)
	assert.Eventually(t, func() bool { return gone(leader) && gone(child) }, 3*time.Second, 20*time.Millisecond,
		"the leader and its child in the group are killed")
}

// TestHarnessTimeoutStopsDetachedChild: a process the CLI started in a session of its own is
// out of the group signals' reach, and the CLI does not stop it; the runner recorded it and
// stops it.
func TestHarnessTimeoutStopsDetachedChild(t *testing.T) {
	f := useFakeClaude(t)
	shortGrace(t, time.Second)
	t.Setenv("FAKE_CLAUDE_MODE", "hang-detached")
	req := f.request("Write NOTES.md")
	req.Timeout = time.Second
	_, err := Run(t.Context(), req)
	require.Error(t, err)
	child := waitPID(t, f.pidfile)
	killAtCleanup(t, child)
	leader, err := strconv.Atoi(f.invocations(t)[0].get("pid"))
	require.NoError(t, err)
	assert.True(t, gone(leader), "the CLI is gone when Run returns")
	assert.True(t, gone(child), "the detached child is gone when Run returns")
}

func TestHarnessLeavesNoGroupProcesses(t *testing.T) {
	for _, ignoreTerm := range []bool{false, true} {
		t.Run(fmt.Sprintf("ignoring SIGTERM %v", ignoreTerm), func(t *testing.T) {
			f := useFakeClaude(t)
			t.Setenv("FAKE_CLAUDE_MODE", "leftover")
			if ignoreTerm {
				t.Setenv("FAKE_CLAUDE_IGNORE_TERM", "1")
			} else {
				// A process that honours SIGTERM is gone long before the SIGKILL would come.
				prev := postExitGrace
				postExitGrace = 8 * time.Second
				t.Cleanup(func() { postExitGrace = prev })
			}
			start := time.Now()
			res, err := Run(t.Context(), f.request("Write NOTES.md"))
			took := time.Since(start)
			require.NoError(t, err)
			assert.Equal(t, "Done: NOTES.md lists the changes.", res.Output)
			assert.Empty(t, res.Warnings, "what the CLI left in its own group is stopped without a warning")
			child := waitPID(t, f.pidfile)
			killAtCleanup(t, child)
			assert.Eventually(t, func() bool { return gone(child) }, 500*time.Millisecond, 10*time.Millisecond,
				"the process the CLI left in its group is stopped before Run returns")
			if ignoreTerm {
				assert.GreaterOrEqual(t, took, postExitGrace, "killed after the grace period")
			} else {
				assert.Less(t, took, postExitGrace, "the leftover process got SIGTERM")
			}
		})
	}
}

// TestHarnessDrainIsBounded runs a CLI that leaves a process behind which keeps writing to
// its stdout, outside the group's reach and not recorded (the process table cannot be read):
// the runner stops reading harnessDrain after the CLI exited, and a cancellation ends the run
// at once.
func TestHarnessDrainIsBounded(t *testing.T) {
	failProcTable(t, errors.New("no process table in this test"))
	run := func(t *testing.T, f fakeCLI, req Request) (time.Duration, error) {
		t.Helper()
		start := time.Now()
		done := make(chan error, 1)
		go func() {
			_, err := Run(context.Background(), req)
			done <- err
		}()
		writer := waitPID(t, f.pidfile)
		killAtCleanup(t, writer)
		select {
		case err := <-done:
			return time.Since(start), err
		case <-time.After(30 * time.Second):
			_ = syscall.Kill(writer, syscall.SIGKILL)
			<-done
			t.Fatal("the run did not end while a left-behind process kept writing to its output")
			return 0, nil
		}
	}

	t.Run("the drain time is a total", func(t *testing.T) {
		f := useFakeClaude(t)
		t.Setenv("FAKE_CLAUDE_MODE", "detached-writer")
		took, err := run(t, f, f.request("Write NOTES.md"))
		require.NoError(t, err)
		assert.GreaterOrEqual(t, took, harnessDrain, "the output is read for harnessDrain after the CLI exited")
		assert.Less(t, took, harnessDrain+5*time.Second)
	})
	t.Run("a timeout during the drain", func(t *testing.T) {
		f := useFakeClaude(t)
		t.Setenv("FAKE_CLAUDE_MODE", "detached-writer")
		prev := harnessDrain
		harnessDrain = time.Minute
		t.Cleanup(func() { harnessDrain = prev })
		req := f.request("Write NOTES.md")
		req.Timeout = 2 * time.Second
		took, err := run(t, f, req)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "the agent did not finish within 2s (timeoutSeconds)")
		assert.Less(t, took, 10*time.Second, "the timeout ends the drain")
	})
}

func TestHarnessLingeringStderrIsNotAnError(t *testing.T) {
	f := useFakeClaude(t)
	t.Setenv("FAKE_CLAUDE_MODE", "stderr-open")
	start := time.Now()
	res, err := Run(t.Context(), f.request("Write NOTES.md"))
	require.NoError(t, err, "a child that keeps stderr open does not fail the run")
	assert.Equal(t, "Done: NOTES.md lists the changes.", res.Output)
	assert.Less(t, time.Since(start), 5*time.Second, "the child is stopped, not waited for")
}

// The provider-process tests run the harness in a copy of the test binary (helper mode
// through TestMain) and signal or kill that process.

const harnessHelperEnv = "AGENTRUN_TEST_HARNESS_HELPER"

// runHarnessHelperMode runs the test binary as the harness helper when the environment says
// so (TestMain, main_test.go).
func runHarnessHelperMode() {
	if dir := os.Getenv(harnessHelperEnv); dir != "" {
		os.Exit(runHarnessHelper(dir))
	}
}

// harnessHelperLateEnv makes the helper try to start another CLI after the SIGINT.
const harnessHelperLateEnv = "AGENTRUN_TEST_HARNESS_HELPER_LATE"

// runHarnessHelper behaves like the provider binary: the Pulumi SDK's provider.MainContext
// ends it with exit status 0 on SIGINT, after the operations in flight (here the CLI run,
// with the fake from PATH and FAKE_CLAUDE_MODE, in dir) ended, and main.go watches the
// signal for the CLIs.
func runHarnessHelper(dir string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	WatchShutdownSignals()
	p, ok := claudecode.New("").(baseharness.ResumableCommandProvider)
	if !ok {
		return 2
	}
	args, err := stdinArgs(p.PrintArgs("hang"), "hang")
	if err != nil {
		return 2
	}
	h := harnessRun{dir: dir, env: newClaudeEnvironment(os.Environ(), nil, true, dir), info: &harnessInfo{},
		debug: func(msg string) { fmt.Fprintln(os.Stderr, "harness helper: debug:", msg) }}
	run := func(ctx context.Context) error {
		return runClaude(ctx, h, args, "hang", &claudeStream{parser: p, handle: func(harness.Event) {}})
	}
	done := make(chan error, 1)
	go func() { done <- run(context.Background()) }()
	<-ctx.Done()
	select {
	case err := <-done:
		fmt.Fprintln(os.Stderr, "harness helper: the CLI ended:", err)
	case <-time.After(time.Minute):
		return 3
	}
	if os.Getenv(harnessHelperLateEnv) != "" {
		late, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := run(late); !errors.Is(err, errShuttingDown) {
			fmt.Fprintln(os.Stderr, "harness helper: a CLI started after SIGINT:", err)
			return 4
		}
	}
	return 0
}

// waitHelper waits for the helper to end, at most 15 s.
func waitHelper(t *testing.T, cmd *exec.Cmd) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(15 * time.Second):
		t.Fatal("the harness helper did not end")
		return nil
	}
}

// startHarnessHelper starts the helper with a fake CLI in mode (and extra environment) and
// returns it with the PIDs of the CLI and of the child it started. The helper's stderr is
// in stderr once it ended.
func startHarnessHelper(t *testing.T, f fakeCLI, mode string, env ...string) (cmd *exec.Cmd, leader, child int,
	stderr *strings.Builder) {
	t.Helper()
	exe, err := os.Executable()
	require.NoError(t, err)
	cmd = exec.Command(exe, "-test.run=^$")
	cmd.Env = append(append(os.Environ(), harnessHelperEnv+"="+t.TempDir(), "FAKE_CLAUDE_MODE="+mode), env...)
	stderr = &strings.Builder{}
	cmd.Stderr = stderr
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	child = waitPID(t, f.pidfile)
	killAtCleanup(t, child)
	leader, err = strconv.Atoi(f.invocations(t)[0].get("pid"))
	require.NoError(t, err)
	killAtCleanup(t, leader)
	return cmd, leader, child, stderr
}

// The signal tests check that a SIGINT stops the CLIs and leaves the provider's own shutdown
// alone: the helper exits with status 0, as the Pulumi SDK makes the provider do.

func TestHarnessSignalStopsHarness(t *testing.T) {
	f := useFakeClaude(t)
	cmd, leader, child, stderr := startHarnessHelper(t, f, "hang")
	start := time.Now()
	require.NoError(t, cmd.Process.Signal(syscall.SIGINT))
	require.NoError(t, waitHelper(t, cmd), "the provider shuts down as the SDK makes it, with exit status 0")
	assert.GreaterOrEqual(t, time.Since(start), signalGrace, "the CLI ignores SIGTERM: killed after signalGrace")
	assert.True(t, gone(leader) && gone(child), "the CLI's group is gone before the provider exits")
	assert.Contains(t, stderr.String(), "the CLI ended: claude was stopped because Pulumi is stopping the provider (SIGINT)")
}

func TestHarnessSignalSendsTermFirst(t *testing.T) {
	f := useFakeClaude(t)
	cmd, leader, child, stderr := startHarnessHelper(t, f, "term-trap")
	start := time.Now()
	require.NoError(t, cmd.Process.Signal(syscall.SIGINT))
	require.NoError(t, waitHelper(t, cmd))
	assert.Contains(t, stderr.String(), "the CLI ended: claude was stopped because Pulumi is stopping the provider (SIGINT)")
	assert.Less(t, time.Since(start), signalGrace, "the CLI stopped on SIGTERM")
	assert.NotEmpty(t, f.invocations(t)[0]["term"], "the CLI got SIGTERM")
	assert.True(t, gone(leader), "the CLI is gone before the provider exits")
	assert.Eventually(t, func() bool { return gone(child) }, 3*time.Second, 20*time.Millisecond,
		"the CLI had the time to stop its detached child")
}

func TestHarnessSignalRefusesLateCLIs(t *testing.T) {
	f := useFakeClaude(t)
	cmd, _, _, _ := startHarnessHelper(t, f, "term-trap", harnessHelperLateEnv+"=1")
	require.NoError(t, cmd.Process.Signal(syscall.SIGINT))
	require.NoError(t, waitHelper(t, cmd), "the helper exits with 4 when a CLI started after SIGINT")
	assert.Len(t, f.invocations(t), 1, "no CLI started after the signal")
}

func TestHarnessPdeathsig(t *testing.T) {
	if goruntime.GOOS != "linux" {
		t.Skip("the parent-death signal is Linux-only")
	}
	f := useFakeClaude(t)
	cmd, leader, child, _ := startHarnessHelper(t, f, "term-trap")
	require.NoError(t, cmd.Process.Kill())
	_ = waitHelper(t, cmd)
	assert.Eventually(t, func() bool {
		inv := f.readInvocations()
		return len(inv) > 0 && len(inv[0]["term"]) > 0
	}, 3*time.Second, 20*time.Millisecond, "the CLI gets SIGTERM when the provider dies")
	assert.Eventually(t, func() bool { return gone(leader) && gone(child) }, 3*time.Second, 20*time.Millisecond)
}

func TestHarnessStructuredOutput(t *testing.T) {
	f := useFakeClaude(t)
	t.Setenv("FAKE_CLAUDE_MODE", "json")
	req := f.request("Review the change and answer as JSON.")
	req.YAML = []byte(claudeAgent + reviewSchema)
	res, err := Run(t.Context(), req)
	require.NoError(t, err)
	assert.True(t, res.HasStructuredOutput)
	assert.Equal(t, map[string]any{"risk": "low", "summary": "fake review"}, res.StructuredOutput, "the fence is removed")
	assert.True(t, strings.HasPrefix(res.Output, "```json"), "output is the result as Claude Code wrote it")
}

func TestStripCodeFence(t *testing.T) {
	assert.Equal(t, `{"a":1}`, stripCodeFence("```json\n{\"a\":1}\n```"))
	assert.Equal(t, `{"a":1}`, stripCodeFence("```\n{\"a\":1}\n```"))
	assert.Equal(t, `{"a":1}`, stripCodeFence(`{"a":1}`))
	assert.Equal(t, "```", stripCodeFence("```"))
	assert.Equal(t, "```json {} ```", stripCodeFence("```json {} ```"))
}

func TestHarnessAsSubAgent(t *testing.T) {
	f := useFakeClaude(t)
	srv := fakeopenai.New(func(r fakeopenai.Request) fakeopenai.Reply {
		turns := 0
		for _, m := range r.Messages {
			if m.Role == "assistant" {
				turns++
			}
		}
		switch turns {
		case 0:
			return fakeopenai.Reply{ToolCalls: []fakeopenai.ToolCall{{ID: "c1", Name: "shell", Arguments: `{"cmd":"rm -rf /tmp/x"}`}}}
		case 1:
			return fakeopenai.Reply{ToolCalls: []fakeopenai.ToolCall{{ID: "c2", Name: "transfer_task",
				Arguments: `{"agent":"coder","task":"Write NOTES.md","expected_output":"a summary"}`}}}
		}
		return fakeopenai.Reply{Content: "The coder said: " + toolResult(r)}
	})
	defer srv.Close()
	body := `agents:
  root:
    model: m
    instruction: You coordinate.
    sub_agents: [coder]
    toolsets:
      - type: shell
  coder:
    description: Writes notes with Claude Code
    harness:
      type: claude-code
      model: sonnet
`
	req := request(srv, body, "Write the notes")
	req.AllowHarnessAgents = true
	req.InheritEnvironment = true
	res, err := Run(t.Context(), req)
	require.NoError(t, err)
	reqs := srv.Requests()
	require.Len(t, reqs, 3)
	assert.Equal(t, "Done: NOTES.md lists the changes.", toolResult(reqs[2]), "transfer_task returns only the result")
	assert.Equal(t, "The coder said: Done: NOTES.md lists the changes.", res.Output)
	assert.Equal(t, "root", res.AgentName)
	assert.Equal(t, []string{"transfer_task", "Write"}, res.ToolCalls)
	assert.Equal(t, []string{"shell"}, res.RejectedToolCalls, "toolApproval deny still applies to the root's tools")
	assert.Contains(t, f.invocations(t)[0].get("stdin"), "Write NOTES.md", "the task reaches the CLI")
}

func TestHarnessToolResultsAreNotRejections(t *testing.T) {
	f := useFakeClaude(t)
	t.Setenv("FAKE_CLAUDE_MODE", "tool-error")
	var rec records
	req := f.request("Run the nested agent")
	req.Progress = rec.progress
	res, err := Run(t.Context(), req)
	require.NoError(t, err)
	assert.Equal(t, []string{"Bash"}, res.ToolCalls, "a failed harness tool call ran")
	assert.Empty(t, res.RejectedToolCalls)
	assert.Empty(t, res.Warnings)
	assert.Contains(t, rec.status, "root: Bash run by the harness, failed")
}

func TestHarnessProgress(t *testing.T) {
	f := useFakeClaude(t)
	run := func(level Logging) records {
		var rec records
		req := f.request("Write NOTES.md")
		req.Logging = level
		req.Progress = rec.progress
		_, err := Run(t.Context(), req)
		require.NoError(t, err)
		return rec
	}

	rec := run(LoggingStatus)
	assert.Contains(t, rec.status, "root: calling Write")
	assert.Contains(t, rec.status, "root: Write run by the harness")
	assert.Len(t, rec.persistent, 2, "the CLI record and the summary: %v", rec.persistent)

	rec = run(LoggingTools)
	assert.Contains(t, rec.persistent, `root called Write {"content":"# Notes\n","file_path":"NOTES.md"}: run by the harness`,
		"the streamed arguments are accumulated")

	rec = run(LoggingFull)
	all := strings.Join(rec.persistent, "\n")
	assert.Contains(t, all, `root called Write {"content":"# Notes\n","file_path":"NOTES.md"}: run by the harness: `+
		"File created successfully at: NOTES.md")
	assert.Contains(t, all, "root: Done: NOTES.md lists the changes.")
	assert.NotContains(t, all, "I'll write the notes.", "the narration is not the answer")

	rec = run(LoggingNone)
	assert.Empty(t, rec.status)
	assert.Empty(t, rec.persistent)
}

func TestHarnessTokenIsRedacted(t *testing.T) {
	f := useFakeClaude(t)
	t.Setenv("FAKE_CLAUDE_MODE", "token")
	var rec records
	req := f.request("Show the token")
	req.Logging = LoggingFull
	req.Progress = rec.progress
	req.Env = map[string]string{claudeOAuthTokenVar: fakeOAuthToken}
	res, err := Run(t.Context(), req)
	require.NoError(t, err)
	assert.Equal(t, "The token is "+fakeOAuthToken+".", res.Output, "the answer itself is not altered")
	assert.True(t, res.OutputContainsSecret)
	all := strings.Join(append(rec.persistent, rec.status...), "\n")
	assert.Contains(t, all, `root called Bash {"command":"echo [secret]"}: run by the harness: [secret]`)
	assert.NotContains(t, all, fakeOAuthToken)
	assert.Contains(t, strings.Join(res.Warnings, "\n"), "the answer contains the value of a secret")
}

func TestHarnessResume(t *testing.T) {
	f := useFakeClaude(t)
	p, err := newHarnessFactory(harnessRun{dir: t.TempDir(), env: claudeEnv(Request{InheritEnvironment: true}, ""),
		info: &harnessInfo{}})(&latest.HarnessConfig{Type: "claude-code"})
	require.NoError(t, err)
	var events []harness.EventType
	require.NoError(t, p.Resume(t.Context(), "the-session", "Continue", func(e harness.Event) { events = append(events, e.Type) }))
	inv := f.invocations(t)[0]
	assert.Contains(t, strings.Join(inv["arg"], " "), "--resume the-session -p")
	assert.Equal(t, "Continue", inv.get("stdin"))
	assert.NotContains(t, events, harness.EventText, "text is not forwarded")
	assert.Contains(t, events, harness.EventResult)
	assert.Error(t, p.Resume(t.Context(), "", "Continue", func(harness.Event) {}))
}

func TestHarnessAnnounceAndBilling(t *testing.T) {
	f := useFakeClaude(t)
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-api03-inherited-key-0123456789")
	t.Setenv("FAKE_CLAUDE_KEY_SOURCE", "ANTHROPIC_API_KEY")
	var rec records
	var warned []string
	req := f.request("Write NOTES.md")
	req.AnnounceHarness = true
	req.Progress = rec.progress
	req.Warn = func(m string) { warned = append(warned, m) }
	res, err := Run(t.Context(), req)
	require.NoError(t, err)
	require.Len(t, warned, 2, "%v", warned)
	assert.Contains(t, warned[0], "coding-harness agents (agents.root.harness: claude-code) run without tool approval")
	assert.Contains(t, warned[1], "claude-code would use ANTHROPIC_API_KEY (inherited from the environment of the Pulumi CLI)")
	assert.NotContains(t, strings.Join(warned, "\n"), "sk-ant-api03")
	assert.Contains(t, res.Warnings, "claude-code authenticated with ANTHROPIC_API_KEY, which bills API usage, not a Claude "+
		"subscription")
	assert.Contains(t, rec.persistent, "claude-code 2.1.283 (claude-sonnet-4-6), apiKeySource ANTHROPIC_API_KEY")

	// A failed run reports the credential source through Warn.
	t.Setenv("FAKE_CLAUDE_MODE", "error")
	warned = nil
	req.AnnounceHarness = false
	_, err = Run(t.Context(), req)
	require.Error(t, err)
	assert.Equal(t, []string{"claude-code authenticated with ANTHROPIC_API_KEY, which bills API usage, not a Claude " +
		"subscription"}, warned)

	// Hidden names.
	t.Setenv("FAKE_CLAUDE_MODE", "")
	warned = nil
	req.AnnounceHarness, req.HideNames = true, true
	_, err = Run(t.Context(), req)
	require.NoError(t, err)
	assert.Contains(t, warned[0], "coding-harness agents (an agent's harness: claude-code) run without tool approval")
	assert.NotContains(t, strings.Join(warned, "\n"), "root")
}

func TestUnsupportedHarnessTypes(t *testing.T) {
	for _, typ := range []string{"pi", "opencode"} {
		t.Run(typ, func(t *testing.T) {
			doc := []byte("agents:\n  root:\n    harness:\n      type: " + typ + "\n")
			want := typ + " harness agents (agents.root.harness) are not supported by AgentRun yet: only claude-code and " +
				"codex are; run them with docker agent run (a ChatGPT subscription can also run a chatgpt/<model> model in " +
				"docker-agent's own runtime, without a coding harness)"
			for _, allow := range []bool{true, false} {
				problems, warnings := CheckConfig(t.Context(), "test.yaml", doc, CheckOptions{AllowHarnessAgents: allow})
				assert.Equal(t, []string{want}, problems, "allowHarnessAgents %v", allow)
				assert.Empty(t, warnings)
			}
			_, err := Run(t.Context(), Request{YAML: doc, Prompt: "hi", AllowHarnessAgents: true})
			require.Error(t, err)
			assert.Contains(t, err.Error(), want)

			_, err = newHarnessFactory(harnessRun{})(&latest.HarnessConfig{Type: typ})
			assert.ErrorContains(t, err, typ+" harness agents are not supported by AgentRun yet")
		})
	}
}

func TestHarnessUnsupportedOS(t *testing.T) {
	prev := harnessOS
	harnessOS = "windows"
	t.Cleanup(func() { harnessOS = prev })
	const want = "coding-harness agents in AgentRun are supported on Linux and macOS, not on windows"
	problems, _ := CheckConfig(t.Context(), "test.yaml", []byte(claudeAgent), CheckOptions{AllowHarnessAgents: true})
	assert.Equal(t, []string{want}, problems)
	problems, _ = CheckConfig(t.Context(), "test.yaml", []byte(claudeAgent), CheckOptions{})
	assert.Equal(t, []string{want}, problems, "the OS, not the opt-in, is the reason")
	_, err := Run(t.Context(), Request{YAML: []byte(claudeAgent), Prompt: "hi", AllowHarnessAgents: true})
	assert.ErrorContains(t, err, want)
	_, err = newHarnessFactory(harnessRun{})(&latest.HarnessConfig{Type: "claude-code"})
	assert.ErrorContains(t, err, want)
}

func TestCheckConfigWarnings(t *testing.T) {
	for _, k := range []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_BASE_URL", "CLAUDE_CODE_USE_BEDROCK",
		"CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_FOUNDRY", "IS_SANDBOX", claudeOAuthTokenVar} {
		t.Setenv(k, "")
	}
	prevEUID := geteuid
	t.Cleanup(func() { geteuid = prevEUID })
	euid := 1000
	geteuid = func() int { return euid }

	const instruction = "    instruction: Write good notes.\n"
	const structured = "    structured_output:\n      name: review\n      schema:\n        type: object\n"
	harnessWith := func(extra string) string { return claudeAgent + extra }
	inheritModel := func(model string) string {
		return "models:\n  m:\n    provider: openai\n    model: gpt-5-mini\nagents:\n  root:\n    model: " + model +
			"\n    harness:\n      type: claude-code\n"
	}
	allowed := CheckOptions{AllowHarnessAgents: true, InheritEnvironment: true}
	check := func(doc string, opts CheckOptions) []string {
		t.Helper()
		problems, warnings := CheckConfig(t.Context(), "test.yaml", []byte(doc), opts)
		require.Empty(t, problems)
		return warnings
	}
	has := func(warnings []string, sub string) bool {
		return slices.ContainsFunc(warnings, func(w string) bool { return strings.Contains(w, sub) })
	}
	const w1 = "coding-harness agents (agents.root.harness: claude-code) run without tool approval: the claude CLI runs " +
		"its own tools (shell commands, file edits, web requests) on the machine that runs pulumi up"
	const w2 = "claude-code would use ANTHROPIC_API_KEY (inherited from the environment of the Pulumi CLI) instead of a " +
		"Claude subscription login and bill API usage to it; pass CLAUDE_CODE_OAUTH_TOKEN in env, set inheritEnvironment: " +
		"false, or blank it with env: {ANTHROPIC_API_KEY: \"\"}"
	const w3 = "the provider runs as root, and Claude Code refuses --dangerously-skip-permissions as root"
	const w7 = "deletePrompt runs with toolApproval deny: the agent can only run read-only tools"

	t.Run("W1 only", func(t *testing.T) {
		w := check(claudeAgent, allowed)
		require.Len(t, w, 1)
		assert.Equal(t, w1+", in workingDir or a temporary directory, with its user's Claude login and settings and any "+
			".claude/ settings or .mcp.json in workingDir (without a trust prompt); toolApproval and permissions do not "+
			"apply to it", w[0])
		assert.Empty(t, check("agents:\n  root:\n    model: openai/gpt-5-mini\n", allowed), "no harness, no W1")
	})
	t.Run("W1 without the opt-in is a problem instead", func(t *testing.T) {
		problems, warnings := CheckConfig(t.Context(), "test.yaml", []byte(claudeAgent), CheckOptions{})
		assert.Len(t, problems, 1)
		assert.Empty(t, warnings)
	})
	t.Run("W2", func(t *testing.T) {
		t.Setenv("ANTHROPIC_API_KEY", "sk-ant-api03-inherited-key-0123456789")
		w := check(claudeAgent, allowed)
		assert.Contains(t, w, w2)
		assert.False(t, has(w, "sk-ant-api03"), "never the value")
		assert.False(t, has(check(claudeAgent, CheckOptions{AllowHarnessAgents: true}), "ANTHROPIC_API_KEY"),
			"not inherited")

		explicit := allowed
		explicit.EnvKeys = map[string]bool{"ANTHROPIC_API_KEY": true}
		assert.True(t, has(check(claudeAgent, explicit), "claude-code would use ANTHROPIC_API_KEY (from env)"))

		blank := allowed
		blank.EnvKeys = map[string]bool{"ANTHROPIC_API_KEY": false}
		assert.False(t, has(check(claudeAgent, blank), "ANTHROPIC_API_KEY"), "env: {ANTHROPIC_API_KEY: \"\"} blanks it")

		token := allowed
		token.EnvKeys = map[string]bool{claudeOAuthTokenVar: true}
		w = check(claudeAgent, token)
		assert.Contains(t, w, "inherited ANTHROPIC_API_KEY is not passed to claude because env sets CLAUDE_CODE_OAUTH_TOKEN")
		assert.False(t, has(w, "would use ANTHROPIC_API_KEY"))
	})
	t.Run("W2 variants", func(t *testing.T) {
		t.Setenv("ANTHROPIC_AUTH_TOKEN", "an-inherited-auth-token")
		t.Setenv("CLAUDE_CODE_USE_BEDROCK", "1")
		t.Setenv("CLAUDE_CODE_USE_VERTEX", "0")
		t.Setenv("ANTHROPIC_BASE_URL", "https://proxy.example.com")
		w := check(claudeAgent, allowed)
		assert.True(t, has(w, "claude-code would use ANTHROPIC_AUTH_TOKEN (inherited"))
		assert.True(t, has(w, "CLAUDE_CODE_USE_BEDROCK (inherited from the environment of the Pulumi CLI) routes Claude "+
			"Code to Amazon Bedrock instead of a Claude subscription login"))
		assert.False(t, has(w, "Vertex"), "a false flag")

		// A flag in env counts by its value when it is known, and as set when it is not.
		flags := allowed
		flags.EnvKeys = map[string]bool{"CLAUDE_CODE_USE_FOUNDRY": true, "CLAUDE_CODE_USE_VERTEX": true}
		flags.EnvValues = map[string]string{"CLAUDE_CODE_USE_FOUNDRY": "0"}
		t.Setenv("CLAUDE_CODE_USE_BEDROCK", "")
		w = check(claudeAgent, flags)
		assert.False(t, has(w, "Microsoft Foundry"), "env: {CLAUDE_CODE_USE_FOUNDRY: \"0\"}")
		assert.True(t, has(w, "CLAUDE_CODE_USE_VERTEX (from env) routes Claude Code to Google Vertex AI"), "an unknown value")
		flags.EnvValues["CLAUDE_CODE_USE_FOUNDRY"] = "1"
		assert.True(t, has(check(claudeAgent, flags), "CLAUDE_CODE_USE_FOUNDRY (from env) routes Claude Code to Microsoft Foundry"))
		assert.True(t, has(w, "ANTHROPIC_BASE_URL (inherited from the environment of the Pulumi CLI) sends Claude Code's "+
			"requests to ANTHROPIC_BASE_URL"))
		assert.False(t, has(w, "proxy.example.com"))
	})
	t.Run("W8 a token of the Pulumi CLI that inheritEnvironment false withholds", func(t *testing.T) {
		const w8 = "CLAUDE_CODE_OAUTH_TOKEN is set in the environment of the Pulumi CLI, but inheritEnvironment: false keeps " +
			"it from the claude CLI, which then uses the Claude login of this machine; pass the token in env (for example " +
			"from secret config) to use it, or set it to \"\" in env to use the login"
		withheld := CheckOptions{AllowHarnessAgents: true}
		assert.False(t, has(check(claudeAgent, withheld), w8), "no token in the environment")
		t.Setenv(claudeOAuthTokenVar, fakeOAuthToken)
		w := check(claudeAgent, withheld)
		assert.Contains(t, w, w8)
		assert.False(t, has(w, fakeOAuthToken), "never the value")
		withheld.EnvKeys = map[string]bool{"OTHER": true}
		assert.Contains(t, check(claudeAgent, withheld), w8, "env without the token")

		token := withheld
		token.EnvKeys = map[string]bool{claudeOAuthTokenVar: true}
		assert.False(t, has(check(claudeAgent, token), w8), "env sets the token (a secret value is fine)")
		blank := withheld
		blank.EnvKeys = map[string]bool{claudeOAuthTokenVar: false}
		assert.False(t, has(check(claudeAgent, blank), w8), "env blanks it on purpose")
		apiKey := withheld
		apiKey.EnvKeys = map[string]bool{"ANTHROPIC_API_KEY": true}
		assert.False(t, has(check(claudeAgent, apiKey), w8), "the CLI uses the API key instead of the login")
		ce := newClaudeEnvironment([]string{claudeOAuthTokenVar + "=" + fakeOAuthToken}, map[string]string{
			"ANTHROPIC_API_KEY": "sk-ant-api03-explicit"}, false, "")
		assert.False(t, ce.noKeySource().withheldToken, "the record agrees: no withheld-token clause with an API key")
		unknown := withheld
		unknown.EnvKeys, unknown.EnvUnknown = nil, true
		assert.False(t, has(check(claudeAgent, unknown), w8), "an unknown env may set the token")
		assert.False(t, has(check(claudeAgent, allowed), w8), "inheritEnvironment: true passes the token")
		t.Setenv(claudeOAuthTokenVar, "")
		assert.False(t, has(check(claudeAgent, withheld), w8), "an empty token")
	})
	t.Run("W3", func(t *testing.T) {
		assert.False(t, has(check(claudeAgent, allowed), w3))
		euid = 0
		defer func() { euid = 1000 }()
		assert.True(t, has(check(claudeAgent, allowed), w3+": run pulumi up as a non-root user, or, in an isolated "+
			"container, set IS_SANDBOX=1 in env"))
		sandbox := allowed
		sandbox.EnvKeys = map[string]bool{"IS_SANDBOX": true}
		assert.False(t, has(check(claudeAgent, sandbox), w3), "an unknown IS_SANDBOX")
		sandbox.EnvValues = map[string]string{"IS_SANDBOX": "0"}
		assert.True(t, has(check(claudeAgent, sandbox), w3), "IS_SANDBOX=0 is no opt-out")
		sandbox.EnvValues = map[string]string{"IS_SANDBOX": "1"}
		assert.False(t, has(check(claudeAgent, sandbox), w3))
		t.Setenv("IS_SANDBOX", "1")
		assert.False(t, has(check(claudeAgent, CheckOptions{AllowHarnessAgents: true}), w3), "IS_SANDBOX is kept")
	})
	t.Run("W4", func(t *testing.T) {
		assert.True(t, has(check(harnessWith(instruction), allowed), "agents.root.instruction is not sent to the coding "+
			"harness: docker-agent sends only the prompt (to a sub-agent: its task); put the instructions into the prompt "+
			"or into a CLAUDE.md in workingDir"))
		assert.False(t, has(check(claudeAgent, allowed), "instruction"))
	})
	t.Run("W5", func(t *testing.T) {
		assert.True(t, has(check(harnessWith(structured), allowed), "agents.root.structured_output is not enforced for "+
			"coding harnesses: AgentRun decodes Claude Code's final result as JSON (a surrounding Markdown code fence is "+
			"removed), so ask for JSON in the prompt"))
		assert.False(t, has(check(claudeAgent, allowed), "structured_output"))
	})
	t.Run("W6", func(t *testing.T) {
		assert.True(t, has(check(inheritModel("m"), allowed), `agents.root.harness has no model, so docker-agent passes `+
			`the agent's model "m" to claude --model; set harness.model (for example sonnet)`))
		assert.True(t, has(check(inheritModel("openai/gpt-5-mini"), allowed), "has no model"))
		assert.False(t, has(check(inheritModel("anthropic/claude-sonnet-4-6"), allowed), "has no model"))
		assert.False(t, has(check(inheritModel("claude-sonnet-4-6"), allowed), "has no model"))
		assert.False(t, has(check(claudeAgent, allowed), "has no model"), "harness.model is set")
		hidden := allowed
		hidden.HideNames = true
		w := check(inheritModel("m"), hidden)
		assert.True(t, has(w, "an agent's harness has no model, so docker-agent passes the agent's model to claude --model"))
	})
	t.Run("W7", func(t *testing.T) {
		plain := "agents:\n  root:\n    model: openai/gpt-5-mini\n"
		del := CheckOptions{DeletePrompt: true, ToolApprovalDeny: true}
		assert.True(t, has(check(plain, del), w7+" and calls that permissions allow, which is rarely enough to clean up; "+
			"add permissions allow rules for the tools the cleanup needs (for example shell or write_file), or set "+
			"toolApproval: allow"))
		assert.Empty(t, check(plain, CheckOptions{DeletePrompt: true}), "toolApproval allow")
		assert.Empty(t, check(plain, CheckOptions{ToolApprovalDeny: true}), "no deletePrompt")
		withHarness := allowed
		withHarness.DeletePrompt, withHarness.ToolApprovalDeny = true, true
		assert.False(t, has(check(claudeAgent, withHarness), w7), "a harness agent can clean up")
	})
	t.Run("hidden names", func(t *testing.T) {
		hidden := allowed
		hidden.HideNames = true
		w := check(harnessWith(instruction+structured), hidden)
		assert.True(t, has(w, "coding-harness agents (an agent's harness: claude-code) run without tool approval"))
		assert.True(t, has(w, "an agent's instruction is not sent"))
		assert.True(t, has(w, "an agent's structured_output is not enforced"))
		assert.False(t, has(w, "root"))
		problems, _ := CheckConfig(t.Context(), "test.yaml", []byte(claudeAgent), CheckOptions{HideNames: true})
		assert.Equal(t, []string{"coding-harness agents (an agent's harness: claude-code) run only with allowHarnessAgents: " +
			"true: they start an external coding agent CLI that runs its own tools without docker-agent's tool approval"},
			problems)
	})
	t.Run("invalid configuration", func(t *testing.T) {
		problems, warnings := CheckConfig(t.Context(), "test.yaml", []byte("agents: ["), allowed)
		assert.Nil(t, problems, "the validator reports it")
		assert.Nil(t, warnings)
	})
}

func TestClaudeHint(t *testing.T) {
	assert.Equal(t, "; "+authHint, claudeHint("", "", 401))
	assert.Equal(t, "; "+authHint, claudeHint("", "", 403))
	assert.Equal(t, "; "+authHint, claudeHint("Invalid API key · Please run /login", "", 0))
	assert.Equal(t, "; "+contentionHint, claudeHint("Could not refresh your login because another Claude Code process is "+
		"refreshing it (or exited mid-refresh) · sign in again with /login", "", 401))
	assert.Equal(t, "; "+rootHint, claudeHint("", "... cannot be used with root/sudo privileges ...", 0))
	assert.Empty(t, claudeHint("rate limited", "some stderr", 429))
}
