// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"strings"
	"testing"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dirien/pulumi-docker-agent/provider/pkg/agentrun/fakeopenai"
)

// loggingAgent thinks once, quoting the API key, and answers with it.
const loggingAgent = `    toolsets:
      - type: think
`

func loggingModel() *fakeopenai.Server {
	return fakeopenai.New(func(r fakeopenai.Request) fakeopenai.Reply {
		if len(r.ToolResults()) == 0 {
			return fakeopenai.Reply{ToolCalls: []fakeopenai.ToolCall{{Name: "think",
				Arguments: `{"thought":"my key is ` + runKey + `"}`}}}
		}
		return fakeopenai.Reply{Content: "Low risk. My key is " + runKey}
	})
}

// runLogged creates an AgentRun and returns what it reported.
func runLogged(t *testing.T, extra map[string]any, modify func(property.Map) property.Map) *recordingLogger {
	t.Helper()
	s, rec := loggingServer(t)
	srv := loggingModel()
	defer srv.Close()
	checked := runCheck(t, s, runInputs(srv, loggingAgent, extra))
	require.Empty(t, checked.Failures, runFailures(checked.Failures))
	inputs := checked.Inputs
	if modify != nil {
		inputs = modify(inputs)
	}
	rec.reset()
	_, err := s.Create(p.CreateRequest{Urn: runURN("run"), Properties: inputs})
	require.NoError(t, err)
	assert.NotContains(t, rec.all(), runKey, "nothing reported quotes the env value")
	return rec
}

// TestAgentRunProgress checks the default `logging: status`: status messages while the agent
// runs and one summary at the end.
func TestAgentRunProgress(t *testing.T) {
	rec := runLogged(t, nil, nil)
	status := strings.Join(rec.messages("status"), "\n")
	assert.Contains(t, status, "root started")
	assert.Contains(t, status, "root: calling think")
	assert.Contains(t, status, "root: think approved")
	info := rec.messages("info")
	require.Len(t, info, 2, rec.all())
	// The credential line: the model and where its credential comes from, never the value.
	assert.Equal(t, "models: fake/fake-model (FAKE_API_KEY from env)", info[0])
	assert.Contains(t, info[1], "agent run finished in ")
	assert.Contains(t, info[1], "root answered after 1 tool calls; 20 input and 10 output tokens; no cost estimate")
	// The summary is the last message (after the warning that the answer quotes a secret).
	entries := rec.messages("")
	assert.Equal(t, info[1], entries[len(entries)-1])
}

func TestAgentRunLoggingLevels(t *testing.T) {
	t.Run("none", func(t *testing.T) {
		rec := runLogged(t, map[string]any{"logging": "none"}, nil)
		assert.Empty(t, rec.messages("status"))
		assert.Empty(t, rec.messages("info"))
	})
	t.Run("tools", func(t *testing.T) {
		rec := runLogged(t, map[string]any{"logging": "tools"}, nil)
		info := strings.Join(rec.messages("info"), "\n")
		assert.Contains(t, info, `root called think {"thought":"my key is [secret]"}: approved`)
		assert.NotContains(t, info, "Low risk")
	})
	t.Run("full", func(t *testing.T) {
		rec := runLogged(t, map[string]any{"logging": "full"}, nil)
		info := strings.Join(rec.messages("info"), "\n")
		assert.Contains(t, info, `root called think {"thought":"my key is [secret]"}: approved`)
		assert.Contains(t, info, "root: Low risk. My key is [secret]")
	})
	t.Run("full with a secret answer", func(t *testing.T) {
		rec := runLogged(t, map[string]any{"logging": "full", "outputSecret": true}, nil)
		info := rec.messages("info")
		require.Len(t, info, 2, "only the credential line and the summary: %s", rec.all())
		assert.Equal(t, "models: fake/fake-model (FAKE_API_KEY from env)", info[0])
		assert.Contains(t, info[1], "root answered")
	})
	t.Run("full with a secret yaml", func(t *testing.T) {
		rec := runLogged(t, map[string]any{"logging": "full"}, func(in property.Map) property.Map {
			return in.Set("yaml", in.Get("yaml").WithSecret(true))
		})
		// docker-agent's own debug records are not progress.
		all := strings.Join(append(rec.messages("status"), rec.messages("info")...), "\n")
		assert.Contains(t, all, "an agent: calling a tool")
		assert.Contains(t, all, "the agent answered after 1 tool calls")
		assert.NotContains(t, all, "root")
		assert.NotContains(t, all, "think")
	})
}

// TestAgentRunWarningsHideNames checks that the warnings of a run whose `yaml` is secret name
// neither agents nor tools, here those about a rejected tool call: one that needs approval
// under toolApproval deny, and one the configuration's permissions deny.
func TestAgentRunWarningsHideNames(t *testing.T) {
	for name, permissions := range map[string]string{
		"needs approval": "",
		"denied":         "permissions:\n  deny: [shell]\n",
	} {
		t.Run(name, func(t *testing.T) {
			s, rec := loggingServer(t)
			srv := fakeopenai.New(func(r fakeopenai.Request) fakeopenai.Reply {
				if len(r.ToolResults()) == 0 {
					return fakeopenai.Reply{ToolCalls: []fakeopenai.ToolCall{{Name: "shell", Arguments: `{"cmd":"ls"}`}}}
				}
				return fakeopenai.Reply{Content: "Done."}
			})
			defer srv.Close()
			doc := strings.Replace(runYAML(srv, "    toolsets:\n      - type: shell\n"), "  root:\n", "  release-captain:\n", 1) +
				permissions
			for _, logging := range []string{"status", "full"} {
				checked := runCheck(t, s, runInputs(srv, "", map[string]any{"yaml": doc, "logging": logging}))
				require.Empty(t, checked.Failures, runFailures(checked.Failures))
				rec.reset()
				_, err := s.Create(p.CreateRequest{Urn: runURN("run"),
					Properties: checked.Inputs.Set("yaml", checked.Inputs.Get("yaml").WithSecret(true))})
				require.NoError(t, err)
				warnings := strings.Join(rec.messages("warning"), "\n")
				assert.Contains(t, warnings, "an agent: a tool call was rejected", "%s: %s", logging, rec.all())
				for _, hidden := range []string{"release-captain", "shell", "fake-model"} {
					assert.NotContains(t, rec.all(), hidden, "%s: a name of the secret configuration", logging)
				}

				// With a public yaml, the warning names them.
				rec.reset()
				_, err = s.Create(p.CreateRequest{Urn: runURN("run"), Properties: checked.Inputs})
				require.NoError(t, err)
				assert.Contains(t, strings.Join(rec.messages("warning"), "\n"),
					`agent "release-captain": tool call "shell" was rejected`)
			}
		})
	}
}
