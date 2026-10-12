// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package agentrun

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/docker/docker-agent/pkg/paths"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dirien/pulumi-docker-agent/provider/pkg/agentrun/fakeopenai"
)

// Runs are serialized by the package (process-global docker-agent paths), so these tests
// do not use t.Parallel.

const fakeKey = "sk-fake-test-key-0123456789"

// agentYAML renders a configuration whose models use the fake server. body holds the
// `agents:` section (and anything else).
func agentYAML(srv *fakeopenai.Server, body string) string {
	return fmt.Sprintf(`providers:
  fake:
    api_type: openai_chatcompletions
    base_url: %s
    token_key: FAKE_API_KEY
models:
  m:
    provider: fake
    model: fake-model
%s`, srv.URL, body)
}

const rootAgent = `agents:
  root:
    model: m
    description: Test agent
    instruction: You are a test agent.
`

func request(srv *fakeopenai.Server, body, prompt string) Request {
	return Request{
		Name:   "test",
		YAML:   []byte(agentYAML(srv, body)),
		Prompt: prompt,
		Env:    map[string]string{"FAKE_API_KEY": fakeKey},
	}
}

// toolResult returns the content of the tool results answering the previous turn.
func toolResult(r fakeopenai.Request) string {
	var parts []string
	for _, m := range r.ToolResults() {
		parts = append(parts, m.Content)
	}
	return strings.Join(parts, "\n")
}

func TestPlainAnswer(t *testing.T) {
	srv := fakeopenai.New(func(r fakeopenai.Request) fakeopenai.Reply {
		return fakeopenai.Reply{Content: "Hello, " + r.LastUser(), PromptTokens: 12, CompletionTokens: 3}
	})
	defer srv.Close()

	res, err := Run(t.Context(), request(srv, rootAgent, "Pulumi"))
	require.NoError(t, err)
	assert.Equal(t, "Hello, Pulumi", res.Output)
	assert.Equal(t, "root", res.AgentName)
	assert.Nil(t, res.StructuredOutput)
	assert.False(t, res.HasStructuredOutput)
	assert.Equal(t, int64(12), res.Usage.InputTokens)
	assert.Equal(t, int64(3), res.Usage.OutputTokens)
	assert.Zero(t, res.Usage.Cost, "custom providers have no pricing")
	assert.Equal(t, []string{}, res.ToolCalls)

	reqs := srv.Requests()
	require.Len(t, reqs, 1)
	assert.Equal(t, "Bearer "+fakeKey, reqs[0].Authorization)
	assert.True(t, reqs[0].Stream, "docker-agent streams")
	assert.Contains(t, reqs[0].System(), "You are a test agent.")
}

func TestSelectedAgent(t *testing.T) {
	srv := fakeopenai.New(func(r fakeopenai.Request) fakeopenai.Reply {
		if strings.Contains(r.System(), "You are the second agent.") {
			return fakeopenai.Reply{Content: "second"}
		}
		return fakeopenai.Reply{Content: "root"}
	})
	defer srv.Close()

	body := rootAgent + `  second:
    model: m
    instruction: You are the second agent.
`
	req := request(srv, body, "hi")
	req.Agent = "second"
	res, err := Run(t.Context(), req)
	require.NoError(t, err)
	assert.Equal(t, "second", res.Output)
	assert.Equal(t, "second", res.AgentName)

	req.Agent = "missing"
	_, err = Run(t.Context(), req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "agent not found: missing")
}

const reviewSchema = `    structured_output:
      name: review
      schema:
        type: object
        properties:
          risk:
            type: string
            enum: [low, medium, high]
          summary:
            type: string
        required: [risk, summary]
        additionalProperties: false
`

func TestStructuredOutputNative(t *testing.T) {
	srv := fakeopenai.New(nil) // Default answers with a JSON document matching the schema.
	defer srv.Close()

	res, err := Run(t.Context(), request(srv, rootAgent+reviewSchema, "Review this change."))
	require.NoError(t, err)
	assert.True(t, res.HasStructuredOutput)
	assert.Equal(t, map[string]any{"risk": "low", "summary": "sample"}, res.StructuredOutput)
	assert.JSONEq(t, `{"risk":"low","summary":"sample"}`, res.Output)

	reqs := srv.Requests()
	require.Len(t, reqs, 1)
	require.NotNil(t, reqs[0].Schema, "the schema is sent as response_format")
	assert.Contains(t, reqs[0].Schema["properties"], "risk")
}

func TestStructuredOutputToolMode(t *testing.T) {
	const outputTool = "__structured_output__"
	srv := fakeopenai.New(func(r fakeopenai.Request) fakeopenai.Reply {
		if r.HasTool(outputTool) && len(r.ToolResults()) == 0 {
			return fakeopenai.Reply{ToolCalls: []fakeopenai.ToolCall{{Name: outputTool, Arguments: `{"risk":"high","summary":"tool mode"}`}}}
		}
		return fakeopenai.Reply{Content: "unexpected"}
	})
	defer srv.Close()

	body := rootAgent + strings.Replace(reviewSchema, "      name: review\n", "      name: review\n      mode: tool\n", 1)
	res, err := Run(t.Context(), request(srv, body, "Review this change."))
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"risk": "high", "summary": "tool mode"}, res.StructuredOutput)
	assert.Equal(t, []string{}, res.ToolCalls, "the internal output tool is not reported")
}

func TestStructuredOutputInvalidJSON(t *testing.T) {
	srv := fakeopenai.New(func(fakeopenai.Request) fakeopenai.Reply { return fakeopenai.Reply{Content: "not json"} })
	defer srv.Close()

	_, err := Run(t.Context(), request(srv, rootAgent+reviewSchema, "Review this change."))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "declares structured_output but its answer is not valid JSON")
}

// scriptAgent has a script tool that writes a marker file into dir.
func scriptAgent(dir string) string {
	return fmt.Sprintf(`agents:
  root:
    model: m
    instruction: Use your tools.
    toolsets:
      - type: think
      - type: script
        shell:
          write_marker:
            cmd: "echo ran > marker.txt"
            description: Writes a marker file
            working_dir: %s
`, dir)
}

// callOnce calls tool once, then answers with the tool result.
func callOnce(tool, args string) fakeopenai.Handler {
	return func(r fakeopenai.Request) fakeopenai.Reply {
		if len(r.ToolResults()) == 0 {
			return fakeopenai.Reply{ToolCalls: []fakeopenai.ToolCall{{Name: tool, Arguments: args}}}
		}
		return fakeopenai.Reply{Content: "done: " + toolResult(r)}
	}
}

func TestToolApprovalDeny(t *testing.T) {
	dir := t.TempDir()
	srv := fakeopenai.New(callOnce("write_marker", "{}"))
	defer srv.Close()

	res, err := Run(t.Context(), request(srv, scriptAgent(dir), "write the marker"))
	require.NoError(t, err, "a rejected tool call does not fail the run")
	assert.NoFileExists(t, filepath.Join(dir, "marker.txt"))
	assert.Contains(t, res.Output, "done: ")
	assert.Contains(t, res.Output, "requires user confirmation", "the model is told why")
	assert.Equal(t, []string{}, res.ToolCalls)
	assert.Equal(t, []string{"write_marker"}, res.RejectedToolCalls)
	assert.Contains(t, strings.Join(res.Warnings, "\n"),
		`agent "root": tool call "write_marker" was rejected because it needs approval and toolApproval is "deny"`)
}

func TestToolApprovalAllow(t *testing.T) {
	dir := t.TempDir()
	srv := fakeopenai.New(callOnce("write_marker", "{}"))
	defer srv.Close()

	req := request(srv, scriptAgent(dir), "write the marker")
	req.ToolApproval = ToolApprovalAllow
	res, err := Run(t.Context(), req)
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(dir, "marker.txt"))
	assert.Equal(t, []string{"write_marker"}, res.ToolCalls)
	assert.Empty(t, res.RejectedToolCalls)
}

func TestReadOnlyToolRunsUnderDeny(t *testing.T) {
	srv := fakeopenai.New(callOnce("think", `{"thought":"planning"}`))
	defer srv.Close()

	res, err := Run(t.Context(), request(srv, scriptAgent(t.TempDir()), "think first"))
	require.NoError(t, err)
	assert.Equal(t, []string{"think"}, res.ToolCalls)
	assert.Empty(t, res.RejectedToolCalls)
}

func TestPermissionsAllowRuleRunsUnderDeny(t *testing.T) {
	dir := t.TempDir()
	srv := fakeopenai.New(callOnce("write_marker", "{}"))
	defer srv.Close()

	body := scriptAgent(dir) + "permissions:\n  allow: [write_marker]\n"
	res, err := Run(t.Context(), request(srv, body, "write the marker"))
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(dir, "marker.txt"))
	assert.Equal(t, []string{"write_marker"}, res.ToolCalls)
}

func TestConfigAutonomousSafetyIsIgnoredUnderDeny(t *testing.T) {
	dir := t.TempDir()
	srv := fakeopenai.New(callOnce("write_marker", "{}"))
	defer srv.Close()

	body := strings.Replace(scriptAgent(dir), "    instruction: Use your tools.\n", "    instruction: Use your tools.\n    safety: autonomous\n", 1)
	res, err := Run(t.Context(), request(srv, body, "write the marker"))
	require.NoError(t, err)
	assert.NoFileExists(t, filepath.Join(dir, "marker.txt"))
	assert.Equal(t, []string{"write_marker"}, res.RejectedToolCalls)
}

func TestPermissionsDenyRuleWinsUnderAllow(t *testing.T) {
	dir := t.TempDir()
	srv := fakeopenai.New(callOnce("write_marker", "{}"))
	defer srv.Close()

	req := request(srv, scriptAgent(dir)+"permissions:\n  deny: [write_marker]\n", "write the marker")
	req.ToolApproval = ToolApprovalAllow
	res, err := Run(t.Context(), req)
	require.NoError(t, err)
	assert.NoFileExists(t, filepath.Join(dir, "marker.txt"))
	assert.Equal(t, []string{"write_marker"}, res.RejectedToolCalls)
	assert.Contains(t, strings.Join(res.Warnings, "\n"), `tool call "write_marker" was rejected: Tool 'write_marker' is denied by permissions configuration.`)
}

func TestSubAgentDelegation(t *testing.T) {
	srv := fakeopenai.New(func(r fakeopenai.Request) fakeopenai.Reply {
		if strings.Contains(r.System(), "You compute answers.") {
			return fakeopenai.Reply{Content: "42"}
		}
		if len(r.ToolResults()) == 0 {
			return fakeopenai.Reply{ToolCalls: []fakeopenai.ToolCall{{
				Name:      "transfer_task",
				Arguments: `{"agent":"helper","task":"compute the answer","expected_output":"a number"}`,
			}}}
		}
		return fakeopenai.Reply{Content: "The helper said: " + toolResult(r)}
	})
	defer srv.Close()

	body := `agents:
  root:
    model: m
    instruction: You coordinate.
    sub_agents: [helper]
  helper:
    model: m
    description: Computes answers
    instruction: You compute answers.
`
	res, err := Run(t.Context(), request(srv, body, "What is the answer?"))
	require.NoError(t, err, "transfer_task is approved without asking")
	assert.Equal(t, "The helper said: 42", res.Output)
	assert.Equal(t, "root", res.AgentName)
	assert.Equal(t, []string{"transfer_task"}, res.ToolCalls)
	assert.Len(t, srv.Requests(), 3)
	assert.Equal(t, int64(30), res.Usage.InputTokens, "sub-agent usage is included")
	assert.Equal(t, int64(15), res.Usage.OutputTokens)
}

func TestHandoff(t *testing.T) {
	srv := fakeopenai.New(func(r fakeopenai.Request) fakeopenai.Reply {
		if strings.Contains(r.System(), "You close conversations.") {
			return fakeopenai.Reply{Content: "Goodbye from the closer."}
		}
		return fakeopenai.Reply{ToolCalls: []fakeopenai.ToolCall{{Name: "handoff", Arguments: `{"agent":"closer"}`}}}
	})
	defer srv.Close()

	body := `agents:
  root:
    model: m
    instruction: You route.
    handoffs: [closer]
  closer:
    model: m
    description: Closes conversations
    instruction: You close conversations.
`
	res, err := Run(t.Context(), request(srv, body, "Bye"))
	require.NoError(t, err)
	assert.Equal(t, "Goodbye from the closer.", res.Output)
	assert.Equal(t, "closer", res.AgentName)
	assert.Equal(t, []string{"handoff"}, res.ToolCalls)
}

func TestTimeout(t *testing.T) {
	srv := fakeopenai.New(func(fakeopenai.Request) fakeopenai.Reply {
		return fakeopenai.Reply{Content: "too late", Delay: 30 * time.Second}
	})
	defer srv.Close()

	req := request(srv, rootAgent, "hi")
	req.Timeout = time.Second
	start := time.Now()
	_, err := Run(t.Context(), req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "did not finish within 1s")
	assert.Less(t, time.Since(start), 20*time.Second)
}

func TestBadAPIKeyDoesNotLeakTheKey(t *testing.T) {
	srv := fakeopenai.New(nil, fakeopenai.WithToken("the-right-key"))
	defer srv.Close()

	_, err := Run(t.Context(), request(srv, rootAgent, "hi"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
	assert.NotContains(t, err.Error(), fakeKey)
	assert.Contains(t, err.Error(), "[secret]", "the echoed key is redacted")
}

func TestMissingCredential(t *testing.T) {
	srv := fakeopenai.New(nil)
	defer srv.Close()

	t.Setenv("FAKE_API_KEY", "from-the-process-environment")
	req := request(srv, rootAgent, "hi")
	req.Env = nil
	req.InheritEnvironment = false
	_, err := Run(t.Context(), req)
	require.Error(t, err, "the process environment is ignored without inheritEnvironment")
	assert.Contains(t, err.Error(), "FAKE_API_KEY")

	req.InheritEnvironment = true
	res, err := Run(t.Context(), req)
	require.NoError(t, err)
	assert.Equal(t, "You said: hi", res.Output)
	assert.Equal(t, "Bearer from-the-process-environment", srv.Requests()[len(srv.Requests())-1].Authorization)
}

func TestEnvOverridesInheritedEnvironment(t *testing.T) {
	srv := fakeopenai.New(nil)
	defer srv.Close()

	t.Setenv("FAKE_API_KEY", "from-the-process-environment")
	req := request(srv, rootAgent, "hi")
	req.InheritEnvironment = true
	_, err := Run(t.Context(), req)
	require.NoError(t, err)
	assert.Equal(t, "Bearer "+fakeKey, srv.Requests()[0].Authorization)
}

func TestIsolation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	// A user config that would make every tool call autonomous and add a global
	// provider: it must be ignored.
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".config", "cagent"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".config", "cagent", "config.yaml"),
		[]byte("settings:\n  YOLO: true\n  safety: autonomous\n"), 0o600))

	var dataDir, workDir string
	srv := fakeopenai.New(func(r fakeopenai.Request) fakeopenai.Reply {
		if len(r.ToolResults()) == 0 {
			dataDir = paths.GetDataDir()
			return fakeopenai.Reply{ToolCalls: []fakeopenai.ToolCall{{Name: "shell", Arguments: `{"cmd":"pwd"}`}}}
		}
		return fakeopenai.Reply{Content: toolResult(r)}
	})
	defer srv.Close()

	body := `agents:
  root:
    model: m
    instruction: Run pwd.
    toolsets:
      - type: shell
      - type: memory
`
	req := request(srv, body, "where are you?")
	req.ToolApproval = ToolApprovalAllow
	res, err := Run(t.Context(), req)
	require.NoError(t, err)
	workDir = strings.TrimSpace(res.Output)

	assert.True(t, strings.HasPrefix(filepath.Base(filepath.Dir(dataDir)), stateRootPrefix), "data dir %s", dataDir)
	assert.NoDirExists(t, filepath.Dir(dataDir), "the state directory is removed")
	assert.Contains(t, filepath.Base(workDir), "pulumi-docker-agent-work-")
	assert.NoDirExists(t, workDir, "the temporary working directory is removed")
	assert.NoDirExists(t, filepath.Join(home, ".cagent"))
	assert.NotEqual(t, filepath.Join(home, ".cagent"), paths.GetDataDir(), "docker-agent never falls back to the user's data dir")

	// The user settings did not apply: under the default deny, shell is rejected.
	srv2 := fakeopenai.New(callOnce("shell", `{"cmd":"echo hi"}`))
	defer srv2.Close()
	res, err = Run(t.Context(), request(srv2, body, "run"))
	require.NoError(t, err)
	assert.Equal(t, []string{"shell"}, res.RejectedToolCalls)
}

func TestWorkingDir(t *testing.T) {
	dir := t.TempDir()
	srv := fakeopenai.New(callOnce("shell", `{"cmd":"pwd"}`))
	defer srv.Close()

	body := `agents:
  root:
    model: m
    instruction: Run pwd.
    toolsets:
      - type: shell
`
	req := request(srv, body, "pwd")
	req.ToolApproval = ToolApprovalAllow
	req.WorkingDir = dir
	res, err := Run(t.Context(), req)
	require.NoError(t, err)
	assert.Contains(t, res.Output, dir)
	assert.DirExists(t, dir, "a given working directory is kept")

	req.WorkingDir = filepath.Join(dir, "missing")
	_, err = Run(t.Context(), req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is not an existing directory")
}

func TestParallelRunsAreSerialized(t *testing.T) {
	var inFlight, maxInFlight atomic.Int32
	srv := fakeopenai.New(func(r fakeopenai.Request) fakeopenai.Reply {
		n := inFlight.Add(1)
		defer inFlight.Add(-1)
		for {
			m := maxInFlight.Load()
			if n <= m || maxInFlight.CompareAndSwap(m, n) {
				break
			}
		}
		time.Sleep(200 * time.Millisecond)
		return fakeopenai.Reply{Content: "ok " + r.LastUser()}
	})
	defer srv.Close()

	var wg sync.WaitGroup
	errs := make([]error, 3)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = Run(context.Background(), request(srv, rootAgent, fmt.Sprint(i)))
		}()
	}
	wg.Wait()
	for _, err := range errs {
		require.NoError(t, err)
	}
	assert.Equal(t, int32(1), maxInFlight.Load())
}

func TestMaxIterationsFailsTheRun(t *testing.T) {
	srv := fakeopenai.New(func(fakeopenai.Request) fakeopenai.Reply {
		return fakeopenai.Reply{ToolCalls: []fakeopenai.ToolCall{{Name: "think", Arguments: `{"thought":"again"}`}}}
	})
	defer srv.Close()

	body := `agents:
  root:
    model: m
    instruction: Think forever.
    max_iterations: 2
    toolsets:
      - type: think
`
	_, err := Run(t.Context(), request(srv, body, "go"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "final answer")
}

func TestHarnessAgentsAreRejected(t *testing.T) {
	srv := fakeopenai.New(nil)
	defer srv.Close()

	body := `agents:
  root:
    harness:
      type: claude-code
    instruction: Code.
`
	doc := agentYAML(srv, body)
	problems, warnings := CheckConfig(t.Context(), "test.yaml", []byte(doc), CheckOptions{})
	const h1 = "coding-harness agents (agents.root.harness: claude-code) run only with allowHarnessAgents: true"
	assert.Contains(t, strings.Join(problems, "\n"), h1)
	assert.Empty(t, warnings)
	_, err := Run(t.Context(), Request{YAML: []byte(doc), Prompt: "hi"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), h1)
}

func TestModelErrorFailsTheRun(t *testing.T) {
	srv := fakeopenai.New(func(fakeopenai.Request) fakeopenai.Reply {
		return fakeopenai.Reply{Status: 400, ErrorMessage: "model fake-model does not exist"}
	})
	defer srv.Close()

	_, err := Run(t.Context(), request(srv, rootAgent, "hi"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not exist")
}

func TestRequestValidation(t *testing.T) {
	_, err := Run(t.Context(), Request{YAML: []byte("agents: {}"), Prompt: " "})
	assert.ErrorContains(t, err, "prompt must not be empty")
	_, err = Run(t.Context(), Request{Prompt: "hi"})
	assert.ErrorContains(t, err, "configuration is empty")
	_, err = Run(t.Context(), Request{YAML: []byte("agents: {}"), Prompt: "hi", ToolApproval: "maybe"})
	assert.ErrorContains(t, err, `invalid toolApproval "maybe"`)
}

func TestRedact(t *testing.T) {
	assert.Equal(t, "key [secret] and [secret]", redact("key abcdef and abcdefgh", []string{"abcdef", "abcdefgh", "ab"}))
	assert.Equal(t, "nothing", redact("nothing", nil))
}

func TestFakeSample(t *testing.T) {
	var schema map[string]any
	require.NoError(t, json.Unmarshal([]byte(`{"type":"object","properties":{"n":{"type":"integer"},"tags":{"type":"array","items":{"type":"string"}},"ok":{"type":"boolean"},"kind":{"enum":["a","b"]}}}`), &schema))
	assert.Equal(t, map[string]any{"n": 1, "tags": []any{"sample"}, "ok": true, "kind": "a"}, fakeopenai.Sample(schema))
}

// recordingHandler stands in for stderr.
type recordingHandler struct {
	mu   sync.Mutex
	msgs []string
}

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	var b strings.Builder
	b.WriteString(r.Message)
	r.Attrs(func(a slog.Attr) bool { fmt.Fprintf(&b, " %s=%v", a.Key, a.Value.Any()); return true })
	h.msgs = append(h.msgs, b.String())
	return nil
}
func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *recordingHandler) WithGroup(string) slog.Handler      { return h }

func (h *recordingHandler) all() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return strings.Join(h.msgs, "\n")
}

func TestLogsStayOffStderrAndAreRedacted(t *testing.T) {
	installLogHandler()
	lh, ok := slog.Default().Handler().(*logHandler)
	require.True(t, ok, "the run log handler is the default")
	stderr := &recordingHandler{}
	prev := lh.next
	lh.next = stderr
	defer func() { lh.next = prev }()

	srv := fakeopenai.New(func(r fakeopenai.Request) fakeopenai.Reply {
		slog.Warn("context-less record mentioning " + fakeKey)        // attributed to the run
		slog.WarnContext(context.TODO(), "unrelated record "+fakeKey) // other work: passed on, redacted
		return fakeopenai.Reply{Status: 401, ErrorMessage: "Incorrect API key provided: " + fakeKey}
	})
	defer srv.Close()

	var mu sync.Mutex
	var runLogs []string
	req := request(srv, rootAgent, "hi")
	req.Log = func(level slog.Level, msg string) {
		mu.Lock()
		defer mu.Unlock()
		runLogs = append(runLogs, level.String()+" "+msg)
	}
	_, err := Run(t.Context(), req)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), fakeKey)

	logs := strings.Join(runLogs, "\n")
	assert.Contains(t, logs, "ERROR Non-retryable error from model")
	assert.Contains(t, logs, "context-less record mentioning [secret]")
	assert.NotContains(t, logs, fakeKey)
	assert.NotContains(t, logs, "Model selected", "records below WARN are dropped")

	out := stderr.all()
	assert.Contains(t, out, "unrelated record [secret]")
	assert.NotContains(t, out, fakeKey)
	assert.NotContains(t, out, "Non-retryable error from model")

	// After the run, records pass through untouched.
	slog.Warn("after the run")
	assert.Contains(t, stderr.all(), "after the run")
}

func TestWaitingForAnotherRunHonoursCancellation(t *testing.T) {
	release := make(chan struct{})
	srv := fakeopenai.New(func(fakeopenai.Request) fakeopenai.Reply {
		<-release
		return fakeopenai.Reply{Content: "first"}
	})
	defer srv.Close()

	done := make(chan error, 1)
	go func() {
		_, err := Run(context.Background(), request(srv, rootAgent, "first"))
		done <- err
	}()
	require.Eventually(t, func() bool { return len(srv.Requests()) == 1 }, 10*time.Second, 10*time.Millisecond)

	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	_, err := Run(ctx, request(srv, rootAgent, "second"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "canceled while waiting for another agent run")

	close(release)
	require.NoError(t, <-done)
}

// The `openai` provider uses the Responses API for gpt-5 models; OPENAI_BASE_URL points it at
// the fake (the docs examples are tested this way).
func TestOpenAIProviderResponsesAPI(t *testing.T) {
	srv := fakeopenai.New(nil)
	defer srv.Close()
	t.Setenv("OPENAI_BASE_URL", srv.URL)

	res, err := Run(t.Context(), Request{
		YAML:   []byte("agents:\n  root:\n    model: openai/gpt-5-mini\n    instruction: Review.\n" + reviewSchema),
		Prompt: "Review this change.",
		Env:    map[string]string{"OPENAI_API_KEY": fakeKey},
	})
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"risk": "low", "summary": "sample"}, res.StructuredOutput)
	assert.Equal(t, int64(10), res.Usage.InputTokens)
	assert.Positive(t, res.Usage.Cost, "gpt-5-mini is priced in the embedded models.dev catalog")
	reqs := srv.Requests()
	require.Len(t, reqs, 1)
	assert.Equal(t, "Bearer "+fakeKey, reqs[0].Authorization)
	assert.NotNil(t, reqs[0].Schema)
}

// approvingHook writes a pre_tool_use (or permission_request) command hook that approves
// every tool call, and returns the hooks YAML for the agent.
func approvingHook(t *testing.T, event string) string {
	t.Helper()
	hook := filepath.Join(t.TempDir(), "approve.sh")
	script := "#!/bin/sh\ncat >/dev/null\necho '{\"hook_specific_output\":{\"hook_event_name\":\"" + event +
		"\",\"permission_decision\":\"allow\",\"permission_decision_reason\":\"ok\"}}'\n"
	require.NoError(t, os.WriteFile(hook, []byte(script), 0o755)) //nolint:gosec // an executable test hook
	return fmt.Sprintf(`    hooks:
      %s:
        - matcher: "*"
          hooks:
            - type: command
              command: %s
`, event, hook)
}

func shellAgent(extra string) string {
	return `agents:
  root:
    model: m
    instruction: Use the shell.
    toolsets:
      - type: shell
` + extra
}

// Review R9: a hook that approves tool calls must not override toolApproval "deny".
func TestHookApprovalsDoNotOverrideDeny(t *testing.T) {
	for _, event := range []string{"pre_tool_use", "permission_request"} {
		t.Run(event, func(t *testing.T) {
			dir := t.TempDir()
			marker := filepath.Join(dir, "marker")
			srv := fakeopenai.New(callOnce("shell", fmt.Sprintf(`{"cmd":"touch %s"}`, marker)))
			defer srv.Close()
			body := shellAgent(approvingHook(t, event))

			res, err := Run(t.Context(), request(srv, body, "go"))
			require.NoError(t, err)
			assert.NoFileExists(t, marker, "a hook approval ran the shell under deny")
			assert.Equal(t, []string{}, res.ToolCalls)
			assert.Equal(t, []string{"shell"}, res.RejectedToolCalls)
			assert.Contains(t, strings.Join(res.Warnings, "\n"), "does not accept hook approvals unless allowHookApprovals is set")

			// The opt-in restores docker-agent's behaviour.
			req := request(srv, body, "go")
			req.AllowHookApprovals = true
			res, err = Run(t.Context(), req)
			require.NoError(t, err)
			assert.FileExists(t, marker)
			assert.Equal(t, []string{"shell"}, res.ToolCalls)
		})
	}
}

// Without hooks, the guard never rejects what docker-agent approved by its rules.
func TestGuardKeepsDockerAgentRules(t *testing.T) {
	srv := fakeopenai.New(callOnce("shell", `{"cmd":"echo allowed-by-rule"}`))
	defer srv.Close()
	res, err := Run(t.Context(), request(srv, shellAgent("permissions:\n  allow: [\"shell:cmd=echo*\"]\n"), "go"))
	require.NoError(t, err)
	assert.Equal(t, []string{"shell"}, res.ToolCalls)
	assert.Contains(t, res.Output, "allowed-by-rule")
}

// Review R1, R2 and R5: without inheritEnvironment, the processes AgentRun controls do not
// see the provider's environment.
func TestProcessesDoNotInheritTheEnvironment(t *testing.T) {
	const procSecret = "proc-only-secret-value"
	t.Setenv("PROC_ONLY_SECRET", procSecret)
	dir := t.TempDir()
	mcpEnv, hookEnvFile := filepath.Join(dir, "mcp-env"), filepath.Join(dir, "hook-env")
	srv := fakeopenai.New(func(r fakeopenai.Request) fakeopenai.Reply {
		if r.HasTool("shell") && len(r.ToolResults()) == 0 {
			return fakeopenai.Reply{ToolCalls: []fakeopenai.ToolCall{
				{Name: "shell", Arguments: `{"cmd":"echo P=$PROC_ONLY_SECRET K=$FAKE_API_KEY S=$SHELL_ONLY H=$HOME"}`},
			}}
		}
		return fakeopenai.Reply{Content: toolResult(r)}
	})
	defer srv.Close()
	body := fmt.Sprintf(`agents:
  root:
    model: m
    instruction: hi
    toolsets:
      - type: shell
        env:
          SHELL_ONLY: from-toolset-env
      - type: mcp
        command: sh
        args: ["-c", "env > %s; exit 1"]
        env:
          MCP_ONLY: from-toolset-env
    hooks:
      session_start:
        - type: command
          command: "env > %s"
`, mcpEnv, hookEnvFile)

	req := request(srv, body, "go")
	req.InheritEnvironment = false
	req.ToolApproval = ToolApprovalAllow
	res, err := Run(t.Context(), req)
	require.NoError(t, err)

	assert.Contains(t, res.Output, "P= K= S=from-toolset-env H="+os.Getenv("HOME"), "shell: system variables and its own env only")
	mcp, err := os.ReadFile(mcpEnv)
	require.NoError(t, err, "the MCP server did not start")
	assert.NotContains(t, string(mcp), procSecret)
	assert.NotContains(t, string(mcp), fakeKey, "env reaches MCP servers only through their own env")
	assert.Contains(t, string(mcp), "MCP_ONLY=from-toolset-env")
	assert.Contains(t, string(mcp), "PATH=")
	hook, err := os.ReadFile(hookEnvFile)
	require.NoError(t, err, "the hook did not run")
	assert.NotContains(t, string(hook), procSecret)
	assert.Contains(t, string(hook), "FAKE_API_KEY="+fakeKey, "hooks get env")
	assert.Contains(t, string(hook), "PATH=")

	// Inheriting keeps docker-agent's behaviour.
	req.InheritEnvironment = true
	res, err = Run(t.Context(), req)
	require.NoError(t, err)
	assert.Contains(t, res.Output, "P="+procSecret)
}

func TestLSPEnvironmentEmptiesOtherVariables(t *testing.T) {
	t.Setenv("PROC_ONLY_SECRET", "proc-only-secret-value")
	env := slices.Concat(emptiedEnvList(), systemEnv())
	assert.Contains(t, env, "PROC_ONLY_SECRET=")
	assert.Contains(t, env, "PATH="+os.Getenv("PATH"))
	assert.NotContains(t, strings.Join(env, "\n"), "proc-only-secret-value")
}

// Review R10: credentials inherited from the environment are redacted too.
func TestInheritedCredentialsAreRedacted(t *testing.T) {
	const inherited = "sk-inherited-process-key-987654"
	const unrelated = "an-unrelated-service-token-123456"
	const tokenKey = "tok-of-a-plainly-named-variable"
	srv := fakeopenai.New(func(r fakeopenai.Request) fakeopenai.Reply {
		return fakeopenai.Reply{Status: 401, ErrorMessage: "rejected " + strings.TrimPrefix(r.Authorization, "Bearer ") +
			" (also saw " + unrelated + " and " + tokenKey + ")"}
	})
	defer srv.Close()
	t.Setenv("FAKE_API_KEY", inherited)
	t.Setenv("OTHER_SERVICE_TOKEN", unrelated)
	t.Setenv("PLAIN_NAME", tokenKey)

	req := request(srv, rootAgent+"  helper:\n    model: other\n    instruction: x\n", "hi")
	req.YAML = []byte(strings.Replace(string(req.YAML), "models:\n", "models:\n  other:\n    provider: fake\n    model: x\n    token_key: PLAIN_NAME\n", 1))
	req.Env = nil
	req.InheritEnvironment = true
	var logs []string
	req.Log = func(_ slog.Level, m string) { logs = append(logs, m) }
	_, err := Run(t.Context(), req)
	require.Error(t, err)
	all := err.Error() + "\n" + strings.Join(logs, "\n")
	for _, secret := range []string{inherited, unrelated, tokenKey} {
		assert.NotContains(t, all, secret)
	}
	assert.Contains(t, err.Error(), "rejected [secret]")
}

// Review R6: an answer that repeats a secret is reported, so it is stored as a secret.
func TestAnswerContainingASecretIsReported(t *testing.T) {
	srv := fakeopenai.New(func(r fakeopenai.Request) fakeopenai.Reply {
		return fakeopenai.Reply{Content: "system was: " + r.System()}
	})
	defer srv.Close()
	body := `agents:
  root:
    model: m
    instruction: "Use token ${env.FAKE_API_KEY} for calls."
`
	res, err := Run(t.Context(), request(srv, body, "go"))
	require.NoError(t, err)
	assert.Contains(t, res.Output, fakeKey, "the answer itself is not altered")
	assert.True(t, res.OutputContainsSecret)
	assert.Contains(t, strings.Join(res.Warnings, "\n"), "the answer contains the value of a secret")
	assert.True(t, AnswerContainsSecret(res.Output, request(srv, body, "go")))

	res, err = Run(t.Context(), request(srv, rootAgent, "go"))
	require.NoError(t, err)
	assert.False(t, res.OutputContainsSecret)
}

func TestTokenKeys(t *testing.T) {
	assert.Equal(t, []string{"A_KEY", "B"}, tokenKeys([]byte("models:\n  m:\n    token_key: B\nproviders:\n  p:\n    token_key: A_KEY\n  q:\n    token_key: B\n")))
	assert.Nil(t, tokenKeys([]byte(":::")))
}
