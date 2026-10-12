// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

//go:build linux || darwin

package examples

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/pulumi/providertest/pulumitest"
	"github.com/pulumi/providertest/pulumitest/assertpreview"
	"github.com/pulumi/providertest/pulumitest/assertrefresh"
	"github.com/pulumi/providertest/pulumitest/opttest"
	"github.com/pulumi/pulumi/sdk/v3/go/auto"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/optrefresh"
	"github.com/pulumi/pulumi/sdk/v3/go/common/apitype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The tests in this file run AgentRuns of Claude Code agents (coding-harness agents, which AgentRun
// supports on Linux and macOS) against the fake `claude` of provider/pkg/agentrun/testdata/fake-claude,
// so they need no Claude login. The Pulumi CLI, and with it the provider, gets a PATH with the fake
// and the tools Pulumi needs only, so the real Claude Code CLI never runs.

const (
	// The prompts of examples/agent-run-claude-code-ts.
	notesPrompt       = "Write NOTES.md with three bullet points about this workspace, then summarise the file in one sentence."
	notesUpdatePrompt = "NOTES.md exists. Bring it up to date and keep its format, then summarise the file in one sentence."
	notesDeletePrompt = "Delete NOTES.md if it exists, then answer: done."
	reviewPrompt      = "Review this summary of NOTES.md in two sentences: "

	// The final result of the fake claude's session.jsonl.
	fakeClaudeResult = "Done: NOTES.md lists the changes."
	// The CLAUDE_CODE_OAUTH_TOKEN of the tests. The fake claude repeats it in mode token and writes
	// it to stderr in mode error; it must never show up in the output or unencrypted in the state.
	fakeClaudeToken = "sk-ant-oat01-fake-e2e-token-0123456789abcdef"
)

// TestExampleAgentRunClaudeCode runs examples/agent-run-claude-code-ts with the fake claude: two
// AgentRuns of one AgentConfig with a claude-code harness, the first in workspace/ with update and
// delete prompts, the second chained to it with `triggers`. It checks what the CLI was started with
// (prompt on stdin, working directory, environment), the outputs, that preview and refresh run
// nothing, that `rerun: always` runs both agents on the next `pulumi up` without drift on refresh,
// and that destroy runs the delete prompt.
func TestExampleAgentRunClaudeCode(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	const lang = "agent-run-claude-code-ts"
	node, err := exec.LookPath("node")
	require.NoError(t, err, "node is not on PATH")
	fake := newFakeClaude(t, root, filepath.Dir(node))

	start := time.Now()
	pt := pulumitest.NewPulumiTest(t, lang,
		opttest.LocalProviderPath(providerName, filepath.Join(root, "bin")),
		opttest.SkipInstall(),
		opttest.SkipStackCreate(),
		opttest.RequireYarnLinks(false),
		opttest.Env("PATH", fake.path),
		// Neither may reach the CLI: the program sets inheritEnvironment: false, and AgentRun removes
		// the markers of a parent Claude Code session in any case.
		opttest.Env("FAKE_MARKER", "leak"),
		opttest.Env("CLAUDECODE", "1"),
	)
	useLocalSDK(t, lang, pt.WorkingDir(), root)
	pt.NewStack(t, "")
	installDependencies(t, pt.WorkingDir())
	require.NoError(t, pt.CurrentStack().SetConfig(pt.Context(), "claudeCodeOauthToken",
		auto.ConfigValue{Value: fakeClaudeToken, Secret: true}))
	workspace := evalSymlinks(t, filepath.Join(pt.WorkingDir(), "workspace"))
	t.Logf("[%s] setup (copy, stack, install): %s", lang, time.Since(start).Round(time.Millisecond))

	step := time.Now()
	up := pt.Up(t)
	t.Logf("[%s] up: %s", lang, time.Since(step).Round(time.Millisecond))
	assert.Equal(t, fakeClaudeResult, stringOutput(t, up.Outputs, "summary"), "output is the final result only")
	assert.Equal(t, fakeClaudeResult, stringOutput(t, up.Outputs, "reviewText"))
	assert.Equal(t, []any{"Write"}, outputValue(t, up.Outputs, "toolCalls"))
	usage, ok := outputValue(t, up.Outputs, "usage").(map[string]any)
	require.True(t, ok, "usage is not an object: %#v", up.Outputs["usage"].Value)
	assert.Equal(t, 15040.0, usage["inputTokens"], "input tokens include cached input")
	assert.Equal(t, 37.0, usage["outputTokens"])
	assert.InDelta(t, 0.0123, usage["cost"], 1e-9, "cost is Claude Code's total_cost_usd")
	assert.FileExists(t, filepath.Join(workspace, "NOTES.md"))
	// The preview warning about tools that run without approval, and the run summary.
	assert.Contains(t, up.StdOut, "run without tool approval")
	assert.Contains(t, up.StdOut, "agent run finished")
	assert.NotContains(t, up.StdOut+up.StdErr, fakeClaudeToken)

	deployment := pt.ExportStack(t).Deployment
	checkAgentRunSecrets(t, deployment)
	for name, r := range agentRunsInState(t, deployment) {
		assert.Regexp(t, `^`+regexp.QuoteMeta(name)+`-[0-9a-f]{8}$`, r.ID, "new AgentRuns get unique IDs")
		assert.True(t, isSecretJSON(r.Inputs["env"]), "%s: env is not a secret", name)
		assert.True(t, isSecretJSON(r.Outputs["env"]), "%s: the env output is not a secret", name)
	}
	assert.NotContains(t, stateWithoutSecrets(t, deployment), fakeClaudeToken, "the state has the token in plaintext")

	calls := fake.calls(t)
	require.Len(t, calls, 2, "up runs notes, then review")
	assert.Equal(t, notesPrompt, calls[0].Stdin)
	assert.Equal(t, workspace, calls[0].Fields["realcwd"], "notes runs in its workingDir")
	assert.Equal(t, reviewPrompt+fakeClaudeResult, calls[1].Stdin, "review gets the answer of notes")
	assert.NotEqual(t, workspace, calls[1].Fields["realcwd"], "review runs in a temporary directory")
	assert.NoDirExists(t, calls[1].Fields["realcwd"], "the temporary working directory is deleted after the run")
	for i, c := range calls {
		assertFakeClaudeCall(t, i, c)
		assert.Equal(t, "sonnet", argAfter(c.Args, "--model"), "call %d: harness.model", i)
		assert.Equal(t, "low", argAfter(c.Args, "--effort"), "call %d: harness.effort", i)
	}

	step = time.Now()
	assertpreview.HasNoChanges(t, pt.Preview(t))
	assertrefresh.HasNoChanges(t, pt.Refresh(t))
	assert.Len(t, fake.calls(t), 2, "preview and refresh never run the agent")
	t.Logf("[%s] preview, refresh: %s", lang, time.Since(step).Round(time.Millisecond))

	// rerun: always runs notes again on the next `pulumi up`, with its update prompt, and review
	// follows it through `triggers`. A refresh reports no drift.
	step = time.Now()
	pt.SetConfig(t, "rerun", "always")
	preview := pt.Preview(t)
	assert.Equal(t, 2, preview.ChangeSummary[apitype.OpUpdate],
		"notes reruns and review follows its unknown ranAt:\n%s", preview.StdOut)
	assert.Contains(t, preview.StdOut, "rerun")
	pt.Refresh(t, optrefresh.ExpectNoChanges())
	assert.Len(t, fake.calls(t), 2, "preview and refresh never run the agent")
	up = pt.Up(t)
	assert.Equal(t, 2, (*up.Summary.ResourceChanges)["update"], "both AgentRuns run again:\n%s", up.StdOut)
	calls = fake.calls(t)
	require.Len(t, calls, 4)
	assert.Equal(t, notesUpdatePrompt, calls[2].Stdin, "a rerun with an unchanged prompt sends updatePrompt")
	assert.Equal(t, workspace, calls[2].Fields["realcwd"])
	assert.Equal(t, reviewPrompt+fakeClaudeResult, calls[3].Stdin)
	assertFakeClaudeCall(t, 2, calls[2])
	assertFakeClaudeCall(t, 3, calls[3])
	assert.NotContains(t, up.StdOut+up.StdErr, fakeClaudeToken)
	t.Logf("[%s] rerun always: %s", lang, time.Since(step).Round(time.Millisecond))

	// Destroy deletes review (no delete prompt) and then notes, whose delete prompt removes NOTES.md.
	step = time.Now()
	destroy := pt.Destroy(t)
	t.Logf("[%s] destroy: %s", lang, time.Since(step).Round(time.Millisecond))
	calls = fake.calls(t)
	require.Len(t, calls, 5, "destroy runs the delete prompt of notes only")
	assert.Equal(t, notesDeletePrompt, calls[4].Stdin)
	assert.Equal(t, workspace, calls[4].Fields["realcwd"])
	assertFakeClaudeCall(t, 4, calls[4])
	assert.NoFileExists(t, filepath.Join(workspace, "NOTES.md"), "the delete prompt removes NOTES.md")
	assert.NotContains(t, destroy.StdOut+destroy.StdErr, fakeClaudeToken)
	t.Logf("[%s] total: %s", lang, time.Since(start).Round(time.Millisecond))
}

// assertFakeClaudeCall checks how AgentRun started the CLI of examples/agent-run-claude-code-ts:
// the prompt on stdin and not in the arguments, the token from `env`, and nothing of the
// provider's own environment.
func assertFakeClaudeCall(t *testing.T, i int, c fakeClaudeCall) {
	t.Helper()
	require.NotEmpty(t, c.Args, "call %d has no arguments", i)
	assert.Equal(t, "-p", c.Args[len(c.Args)-1], "call %d: the prompt goes to stdin, after -p", i)
	assert.Contains(t, c.Args, "--dangerously-skip-permissions", "call %d", i)
	assert.NotContains(t, c.Args, c.Stdin, "call %d: the prompt is in the arguments", i)
	assert.Equal(t, strconv.Itoa(len(c.Args)), c.Fields["argc"], "call %d", i)
	assert.Equal(t, "set", c.Fields["oauth_token"], "call %d: CLAUDE_CODE_OAUTH_TOKEN from env", i)
	assert.Equal(t, "", c.Fields["marker"], "call %d: inheritEnvironment: false passes the provider's environment", i)
	assert.Equal(t, "unset", c.Fields["claudecode"], "call %d: CLAUDECODE reached the CLI", i)
	// A temporary working directory is gone after the run, so resolve the symlinks of its parent
	// (/tmp is /private/tmp on macOS).
	pwd := c.Fields["pwd_env"]
	require.NotEmpty(t, pwd, "call %d: no PWD", i)
	assert.Equal(t, c.Fields["realcwd"], filepath.Join(evalSymlinks(t, filepath.Dir(pwd)), filepath.Base(pwd)),
		"call %d: PWD is the working directory", i)
}

// TestAgentRunClaudeCodeRedaction runs testdata/agent-run-claude-code with the fake claude in mode
// token, which repeats CLAUDE_CODE_OAUTH_TOKEN in a tool call, its result and the final result:
// the token never shows up in the output, the progress shows [secret] instead, and the answer is
// stored as a secret.
func TestAgentRunClaudeCodeRedaction(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	fake := newFakeClaude(t, root)
	pt := newClaudeCodeFixture(t, root, fake, map[string]string{"mode": "token", "logging": "full"})

	up := pt.Up(t)
	require.Len(t, fake.calls(t), 1)
	assert.NotContains(t, up.StdOut+up.StdErr, fakeClaudeToken)
	assert.Contains(t, up.StdOut, "[secret]", "logging: full shows the redacted tool call")
	assert.True(t, up.Outputs["output"].Secret, "an answer that contains the token is a secret")

	deployment := pt.ExportStack(t).Deployment
	assert.NotContains(t, stateWithoutSecrets(t, deployment), fakeClaudeToken, "the state has the token in plaintext")
	r, ok := agentRunsInState(t, deployment)["notes"]
	require.True(t, ok, "no AgentRun in the state")
	assert.True(t, isSecretJSON(r.Outputs["output"]), "output is not a secret in the state")
	assert.True(t, isSecretJSON(r.Inputs["env"]), "env is not a secret in the state")
}

// TestAgentRunClaudeCodeFailure runs testdata/agent-run-claude-code with the fake claude in mode
// error, an expired login: `pulumi up` fails with Claude Code's message and the hint to use
// `claude setup-token`, without the token it wrote to stderr, and no AgentRun is stored.
func TestAgentRunClaudeCodeFailure(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	fake := newFakeClaude(t, root)
	pt := newClaudeCodeFixture(t, root, fake, map[string]string{"mode": "error"})

	_, err := pt.UpErr(t)
	require.Error(t, err, "up must fail")
	require.Len(t, fake.calls(t), 1)
	msg := err.Error() // the automation API includes stdout and stderr
	assert.Contains(t, msg, "Failed to authenticate")
	assert.Contains(t, msg, "claude setup-token")
	assert.NotContains(t, msg, fakeClaudeToken)
	assert.Empty(t, agentRunsInState(t, pt.ExportStack(t).Deployment), "a failed create stores no AgentRun")
}

// TestAgentRunClaudeCodeTimeout runs testdata/agent-run-claude-code with the fake claude in mode
// term-trap and timeoutSeconds 3: the CLI gets SIGTERM before SIGKILL, kills its detached child
// like Claude Code stops its shell commands, and `pulumi up` fails with the timeout.
func TestAgentRunClaudeCodeTimeout(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	fake := newFakeClaude(t, root)
	pt := newClaudeCodeFixture(t, root, fake, map[string]string{"mode": "term-trap", "timeout": "3"})

	_, err := pt.UpErr(t)
	require.Error(t, err, "up must fail")
	assert.Contains(t, err.Error(), "did not finish within")
	calls := fake.calls(t)
	require.Len(t, calls, 1)
	assert.True(t, calls[0].Term, "the fake did not receive SIGTERM before SIGKILL")
	pids := fake.pids(t)
	require.NotEmpty(t, pids, "the fake wrote no pidfile")
	for _, pid := range pids {
		assert.Eventually(t, func() bool { return processGone(pid) }, 5*time.Second, 100*time.Millisecond,
			"the detached child %d is still running", pid)
	}
}

// TestAgentRunClaudeCodeDeleteEscapeHatch runs testdata/agent-run-claude-code and makes its delete
// prompt fail: `pulumi destroy` fails and keeps the AgentRun in the state, and with
// DOCKER_AGENT_SKIP_DELETE_PROMPTS=true it succeeds without running the prompt.
func TestAgentRunClaudeCodeDeleteEscapeHatch(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	fake := newFakeClaude(t, root)
	pt := newClaudeCodeFixture(t, root, fake, nil)

	up := pt.Up(t)
	assert.Equal(t, fakeClaudeResult, stringOutput(t, up.Outputs, "output"))
	work := filepath.Join(pt.WorkingDir(), "work")
	require.FileExists(t, filepath.Join(work, "NOTES.md"))

	// The fake fails delete prompts while .fail-delete exists. This cleanup is registered after
	// the stack's, so it runs first and lets the stack's cleanup destroy succeed.
	failDelete := filepath.Join(work, ".fail-delete")
	require.NoError(t, os.WriteFile(failDelete, nil, 0o600))
	t.Cleanup(func() { _ = os.Remove(failDelete) })

	_, err := pt.DestroyErr(t)
	require.Error(t, err, "destroy must fail when the delete prompt fails")
	assert.Contains(t, err.Error(), "deletePrompt failed")
	assert.Contains(t, err.Error(), "DOCKER_AGENT_SKIP_DELETE_PROMPTS")
	assert.NotContains(t, err.Error(), fakeClaudeToken)
	assert.Contains(t, agentRunsInState(t, pt.ExportStack(t).Deployment), "notes", "the AgentRun must stay in the state")
	calls := fake.calls(t)
	require.Len(t, calls, 2)
	assert.Equal(t, "Delete NOTES.md", calls[1].Stdin)
	assert.Equal(t, "error", calls[1].Fields["mode"])

	pt.CurrentStack().Workspace().SetEnvVar("DOCKER_AGENT_SKIP_DELETE_PROMPTS", "true")
	destroy := pt.Destroy(t)
	assert.Contains(t, destroy.StdOut, "deletePrompt not run")
	assert.Len(t, fake.calls(t), 2, "a skipped delete prompt runs nothing")
	assert.Empty(t, agentRunsInState(t, pt.ExportStack(t).Deployment))
	assert.FileExists(t, filepath.Join(work, "NOTES.md"), "what the skipped prompt would have cleaned up stays")
}

// newClaudeCodeFixture prepares testdata/agent-run-claude-code with the fake claude on PATH, the
// test token and the given stack settings (mode, logging, timeout).
func newClaudeCodeFixture(t *testing.T, root string, fake *fakeClaude, config map[string]string) *pulumitest.PulumiTest {
	t.Helper()
	pt := pulumitest.NewPulumiTest(t, filepath.Join("testdata", "agent-run-claude-code"),
		opttest.LocalProviderPath(providerName, filepath.Join(root, "bin")),
		opttest.SkipInstall(),
		opttest.SkipStackCreate(),
		opttest.Env("PATH", fake.path),
	)
	pt.NewStack(t, "")
	require.NoError(t, pt.CurrentStack().SetConfig(pt.Context(), "token",
		auto.ConfigValue{Value: fakeClaudeToken, Secret: true}))
	for key, value := range config {
		pt.SetConfig(t, key, value)
	}
	return pt
}

// fakeClaude is the fake Claude Code CLI of provider/pkg/agentrun/testdata/fake-claude, set up for
// one test: a copy of the script and its fixtures, and a `claude` wrapper that runs it with the
// test's log and pid files. The provider controls the environment of the CLI (inheritEnvironment
// false passes neither FAKE_CLAUDE_LOG nor FAKE_CLAUDE_PIDFILE), so the wrapper sets them.
type fakeClaude struct {
	path    string // the PATH for the Pulumi CLI and the provider
	log     string // FAKE_CLAUDE_LOG
	pidFile string // FAKE_CLAUDE_PIDFILE
}

func newFakeClaude(t *testing.T, root string, extraDirs ...string) *fakeClaude {
	t.Helper()
	src := filepath.Join(root, "provider", "pkg", "agentrun", "testdata", "fake-claude")
	require.FileExists(t, filepath.Join(src, "claude"), "the fake claude of provider/pkg/agentrun is missing")
	dir := t.TempDir()

	// The script finds the fixtures next to itself. The copy gets its own mode bits, whatever git
	// checked out.
	fakeDir := filepath.Join(dir, "fake-claude")
	require.NoError(t, os.Mkdir(fakeDir, 0o750))
	fixtures, err := filepath.Glob(filepath.Join(src, "*.jsonl"))
	require.NoError(t, err)
	require.NotEmpty(t, fixtures)
	for _, f := range append(fixtures, filepath.Join(src, "claude")) {
		data, err := os.ReadFile(f) //nolint:gosec // the fake's files in this repository
		require.NoError(t, err)
		mode := os.FileMode(0o644)
		if filepath.Base(f) == "claude" {
			mode = 0o755
		}
		require.NoError(t, os.WriteFile(filepath.Join(fakeDir, filepath.Base(f)), data, mode))
	}

	f := &fakeClaude{log: filepath.Join(dir, "fake-claude.log"), pidFile: filepath.Join(dir, "fake-claude.pid")}
	for _, p := range []string{dir, f.log} {
		require.NotContains(t, p, "'", "the wrapper quotes paths with single quotes")
	}
	binDir := filepath.Join(dir, "bin")
	require.NoError(t, os.Mkdir(binDir, 0o750))
	wrapper := filepath.Join(binDir, "claude")
	script := fmt.Sprintf("#!/bin/sh\n# The fake claude with the log and pid files of %s.\n"+
		"FAKE_CLAUDE_LOG='%s'\nFAKE_CLAUDE_PIDFILE='%s'\nexport FAKE_CLAUDE_LOG FAKE_CLAUDE_PIDFILE\nexec '%s' \"$@\"\n",
		t.Name(), f.log, f.pidFile, filepath.Join(fakeDir, "claude"))
	require.NoError(t, os.WriteFile(wrapper, []byte(script), 0o755)) //nolint:gosec // an executable script

	// The wrapper comes first; then what Pulumi needs (its CLI and language hosts, and node for
	// TypeScript), and the system tools the fake uses.
	pulumi, err := exec.LookPath("pulumi")
	require.NoError(t, err, "pulumi is not on PATH")
	dirs := []string{binDir, filepath.Dir(pulumi)}
	dirs = append(dirs, extraDirs...)
	dirs = append(dirs, "/usr/bin", "/bin")
	f.path = strings.Join(slices.Compact(dirs), string(os.PathListSeparator))
	got, ok := lookPathIn(f.path, "claude")
	require.True(t, ok, "claude is not on the test PATH %s", f.path)
	require.Equal(t, wrapper, got, "claude on the test PATH is not the fake")

	// Kill what a failed test may have left running: the pidfile names `sleep 300` children.
	t.Cleanup(func() {
		for _, pid := range f.pidsIfAny() {
			if !processGone(pid) && isSleep(pid) {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	})
	return f
}

// fakeClaudeCall is one invocation of the fake claude, read from its log.
type fakeClaudeCall struct {
	Args   []string
	Fields map[string]string // argc, cwd, realcwd, pwd_env, home, pid, mode, oauth_token, ...
	Stdin  string            // the prompt
	Term   bool              // mode term-trap received SIGTERM
}

var fakeClaudeFieldRE = regexp.MustCompile(`^([a-z_]+)=(.*)$`)

// calls reads the log of the fake claude. It fails the test when the log is missing: then the
// fake never ran.
func (f *fakeClaude) calls(t *testing.T) []fakeClaudeCall {
	t.Helper()
	file, err := os.Open(f.log)
	require.NoError(t, err, "the fake claude never ran (no log)")
	defer file.Close() //nolint:errcheck
	var calls []fakeClaudeCall
	inStdin := false
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "--- invocation" {
			calls = append(calls, fakeClaudeCall{Fields: map[string]string{}})
			inStdin = false
			continue
		}
		require.NotEmpty(t, calls, "the fake claude's log does not start with an invocation: %q", line)
		c := &calls[len(calls)-1]
		switch {
		case line == "term":
			c.Term = true
		case inStdin:
			// stdin is the last field, and a prompt can span lines.
			c.Stdin += "\n" + line
		default:
			m := fakeClaudeFieldRE.FindStringSubmatch(line)
			require.NotNil(t, m, "unexpected line in the fake claude's log: %q", line)
			switch m[1] {
			case "arg":
				c.Args = append(c.Args, m[2])
			case "stdin":
				c.Stdin, inStdin = m[2], true
			default:
				c.Fields[m[1]] = m[2]
			}
		}
	}
	require.NoError(t, scanner.Err())
	return calls
}

// pids reads the PIDs that the fake claude wrote to its pidfile.
func (f *fakeClaude) pids(t *testing.T) []int {
	t.Helper()
	_, err := os.Stat(f.pidFile)
	require.NoError(t, err, "the fake claude wrote no pidfile")
	return f.pidsIfAny()
}

func (f *fakeClaude) pidsIfAny() []int {
	data, err := os.ReadFile(f.pidFile)
	if err != nil {
		return nil
	}
	var pids []int
	for _, field := range strings.Fields(string(data)) {
		if pid, err := strconv.Atoi(field); err == nil && pid > 0 {
			pids = append(pids, pid)
		}
	}
	return pids
}

// processGone reports whether a process has exited: it no longer exists, or it is a zombie that
// waits for its parent. `kill -0` cannot tell, because it succeeds on zombies.
func processGone(pid int) bool {
	if runtime.GOOS == "linux" {
		data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if err != nil {
			return true
		}
		// The state follows the command name in parentheses, which may contain spaces.
		s := string(data)
		i := strings.LastIndexByte(s, ')')
		return i < 0 || i+2 >= len(s) || s[i+2] == 'Z'
	}
	out, _ := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output() //nolint:gosec // a PID
	state := strings.TrimSpace(string(out))
	return state == "" || strings.HasPrefix(state, "Z")
}

// isSleep reports whether a process runs `sleep`, so that a cleanup never kills a reused PID.
func isSleep(pid int) bool {
	if runtime.GOOS == "linux" {
		data, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
		return err == nil && strings.TrimSpace(string(data)) == "sleep"
	}
	out, err := exec.Command("ps", "-o", "comm=", "-p", strconv.Itoa(pid)).Output() //nolint:gosec // a PID
	return err == nil && filepath.Base(strings.TrimSpace(string(out))) == "sleep"
}

// lookPathIn finds an executable like exec.LookPath, but in the given PATH.
func lookPathIn(path, name string) (string, bool) {
	for _, dir := range filepath.SplitList(path) {
		if !filepath.IsAbs(dir) {
			continue
		}
		p := filepath.Join(dir, name)
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() && fi.Mode().Perm()&0o111 != 0 {
			return p, true
		}
	}
	return "", false
}

// argAfter returns the argument after flag, or "" when flag is not there.
func argAfter(args []string, flag string) string {
	if i := slices.Index(args, flag); i >= 0 && i+1 < len(args) {
		return args[i+1]
	}
	return ""
}

func evalSymlinks(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	require.NoError(t, err)
	return resolved
}
