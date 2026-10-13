// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package agentrun

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dirien/pulumi-docker-agent/provider/pkg/agentrun/fakeopenai"
)

// progressAgents is a team whose root agent thinks, tries the shell (rejected under
// toolApproval deny), transfers a task to a helper and answers. Every message of the model
// quotes the API key, which must never be reported.
const progressAgents = `agents:
  root:
    model: m
    instruction: You coordinate.
    sub_agents: [helper]
    toolsets:
      - type: think
      - type: shell
  helper:
    model: m
    description: Computes answers
    instruction: You compute answers.
`

func progressServer() *fakeopenai.Server {
	return fakeopenai.New(func(r fakeopenai.Request) fakeopenai.Reply {
		if strings.Contains(r.System(), "You compute answers.") {
			return fakeopenai.Reply{Content: "42, says the helper with " + fakeKey}
		}
		turns := 0
		for _, m := range r.Messages {
			if m.Role == "assistant" {
				turns++
			}
		}
		switch turns {
		case 0:
			return fakeopenai.Reply{ToolCalls: []fakeopenai.ToolCall{
				{ID: "c1", Name: "think", Arguments: `{"thought":"the key is ` + fakeKey + `"}`},
				{ID: "c2", Name: "shell", Arguments: `{"cmd":"echo ` + fakeKey + `"}`},
			}}
		case 1:
			return fakeopenai.Reply{ToolCalls: []fakeopenai.ToolCall{{
				ID: "c3", Name: "transfer_task", Arguments: `{"agent":"helper","task":"compute","expected_output":"a number"}`,
			}}}
		}
		return fakeopenai.Reply{Content: "The answer is 42 (key " + fakeKey + ")"}
	})
}

// runWithProgress runs the progress team at a logging level and returns its messages.
func runWithProgress(t *testing.T, level Logging, hideNames bool) (status, persistent []string) {
	t.Helper()
	srv := progressServer()
	defer srv.Close()
	req := request(srv, progressAgents, "What is the answer?")
	req.Logging = level
	req.HideNames = hideNames
	req.Progress = func(p Progress) {
		if p.Persistent {
			persistent = append(persistent, p.Message)
		} else {
			status = append(status, p.Message)
		}
	}
	res, err := Run(t.Context(), req)
	require.NoError(t, err)
	assert.Equal(t, []string{"think", "transfer_task"}, res.ToolCalls)
	assert.Equal(t, []string{"shell"}, res.RejectedToolCalls)
	all := strings.Join(append(append([]string{}, status...), persistent...), "\n")
	assert.NotContains(t, all, fakeKey, "progress is redacted")
	return status, persistent
}

func TestProgressStatus(t *testing.T) {
	status, persistent := runWithProgress(t, "", false)
	all := strings.Join(status, "\n")
	for _, want := range []string{
		"root started",
		"root: calling think",
		"root: think approved",
		"root: shell rejected (it needs approval and toolApproval is deny)",
		"root -> helper: task transferred",
		"helper started",
		"helper -> root: task finished",
		"root: transfer_task approved",
	} {
		assert.Contains(t, all, want)
	}
	require.Len(t, persistent, 2, "status only adds the credential line and the summary: %v", persistent)
	assert.Equal(t, "models: fake/fake-model (FAKE_API_KEY from env)", persistent[0])
	assert.Regexp(t, `^agent run finished in [0-9.]+m?s: root answered after 3 tool calls \(1 rejected\); `+
		`40 input and 20 output tokens; no cost estimate`, persistent[1])
}

func TestProgressTools(t *testing.T) {
	_, persistent := runWithProgress(t, LoggingTools, false)
	all := strings.Join(persistent, "\n")
	assert.Contains(t, all, `root called think {"thought":"the key is [secret]"}: approved`)
	assert.Contains(t, all, `root called shell {"cmd":"echo [secret]"}: rejected (it needs approval`)
	assert.Contains(t, all, `root called transfer_task {"agent":"helper","task":"compute","expected_output":"a number"}: approved`)
	assert.NotContains(t, all, "The answer is", "tools does not report the agents' messages")
	assert.Contains(t, persistent[len(persistent)-1], "agent run finished")
}

func TestProgressFull(t *testing.T) {
	_, persistent := runWithProgress(t, LoggingFull, false)
	all := strings.Join(persistent, "\n")
	assert.Contains(t, all, "root: The answer is 42 (key [secret])")
	assert.Contains(t, all, "helper: 42, says the helper with [secret]")
	assert.Contains(t, all, `root called transfer_task {"agent":"helper","task":"compute","expected_output":"a number"}: `+
		"approved: 42, says the helper with [secret]", "full reports tool results")
}

func TestProgressNone(t *testing.T) {
	status, persistent := runWithProgress(t, LoggingNone, false)
	assert.Empty(t, status)
	assert.Empty(t, persistent)
}

func TestProgressHidesNames(t *testing.T) {
	status, persistent := runWithProgress(t, LoggingTools, true)
	all := strings.Join(append(status, persistent...), "\n")
	assert.Contains(t, all, "an agent started")
	assert.Contains(t, all, "an agent: a tool rejected (it needs approval")
	assert.Contains(t, all, "an agent -> another agent: task transferred")
	assert.Contains(t, all, "an agent called a tool: approved")
	assert.Contains(t, all, "the agent answered after 3 tool calls (1 rejected)")
	for _, name := range []string{"root", "helper", "think", "shell", "transfer_task", "compute"} {
		assert.NotContains(t, all, name)
	}
}

func TestProgressHandoff(t *testing.T) {
	srv := fakeopenai.New(func(r fakeopenai.Request) fakeopenai.Reply {
		if strings.Contains(r.System(), "You close conversations.") {
			return fakeopenai.Reply{Content: "Goodbye."}
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
	var status []string
	req := request(srv, body, "Bye")
	req.Progress = func(p Progress) { status = append(status, p.Message) }
	_, err := Run(t.Context(), req)
	require.NoError(t, err)
	assert.Contains(t, strings.Join(status, "\n"), "root -> closer: conversation handed off")
	assert.Contains(t, status[len(status)-1], "closer answered after 1 tool calls")
}

func TestTruncate(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "a b c", truncate("a\n b\t\tc ", 10))
	assert.Equal(t, "äöü...", truncate("äöüß", 3))
}

func TestProgressModelFallback(t *testing.T) {
	srv := fakeopenai.New(func(r fakeopenai.Request) fakeopenai.Reply {
		if r.Model == "fake-model" {
			return fakeopenai.Reply{Status: 400, ErrorMessage: "rejected key " + fakeKey}
		}
		return fakeopenai.Reply{Content: "from the fallback"}
	})
	defer srv.Close()
	body := `  backup:
    provider: fake
    model: fake-backup
agents:
  root:
    model: m
    instruction: Answer.
    fallback:
      models: [backup]
`
	var status []string
	req := request(srv, body, "hi")
	req.Progress = func(p Progress) { status = append(status, p.Message) }
	res, err := Run(t.Context(), req)
	require.NoError(t, err)
	assert.Equal(t, "from the fallback", res.Output)
	all := strings.Join(status, "\n")
	assert.Contains(t, all, "root: model fake/fake-model failed (")
	assert.Contains(t, all, "falling back to fake/fake-backup")
	assert.NotContains(t, all, fakeKey)
}

// TestProgressRedactsEscapedSecrets checks that secrets with characters that JSON escapes
// (quotes, backslashes, and <, > and & under HTML escaping) are redacted in tool arguments,
// tool results and messages, where they appear JSON-escaped rather than verbatim.
func TestProgressRedactsEscapedSecrets(t *testing.T) {
	quoted, html := `p@ss"w0rd\with-quote`, `a<b>&c`
	thought, err := json.Marshal(map[string]string{"thought": "the passwords are " + quoted + " and " + html})
	require.NoError(t, err)
	bs := `\`
	escapedHTML := "a" + bs + "u003cb" + bs + "u003e" + bs + "u0026c"
	require.Contains(t, string(thought), `p@ss\"w0rd\\with-quote and `+escapedHTML,
		"Go escapes quotes, backslashes and HTML characters")
	escapedAnswer, err := json.Marshal("Done with " + quoted + " and " + html)
	require.NoError(t, err)
	srv := fakeopenai.New(func(r fakeopenai.Request) fakeopenai.Reply {
		if len(r.ToolResults()) == 0 {
			return fakeopenai.Reply{ToolCalls: []fakeopenai.ToolCall{{ID: "c1", Name: "think", Arguments: string(thought)}}}
		}
		return fakeopenai.Reply{Content: string(escapedAnswer)}
	})
	defer srv.Close()
	req := request(srv, "agents:\n  root:\n    model: m\n    instruction: Think.\n    toolsets:\n      - type: think\n", "Go")
	req.Secrets = []string{quoted, html}
	req.Logging = LoggingFull
	var messages []string
	req.Progress = func(p Progress) { messages = append(messages, p.Message) }
	res, err := Run(t.Context(), req)
	require.NoError(t, err)
	assert.True(t, res.OutputContainsSecret, "the answer quotes the secrets JSON-escaped")

	all := strings.Join(messages, "\n")
	assert.Contains(t, all, `root called think {"thought":"the passwords are [secret] and [secret]"}: approved`)
	for _, leak := range []string{"p@ss", "w0rd", `a<b>&c`, "a" + bs + "u003cb", bs + "u0026c", "with-quote"} {
		assert.NotContains(t, all, leak)
	}
}

// TestBillingNote: the summary counts the usage billed per token and names the plans that paid
// for the rest, never dropping a plan because another part has a cost (codex review #1).
func TestBillingNote(t *testing.T) {
	codex := planUsage{plan: chatgptPlan, part: "codex", price: 0.0421, priced: true}
	claude := planUsage{plan: claudePlan, part: "claude-code", price: 0.03, priced: true}
	for _, c := range []struct {
		chatgpt bool
		plans   []planUsage
		want    string
	}{
		{false, nil, ""},
		{true, nil, "billed to the ChatGPT plan"},
		{false, []planUsage{codex}, "billed to the ChatGPT plan (API price about $0.0421)"},
		{true, []planUsage{codex}, "billed to the ChatGPT plan (API price about $0.0421 for codex)"},
		{false, []planUsage{claude, codex}, "billed to the ChatGPT plan (API price about $0.0421) and the Claude " +
			"subscription (API price about $0.0300)"},
		{false, []planUsage{{plan: claudePlan, part: "claude-code"}}, "billed to the Claude subscription"},
	} {
		assert.Equal(t, c.want, billingNote(c.chatgpt, c.plans))
	}

	summary := func(res *Result) string {
		var got []Progress
		p := newProgress(Request{Progress: func(m Progress) { got = append(got, m) }}, nil, nil)
		p.summary(res)
		require.Len(t, got, 1)
		return got[0].Message
	}
	assert.Contains(t, summary(&Result{Usage: Usage{Cost: 0.01}, costNote: "billed to the ChatGPT plan"}),
		"; estimated cost $0.0100, and usage billed to the ChatGPT plan")
	assert.Contains(t, summary(&Result{costNote: "billed to the ChatGPT plan"}), "; no per-token cost: billed to the "+
		"ChatGPT plan")
	assert.Contains(t, summary(&Result{Usage: Usage{Cost: 0.01}}), "; estimated cost $0.0100")
	assert.Contains(t, summary(&Result{}), "; no cost estimate (no known pricing for the models)")
}
