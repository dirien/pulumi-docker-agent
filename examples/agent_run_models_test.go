// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package examples

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pulumi/providertest/pulumitest"
	"github.com/pulumi/providertest/pulumitest/assertpreview"
	"github.com/pulumi/providertest/pulumitest/opttest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExampleAgentRunOllama runs examples/agent-run-ollama-yaml, which runs an agent on an
// `ollama` model during `pulumi up`. The model's baseUrl points at a fake OpenAI-compatible
// server started by the test, so no Ollama is needed. The Pulumi CLI gets an OPENAI_API_KEY:
// the `ollama` provider has no key of its own, so the OpenAI SDK would send that variable, and
// the program's `inheritEnvironment: false` must keep it from the model (since v0.2.0 the run
// child's environment holds only `env` and the base environment). The test also checks the
// credential line and the summary of the run, that preview does not run the agent again, and
// that destroy runs nothing.
func TestExampleAgentRunOllama(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	const lang = "agent-run-ollama-yaml"
	const shellKey = "sk-fake-shell-key-must-not-reach-ollama-0123456789"
	llm := newFakeLLM(t)

	pt := pulumitest.NewPulumiTest(t, lang,
		opttest.LocalProviderPath(providerName, filepath.Join(root, "bin")),
		opttest.SkipInstall(),
		opttest.SkipStackCreate(),
		opttest.Env("OPENAI_API_KEY", shellKey),
	)
	pt.NewStack(t, "")
	pt.SetConfig(t, "ollamaUrl", llm.URL)
	pt.SetConfig(t, "model", "fake-model")

	up := pt.Up(t)
	notes := stringOutput(t, up.Outputs, "releaseNotes")
	assert.True(t, strings.HasPrefix(notes, "You said: "), notes)
	assert.Contains(t, notes, "PostgreSQL cluster moves from version 15 to 17")
	assert.Equal(t, "root", stringOutput(t, up.Outputs, "agentName"))
	assert.Equal(t, map[string]any{"inputTokens": 10.0, "outputTokens": 5.0, "cost": 0.0}, outputValue(t, up.Outputs, "usage"))
	// `logging: status` shows where the model's credential comes from, and why there is no cost.
	assert.Contains(t, up.StdOut, "models: ollama/fake-model (no key)")
	assert.Contains(t, up.StdOut, "no cost estimate")
	assert.NotContains(t, up.StdOut+up.StdErr, shellKey)

	reqs := llm.received()
	require.Len(t, reqs, 1, "up runs the agent once")
	assert.Equal(t, "fake-model", reqs[0].Model)
	assert.Contains(t, reqs[0].System, "You write release notes for infrastructure changes")
	assert.Contains(t, reqs[0].User, "Sunday maintenance window")
	assert.NotContains(t, reqs[0].Authorization, shellKey,
		"inheritEnvironment: false keeps the Pulumi CLI's OPENAI_API_KEY from the Ollama server")
	assert.Empty(t, reqs[0].Authorization, "the ollama provider sends no key")

	assertpreview.HasNoChanges(t, pt.Preview(t))
	assert.Len(t, llm.received(), 1, "a preview never runs the agent")

	pt.SetConfig(t, "revision", "2")
	up = pt.Up(t)
	assert.Equal(t, 1, (*up.Summary.ResourceChanges)["update"], "only the AgentRun is updated:\n%s", up.StdOut)
	assert.Len(t, llm.received(), 2, "a new trigger runs the agent again")

	pt.Destroy(t)
	assert.Len(t, llm.received(), 2, "destroy runs nothing")
}

// chatGPTProgram is a Pulumi YAML program with a `chatgpt` model whose baseUrl is the fake
// ChatGPT backend of the test (the real one is https://chatgpt.com/backend-api/codex).
const chatGPTProgram = `name: agent-run-chatgpt
runtime: yaml
config:
  backendUrl:
    type: string
resources:
  assistant:
    type: docker-agent:index:AgentConfig
    properties:
      models:
        plan:
          provider: chatgpt
          model: gpt-5.5
          baseUrl: ${backendUrl}
      agents:
        - name: root
          model: plan
          instruction: You answer in one sentence.
  answer:
    type: docker-agent:index:AgentRun
    properties:
      yaml: ${assistant.yaml}
      prompt: Say hello.
      inheritEnvironment: false
outputs:
  answer: ${answer.output}
  usage: ${answer.usage}
`

// TestExampleAgentRunChatGPTLogin runs a `chatgpt` model with the ChatGPT login that
// `docker agent setup` stores in ~/.config/cagent/chatgpt-auth.json. HOME is a temporary
// directory with a login that does not expire soon, so AgentRun needs no refresh (whose
// endpoint cannot be faked), and the model's baseUrl points at a fake ChatGPT backend that
// speaks the Responses API. The test checks that the run sends the login's access token with
// `inheritEnvironment: false`, that `pulumi up` shows the login as the credential and the plan
// instead of a cost, that the token appears in no output, that the login file is unchanged,
// and that destroy runs nothing.
func TestExampleAgentRunChatGPTLogin(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	const accessToken = "fake-chatgpt-access-token-0123456789abcdef"
	const refreshToken = "fake-chatgpt-refresh-token-0123456789abcdef"
	backend := newFakeChatGPT(t)

	home := t.TempDir()
	login := filepath.Join(home, ".config", "cagent", "chatgpt-auth.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(login), 0o700))
	creds, err := json.Marshal(map[string]any{
		"access_token":  accessToken,
		"refresh_token": refreshToken,
		"expires_at":    time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339),
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(login, creds, 0o600))

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Pulumi.yaml"), []byte(chatGPTProgram), 0o600))
	pt := pulumitest.NewPulumiTest(t, dir,
		opttest.LocalProviderPath(providerName, filepath.Join(root, "bin")),
		opttest.SkipInstall(),
		opttest.SkipStackCreate(),
		opttest.Env("HOME", home),
		opttest.Env("PULUMI_HOME", userPulumiHome(t)),
	)
	pt.NewStack(t, "")
	pt.SetConfig(t, "backendUrl", backend.URL)

	up := pt.Up(t)
	assert.Equal(t, "Hello from the fake ChatGPT backend.", stringOutput(t, up.Outputs, "answer"))
	assert.Equal(t, map[string]any{"inputTokens": 10.0, "outputTokens": 5.0, "cost": 0.0}, outputValue(t, up.Outputs, "usage"))
	assert.Contains(t, up.StdOut,
		"models: chatgpt/gpt-5.5 (the ChatGPT login of docker agent setup, billed to the ChatGPT plan)")
	assert.Contains(t, up.StdOut, "no per-token cost: billed to the ChatGPT plan")
	assert.NotContains(t, up.StdOut+up.StdErr, accessToken)
	assert.NotContains(t, up.StdOut+up.StdErr, refreshToken)

	reqs := backend.received()
	require.Len(t, reqs, 1, "up runs the agent once")
	assert.Equal(t, "Bearer "+accessToken, reqs[0].Authorization, "the run uses the login's access token")
	assert.Equal(t, "gpt-5.5", reqs[0].Model)
	assert.Equal(t, "/backend-api/codex/responses", reqs[0].Path)
	assert.Contains(t, reqs[0].Instructions, "You answer in one sentence.")

	after, err := os.ReadFile(login) //nolint:gosec // the test's own login file
	require.NoError(t, err)
	assert.JSONEq(t, string(creds), string(after), "a login the run did not refresh is not written back")
	assert.NotContains(t, stateWithoutSecrets(t, pt.ExportStack(t).Deployment), accessToken)

	assertpreview.HasNoChanges(t, pt.Preview(t))
	pt.Destroy(t)
	assert.Len(t, backend.received(), 1, "preview and destroy run nothing")
}

// userPulumiHome returns the Pulumi home directory of the user, so that a test with another HOME
// still finds the installed plugins and language hosts.
func userPulumiHome(t *testing.T) string {
	t.Helper()
	if h := os.Getenv("PULUMI_HOME"); h != "" {
		return h
	}
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	return filepath.Join(home, ".pulumi")
}

// fakeChatGPT is a minimal ChatGPT Codex backend: the OpenAI Responses API at
// <URL>/responses, streamed as server-sent events. It answers every request with one sentence
// and reports 10 input and 5 output tokens.
type fakeChatGPT struct {
	URL string // the backend's base URL, like https://chatgpt.com/backend-api/codex

	mu       sync.Mutex
	requests []fakeChatGPTRequest
}

type fakeChatGPTRequest struct {
	Path, Authorization, Model, Instructions string
}

func newFakeChatGPT(t *testing.T) *fakeChatGPT {
	t.Helper()
	f := &fakeChatGPT{}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	f.URL = srv.URL + "/backend-api/codex"
	return f
}

func (f *fakeChatGPT) received() []fakeChatGPTRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]fakeChatGPTRequest(nil), f.requests...)
}

func (f *fakeChatGPT) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/responses") {
		http.NotFound(w, r)
		return
	}
	body, _ := io.ReadAll(r.Body)
	var req struct {
		Model        string `json:"model"`
		Instructions string `json:"instructions"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	f.requests = append(f.requests, fakeChatGPTRequest{Path: r.URL.Path, Authorization: r.Header.Get("Authorization"),
		Model: req.Model, Instructions: req.Instructions})
	f.mu.Unlock()

	const text = "Hello from the fake ChatGPT backend."
	w.Header().Set("Content-Type", "text/event-stream")
	flusher, _ := w.(http.Flusher)
	send := func(v map[string]any) {
		b, _ := json.Marshal(v)
		_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", v["type"], b)
		if flusher != nil {
			flusher.Flush()
		}
	}
	response := func(status string, output []any) map[string]any {
		resp := map[string]any{"id": "resp_fake", "object": "response", "created_at": 1, "status": status,
			"model": req.Model, "output": output}
		if status == "completed" {
			resp["usage"] = map[string]any{"input_tokens": 10, "output_tokens": 5, "total_tokens": 15,
				"input_tokens_details":  map[string]any{"cached_tokens": 0},
				"output_tokens_details": map[string]any{"reasoning_tokens": 0}}
		}
		return resp
	}
	send(map[string]any{"type": "response.created", "sequence_number": 0, "response": response("in_progress", []any{})})
	item := map[string]any{"type": "message", "id": "msg_fake", "role": "assistant", "status": "in_progress",
		"content": []any{}}
	send(map[string]any{"type": "response.output_item.added", "output_index": 0, "item": item})
	send(map[string]any{"type": "response.output_text.delta", "item_id": "msg_fake", "output_index": 0,
		"content_index": 0, "delta": text})
	done := map[string]any{"type": "message", "id": "msg_fake", "role": "assistant", "status": "completed",
		"content": []any{map[string]any{"type": "output_text", "text": text, "annotations": []any{}}}}
	send(map[string]any{"type": "response.output_item.done", "output_index": 0, "item": done})
	send(map[string]any{"type": "response.completed", "response": response("completed", []any{done})})
}
