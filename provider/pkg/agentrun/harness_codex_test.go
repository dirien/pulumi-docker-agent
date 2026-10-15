// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

//go:build linux || darwin

package agentrun

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/docker/docker-agent/pkg/config/latest"
	"github.com/docker/docker-agent/pkg/harness"
	baseharness "github.com/rumpl/harness"
	"github.com/rumpl/harness/codex"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dirien/pulumi-docker-agent/provider/pkg/agentrun/fakeopenai"
)

// The codex tests run the fake Codex CLI of testdata/fake-codex (see its README), never the
// real one: PATH holds the fake's directory and the system directories only, and CODEX_HOME is
// a directory of the test's own with a fake ChatGPT login. The fake's mode and log come from
// the test's environment, because AgentRun redacts the values of Request.Env.

const codexAgent = `agents:
  root:
    description: Writes notes with Codex
    harness:
      type: codex
      model: gpt-6.1-sol
`

// The fake ChatGPT login of a test's CODEX_HOME.
const (
	fakeCodexAccessToken  = "eyJhbGciOiJSUzI1NiJ9.fake-codex-access-token-0123456789"
	fakeCodexRefreshToken = "rt_fake-codex-refresh-token-0123456789"
	fakeCodexEmail        = "someone@example.com"
	fakeCodexAccount      = "acct-fake-0123456789"
	fakeCodexAPIKey       = "sk-proj-fake-codex-api-key-0123456789"
	fakeCodexThread       = "0199a213-81c0-7800-8aa1-bbab2a035a53"
)

// fakeIDToken is an ID token of a ChatGPT login with a plan claim (not signed).
func fakeIDToken(plan string) string {
	b, _ := json.Marshal(map[string]any{"email": fakeCodexEmail, "https://api.openai.com/auth": map[string]any{
		"chatgpt_plan_type": plan, "chatgpt_account_id": fakeCodexAccount}})
	return "eyJhbGciOiJSUzI1NiJ9." + base64.RawURLEncoding.EncodeToString(b) + ".fake-signature-0123456789"
}

// chatgptAuthJSON is the auth.json of a ChatGPT login, as codex login writes it.
func chatgptAuthJSON(plan string) string {
	b, _ := json.Marshal(map[string]any{"auth_mode": "chatgpt", "OPENAI_API_KEY": nil,
		"tokens": map[string]any{"id_token": fakeIDToken(plan), "access_token": fakeCodexAccessToken,
			"refresh_token": fakeCodexRefreshToken, "account_id": fakeCodexAccount},
		"last_refresh": "2026-10-13T09:00:00Z"})
	return string(b)
}

// fakeCodexCLI is a copy of the fake codex, the files it writes and its CODEX_HOME.
type fakeCodexCLI struct {
	dir, log, pidfile, home string
}

// useFakeCodex copies the fake codex into a directory of the test's own, puts it first on PATH
// (before the extra directories), points its log and pidfile into another one, and gives it a
// CODEX_HOME with a ChatGPT Plus login.
func useFakeCodex(t *testing.T, extraPath ...string) fakeCodexCLI {
	t.Helper()
	src := filepath.Join("testdata", "fake-codex")
	dir := t.TempDir()
	entries, err := os.ReadDir(src)
	require.NoError(t, err)
	for _, e := range entries {
		name := e.Name()
		if name == "README.md" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(src, name))
		require.NoError(t, err)
		mode := os.FileMode(0o644)
		if name == "codex" || name == "bwrap" {
			mode = 0o755
		}
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), b, mode))
	}
	path := strings.Join(slices.Concat([]string{dir}, extraPath, []string{"/usr/bin", "/bin"}), string(os.PathListSeparator))
	t.Setenv("PATH", path)
	bin, err := lookPathIn("codex", path)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(dir, "codex"), bin, "codex must resolve to the fake")

	state := t.TempDir()
	f := fakeCodexCLI{dir: dir, log: filepath.Join(state, "codex.log"), pidfile: filepath.Join(state, "codex.pid"),
		home: t.TempDir()}
	f.writeHome(t, "auth.json", chatgptAuthJSON("plus"))
	t.Setenv("CODEX_HOME", f.home)
	t.Setenv("FAKE_CODEX_LOG", f.log)
	t.Setenv("FAKE_CODEX_PIDFILE", f.pidfile)
	// Variables of the developer's environment that change what the fake or the warnings see.
	for _, k := range []string{"FAKE_CODEX_MODE", "FAKE_CODEX_STDERR", "FAKE_CODEX_PRINT", "FAKE_CODEX_IGNORE_TERM",
		"FAKE_CODEX_NO_LAST", "FAKE_CODEX_VERSION", "FAKE_CODEX_VERSION_FAIL", "FAKE_CODEX_SANDBOX", "FAKE_CODEX_ROTATED",
		"FAKE_BWRAP", "FAKE_MARKER", codexAPIKeyVar, codexAccessTokenVar, "OPENAI_IDENTITY_TOKEN_FILE",
		"OPENAI_FEDERATION_RULE_ID", "RUST_LOG", "CODEX_THREAD_ID", "CODEX_SANDBOX"} {
		t.Setenv(k, "")
	}
	// Unset, not empty: the workload identity variables count when they are set at all.
	for _, k := range codexWorkloadIdentityVars {
		require.NoError(t, os.Unsetenv(k))
	}
	prev := codexSystemConfigDir
	codexSystemConfigDir = t.TempDir()
	t.Cleanup(func() { codexSystemConfigDir = prev })
	return f
}

// writeHome writes a file into the fake's CODEX_HOME.
func (f fakeCodexCLI) writeHome(t *testing.T, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(f.home, name), []byte(content), 0o600))
}

// request is a run of the codex agent that inherits the test's environment.
func (f fakeCodexCLI) request(prompt string) Request {
	return Request{Name: "test", YAML: []byte(codexAgent), Prompt: prompt, InheritEnvironment: true,
		AllowHarnessAgents: true}
}

// entries parses the fake's log; it is nil when the fake never ran.
func (f fakeCodexCLI) entries() []invocation {
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
		case line == "int" || line == "term":
			cur[line] = append(cur[line], line)
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

// kinds returns the kinds of the fake's invocations, in order.
func (f fakeCodexCLI) kinds() []string {
	var out []string
	for _, e := range f.entries() {
		out = append(out, e.get("kind"))
	}
	return out
}

// execs returns the turns the fake ran and fails the test when it ran none.
func (f fakeCodexCLI) execs(t *testing.T) []invocation {
	t.Helper()
	var out []invocation
	for _, e := range f.entries() {
		if e.get("kind") == "exec" {
			out = append(out, e)
		}
	}
	require.NotEmpty(t, out, "the fake codex did not run a turn (log at %s)", f.log)
	return out
}

func TestFakeCodexStreamMatchesTheParser(t *testing.T) {
	// What docker-agent's codex driver makes of each fixture: the subset it understands. A
	// driver upgrade that changes it fails here first, and AgentRun's adapter must be reviewed.
	type ev struct {
		typ          baseharness.EventType
		tool, output string
	}
	completed := []ev{{typ: "result"}} // turn.completed: a result with the usage
	text := func(s string) []ev { return []ev{{typ: "text", output: s}, {typ: "result", output: s}} }
	cat := func(parts ...[]ev) []ev { return slices.Concat(parts...) }
	failed := []ev{{typ: "session_id"}}
	expected := map[string][]ev{
		"session.jsonl": cat([]ev{{typ: "session_id"}, {typ: "tool_call", tool: "Bash"},
			{typ: "tool_result", tool: "Bash", output: "NOTES.md\n"}}, text("I'll write NOTES.md now."),
			text("Done: NOTES.md lists the changes."), completed),
		"session-json.jsonl": cat([]ev{{typ: "session_id"}},
			text("```json\n{\"risk\":\"low\",\"summary\":\"fake review\"}\n```"), completed),
		"session-schema.jsonl": cat([]ev{{typ: "session_id"}}, text(`{"risk":"low","summary":"fake review"}`), completed),
		"session-token.jsonl": cat([]ev{{typ: "session_id"}, {typ: "tool_call", tool: "Bash"},
			{typ: "tool_result", tool: "Bash", output: "__TOKEN__\n"}}, text("The token is __TOKEN__."), completed),
		// The driver counts a declined command (exit_code null) as a success.
		"session-declined.jsonl": cat([]ev{{typ: "session_id"}, {typ: "tool_call", tool: "Bash"},
			{typ: "tool_result", tool: "Bash"}}, text("The network is not available."), completed),
		"session-tool-failed.jsonl": cat([]ev{{typ: "session_id"}, {typ: "tool_call", tool: "Bash"},
			{typ: "tool_result", tool: "Bash", output: "cat: missing.txt: No such file or directory\n"}},
			text("Some tools failed."), completed),
		"session-item-error.jsonl":     cat([]ev{{typ: "session_id"}}, text("Done: NOTES.md lists the changes."), completed),
		"session-retry.jsonl":          cat([]ev{{typ: "session_id"}}, text("Done: NOTES.md lists the changes."), completed),
		"session-failed-auth.jsonl":    failed,
		"session-refresh-reused.jsonl": failed,
		"session-usage-limit.jsonl":    failed,
		"session-not-included.jsonl":   failed,
		"session-quota.jsonl":          failed,
		"session-unknown-model.jsonl":  failed,
		"session-no-message.jsonl":     cat([]ev{{typ: "session_id"}}, completed),
		"session-incomplete.jsonl":     {{typ: "session_id"}},
		"session-not-json.jsonl":       cat([]ev{{typ: "session_id"}}, text("Done: NOTES.md lists the changes."), completed),
		"session-sandbox-denied.jsonl": cat([]ev{{typ: "session_id"}, {typ: "tool_call", tool: "Bash"},
			{typ: "tool_result", tool: "Bash", output: "bwrap: No permissions to create a new namespace, likely because the " +
				"kernel does not allow non-privileged user namespaces.\n"}}, text("I could not run any command."), completed),
		"session-resume.jsonl": cat([]ev{{typ: "session_id"}}, text("Continued: NOTES.md is up to date."), completed),
		"session-lagged.jsonl": cat([]ev{{typ: "session_id"}}, completed),
		"session-collab.jsonl": cat([]ev{{typ: "session_id"}}, text("A reviewer is looking at NOTES.md."), completed),
	}
	// What the driver's validator says: AgentRun is lenient with lines that are no JSON events.
	validation := map[string]string{
		"session-failed-auth.jsonl": "codex turn failed", "session-refresh-reused.jsonl": "codex turn failed",
		"session-usage-limit.jsonl": "codex turn failed", "session-not-included.jsonl": "codex turn failed",
		"session-quota.jsonl": "codex turn failed", "session-unknown-model.jsonl": "codex turn failed",
		"session-incomplete.jsonl": "codex stream ended without turn.completed",
		"session-not-json.jsonl":   "invalid Codex JSON event",
	}
	files, err := filepath.Glob(filepath.Join("testdata", "fake-codex", "*.jsonl"))
	require.NoError(t, err)
	require.Len(t, files, len(expected), "every fixture has expectations")
	for _, file := range files {
		name := filepath.Base(file)
		t.Run(name, func(t *testing.T) {
			want, ok := expected[name]
			require.True(t, ok)
			b, err := os.ReadFile(file)
			require.NoError(t, err)
			lines := strings.Split(strings.TrimSpace(string(b)), "\n")
			assert.Equal(t, `{"type":"thread.started","thread_id":"__THREAD__"}`, lines[0])
			p := codex.New("")
			var got []ev
			var usage *baseharness.Usage
			for _, line := range lines {
				for _, e := range p.ParseStreamLine(line) {
					out := e.ToolOutput
					switch e.Type {
					case baseharness.EventText:
						out = e.Text
					case baseharness.EventResult:
						out = e.Result
						if e.Usage != nil {
							usage = e.Usage
						}
					}
					got = append(got, ev{typ: e.Type, tool: e.ToolName, output: out})
				}
			}
			assert.Equal(t, want, got)
			if name == "session.jsonl" {
				require.NotNil(t, usage)
				assert.Equal(t, baseharness.Usage{InputTokens: 24763, OutputTokens: 122, CacheReadInputTokens: 24448}, *usage,
					"the driver keeps cached tokens in input_tokens, which double-counts them in docker-agent")
			}

			vp, ok := p.(interface {
				NewStreamValidator() baseharness.StreamValidator
			})
			require.True(t, ok)
			v := vp.NewStreamValidator()
			var verr error
			for _, line := range lines {
				if verr = v.ValidateStreamLine(line); verr != nil {
					break
				}
			}
			if verr == nil {
				verr = v.EndStream()
			}
			if want := validation[name]; want == "" {
				assert.NoError(t, verr)
			} else {
				assert.ErrorContains(t, verr, want)
			}
		})
	}

	// docker-agent's driver passes the prompt in argv and always bypasses the sandbox; AgentRun
	// builds its own arguments (codexArgs). A change of the driver's shape needs a review.
	p, ok := codex.New("m").(baseharness.ResumableCommandProvider)
	require.True(t, ok)
	assert.Equal(t, []string{"codex", "exec", "--json", "--dangerously-bypass-approvals-and-sandbox", "-m", "m", "x"},
		p.PrintArgs("x"))
	assert.Equal(t, []string{"codex", "exec", "resume", "t", "--json", "--dangerously-bypass-approvals-and-sandbox", "-m",
		"m", "x"}, p.ResumeArgs("t", "x"))
}

func TestCodexArgs(t *testing.T) {
	last := "--output-last-message=/s/last"
	assert.Equal(t, []string{"exec", "--json", "--color=never", "--skip-git-repo-check", "--sandbox=workspace-write",
		`--config=approval_policy="never"`, last, "-"}, codexArgs(codexInvocation{deny: true, lastMessage: "/s/last"}))
	assert.Equal(t, []string{"exec", "--json", "--color=never", "--skip-git-repo-check",
		"--dangerously-bypass-approvals-and-sandbox", "--model=gpt-6.1-sol", "--output-schema=/s/schema.json", last, "-"},
		codexArgs(codexInvocation{model: "gpt-6.1-sol", schema: "/s/schema.json", lastMessage: "/s/last"}))
	// Every flag before resume; the thread and `-` last.
	assert.Equal(t, []string{"exec", "--json", "--color=never", "--skip-git-repo-check", "--sandbox=workspace-write",
		`--config=approval_policy="never"`, "--model=m", last, "resume", "thread-1", "-"},
		codexArgs(codexInvocation{deny: true, model: "m", lastMessage: "/s/last", resume: "thread-1"}))
	// A value that starts with - stays one argument.
	assert.Contains(t, codexArgs(codexInvocation{model: "--yolo", lastMessage: "x"}), "--model=--yolo")
}

func TestHarnessCodex(t *testing.T) {
	f := useFakeCodex(t)
	var rec records
	req := f.request("Write NOTES.md")
	req.Progress = rec.progress
	res, err := Run(t.Context(), req)
	require.NoError(t, err)
	assert.Equal(t, "Done: NOTES.md lists the changes.", res.Output, "only Codex's final message")
	assert.Equal(t, "root", res.AgentName)
	assert.Equal(t, []string{"Bash", "mcp__docs__search", "web_search", "apply_patch"}, res.ToolCalls)
	assert.Empty(t, res.RejectedToolCalls)
	// Codex's input_tokens includes the cached tokens. A ChatGPT plan pays for the turn: no
	// per-token cost; the summary names the plan with the API-price estimate of gpt-6.1-sol (2,
	// 0.1 and 10 USD per 1M fresh input, cached input and output tokens).
	assert.Equal(t, int64(24763), res.Usage.InputTokens)
	assert.Equal(t, int64(122), res.Usage.OutputTokens)
	assert.Zero(t, res.Usage.Cost, "a ChatGPT plan bills no per-token cost")
	price := fmt.Sprintf("$%.4f", (315*2+24448*0.1+122*10)/1e6)
	assert.False(t, res.HasStructuredOutput)
	assert.Empty(t, res.Warnings)

	assert.Equal(t, []string{"version", "sandbox", "exec"}, f.kinds(), "the checks run before the first turn")
	inv := f.execs(t)[0]
	args := inv["arg"]
	assert.Equal(t, strconv.Itoa(len(args)), inv.get("argc"))
	last := inv.get("last_message")
	assert.Equal(t, []string{"exec", "--json", "--color=never", "--skip-git-repo-check", "--sandbox=workspace-write",
		`--config=approval_policy="never"`, "--model=gpt-6.1-sol", "--output-last-message=" + last, "-"}, args)
	assert.NotContains(t, args, "Write NOTES.md", "the prompt is not an argument")
	assert.Equal(t, "Write NOTES.md", inv.get("stdin"))
	assert.Equal(t, "workspace-write", inv.get("sandbox"))
	assert.NoFileExists(t, last, "the final message's file is removed")
	cwd := inv.get("cwd")
	assert.Contains(t, filepath.Base(cwd), "pulumi-docker-agent-work-")
	assert.Equal(t, cwd, inv.get("pwd_env"), "PWD is the working directory")
	assert.Equal(t, f.home, inv.get("codex_home"))
	assert.Equal(t, "set", inv.get("cookie"), "the CLI gets the run's cookie")
	assert.Equal(t, "set", inv.get("agentrun_cookie"), "and the run child's")

	record := "codex 0.162.1 (gpt-6.1-sol), sandbox workspace-write, auth the ChatGPT login of this machine (" +
		filepath.Join(f.home, "auth.json") + ", plan plus), CODEX_HOME " + f.home + ", thread " + fakeCodexThread +
		", billed to the ChatGPT plan, not per token (API price about " + price + ")"
	assert.Contains(t, rec.persistent, record)
	assert.Contains(t, rec.persistent[len(rec.persistent)-1], "; no per-token cost: billed to the ChatGPT plan (API "+
		"price about "+price+")")
	all := strings.Join(append(rec.persistent, rec.status...), "\n")
	assert.NotContains(t, all, fakeCodexEmail, "never the login's e-mail address")
	assert.NotContains(t, all, fakeCodexAccount, "nor its account")
	assert.Contains(t, rec.persistent[len(rec.persistent)-1], "agent run finished")
}

func TestHarnessCodexAllow(t *testing.T) {
	f := useFakeCodex(t)
	t.Setenv("FAKE_CODEX_SANDBOX", "unavailable")
	var rec records
	req := f.request("Write NOTES.md")
	req.ToolApproval = ToolApprovalAllow
	req.Progress = rec.progress
	_, err := Run(t.Context(), req)
	require.NoError(t, err, "toolApproval allow does not check the sandbox it turns off")
	assert.Equal(t, []string{"version", "exec"}, f.kinds())
	inv := f.execs(t)[0]
	assert.Equal(t, "off", inv.get("sandbox"))
	assert.Contains(t, inv["arg"], "--dangerously-bypass-approvals-and-sandbox")
	assert.True(t, slices.ContainsFunc(rec.persistent, func(s string) bool {
		return strings.HasPrefix(s, "codex 0.162.1 (gpt-6.1-sol), sandbox off, auth ")
	}), "%v", rec.persistent)
}

func TestHarnessCodexOutput(t *testing.T) {
	f := useFakeCodex(t)
	t.Run("the final message file wins over the stream", func(t *testing.T) {
		t.Setenv("FAKE_CODEX_MODE", "lagged")
		res, err := Run(t.Context(), f.request("Write NOTES.md"))
		require.NoError(t, err)
		assert.Equal(t, "Done: NOTES.md lists the changes.", res.Output, "the stream dropped the message")
		assert.Contains(t, res.Warnings, "codex: in-process app-server event stream lagged; dropped 3 events")
	})
	t.Run("without the file the last message of the stream", func(t *testing.T) {
		t.Setenv("FAKE_CODEX_NO_LAST", "1")
		res, err := Run(t.Context(), f.request("Write NOTES.md"))
		require.NoError(t, err)
		assert.Equal(t, "Done: NOTES.md lists the changes.", res.Output, "not the commentary before it")
	})
	t.Run("no final message", func(t *testing.T) {
		t.Setenv("FAKE_CODEX_MODE", "no-message")
		_, err := Run(t.Context(), f.request("Write NOTES.md"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "harness codex failed: codex completed its turn without a final message")
	})
	t.Run("lines that are no JSON events", func(t *testing.T) {
		t.Setenv("FAKE_CODEX_MODE", "not-json")
		log := &debugLog{}
		req := f.request("Write NOTES.md")
		req.Log = log.log
		res, err := Run(t.Context(), req)
		require.NoError(t, err)
		assert.Equal(t, "Done: NOTES.md lists the changes.", res.Output)
		assert.Contains(t, log.String(), "codex wrote a line to stdout that is not a JSON event (24 bytes)")
		assert.NotContains(t, log.String(), "notify:", "never the line")
	})
	t.Run("a transient error", func(t *testing.T) {
		t.Setenv("FAKE_CODEX_MODE", "retry")
		res, err := Run(t.Context(), f.request("Write NOTES.md"))
		require.NoError(t, err)
		assert.Empty(t, res.Warnings)
	})
	t.Run("Codex's own warnings", func(t *testing.T) {
		t.Setenv("FAKE_CODEX_MODE", "item-error")
		res, err := Run(t.Context(), f.request("Write NOTES.md"))
		require.NoError(t, err)
		assert.Equal(t, []string{"codex: unknown configuration key `foo` in config.toml",
			"codex: model rerouted: gpt-6.1-sol -> gpt-6-sol (HighDemand)"}, res.Warnings)
		req := f.request("Write NOTES.md")
		req.HideNames = true
		res, err = Run(t.Context(), req)
		require.NoError(t, err)
		assert.Empty(t, res.Warnings, "they can name models")
	})
	t.Run("declined commands", func(t *testing.T) {
		t.Setenv("FAKE_CODEX_MODE", "declined")
		var rec records
		req := f.request("Fetch the page")
		req.Progress = rec.progress
		res, err := Run(t.Context(), req)
		require.NoError(t, err)
		assert.Equal(t, []string{"Bash"}, res.ToolCalls)
		assert.Contains(t, rec.status, "root: Bash run by the harness, failed", "a declined command is not a success")
		assert.Contains(t, res.Warnings, "codex declined 1 command(s) that needed an approval: codex exec does not ask "+
			"for approvals, and toolApproval deny runs it with approval_policy never")
	})
	t.Run("spawned agents", func(t *testing.T) {
		t.Setenv("FAKE_CODEX_MODE", "collab")
		var rec records
		req := f.request("Review")
		req.Progress = rec.progress
		res, err := Run(t.Context(), req)
		require.NoError(t, err)
		assert.Equal(t, []string{"spawn_agent"}, res.ToolCalls)
		assert.True(t, slices.ContainsFunc(rec.persistent, func(s string) bool {
			return strings.Contains(s, ", 1 spawned agent(s) whose tokens and tool calls are not counted")
		}), "%v", rec.persistent)
	})
}

func TestHarnessCodexStructuredOutput(t *testing.T) {
	f := useFakeCodex(t)
	t.Run("decoded", func(t *testing.T) {
		t.Setenv("FAKE_CODEX_MODE", "json")
		req := f.request("Review the change and answer as JSON.")
		req.YAML = []byte(codexAgent + reviewSchema)
		res, err := Run(t.Context(), req)
		require.NoError(t, err)
		assert.True(t, res.HasStructuredOutput)
		assert.Equal(t, map[string]any{"risk": "low", "summary": "fake review"}, res.StructuredOutput, "the fence is removed")
		inv := f.execs(t)
		assert.Empty(t, inv[len(inv)-1].get("output_schema"), "without strict, no --output-schema")
	})
	t.Run("enforced with strict", func(t *testing.T) {
		t.Setenv("FAKE_CODEX_MODE", "schema")
		req := f.request("Review the change and answer as JSON.")
		req.YAML = []byte(codexAgent + reviewSchema + "      strict: true\n")
		res, err := Run(t.Context(), req)
		require.NoError(t, err)
		assert.Equal(t, map[string]any{"risk": "low", "summary": "fake review"}, res.StructuredOutput)
		inv := f.execs(t)
		e := inv[len(inv)-1]
		schemaFile := e.get("output_schema")
		require.NotEmpty(t, schemaFile)
		assert.Contains(t, e["arg"], "--output-schema="+schemaFile)
		var schema map[string]any
		require.NoError(t, json.Unmarshal([]byte(e.get("schema")), &schema))
		assert.Equal(t, "object", schema["type"])
		assert.Contains(t, schema, "properties")
		assert.NoFileExists(t, schemaFile, "the schema file is removed")
		assert.NotEqual(t, e.get("cwd"), filepath.Dir(schemaFile), "not in the working directory")
	})
}

func TestHarnessCodexToolCalls(t *testing.T) {
	f := useFakeCodex(t)
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
	for _, tool := range []string{"Bash", "mcp__docs__search", "web_search", "apply_patch"} {
		assert.Contains(t, rec.status, "root: calling "+tool)
		assert.Contains(t, rec.status, "root: "+tool+" run by the harness")
	}
	rec = run(LoggingFull)
	all := strings.Join(rec.persistent, "\n")
	for _, want := range []string{
		`root called Bash {"command":"/bin/bash -lc ls"}: run by the harness: NOTES.md`,
		`root called mcp__docs__search {"query":"pulumi"}: run by the harness: Pulumi is infrastructure as code.`,
		`root called web_search {"action":{"query":"codex exec json","type":"search"},"query":"codex exec json"}: run ` +
			`by the harness: searched for codex exec json`,
		`root called apply_patch {"changes":[{"kind":"add","path":"NOTES.md"}]}: run by the harness: added NOTES.md`,
		"root: Done: NOTES.md lists the changes.",
	} {
		assert.Contains(t, all, want)
	}
	assert.NotContains(t, all, "I'll write NOTES.md now.", "the commentary is not the answer")

	t.Setenv("FAKE_CODEX_MODE", "tool-failed")
	rec = run(LoggingFull)
	all = strings.Join(rec.persistent, "\n")
	assert.Contains(t, all, `root called Bash {"command":"/bin/bash -lc 'cat missing.txt'"}: run by the harness, failed`)
	assert.Contains(t, all, `root called apply_patch {"changes":[{"kind":"update","path":"README.md"},{"kind":"update",`+
		`"path":"/etc/hosts"}]}: run by the harness, failed`, "paths outside workingDir stay absolute")
	assert.Contains(t, all, `root called mcp__docs__search {"query":"x"}: run by the harness, failed`)
}

func TestHarnessCodexErrorMapping(t *testing.T) {
	f := useFakeCodex(t)
	for _, c := range []struct {
		mode string
		want []string
	}{
		{"failed-auth", []string{"the agent run failed: harness codex failed: codex reported an error: Your access token " +
			"could not be refreshed because your refresh token has expired. Please log out and sign in again.; " +
			codexAuthHint}},
		{"refresh-reused", []string{"refresh token was already used", "; " + codexContentionHint}},
		{"usage-limit", []string{"codex reported an error: You’ve hit your usage limit.", "; " + codexUsageLimitHint}},
		{"not-included", []string{"To use Codex with your ChatGPT plan", "; " + codexPlanHint}},
		{"quota", []string{"Quota exceeded.", "; " + codexQuotaHint}},
		{"unknown-model", []string{"model is not supported", "; " + codexModelHint}},
		{"incomplete", []string{"harness codex failed: codex ended without completing its turn"}},
		{"exit-after-complete", []string{"codex exited with status 1 after completing its turn: ",
			"error: the turn ended with an error notification"}},
		{"cli-args", []string{"codex exited with status 2: ", "error: unexpected argument '--output-last-message' found; " +
			codexDriftHint}},
		{"mcp-failed", []string{"codex exited with status 1: ", "required MCP server", "; " + codexMCPHint}},
	} {
		t.Run(c.mode, func(t *testing.T) {
			t.Setenv("FAKE_CODEX_MODE", c.mode)
			// The login's token and an API key reach stderr in every mode.
			t.Setenv("FAKE_CODEX_STDERR", "the token is "+fakeCodexAccessToken+" and the key "+fakeCodexAPIKey)
			req := f.request("Write NOTES.md")
			req.Env = map[string]string{"OTHER_KEY": fakeCodexAPIKey}
			var warnings []string
			req.Warn = func(m string) { warnings = append(warnings, m) }
			_, err := Run(t.Context(), req)
			require.Error(t, err)
			for _, want := range c.want {
				assert.Contains(t, err.Error(), want)
			}
			for _, secret := range []string{fakeCodexAccessToken, fakeCodexAPIKey, fakeCodexAccessToken[len(fakeCodexAccessToken)-12:]} {
				assert.NotContains(t, err.Error(), secret)
			}
			assert.Empty(t, warnings)
		})
	}
	t.Run("stderr is redacted", func(t *testing.T) {
		t.Setenv("FAKE_CODEX_MODE", "incomplete")
		t.Setenv("FAKE_CODEX_STDERR", "the token is "+fakeCodexAccessToken)
		_, err := Run(t.Context(), f.request("Write NOTES.md"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "codex ended without completing its turn: the token is [secret]",
			"the token of auth.json is a secret of the run")
	})
}

func TestCodexHint(t *testing.T) {
	assert.Equal(t, "; "+codexAuthHint, codexHint("unexpected status 401 Unauthorized: missing bearer", ""))
	assert.Equal(t, "; "+codexAuthHint, codexHint("", "Error: not logged in; run codex login"))
	assert.Equal(t, "; "+codexContentionHint, codexHint("Your access token could not be refreshed because your refresh "+
		"token was already used. Please log out and sign in again.", ""))
	assert.Equal(t, "; "+codexUsageLimitHint, codexHint("You've hit your usage limit.", ""), "a straight apostrophe too")
	assert.Equal(t, "; "+codexForcedLoginHint, codexHint("", "Error: ChatGPT login is required, but an API key is "+
		"currently being used. Logging out."))
	assert.Equal(t, "; "+codexHomeHint, codexHint("", "Error finding codex home: CODEX_HOME points to \"/x\", but that "+
		"path does not exist"))
	assert.Equal(t, "; codex's sandbox could not start: "+codexSandboxAdvice, codexHint("sandbox error: landlock", ""))
	assert.Empty(t, codexHint("rate limit exceeded: try again", "some stderr"))
}

func TestHarnessCodexEnvironment(t *testing.T) {
	f := useFakeCodex(t)
	t.Setenv("FAKE_MARKER", "from-the-provider")
	t.Setenv("CODEX_THREAD_ID", "the-parent-thread")
	t.Setenv("CODEX_SANDBOX", "seatbelt")
	t.Setenv("RUST_LOG", "trace")
	t.Setenv("HTTPS_PROXY", "http://proxy.example.com:3128")
	t.Setenv("OPENAI_API_KEY", "sk-inherited-openai-key-0123456789")
	t.Setenv(codexAPIKeyVar, fakeCodexAPIKey)
	last := func() invocation {
		inv := f.execs(t)
		return inv[len(inv)-1]
	}
	t.Run("inherited", func(t *testing.T) {
		var warned []string
		req := f.request("hi")
		req.Warn = func(m string) { warned = append(warned, m) }
		res, err := Run(t.Context(), req)
		require.NoError(t, err)
		inv := last()
		assert.Equal(t, "from-the-provider", inv.get("marker"))
		assert.Equal(t, "unset", inv.get("codex_thread_id"), "the CLI starts a thread of its own")
		assert.Equal(t, "unset", inv.get("codex_sandbox"))
		assert.Empty(t, inv.get("rust_log"), "an inherited RUST_LOG would log every command's environment")
		assert.Equal(t, "set", inv.get("codex_api_key"))
		assert.Equal(t, "set", inv.get("openai_api_key"), "Codex's default provider ignores it")
		assert.Contains(t, res.Warnings, "codex authenticated with CODEX_API_KEY, which bills API usage, not a ChatGPT plan")
	})
	t.Run("not inherited", func(t *testing.T) {
		req := f.request("hi")
		req.InheritEnvironment = false
		req.Env = map[string]string{"FAKE_CODEX_LOG": f.log}
		res, err := Run(t.Context(), req)
		require.NoError(t, err)
		inv := last()
		assert.Empty(t, inv.get("marker"), "the provider's environment does not reach the CLI")
		assert.Equal(t, "unset", inv.get("codex_api_key"))
		assert.Equal(t, "unset", inv.get("openai_api_key"))
		assert.Equal(t, f.home, inv.get("codex_home"), "the Codex configuration location is kept")
		assert.Equal(t, "http://proxy.example.com:3128", inv.get("https_proxy"), "the proxy is kept")
		assert.Equal(t, os.Getenv("HOME"), inv.get("home"))
		assert.Equal(t, "set", inv.get("cookie"))
		assert.Empty(t, res.Warnings, "the ChatGPT login")
	})
	t.Run("an explicit access token drops the inherited API key", func(t *testing.T) {
		req := f.request("hi")
		req.Env = map[string]string{codexAccessTokenVar: "at-explicit-access-token-0123456789"}
		res, err := Run(t.Context(), req)
		require.NoError(t, err)
		inv := last()
		assert.Equal(t, "set", inv.get("codex_access_token"))
		assert.Equal(t, "unset", inv.get("codex_api_key"))
		assert.Empty(t, res.Warnings)
	})
	t.Run("env sets RUST_LOG", func(t *testing.T) {
		req := f.request("hi")
		req.Env = map[string]string{"RUST_LOG": "warn"}
		_, err := Run(t.Context(), req)
		require.NoError(t, err)
		assert.Equal(t, "warn", last().get("rust_log"))
	})
}

func TestCodexEnvironment(t *testing.T) {
	environ := []string{"PATH=/bin", "HOME=/home/u", "SECRET_THING=x", "CODEX_THREAD_ID=t", "CODEX_HOME=/c",
		"CODEX_MANAGED_BY_NPM=1", "CODEX_ESCALATE_SOCKET=3", "EXEC_WRAPPER=/w", "CODEX_API_KEY=inherited", "RUST_LOG=trace",
		"HTTPS_PROXY=http://p", "CODEX_CA_CERTIFICATE=/ca.pem", "CLAUDECODE=1", "CLAUDE_CODE_SESSION_ID=s",
		"TRACEPARENT=00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01", "TRACESTATE=k=v", "PWD=/elsewhere", "=C:=C:\\",
		childCookieVar + "=c1", runCookieVar + "=c2"}

	e := newCodexEnvironment(environ, map[string]string{"A": "1"}, true, "/work")
	assert.Equal(t, "x", e.vars["SECRET_THING"])
	// A parent Codex or Claude Code session's markers and its trace context are dropped (codex
	// review #7): Codex continues a TRACEPARENT and forwards it to OpenAI.
	for _, k := range []string{"CODEX_THREAD_ID", "CODEX_MANAGED_BY_NPM", "CODEX_ESCALATE_SOCKET", "EXEC_WRAPPER", "RUST_LOG",
		"CLAUDECODE", "CLAUDE_CODE_SESSION_ID", "TRACEPARENT", "TRACESTATE"} {
		assert.NotContains(t, e.vars, k)
	}
	assert.Equal(t, "/work", e.vars["PWD"])
	assert.Equal(t, "inherited", e.vars[codexAPIKeyVar])
	assert.Equal(t, "inherited from the environment of the Pulumi CLI", e.origin(codexAPIKeyVar))
	assert.Equal(t, "from env", e.origin("A"))

	e = newCodexEnvironment(environ, nil, false, "/work")
	assert.Equal(t, map[string]string{"PATH": "/bin", "HOME": "/home/u", "CODEX_HOME": "/c", "HTTPS_PROXY": "http://p",
		"CODEX_CA_CERTIFICATE": "/ca.pem", childCookieVar: "c1", runCookieVar: "c2", "PWD": "/work"}, e.vars)

	e = newCodexEnvironment(environ, map[string]string{codexAccessTokenVar: "tok"}, true, "")
	assert.NotContains(t, e.vars, codexAPIKeyVar)
	assert.Equal(t, []string{codexAPIKeyVar}, e.dropped)
	assert.Equal(t, "/elsewhere", e.vars["PWD"], "no dir, no PWD override")

	e = newCodexEnvironment(environ, map[string]string{codexAccessTokenVar: "tok", codexAPIKeyVar: ""}, true, "")
	assert.Equal(t, "", e.vars[codexAPIKeyVar], "an explicit value wins, also a blank one")
	assert.Empty(t, e.dropped)
	assert.False(t, e.set(codexAPIKeyVar))

	e = newCodexEnvironment(environ, map[string]string{codexAccessTokenVar: "  "}, true, "")
	assert.Equal(t, "inherited", e.vars[codexAPIKeyVar], "a blank token drops nothing")

	// CODEX_HOME: empty counts as unset, relative resolves against the working directory.
	h := newCodexEnvironment([]string{"HOME=/home/u", "CODEX_HOME="}, nil, true, "/work").home()
	assert.Equal(t, codexHome{dir: "/home/u/.codex"}, h)
	h = newCodexEnvironment([]string{"CODEX_HOME=rel"}, nil, true, "/work").home()
	assert.Equal(t, codexHome{dir: "/work/rel", set: true, value: "rel"}, h)
	h = newCodexEnvironment([]string{"CODEX_HOME=rel"}, nil, true, "").home()
	assert.Equal(t, codexHome{set: true, value: "rel"}, h, "unknown in a preview")
}

func TestCodexAuth(t *testing.T) {
	prev := codexSystemConfigDir
	codexSystemConfigDir = t.TempDir()
	t.Cleanup(func() { codexSystemConfigDir = prev })
	home := func(t *testing.T, files map[string]string) string {
		dir := t.TempDir()
		for name, content := range files {
			require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
		}
		return dir
	}
	auth := func(dir string, env map[string]string, environ ...string) codexAuth {
		return codexAuthOf(newCodexEnvironment(append(environ, "CODEX_HOME="+dir), env, true, "/work"), true)
	}
	apiKeyLogin := `{"auth_mode":"apikey","OPENAI_API_KEY":"` + fakeCodexAPIKey + `"}`

	t.Run("a ChatGPT login", func(t *testing.T) {
		dir := home(t, map[string]string{"auth.json": chatgptAuthJSON("plus")})
		a := auth(dir, nil)
		assert.Equal(t, "the ChatGPT login of this machine ("+filepath.Join(dir, "auth.json")+", plan plus)", a.source)
		assert.Equal(t, "plus", a.plan)
		assert.Empty(t, a.billing+a.billed+a.conflict)
		assert.False(t, a.missing)
		assert.ElementsMatch(t, []string{fakeCodexAccessToken, fakeCodexRefreshToken, fakeIDToken("plus")}, a.secrets,
			"the tokens are secrets, the account ID is not needed")
		assert.Empty(t, codexAuthOf(newCodexEnvironment([]string{"CODEX_HOME=" + dir}, nil, true, ""), false).secrets,
			"a preview keeps no token")
	})
	t.Run("no login", func(t *testing.T) {
		a := auth(home(t, nil), nil)
		assert.True(t, a.missing)
		assert.Contains(t, a.source, "no Codex login (")
	})
	t.Run("the keyring", func(t *testing.T) {
		for _, store := range []string{"keyring", "auto"} {
			a := auth(home(t, map[string]string{"config.toml": "cli_auth_credentials_store = \"" + store + "\"\n"}), nil)
			assert.False(t, a.missing)
			assert.Equal(t, fmt.Sprintf("the Codex login in the OS keyring (cli_auth_credentials_store = %q)", store), a.source)
		}
		a := auth(home(t, map[string]string{"config.toml": "cli_auth_credentials_store = \"ephemeral\"\n",
			"auth.json": chatgptAuthJSON("plus")}), nil)
		assert.True(t, a.missing, "an ephemeral store keeps no login")
	})
	t.Run("an API-key login", func(t *testing.T) {
		for _, file := range []string{apiKeyLogin, `{"OPENAI_API_KEY":"` + fakeCodexAPIKey + `"}`} {
			dir := home(t, map[string]string{"auth.json": file})
			a := auth(dir, nil)
			assert.Equal(t, "an API key stored by codex login ("+filepath.Join(dir, "auth.json")+")", a.source)
			assert.Contains(t, a.billing, "codex is logged in with an API key")
			assert.Contains(t, a.billed, "which bills API usage")
			assert.Contains(t, a.secrets, fakeCodexAPIKey)
		}
	})
	t.Run("Amazon Bedrock", func(t *testing.T) {
		a := auth(home(t, map[string]string{"auth.json": `{"bedrock_api_key":{"api_key":"bedrock-key-0123456789"}}`}), nil)
		assert.Contains(t, a.source, "Amazon Bedrock credentials")
		assert.Contains(t, a.billing, "that AWS account")
		assert.Contains(t, a.secrets, "bedrock-key-0123456789")
	})
	t.Run("an unreadable file", func(t *testing.T) {
		prevRetry := authReadRetry
		authReadRetry = time.Millisecond
		t.Cleanup(func() { authReadRetry = prevRetry })
		a := auth(home(t, map[string]string{"auth.json": `{"auth_mode":"chat`}), nil)
		assert.Contains(t, a.source, "could not be read")
		assert.False(t, a.missing, "only a missing file is a missing login")
	})
	t.Run("credentials in the environment", func(t *testing.T) {
		dir := home(t, map[string]string{"auth.json": chatgptAuthJSON("plus")})
		a := auth(dir, map[string]string{codexAPIKeyVar: fakeCodexAPIKey})
		assert.Equal(t, "CODEX_API_KEY (from env)", a.source)
		assert.Equal(t, "codex would use CODEX_API_KEY (from env) instead of a ChatGPT login and bill API usage to it "+
			"(OPENAI_ORGANIZATION and OPENAI_PROJECT, when set, select the billed organization and project); remove it from "+
			"env to use the Codex login of the machine", a.billing)
		assert.Contains(t, a.secrets, fakeCodexAccessToken, "the file's tokens are secrets anyway")
		a = auth(dir, nil, codexAPIKeyVar+"="+fakeCodexAPIKey)
		assert.Contains(t, a.billing, "CODEX_API_KEY (inherited from the environment of the Pulumi CLI)")
		assert.Contains(t, a.billing, "project); set inheritEnvironment: false, or blank it with env: {CODEX_API_KEY: \"\"}, "+
			"to use the Codex login of the machine")
		a = auth(dir, map[string]string{codexAccessTokenVar: "at-0123456789"})
		assert.Equal(t, "CODEX_ACCESS_TOKEN (from env, a ChatGPT workspace)", a.source)
		assert.Empty(t, a.billing)
		a = auth(dir, map[string]string{"OPENAI_IDENTITY_TOKEN_FILE": "/var/run/token"})
		assert.Equal(t, "workload identity (OPENAI_IDENTITY_TOKEN_FILE, from env, a ChatGPT workspace)", a.source)
		assert.Contains(t, a.billing, "selects Codex's workload identity login")
		a = auth(dir, nil, "OPENAI_API_KEY="+fakeCodexAPIKey)
		assert.Contains(t, a.source, "the ChatGPT login", "OPENAI_API_KEY does not authenticate Codex's default provider")
	})
	t.Run("forced_login_method", func(t *testing.T) {
		forced := func(method string) string { return "forced_login_method = \"" + method + "\"\n" }
		dir := home(t, map[string]string{"config.toml": forced("chatgpt"), "auth.json": chatgptAuthJSON("plus")})
		a := auth(dir, map[string]string{codexAPIKeyVar: fakeCodexAPIKey})
		assert.Equal(t, "codex would delete the stored login of its CODEX_HOME ("+dir+"): "+filepath.Join(dir, "config.toml")+
			" sets forced_login_method = \"chatgpt\", but codex would use CODEX_API_KEY (from env), so it logs out "+
			"(removing auth.json and the keyring entry) and exits; remove it from env to use the Codex login of the "+
			"machine, or give AgentRun a CODEX_HOME of its own (env: {CODEX_HOME: ...}) with its own codex login", a.conflict)
		assert.Empty(t, auth(dir, nil).conflict, "the ChatGPT login is allowed")
		dir = home(t, map[string]string{"config.toml": forced("api"), "auth.json": chatgptAuthJSON("plus")})
		assert.Contains(t, auth(dir, nil).conflict, "but codex would use the ChatGPT login of")
		assert.Contains(t, auth(dir, map[string]string{codexAccessTokenVar: "at-0123456789"}).conflict,
			"but codex would use CODEX_ACCESS_TOKEN")
		assert.Empty(t, auth(dir, map[string]string{codexAPIKeyVar: fakeCodexAPIKey}).conflict)
		// The managed file wins over the user's.
		require.NoError(t, os.WriteFile(filepath.Join(codexSystemConfigDir, "managed_config.toml"), []byte(forced("chatgpt")), 0o600))
		defer func() { _ = os.Remove(filepath.Join(codexSystemConfigDir, "managed_config.toml")) }()
		a = auth(dir, map[string]string{codexAPIKeyVar: fakeCodexAPIKey})
		assert.Contains(t, a.conflict, filepath.Join(codexSystemConfigDir, "managed_config.toml")+" sets forced_login_method = \"chatgpt\"")
	})
	t.Run("a custom model provider", func(t *testing.T) {
		dir := home(t, map[string]string{"auth.json": chatgptAuthJSON("plus"), "config.toml": `profile = "local"
model = "gpt-6.1-sol"
service_tier = "fast"

[profiles.local]
model_provider = "ollama"
model = "qwen3-coder"

[model_providers.ollama]
name = "Ollama"
env_key = "OLLAMA_KEY"
`})
		a := auth(dir, nil)
		assert.Equal(t, "model_provider ollama of config.toml, with the key in OLLAMA_KEY", a.source)
		assert.Empty(t, a.plan)
		assert.Equal(t, "qwen3-coder", a.cfg.model, "the profile's model")
		assert.Equal(t, "fast", a.cfg.serviceTier)
		assert.Contains(t, a.billing, `codex uses model_provider "ollama" of its config.toml`)
		assert.Contains(t, a.billed, "which bills that provider, not a ChatGPT plan")
	})
	t.Run("forced_chatgpt_workspace_id", func(t *testing.T) {
		for _, value := range []string{`"ws-A"`, `["ws-A", "ws-B"]`} {
			dir := home(t, map[string]string{"config.toml": "forced_chatgpt_workspace_id = " + value + "\n",
				"auth.json": chatgptAuthJSON("plus")})
			a := auth(dir, map[string]string{codexAccessTokenVar: "at-0123456789"})
			assert.Equal(t, "codex would delete the stored login of its CODEX_HOME ("+dir+"): "+
				filepath.Join(dir, "config.toml")+" sets forced_chatgpt_workspace_id, and codex would use CODEX_ACCESS_TOKEN "+
				"(from env), whose workspace AgentRun cannot check: when the token belongs to another workspace, or names "+
				"none, codex logs out (removing auth.json and the keyring entry) and exits; remove it from env to use the "+
				"Codex login of the machine, or give AgentRun a CODEX_HOME of its own (env: {CODEX_HOME: ...}) with its own "+
				"codex login", a.conflict, value)
			assert.Empty(t, auth(dir, map[string]string{codexAPIKeyVar: fakeCodexAPIKey}).conflict, "an API key has no workspace")
			assert.Empty(t, auth(dir, nil).conflict, "the stored login is the user's own")
			assert.Empty(t, auth(dir, map[string]string{"OPENAI_IDENTITY_TOKEN_FILE": "/t"}).conflict,
				"codex exec does not check workload identity")
		}
	})
	t.Run("workload identity under forced_login_method api", func(t *testing.T) {
		dir := home(t, map[string]string{"config.toml": "forced_login_method = \"api\"\n",
			"auth.json": chatgptAuthJSON("plus")})
		a := auth(dir, map[string]string{"OPENAI_IDENTITY_TOKEN_FILE": "/var/run/token"})
		assert.Equal(t, "codex would refuse to start: "+filepath.Join(dir, "config.toml")+" sets forced_login_method = "+
			"\"api\", but workload identity (OPENAI_IDENTITY_TOKEN_FILE) needs a login policy that permits ChatGPT "+
			"authentication; remove it from env to use the Codex login of the machine, or give AgentRun a CODEX_HOME of its "+
			"own (env: {CODEX_HOME: ...}) with its own codex login", a.conflict)
		assert.NotContains(t, a.conflict, "delete", "codex deletes nothing then")
	})
	t.Run("gateways that keep Codex's login", func(t *testing.T) {
		dir := home(t, map[string]string{"auth.json": chatgptAuthJSON("plus"), "config.toml": `model_provider = "proxy"

[model_providers.proxy]
name = "Proxy"
base_url = "https://proxy.example/v1"
requires_openai_auth = true
`})
		a := auth(dir, nil)
		assert.Equal(t, "the ChatGPT login of this machine ("+filepath.Join(dir, "auth.json")+", plan plus), through "+
			"model_provider proxy of config.toml", a.source)
		assert.Empty(t, a.billing+a.billed, "the login decides the billing")
		assert.Equal(t, "plus", a.plan)

		dir = home(t, map[string]string{"auth.json": chatgptAuthJSON("plus"),
			"config.toml": "openai_base_url = \"https://gw.example/v1/openai\"\n"})
		a = auth(dir, nil)
		assert.Equal(t, "the ChatGPT login of this machine ("+filepath.Join(dir, "auth.json")+", plan plus), through "+
			"openai_base_url https://gw.example of config.toml", a.source)
		assert.Empty(t, a.billing+a.billed)
	})
	t.Run("a trusted project's .codex/config.toml", func(t *testing.T) {
		project := t.TempDir()
		work := filepath.Join(project, "sub")
		require.NoError(t, os.MkdirAll(filepath.Join(work, ".codex"), 0o700))
		require.NoError(t, os.Mkdir(filepath.Join(project, ".git"), 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(work, ".codex", "config.toml"), []byte("model = \"gpt-6.2-sol\"\n"+
			"service_tier = \"flex\"\nmodel_provider = \"evil\"\nopenai_base_url = \"https://evil.example\"\n"), 0o600))
		of := func(cfg string) codexConfig {
			dir := home(t, map[string]string{"auth.json": chatgptAuthJSON("plus"), "config.toml": cfg})
			return codexAuthOf(newCodexEnvironment([]string{"CODEX_HOME=" + dir}, nil, true, work), false).cfg
		}
		c := of("model = \"gpt-6.1-sol\"\n")
		assert.Equal(t, "gpt-6.1-sol", c.model, "an untrusted project is not read")
		c = of("model = \"gpt-6.1-sol\"\n\n[projects.\"" + project + "\"]\ntrust_level = \"trusted\"\n")
		assert.Equal(t, "gpt-6.2-sol", c.model, "the project layer is above the user's")
		assert.Equal(t, "flex", c.serviceTier)
		assert.Empty(t, c.modelProvider, "a project cannot choose where the login goes")
		assert.Empty(t, c.openAIBaseURL)
	})
	t.Run("an invalid config.toml", func(t *testing.T) {
		a := auth(home(t, map[string]string{"config.toml": "model = [", "auth.json": chatgptAuthJSON("plus")}), nil)
		assert.Len(t, a.cfg.problems, 1)
		assert.Contains(t, a.source, "the ChatGPT login")
	})
	t.Run("the plan claim", func(t *testing.T) {
		for _, c := range []struct{ token, plan string }{
			{fakeIDToken("pro"), "pro"}, {"not-a-jwt", ""}, {"a.!!!.c", ""}, {fakeIDToken("plus; rm -rf /"), ""},
		} {
			doc := map[string]json.RawMessage{"tokens": json.RawMessage(`{"id_token":"` + c.token + `"}`)}
			assert.Equal(t, c.plan, codexPlan(doc), c.token)
		}
	})
	t.Run("never the e-mail address", func(t *testing.T) {
		dir := home(t, map[string]string{"auth.json": chatgptAuthJSON("plus")})
		for _, a := range []codexAuth{auth(dir, nil), auth(dir, map[string]string{codexAPIKeyVar: fakeCodexAPIKey})} {
			all := a.source + a.billing + a.billed + a.conflict + a.plan
			assert.NotContains(t, all, fakeCodexEmail)
			assert.NotContains(t, all, fakeCodexAccount)
			assert.NotContains(t, a.secrets, fakeCodexAccount)
		}
	})
	t.Run("rotated tokens", func(t *testing.T) {
		dir := home(t, map[string]string{"auth.json": chatgptAuthJSON("plus")})
		a := auth(dir, nil)
		assert.Empty(t, a.refreshSecrets(), "unchanged")
		rotated := strings.Replace(chatgptAuthJSON("plus"), fakeCodexAccessToken, "eyJrotated-access-token-0123456789abcd", 1)
		require.NoError(t, os.WriteFile(filepath.Join(dir, "auth.json"), []byte(rotated), 0o600))
		assert.Equal(t, []string{"eyJrotated-access-token-0123456789abcd"}, a.refreshSecrets())
		assert.Empty(t, a.refreshSecrets())
	})
}

func TestHarnessCodexForcedLogin(t *testing.T) {
	f := useFakeCodex(t)
	f.writeHome(t, "config.toml", "forced_login_method = \"chatgpt\"\n")
	req := f.request("Write NOTES.md")
	req.Env = map[string]string{codexAPIKeyVar: fakeCodexAPIKey}
	_, err := Run(t.Context(), req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "codex would delete the stored login of its CODEX_HOME")
	assert.Nil(t, f.entries(), "codex never started")
	assert.FileExists(t, filepath.Join(f.home, "auth.json"), "the login is still there")

	problems, _ := CheckConfig(t.Context(), "test.yaml", []byte(codexAgent), CheckOptions{AllowHarnessAgents: true,
		InheritEnvironment: true, EnvKeys: map[string]bool{codexAPIKeyVar: true}})
	require.Len(t, problems, 1)
	assert.Contains(t, problems[0], "sets forced_login_method = \"chatgpt\", but codex would use CODEX_API_KEY (from env)")

	// What AgentRun prevents: the fake deletes the login as Codex would.
	cmd := exec.Command(filepath.Join(f.dir, "codex"), "exec", "--json", "--color=never", "--skip-git-repo-check",
		"--dangerously-bypass-approvals-and-sandbox", "--output-last-message="+filepath.Join(t.TempDir(), "last"), "-")
	cmd.Env = append(os.Environ(), codexAPIKeyVar+"="+fakeCodexAPIKey)
	cmd.Dir = t.TempDir()
	cmd.Stdin = strings.NewReader("hi")
	out, err := cmd.CombinedOutput()
	require.Error(t, err)
	assert.Contains(t, string(out), "ChatGPT login is required, but an API key is currently being used. Logging out.")
	assert.NoFileExists(t, filepath.Join(f.home, "auth.json"))
}

func TestHarnessCodexHome(t *testing.T) {
	f := useFakeCodex(t)
	missing := filepath.Join(t.TempDir(), "nope")
	t.Setenv("CODEX_HOME", missing)
	_, err := Run(t.Context(), f.request("Write NOTES.md"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "CODEX_HOME points to "+missing+", which does not exist, so codex would not start: "+
		"create it and log in with CODEX_HOME="+missing+" codex login, or remove CODEX_HOME")
	assert.Nil(t, f.entries())
}

func TestHarnessCodexSandboxCheck(t *testing.T) {
	f := useFakeCodex(t)
	t.Run("unavailable", func(t *testing.T) {
		t.Setenv("FAKE_CODEX_SANDBOX", "unavailable")
		_, err := Run(t.Context(), f.request("Write NOTES.md"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "harness codex failed: codex's sandbox cannot start on this machine (bwrap: No "+
			"permissions to create a new namespace, likely because the kernel does not allow non-privileged user "+
			"namespaces.")
		assert.Contains(t, err.Error(), "): with toolApproval deny codex runs the model's commands in its workspace-write "+
			"sandbox, so none of them could run; "+codexSandboxAdvice)
		assert.NotContains(t, f.kinds(), "exec", "no turn, so no model call")
	})
	t.Run("checked once per run", func(t *testing.T) {
		h := harnessRun{dir: t.TempDir(), info: &harnessInfo{}}
		h.codex = newCodexRun(Request{InheritEnvironment: true}, h.dir, ToolApprovalDeny, h.info, nil)
		p, err := newHarnessFactory(h)(&latest.HarnessConfig{Type: "codex"})
		require.NoError(t, err)
		before := len(f.entries())
		require.NoError(t, p.Run(t.Context(), "Write NOTES.md", func(harness.Event) {}))
		require.NoError(t, p.Resume(t.Context(), fakeCodexThread, "Continue", func(harness.Event) {}))
		var kinds []string
		for _, e := range f.entries()[before:] {
			kinds = append(kinds, e.get("kind"))
		}
		assert.Equal(t, []string{"version", "sandbox", "exec", "exec"}, kinds)
	})
	t.Run("a codex without the check falls back to bubblewrap", func(t *testing.T) {
		if harnessOS != "linux" {
			t.Skip("bubblewrap is Linux's")
		}
		t.Setenv("FAKE_CODEX_SANDBOX", "missing")
		_, err := Run(t.Context(), f.request("Write NOTES.md"))
		require.NoError(t, err)
		entries := f.entries()
		var bwrap invocation
		for _, e := range entries {
			if e.get("kind") == "bwrap" {
				bwrap = e
			}
		}
		require.NotNil(t, bwrap, "the fake bwrap ran")
		assert.Equal(t, []string{"--unshare-user", "--unshare-net", "--ro-bind", "/", "/", "true"}, bwrap["arg"])
		t.Setenv("FAKE_BWRAP", "unavailable")
		_, err = Run(t.Context(), f.request("Write NOTES.md"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "codex's sandbox cannot start on this machine (bwrap: setting up uid map: "+
			"Permission denied)")
	})
	t.Run("a check that hangs", func(t *testing.T) {
		prev := codexProbeTimeout
		codexProbeTimeout = time.Second
		t.Cleanup(func() { codexProbeTimeout = prev })
		t.Setenv("FAKE_CODEX_SANDBOX", "hang")
		start := time.Now()
		_, err := Run(t.Context(), f.request("Write NOTES.md"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), `the check of codex's sandbox (codex --config=sandbox_mode="workspace-write" `+
			`sandbox -- true) failed (codex --config=sandbox_mode="workspace-write" sandbox -- true did not finish within `+
			`1s), which does not say that the sandbox cannot start`)
		assert.NotContains(t, err.Error(), codexSandboxAdvice)
		assert.Less(t, time.Since(start), 10*time.Second)
		sleeper := waitPID(t, f.pidfile)
		killAtCleanup(t, sleeper)
		assert.Eventually(t, func() bool { return gone(sleeper) }, 3*time.Second, 20*time.Millisecond,
			"the check's process is stopped")
	})
}

// TestHarnessCodexBrokenCheck: a codex that fails its sandbox check for another reason (a
// config.toml it rejects) gets no sandbox advice (codex review #3).
func TestHarnessCodexBrokenCheck(t *testing.T) {
	f := useFakeCodex(t)
	t.Setenv("FAKE_CODEX_SANDBOX", "broken")
	_, err := Run(t.Context(), f.request("Write NOTES.md"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "harness codex failed: the check of codex's sandbox (codex "+
		`--config=sandbox_mode="workspace-write" sandbox -- true) failed (Error loading config.toml: unknown variant `+
		"`full-auto`, expected one of `read-only`, `workspace-write`), which does not say that the sandbox cannot start: "+
		"under toolApproval deny AgentRun runs it before the first codex turn; run it yourself as the user that runs "+
		"pulumi up (it needs no login and calls no model) and fix what it reports")
	assert.NotContains(t, err.Error(), codexSandboxAdvice)
	assert.NotContains(t, f.kinds(), "exec")
}

func TestHarnessCodexVersion(t *testing.T) {
	f := useFakeCodex(t)
	t.Setenv("FAKE_CODEX_VERSION", "0.163.0-alpha.2")
	res, err := Run(t.Context(), f.request("Write NOTES.md"))
	require.NoError(t, err)
	assert.Equal(t, []string{"codex 0.163.0-alpha.2 is installed, but AgentRun was tested with Codex 0.162, and Codex " +
		"changes the arguments and events of codex exec often: if codex agents misbehave, install Codex 0.162 or a " +
		"pulumi-docker-agent release tested with yours"}, res.Warnings)

	// A codex that cannot print its version does not run (a broken install): no sandbox check,
	// no sandbox advice, no turn (codex review #3).
	t.Setenv("FAKE_CODEX_VERSION", "")
	t.Setenv("FAKE_CODEX_VERSION_FAIL", "1")
	before := len(f.entries())
	_, err = Run(t.Context(), f.request("Write NOTES.md"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "harness codex failed: the codex CLI does not run (error: the version is not known): "+
		"reinstall Codex (npm i -g @openai/codex, or brew install codex) on the machine that runs pulumi up, or set PATH "+
		"in env to a codex that runs; codex --version must print its version")
	assert.NotContains(t, err.Error(), codexSandboxAdvice)
	var kinds []string
	for _, e := range f.entries()[before:] {
		kinds = append(kinds, e.get("kind"))
	}
	assert.Equal(t, []string{"version"}, kinds)

	assert.Equal(t, "0.162.1", parseCodexVersion("codex-cli 0.162.1\n"))
	assert.Equal(t, "", parseCodexVersion("codex-cli\n"))
	assert.Empty(t, codexVersionWarning("0.162.7"))
	assert.Empty(t, codexVersionWarning(""))
}

func TestHarnessCodexInfoLine(t *testing.T) {
	f := useFakeCodex(t)
	run := func(req Request) []string {
		var rec records
		req.Progress = rec.progress
		_, err := Run(t.Context(), req)
		require.NoError(t, err)
		return rec.persistent
	}
	req := f.request("Write NOTES.md")
	req.HideNames = true
	persistent := run(req)
	assert.True(t, slices.ContainsFunc(persistent, func(s string) bool {
		return strings.HasPrefix(s, "codex 0.162.1 (a model), sandbox workspace-write")
	}), "%v", persistent)

	req = f.request("Write NOTES.md")
	req.YAML = []byte("agents:\n  root:\n    harness:\n      type: codex\n")
	persistent = run(req)
	assert.True(t, slices.ContainsFunc(persistent, func(s string) bool {
		return strings.HasPrefix(s, "codex 0.162.1 (its default model), ") &&
			strings.Contains(s, ", cost unknown (Codex's default model is not known)")
	}), "%v", persistent)
	inv := f.execs(t)
	assert.NotContains(t, strings.Join(inv[len(inv)-1]["arg"], " "), "--model")

	// config.toml's model prices the turn when harness.model is empty.
	f.writeHome(t, "config.toml", "model = \"gpt-6.1-sol\"\n")
	persistent = run(req)
	assert.True(t, slices.ContainsFunc(persistent, func(s string) bool {
		return strings.HasPrefix(s, "codex 0.162.1 (gpt-6.1-sol), ")
	}), "%v", persistent)

	// A model the catalog does not know.
	req.YAML = []byte("agents:\n  root:\n    harness:\n      type: codex\n      model: gpt-0-nope\n")
	persistent = run(req)
	assert.True(t, slices.ContainsFunc(persistent, func(s string) bool {
		return strings.Contains(s, ", cost unknown (Codex reports none, and the model is not in the catalog)")
	}), "%v", persistent)

	req = f.request("Write NOTES.md")
	req.Logging = LoggingNone
	assert.Empty(t, run(req))
}

// TestHarnessCodexPerTokenCost: a login billed per token (CODEX_API_KEY) counts Codex's turns at
// API prices in usage.cost (codex review #1).
func TestHarnessCodexPerTokenCost(t *testing.T) {
	f := useFakeCodex(t)
	var rec records
	req := f.request("Write NOTES.md")
	req.Env = map[string]string{codexAPIKeyVar: fakeCodexAPIKey}
	req.Progress = rec.progress
	res, err := Run(t.Context(), req)
	require.NoError(t, err)
	cost := (315*2 + 24448*0.1 + 122*10) / 1e6
	assert.InDelta(t, cost, res.Usage.Cost, 1e-9)
	assert.True(t, slices.ContainsFunc(rec.persistent, func(s string) bool {
		return strings.HasPrefix(s, "codex 0.162.1 (gpt-6.1-sol), sandbox workspace-write, auth CODEX_API_KEY (from env)") &&
			strings.HasSuffix(s, ", cost an API-price estimate")
	}), "%v", rec.persistent)
	summary := rec.persistent[len(rec.persistent)-1]
	assert.Contains(t, summary, fmt.Sprintf("; estimated cost $%.4f", cost))
	assert.NotContains(t, summary, "billed to")
}

func TestCodexUsageAndCost(t *testing.T) {
	r := &codexRun{totals: map[string]codexUsage{}}
	first := &codexUsage{InputTokens: 24763, CachedInputTokens: 24448, OutputTokens: 122, ReasoningOutputTokens: 64}
	assert.Equal(t, &harness.Usage{InputTokens: 315, CacheReadInputTokens: 24448, OutputTokens: 122}, r.turnUsage("t", first))
	// Totals are per thread and cumulative.
	second := &codexUsage{InputTokens: 50000, CachedInputTokens: 48000, CacheWriteInputTokens: 100, OutputTokens: 300}
	assert.Equal(t, &harness.Usage{InputTokens: 1585, CacheReadInputTokens: 23552, CacheCreationInputTokens: 100,
		OutputTokens: 178}, r.turnUsage("t", second))
	assert.Equal(t, &harness.Usage{}, r.turnUsage("t", &codexUsage{}), "a turn without usage updates")
	assert.Equal(t, &harness.Usage{InputTokens: 2, OutputTokens: 1}, r.turnUsage("t", &codexUsage{InputTokens: 50002,
		CachedInputTokens: 48000, CacheWriteInputTokens: 100, OutputTokens: 301}), "the zeros did not lower the total")
	assert.Equal(t, &harness.Usage{InputTokens: 10}, r.turnUsage("other", &codexUsage{InputTokens: 10}))
	assert.Equal(t, &harness.Usage{}, r.turnUsage("t", &codexUsage{InputTokens: 1, OutputTokens: 1}), "never negative")
	assert.Nil(t, r.turnUsage("t", nil))

	u := &harness.Usage{InputTokens: 1_000_000, CacheReadInputTokens: 1_000_000, CacheCreationInputTokens: 1_000_000,
		OutputTokens: 1_000_000}
	cost, known := codexCost("gpt-6.1-sol", "", u)
	assert.True(t, known)
	assert.InDelta(t, 2+0.1+2.5+10, cost, 1e-9)
	cost, _ = codexCost("openai/gpt-6.1-sol", "priority", u)
	assert.InDelta(t, 2*(2+0.1+2.5+10), cost, 1e-9, "the priority tier costs twice")
	for _, model := range []string{"", "gpt-0-nope"} {
		cost, known = codexCost(model, "", u)
		assert.False(t, known)
		assert.Zero(t, cost)
	}
	cost, known = codexCost("gpt-6.1-sol", "", nil)
	assert.True(t, known)
	assert.Zero(t, cost)
	assert.False(t, math.IsNaN(cost))
}

func TestHarnessCodexResume(t *testing.T) {
	f := useFakeCodex(t)
	h := harnessRun{dir: t.TempDir(), info: &harnessInfo{}}
	h.codex = newCodexRun(Request{InheritEnvironment: true}, h.dir, ToolApprovalDeny, h.info, nil)
	p, err := newHarnessFactory(h)(&latest.HarnessConfig{Type: "codex", Model: "gpt-6.1-sol"})
	require.NoError(t, err)
	assert.Equal(t, "codex", p.Name())
	var results []harness.Event
	collect := func(e harness.Event) {
		if e.Type == harness.EventResult {
			results = append(results, e)
		}
		assert.NotEqual(t, harness.EventText, e.Type, "text is not forwarded")
	}
	require.NoError(t, p.Run(t.Context(), "Write NOTES.md", collect))
	t.Setenv("FAKE_CODEX_MODE", "resume")
	require.NoError(t, p.Resume(t.Context(), fakeCodexThread, "Continue", collect))
	inv := f.execs(t)
	require.Len(t, inv, 2)
	args := inv[1]["arg"]
	assert.Equal(t, []string{"resume", fakeCodexThread, "-"}, args[len(args)-3:])
	assert.Equal(t, fakeCodexThread, inv[1].get("resume"))
	assert.Equal(t, "Continue", inv[1].get("stdin"))
	require.Len(t, results, 2)
	assert.Equal(t, "Continued: NOTES.md is up to date.", results[1].Result)
	assert.Equal(t, &harness.Usage{InputTokens: 1585, CacheReadInputTokens: 23552, CacheCreationInputTokens: 100,
		OutputTokens: 178}, results[1].Usage, "the turn's share of the thread's total")
	assert.Error(t, p.Resume(t.Context(), "", "Continue", func(harness.Event) {}))

	_, err = newHarnessFactory(harnessRun{})(&latest.HarnessConfig{Type: "codex"})
	assert.ErrorContains(t, err, "the codex agents of this run were not prepared")
}

func TestHarnessCodexNotFound(t *testing.T) {
	useFakeCodex(t)
	t.Setenv("PATH", t.TempDir())
	_, err := Run(t.Context(), Request{YAML: []byte(codexAgent), Prompt: "hi", AllowHarnessAgents: true,
		InheritEnvironment: true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "the codex harness needs the codex CLI, which was not found on PATH: install Codex "+
		"(npm i -g @openai/codex, or brew install codex) on the machine that runs pulumi up, or set PATH in env")
	_, err = lookPathIn("codex", "")
	assert.ErrorIs(t, err, errCodexNotFound)
}

func TestHarnessCodexWorkingDir(t *testing.T) {
	f := useFakeCodex(t)
	dir := t.TempDir()
	req := f.request("Write NOTES.md")
	req.WorkingDir = dir
	_, err := Run(t.Context(), req)
	require.NoError(t, err)
	notes, err := os.ReadFile(filepath.Join(dir, "NOTES.md"))
	require.NoError(t, err, "the CLI runs in workingDir")
	assert.Equal(t, "Write NOTES.md\n", string(notes))
	inv := f.execs(t)
	assert.Equal(t, dir, inv[len(inv)-1].get("cwd"))

	req.Prompt = "Delete NOTES.md"
	_, err = Run(t.Context(), req)
	require.NoError(t, err)
	assert.NoFileExists(t, filepath.Join(dir, "NOTES.md"))
	assert.DirExists(t, dir)
}

func TestHarnessCodexTokenIsRedacted(t *testing.T) {
	f := useFakeCodex(t)
	for _, c := range []struct{ mode, token string }{
		// A command prints the login's token (`cat auth.json`).
		{"token", fakeCodexAccessToken},
		// Codex refreshed the login during the turn, and a command prints the new token.
		{"rotate", "eyJrotated-access-0123456789abcdef"},
	} {
		t.Run(c.mode, func(t *testing.T) {
			t.Setenv("FAKE_CODEX_MODE", c.mode)
			t.Setenv("FAKE_CODEX_PRINT", fakeCodexAccessToken)
			t.Setenv("FAKE_CODEX_ROTATED", c.token)
			var rec records
			req := f.request("Show the token")
			req.Logging = LoggingFull
			req.Progress = rec.progress
			res, err := Run(t.Context(), req)
			require.NoError(t, err)
			assert.Equal(t, "The token is "+c.token+".", res.Output, "the answer itself is not altered")
			assert.True(t, res.OutputContainsSecret)
			all := strings.Join(append(rec.persistent, rec.status...), "\n")
			assert.Contains(t, all, `root called Bash {"command":"/bin/bash -lc 'echo [secret]'"}: run by the harness: [secret]`)
			assert.Contains(t, all, "root: The token is [secret].")
			assert.NotContains(t, all, c.token)
			assert.Contains(t, strings.Join(res.Warnings, "\n"), "the answer contains the value of a secret")
		})
	}
}

// cancelWhenStarted runs req and cancels it once the fake wrote its pidfile (with the hanging
// modes, once its traps are set), and returns the run's error, the PID in the pidfile and the
// time from the cancellation to the end of the run.
func cancelWhenStarted(t *testing.T, f fakeCodexCLI, req Request) (int, time.Duration, error) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := Run(ctx, req)
		done <- err
	}()
	child := waitPID(t, f.pidfile)
	killAtCleanup(t, child)
	start := time.Now()
	cancel()
	select {
	case err := <-done:
		return child, time.Since(start), err
	case <-time.After(30 * time.Second):
		t.Fatal("the run did not end after its cancellation")
		return child, 0, nil
	}
}

// TestHarnessCodexTimeout: a stop interrupts Codex first (SIGINT), which ends the commands it
// started; what does not stop gets SIGTERM interruptGrace later and SIGKILL after the grace.
func TestHarnessCodexTimeout(t *testing.T) {
	t.Run("timeoutSeconds", func(t *testing.T) {
		f := useFakeCodex(t)
		shortGrace(t, 5*time.Second)
		t.Setenv("FAKE_CODEX_MODE", "interrupt")
		req := f.request("Write NOTES.md")
		// The checks before the turn and the fake's start can take seconds on a busy machine.
		req.Timeout = 5 * time.Second
		start := time.Now()
		_, err := Run(t.Context(), req)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "the agent did not finish within 5s (timeoutSeconds)")
		assert.Less(t, time.Since(start), 5*time.Second+harnessGrace, "codex stopped on SIGINT, before the SIGKILL")
		child := waitPID(t, f.pidfile)
		killAtCleanup(t, child)
		inv := f.execs(t)[0]
		assert.NotEmpty(t, inv["int"], "codex got SIGINT")
		assert.Empty(t, inv["term"], "and stopped before a SIGTERM")
		assert.Eventually(t, func() bool { return gone(child) }, 3*time.Second, 20*time.Millisecond)
	})
	t.Run("SIGINT", func(t *testing.T) {
		f := useFakeCodex(t)
		shortGrace(t, 5*time.Second)
		t.Setenv("FAKE_CODEX_MODE", "interrupt")
		child, took, err := cancelWhenStarted(t, f, f.request("Write NOTES.md"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "the agent run was canceled")
		assert.Less(t, took, 3*time.Second, "codex stopped on SIGINT")
		inv := f.execs(t)[0]
		assert.NotEmpty(t, inv["int"], "codex got SIGINT")
		assert.Empty(t, inv["term"], "and stopped before a SIGTERM")
		assert.Eventually(t, func() bool { return gone(child) }, 3*time.Second, 20*time.Millisecond,
			"codex stopped its command")
	})
	t.Run("SIGTERM after SIGINT", func(t *testing.T) {
		f := useFakeCodex(t)
		shortGrace(t, 5*time.Second)
		prev := interruptGrace
		interruptGrace = 500 * time.Millisecond
		t.Cleanup(func() { interruptGrace = prev })
		t.Setenv("FAKE_CODEX_MODE", "term-trap")
		child, took, err := cancelWhenStarted(t, f, f.request("Write NOTES.md"))
		require.Error(t, err)
		assert.GreaterOrEqual(t, took, interruptGrace, "SIGTERM came interruptGrace after SIGINT")
		assert.Less(t, took, 4*time.Second, "not the SIGKILL after the grace")
		inv := f.execs(t)[0]
		assert.NotEmpty(t, inv["int"])
		assert.NotEmpty(t, inv["term"], "SIGTERM followed")
		assert.Eventually(t, func() bool { return gone(child) }, 3*time.Second, 20*time.Millisecond)
	})
	t.Run("SIGKILL after the grace", func(t *testing.T) {
		f := useFakeCodex(t)
		shortGrace(t, time.Second)
		t.Setenv("FAKE_CODEX_MODE", "hang")
		child, took, err := cancelWhenStarted(t, f, f.request("Write NOTES.md"))
		require.Error(t, err)
		assert.GreaterOrEqual(t, took, time.Second-100*time.Millisecond, "codex ignores both: killed after the grace")
		assert.Less(t, took, 10*time.Second)
		inv := f.execs(t)[0]
		assert.NotEmpty(t, inv["int"], "SIGINT first")
		leader, err := strconv.Atoi(inv.get("pid"))
		require.NoError(t, err)
		assert.Eventually(t, func() bool { return gone(leader) && gone(child) }, 3*time.Second, 20*time.Millisecond)
	})
	t.Run("what codex left", func(t *testing.T) {
		f := useFakeCodex(t)
		shortGrace(t, time.Second)
		t.Setenv("FAKE_CODEX_MODE", "hang-detached")
		child, _, err := cancelWhenStarted(t, f, f.request("Write NOTES.md"))
		require.Error(t, err)
		leader, err := strconv.Atoi(f.execs(t)[0].get("pid"))
		require.NoError(t, err)
		assert.True(t, gone(leader), "codex is gone when Run returns")
		assert.True(t, gone(child), "the command in a session of its own is gone when Run returns")
	})
}

func TestHarnessCodexLeftRunning(t *testing.T) {
	for _, c := range []struct {
		mode string
		pids int
		warn bool
	}{
		{"background-session", 2, true},
		{"oneshot", 3, true}, // only the cookie finds them
		{"leftover", 1, false},
		{"mcp-group", 1, false}, // an MCP server Codex was still stopping
	} {
		t.Run(c.mode, func(t *testing.T) {
			f := useFakeCodex(t)
			t.Setenv("FAKE_CODEX_MODE", c.mode)
			prev := postExitGrace
			postExitGrace = 8 * time.Second
			t.Cleanup(func() { postExitGrace = prev })
			if c.mode == "oneshot" {
				noPolls(t)
			}
			start := time.Now()
			res, err := Run(t.Context(), f.request("Write NOTES.md"))
			took := time.Since(start)
			pids := waitPIDs(t, f.pidfile, c.pids)
			killAllAtCleanup(t, pids)
			require.NoError(t, err)
			assertGone(t, pids, "what codex left running is gone when Run returns")
			if c.warn {
				assert.Equal(t, []string{leftRunningWarningOf("codex", c.pids)}, res.Warnings)
			} else {
				assert.Empty(t, res.Warnings)
			}
			assert.Less(t, took, postExitGrace, "the processes got SIGTERM")
		})
	}
	assert.Equal(t, "codex left 2 processes running when it exited (for example a command it ran in the background); "+
		"AgentRun stopped them", leftRunningWarningOf("codex", 2))
}

// TestHarnessCodexMixed runs a normal root agent that hands tasks to a codex and a claude-code
// sub-agent.
func TestHarnessCodexMixed(t *testing.T) {
	fc := useFakeClaude(t)
	f := useFakeCodex(t, fc.dir)
	srv := fakeopenai.New(func(r fakeopenai.Request) fakeopenai.Reply {
		turns := 0
		for _, m := range r.Messages {
			if m.Role == "assistant" {
				turns++
			}
		}
		switch turns {
		case 0:
			return fakeopenai.Reply{ToolCalls: []fakeopenai.ToolCall{{ID: "c1", Name: "transfer_task",
				Arguments: `{"agent":"coder","task":"Write NOTES.md","expected_output":"a summary"}`}}}
		case 1:
			return fakeopenai.Reply{ToolCalls: []fakeopenai.ToolCall{{ID: "c2", Name: "transfer_task",
				Arguments: `{"agent":"reviewer","task":"Review NOTES.md","expected_output":"a review"}`}}}
		}
		return fakeopenai.Reply{Content: "Both said: " + toolResult(r)}
	})
	defer srv.Close()
	body := `agents:
  root:
    model: m
    instruction: You coordinate.
    sub_agents: [coder, reviewer]
  coder:
    description: Writes notes with Codex
    harness:
      type: codex
      model: gpt-6.1-sol
  reviewer:
    description: Reviews with Claude Code
    harness:
      type: claude-code
      model: sonnet
`
	var rec records
	req := request(srv, body, "Write and review the notes")
	req.AllowHarnessAgents, req.InheritEnvironment = true, true
	req.Progress = rec.progress
	res, err := Run(t.Context(), req)
	require.NoError(t, err)
	assert.Equal(t, "Both said: Done: NOTES.md lists the changes.", res.Output)
	assert.Equal(t, []string{"transfer_task", "Bash", "mcp__docs__search", "web_search", "apply_patch", "transfer_task",
		"Write"}, res.ToolCalls)
	assert.Empty(t, res.Warnings)
	assert.Contains(t, f.execs(t)[0].get("stdin"), "Write NOTES.md", "the task reaches codex")
	assert.Contains(t, fc.invocations(t)[0].get("stdin"), "Review NOTES.md", "and claude")
	assert.True(t, slices.ContainsFunc(rec.persistent, func(s string) bool { return strings.HasPrefix(s, "codex 0.162.1 ") }))
	assert.True(t, slices.ContainsFunc(rec.persistent, func(s string) bool { return strings.HasPrefix(s, "claude-code 2.1.283 ") }))
	// The usage of all three: the root's, Codex's and Claude Code's. Each part is counted by its
	// own billing (codex review #1): the root's model has no price, a ChatGPT plan pays for
	// Codex, the Claude subscription for Claude Code, and the summary names both plans.
	assert.GreaterOrEqual(t, res.Usage.InputTokens, int64(24763+15040))
	assert.Zero(t, res.Usage.Cost)
	assert.Contains(t, rec.persistent[len(rec.persistent)-1], fmt.Sprintf("; no per-token cost: billed to the ChatGPT "+
		"plan (API price about $%.4f) and the Claude subscription (API price about $0.0123)", (315*2+24448*0.1+122*10)/1e6))

	// The preview names each harness in its own terms.
	doc := []byte(agentYAML(srv, body))
	_, warnings := CheckConfig(t.Context(), "test.yaml", doc, CheckOptions{AllowHarnessAgents: true,
		InheritEnvironment: true, ToolApprovalDeny: true})
	assert.True(t, slices.ContainsFunc(warnings, func(w string) bool {
		return strings.HasPrefix(w, "coding-harness agents (agents.reviewer.harness: claude-code) run without tool approval")
	}), "%v", warnings)
	assert.True(t, slices.ContainsFunc(warnings, func(w string) bool {
		return strings.HasPrefix(w, "coding-harness agents (agents.coder.harness: codex) run their own tools")
	}), "%v", warnings)
	problems, _ := CheckConfig(t.Context(), "test.yaml", doc, CheckOptions{})
	assert.Equal(t, []string{"coding-harness agents (agents.coder.harness: codex, agents.reviewer.harness: claude-code) " +
		"run only with allowHarnessAgents: true: they start an external coding agent CLI that runs its own tools without " +
		"docker-agent's tool approval"}, problems)
}

// TestHarnessCodexFailureHints: the hints about the models' credentials and endpoints concern
// the agents' own models, not a coding-harness CLI that failed (codex review #5).
func TestHarnessCodexFailureHints(t *testing.T) {
	f := useFakeCodex(t)
	srv := fakeopenai.New(func(fakeopenai.Request) fakeopenai.Reply { return fakeopenai.Reply{Content: "unused"} })
	defer srv.Close()
	// The root is Codex; another agent has an in-process model, which the run never calls.
	body := `agents:
  root:
    description: Writes notes with Codex
    harness:
      type: codex
      model: gpt-6.1-sol
  helper:
    model: m
    instruction: You help.
`
	t.Setenv("FAKE_CODEX_MODE", "incomplete")
	t.Setenv("FAKE_CODEX_STDERR", "unexpected status 401 Unauthorized")
	req := request(srv, body, "Write the notes")
	req.AllowHarnessAgents, req.InheritEnvironment = true, true
	_, err := Run(t.Context(), req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "harness codex failed: codex ended without completing its turn: unexpected status "+
		"401 Unauthorized; "+codexAuthHint, "Codex's own hint")
	assert.NotContains(t, err.Error(), "the run's models:", "not the credentials of the other agents")
	assert.NotContains(t, err.Error(), chatgptRaceHint)
	assert.NotEmpty(t, f.execs(t))

	// The parent's notes about the models' SDK variables (inheritEnvironment false) are left out
	// when only coding-harness CLIs failed.
	t.Setenv("OPENAI_ORG_ID", "org-of-the-cli")
	req.InheritEnvironment = false
	msg := "the agent run failed: harness codex failed: codex ended without completing its turn: 401 Unauthorized"
	assert.Contains(t, parentErrorHints(t.Context(), req, msg, nil, false), "OPENAI_ORG_ID is set in the environment of "+
		"the Pulumi CLI")
	assert.Equal(t, msg, parentErrorHints(t.Context(), req, msg, nil, true))
	assert.True(t, endMessage(nil, &runFailure{msg: msg}).HarnessOnly)
	assert.False(t, endMessage(nil, &runFailure{msg: msg, inProcess: "model failed: 401"}).HarnessOnly)
}

func TestHarnessCodexAnnounce(t *testing.T) {
	f := useFakeCodex(t)
	t.Setenv(codexAPIKeyVar, fakeCodexAPIKey)
	var warned []string
	req := f.request("Write NOTES.md")
	req.AnnounceHarness = true
	req.Warn = func(m string) { warned = append(warned, m) }
	_, err := Run(t.Context(), req)
	require.NoError(t, err)
	require.Len(t, warned, 2, "%v", warned)
	assert.True(t, strings.HasPrefix(warned[0], "coding-harness agents (agents.root.harness: codex) run their own tools"))
	assert.Contains(t, warned[0], "toolApproval deny runs codex's commands in its workspace-write sandbox")
	assert.Contains(t, warned[1], "codex would use CODEX_API_KEY (inherited from the environment of the Pulumi CLI)")
	assert.NotContains(t, strings.Join(warned, "\n"), fakeCodexAPIKey)

	// A failed run reports the billing through Warn.
	t.Setenv("FAKE_CODEX_MODE", "failed-auth")
	warned = nil
	req.AnnounceHarness = false
	_, err = Run(t.Context(), req)
	require.Error(t, err)
	assert.Equal(t, []string{"codex authenticated with CODEX_API_KEY, which bills API usage, not a ChatGPT plan"}, warned)
}

func TestHarnessCodexUnsupportedOS(t *testing.T) {
	prev := harnessOS
	harnessOS = "windows"
	t.Cleanup(func() { harnessOS = prev })
	const want = "coding-harness agents in AgentRun are supported on Linux and macOS, not on windows"
	problems, _ := CheckConfig(t.Context(), "test.yaml", []byte(codexAgent), CheckOptions{AllowHarnessAgents: true})
	assert.Equal(t, []string{want}, problems)
	_, err := Run(t.Context(), Request{YAML: []byte(codexAgent), Prompt: "hi", AllowHarnessAgents: true})
	assert.ErrorContains(t, err, want)
	_, err = newHarnessFactory(harnessRun{})(&latest.HarnessConfig{Type: "codex"})
	assert.ErrorContains(t, err, want)
}

func TestCheckConfigCodexWarnings(t *testing.T) {
	f := useFakeCodex(t)
	// No sandbox doubts unless a test asks for them.
	prevProc, prevMarkers, prevCgroup := procSysDir, containerMarkers, initCgroupFile
	procSysDir, containerMarkers, initCgroupFile = t.TempDir(), nil, filepath.Join(t.TempDir(), "cgroup")
	t.Cleanup(func() { procSysDir, containerMarkers, initCgroupFile = prevProc, prevMarkers, prevCgroup })
	// The test's CODEX_HOME is in the temporary directory: only the subtest about it says so.
	prevTemp := tempDirOf
	tempDirOf = func() string { return "/nonexistent-temporary-directory" }
	t.Cleanup(func() { tempDirOf = prevTemp })

	deny := CheckOptions{AllowHarnessAgents: true, InheritEnvironment: true, ToolApprovalDeny: true}
	allow := CheckOptions{AllowHarnessAgents: true, InheritEnvironment: true}
	check := func(doc string, opts CheckOptions) []string {
		t.Helper()
		problems, warnings := CheckConfig(t.Context(), "test.yaml", []byte(doc), opts)
		require.Empty(t, problems)
		return warnings
	}
	has := func(warnings []string, sub string) bool {
		return slices.ContainsFunc(warnings, func(w string) bool { return strings.Contains(w, sub) })
	}
	const w1 = "coding-harness agents (agents.root.harness: codex) run their own tools: the codex CLI runs shell " +
		"commands, file edits, MCP tools and web searches on the machine that runs pulumi up, in workingDir or a " +
		"temporary directory, with its user's Codex login, config.toml (MCP servers, profiles, rules, hooks) and " +
		"AGENTS.md files, and without docker-agent's permissions; "

	t.Run("W1", func(t *testing.T) {
		w := check(codexAgent, deny)
		require.Len(t, w, 1, "%v", w)
		assert.Equal(t, w1+"toolApproval deny runs codex's commands in its workspace-write sandbox, which limits writes "+
			"and network but not reads: they can write only in workingDir (not its .git, .codex or .agents), the temporary "+
			"directories and writable_roots of config.toml, and reach the network only when config.toml allows it, but they "+
			"can read every file the user can, and the model sees what they read; MCP servers, hooks and notify programs "+
			"(also those of a trusted project's .codex/) run outside the sandbox, and hosted web search stays on", w[0])
		w = check(codexAgent, allow)
		require.Len(t, w, 1, "%v", w)
		assert.Equal(t, w1+"toolApproval allow (also one not known yet) turns off Codex's sandbox too "+
			"(--dangerously-bypass-approvals-and-sandbox), so they can do everything the user can: use it only on an "+
			"externally isolated machine or in a container", w[0])
	})
	t.Run("W2", func(t *testing.T) {
		t.Setenv(codexAPIKeyVar, fakeCodexAPIKey)
		w := check(codexAgent, deny)
		assert.True(t, has(w, "codex would use CODEX_API_KEY (inherited from the environment of the Pulumi CLI) instead "+
			"of a ChatGPT login and bill API usage to it"))
		assert.False(t, has(w, fakeCodexAPIKey))
		assert.False(t, has(check(codexAgent, CheckOptions{AllowHarnessAgents: true, ToolApprovalDeny: true}), "CODEX_API_KEY"),
			"not inherited")
		blank := deny
		blank.EnvKeys = map[string]bool{codexAPIKeyVar: false}
		assert.False(t, has(check(codexAgent, blank), "CODEX_API_KEY"), "env: {CODEX_API_KEY: \"\"} blanks it")
		token := deny
		token.EnvKeys = map[string]bool{codexAccessTokenVar: true}
		w = check(codexAgent, token)
		assert.Contains(t, w, "inherited CODEX_API_KEY is not passed to codex because env sets CODEX_ACCESS_TOKEN")
		assert.False(t, has(w, "would use CODEX_API_KEY"))
	})
	t.Run("an API-key login", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "auth.json"), []byte(`{"OPENAI_API_KEY":"`+fakeCodexAPIKey+`"}`), 0o600))
		opts := deny
		opts.EnvKeys, opts.EnvValues = map[string]bool{"CODEX_HOME": true}, map[string]string{"CODEX_HOME": dir}
		w := check(codexAgent, opts)
		assert.True(t, has(w, "codex is logged in with an API key ("+filepath.Join(dir, "auth.json")+"), which bills API "+
			"usage, not a ChatGPT plan; run codex login to use your ChatGPT plan"))
		assert.False(t, has(w, "temporary directory, where"))
		tempDirOf = os.TempDir
		defer func() { tempDirOf = func() string { return "/nonexistent-temporary-directory" } }()
		assert.True(t, has(check(codexAgent, opts), "CODEX_HOME ("+dir+") is in the temporary directory, where codex "+
			"refuses to create its helper commands (such as its sandbox launcher on Linux) and runs without them; use a "+
			"directory outside "+os.TempDir()))
	})
	t.Run("no login", func(t *testing.T) {
		dir := t.TempDir()
		opts := deny
		opts.EnvKeys, opts.EnvValues = map[string]bool{"CODEX_HOME": true}, map[string]string{"CODEX_HOME": dir}
		assert.True(t, has(check(codexAgent, opts), "found no Codex login for the user that runs pulumi up ("+
			filepath.Join(dir, "auth.json")+" does not exist): run codex login as that user (codex login --device-auth on a "+
			"machine without a browser), preferably into a CODEX_HOME of AgentRun's own (env: {CODEX_HOME: ...}), or pass "+
			"CODEX_API_KEY in env (bills API usage); a login in the OS keyring counts only when config.toml sets "+
			"cli_auth_credentials_store"))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "config.toml"), []byte(`cli_auth_credentials_store = "keyring"`), 0o600))
		assert.False(t, has(check(codexAgent, opts), "found no Codex login"), "the keyring")
		unknown := deny
		unknown.EnvUnknown = true
		assert.Len(t, check(codexAgent, unknown), 1, "an unknown env: W1 only")
		computed := deny
		computed.EnvKeys = map[string]bool{"CODEX_HOME": true}
		assert.Len(t, check(codexAgent, computed), 1, "an unknown CODEX_HOME: W1 only")
	})
	t.Run("CODEX_HOME", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "nope")
		opts := deny
		opts.EnvKeys, opts.EnvValues = map[string]bool{"CODEX_HOME": true}, map[string]string{"CODEX_HOME": missing}
		problems, _ := CheckConfig(t.Context(), "test.yaml", []byte(codexAgent), opts)
		assert.Equal(t, []string{"CODEX_HOME points to " + missing + ", which does not exist, so codex would not start: " +
			"create it and log in with CODEX_HOME=" + missing + " codex login, or remove CODEX_HOME"}, problems)
		opts.EnvValues["CODEX_HOME"] = "relative/home"
		assert.True(t, has(check(codexAgent, opts), "CODEX_HOME (relative/home) is a relative path, which codex resolves "+
			"against workingDir; use an absolute path"))
	})
	t.Run("forced_login_method", func(t *testing.T) {
		f.writeHome(t, "config.toml", "forced_login_method = \"api\"\n")
		defer func() { _ = os.Remove(filepath.Join(f.home, "config.toml")) }()
		problems, _ := CheckConfig(t.Context(), "test.yaml", []byte(codexAgent), deny)
		require.Len(t, problems, 1)
		assert.Contains(t, problems[0], "codex would delete the stored login of its CODEX_HOME ("+f.home+")")
		assert.NotContains(t, problems[0], fakeCodexEmail)
		unknown := deny
		unknown.EnvUnknown = true
		problems, _ = CheckConfig(t.Context(), "test.yaml", []byte(codexAgent), unknown)
		assert.Empty(t, problems, "env may hold other credentials")
	})
	t.Run("the sandbox", func(t *testing.T) {
		prevOS := harnessOS
		harnessOS = "linux"
		t.Cleanup(func() { harnessOS = prevOS })
		require.NoError(t, os.MkdirAll(filepath.Join(procSysDir, "kernel"), 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(procSysDir, "kernel", "apparmor_restrict_unprivileged_userns"),
			[]byte("1\n"), 0o600))
		require.NoError(t, os.WriteFile(initCgroupFile, []byte("0::/docker/0123abcd\n"), 0o600))
		w := check(codexAgent, deny)
		assert.Contains(t, w, "codex's sandbox may not start on this machine (AppArmor restricts unprivileged user "+
			"namespaces; the provider runs in a container): under toolApproval deny AgentRun checks it before the first "+
			"codex turn and fails the run if it cannot start; "+codexSandboxAdvice)
		assert.False(t, has(check(codexAgent, allow), "sandbox may not start"), "allow turns it off")
		harnessOS = "darwin"
		assert.False(t, has(check(codexAgent, deny), "sandbox may not start"))
	})
	t.Run("W4 W5 W6 W7", func(t *testing.T) {
		const structured = "    structured_output:\n      name: review\n      schema:\n        type: object\n"
		w := check(codexAgent+"    instruction: Write good notes.\n", deny)
		assert.True(t, has(w, "agents.root.instruction is not sent to the coding harness: docker-agent sends only the "+
			"prompt (to a sub-agent: its task); put the instructions into the prompt or into an AGENTS.md in workingDir"))
		assert.True(t, has(check(codexAgent+structured, deny), "agents.root.structured_output is not enforced for codex: "+
			"AgentRun decodes Codex's final message as JSON (a surrounding Markdown code fence is removed); set "+
			"structured_output.strict: true to have Codex enforce the schema, or ask for JSON in the prompt"))
		assert.True(t, has(check(codexAgent+structured+"      strict: true\n", deny), "agents.root.structured_output is "+
			"enforced by codex --output-schema in OpenAI's strict mode: every property must be required and "+
			"additionalProperties false, or the turn fails"))
		inherit := func(model string) string {
			return "models:\n  m:\n    provider: openai\n    model: gpt-6.1-sol\nagents:\n  root:\n    model: " + model +
				"\n    harness:\n      type: codex\n"
		}
		assert.True(t, has(check(inherit("openai/gpt-6.1-sol"), deny), `agents.root.harness has no model, so docker-agent `+
			`passes the agent's model "openai/gpt-6.1-sol" to codex --model, which takes Codex model names only; set `+
			`harness.model (for example gpt-6.1-sol), or remove the agent's model to use Codex's default`))
		assert.True(t, has(check(inherit("m"), deny), "has no model"), "a models: key")
		assert.True(t, has(check(inherit("anthropic/claude-sonnet-4-6"), deny), "has no model"), "any provider")
		assert.False(t, has(check(inherit("gpt-6.1-sol"), deny), "has no model"))
		del := deny
		del.DeletePrompt = true
		assert.True(t, has(check(codexAgent, del), "deletePrompt runs codex in its workspace-write sandbox (toolApproval "+
			"deny): its commands can write only in workingDir and the temporary directories, without network, which may "+
			"not be enough to clean up"))
		del.ToolApprovalDeny = false
		assert.False(t, has(check(codexAgent, del), "deletePrompt"))
	})
	t.Run("hidden names", func(t *testing.T) {
		hidden := deny
		hidden.HideNames = true
		w := check(codexAgent+"    instruction: Write good notes.\n", hidden)
		assert.True(t, has(w, "coding-harness agents (an agent's harness: codex) run their own tools"))
		assert.True(t, has(w, "an agent's instruction is not sent"))
		assert.False(t, has(w, "agents.root"))
	})
}

// The provider-process test runs a codex turn in a copy of the test binary (helper mode through
// TestMain) and sends it SIGINT, as Pulumi stops the provider.

const codexHelperEnv = "AGENTRUN_TEST_CODEX_HELPER"

func runCodexHelperMode() {
	if dir := os.Getenv(codexHelperEnv); dir != "" {
		os.Exit(runCodexHelper(dir))
	}
}

// runCodexHelper behaves like the provider binary: it runs a codex turn (with the fake from
// PATH and FAKE_CODEX_MODE) in dir until SIGINT ends it, and reports how the turn ended.
func runCodexHelper(dir string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	WatchShutdownSignals()
	h := harnessRun{dir: dir, info: &harnessInfo{}, debug: func(msg string) { fmt.Fprintln(os.Stderr, "codex helper:", msg) }}
	h.codex = newCodexRun(Request{InheritEnvironment: true}, dir, ToolApprovalAllow, h.info, nil)
	p, err := newHarnessFactory(h)(&latest.HarnessConfig{Type: "codex"})
	if err != nil {
		return 2
	}
	done := make(chan error, 1)
	go func() { done <- p.Run(context.Background(), "hang", func(harness.Event) {}) }()
	<-ctx.Done()
	select {
	case err := <-done:
		fmt.Fprintln(os.Stderr, "codex helper: the CLI ended:", err)
	case <-time.After(time.Minute):
		return 3
	}
	return 0
}

func TestHarnessCodexShutdown(t *testing.T) {
	f := useFakeCodex(t)
	exe, err := os.Executable()
	require.NoError(t, err)
	cmd := exec.Command(exe, "-test.run=^$")
	cmd.Env = append(os.Environ(), codexHelperEnv+"="+t.TempDir(), "FAKE_CODEX_MODE=interrupt")
	stderr := &strings.Builder{}
	cmd.Stderr = stderr
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	child := waitPID(t, f.pidfile)
	killAtCleanup(t, child)
	start := time.Now()
	require.NoError(t, cmd.Process.Signal(syscall.SIGINT))
	require.NoError(t, waitHelper(t, cmd), "the provider shuts down with exit status 0: %s", stderr)
	assert.Less(t, time.Since(start), signalGrace, "codex stopped on SIGINT")
	assert.Contains(t, stderr.String(), "the CLI ended: codex was stopped because Pulumi is stopping the provider (SIGINT)")
	assert.NotEmpty(t, f.execs(t)[0]["int"], "codex got SIGINT")
	assert.Eventually(t, func() bool { return gone(child) }, 3*time.Second, 20*time.Millisecond)
}
