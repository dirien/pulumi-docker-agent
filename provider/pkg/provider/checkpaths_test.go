// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/blang/semver"
	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/integration"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingLogger captures what the provider reports to the user.
type recordingLogger struct {
	mu      sync.Mutex
	entries []logEntry
}

type logEntry struct {
	level, msg string
}

func (r *recordingLogger) add(level, msg string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = append(r.entries, logEntry{level, msg})
}

func (r *recordingLogger) Debug(msg string)      { r.add("debug", msg) }
func (r *recordingLogger) Info(msg string)       { r.add("info", msg) }
func (r *recordingLogger) InfoStatus(msg string) { r.add("status", msg) }
func (r *recordingLogger) Warning(msg string)    { r.add("warning", msg) }

// messages returns the messages of a level, or of every level for "".
func (r *recordingLogger) messages(level string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, e := range r.entries {
		if level == "" || e.level == level {
			out = append(out, e.msg)
		}
	}
	return out
}

func (r *recordingLogger) all() string {
	var b strings.Builder
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.entries {
		b.WriteString(e.level + ": " + e.msg + "\n")
	}
	return b.String()
}

func (r *recordingLogger) reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = nil
}

// loggingServer is server with a recording logger.
func loggingServer(t *testing.T) (integration.Server, *recordingLogger) {
	t.Helper()
	rec := &recordingLogger{}
	s, err := integration.NewServer(context.WithValue(t.Context(), loggerKey{}, rec), Name, semver.MustParse("0.1.0"),
		integration.WithProvider(Provider()))
	require.NoError(t, err)
	return s, rec
}

// TestAgentConfigCheckFailurePaths checks that docker-agent's validation errors fail the
// input that produced them, not the resource as a whole.
func TestAgentConfigCheckFailurePaths(t *testing.T) {
	t.Parallel()
	resp := check(t, server(t), agentConfigToken, "paths", map[string]any{
		"agents": []any{map[string]any{"name": "root", "model": "openai/gpt-5-mini", "subAgents": []any{"missing"}}},
	})
	require.Len(t, resp.Failures, 1, failureText(resp.Failures))
	assert.Equal(t, "agents[0].subAgents", resp.Failures[0].Property)
	assert.Equal(t, "invalid docker-agent configuration: agent 'root' references non-existent sub-agent 'missing'",
		resp.Failures[0].Reason)
}

// TestAgentConfigReportsIndependentProblems is the regression test for G4: docker-agent stops
// at the first problem, Check reports every independent one on its input.
func TestAgentConfigReportsIndependentProblems(t *testing.T) {
	t.Parallel()
	resp := check(t, server(t), agentConfigToken, "three", map[string]any{
		"models": map[string]any{"claude": map[string]any{
			"provider": "anthropic", "model": "claude-sonnet-4-5", "compactionThreshold": 7,
		}},
		"agents": []any{map[string]any{
			"name": "root", "model": "claude", "subAgents": []any{"missing"},
			"toolsets": []any{
				map[string]any{"type": "think"},
				map[string]any{"type": "fetch"},
				map[string]any{"type": "shell", "ref": "foo"},
			},
		}},
	})
	require.Len(t, resp.Failures, 3, failureText(resp.Failures))
	// In the order of the rendered document, where agents come first.
	assert.Equal(t, []string{"agents[0].subAgents", "agents[0].toolsets[2].ref", `models["claude"].compactionThreshold`},
		[]string{resp.Failures[0].Property, resp.Failures[1].Property, resp.Failures[2].Property}, failureText(resp.Failures))
	assert.Contains(t, resp.Failures[0].Reason, "agent 'root' references non-existent sub-agent 'missing'")
	assert.Contains(t, resp.Failures[1].Reason, "ref can only be used with type 'mcp' or 'rag'")
	assert.Contains(t, resp.Failures[2].Reason, "compaction_threshold must be greater than 0 and at most 1, got 7")
}

// TestAgentConfigEnumAndConfigProblems checks that an enum failure does not hide the other
// problems of the configuration, and that docker-agent's own error for the enum value is not
// repeated.
func TestAgentConfigEnumAndConfigProblems(t *testing.T) {
	t.Parallel()
	resp := check(t, server(t), agentConfigToken, "mixed", map[string]any{
		"agents": []any{map[string]any{
			"name": "root", "model": "openai/gpt-5-mini", "safety": "yolo", "handoffs": []any{"nobody"},
		}},
	})
	assert.Equal(t, []string{"agents[0].handoffs", "agents[0].safety"}, failureProperties(resp.Failures),
		failureText(resp.Failures))
	assert.Contains(t, failureText(resp.Failures), `"yolo" is not a valid value for the enum "SafetyMode"`)
	assert.NotContains(t, failureText(resp.Failures), "invalid safety mode")

	// skipValidation skips both.
	resp = check(t, server(t), agentConfigToken, "mixed", map[string]any{
		"skipValidation": true,
		"agents": []any{map[string]any{
			"name": "root", "model": "openai/gpt-5-mini", "safety": "yolo", "handoffs": []any{"nobody"},
		}},
	})
	assert.Empty(t, resp.Failures, failureText(resp.Failures))
}

// TestAgentConfigWarnsAboutLiteralCredentials checks the warning for credentials written into
// `headers` and `env` instead of `${env.NAME}` placeholders, and that it never quotes them.
func TestAgentConfigWarnsAboutLiteralCredentials(t *testing.T) {
	t.Parallel()
	s, rec := loggingServer(t)
	const token = "ghp_0123456789abcdefghijklmnop"
	inputs := pm(map[string]any{
		"agents": []any{map[string]any{
			"name": "root", "model": "openai/gpt-5-mini",
			"toolsets": []any{
				map[string]any{"type": "mcp", "remote": map[string]any{
					"url": "https://example.com/mcp",
					"headers": map[string]any{
						"Authorization": "Bearer " + token,          // literal: warned
						"X-Api-Key":     "${env.EXAMPLE_API_KEY}",   // placeholder
						"X-Request-Id":  "pulumi-docker-agent-test", // not a credential
					},
				}},
				map[string]any{"type": "mcp", "command": "server", "env": map[string]any{
					"GITHUB_TOKEN": token,   // literal: warned
					"DEBUG":        "true",  // not a credential
					"API_KEY":      "$KEY",  // placeholder
					"PASSWORD":     "short", // too short to be a credential
					// A Pulumi secret is not reported.
					"SECRET_TOKEN": resource.MakeSecret(resource.NewProperty(token)),
				}},
			},
		}},
	})
	resp, err := s.Check(p.CheckRequest{Urn: urn(agentConfigToken, "creds"), Inputs: inputs})
	require.NoError(t, err)
	require.Empty(t, resp.Failures, failureText(resp.Failures))
	warnings := rec.messages("warning")
	require.Len(t, warnings, 2, rec.all())
	assert.Contains(t, warnings[0], `agents[0].toolsets[0].remote.headers["Authorization"] looks like a literal credential`)
	assert.Contains(t, warnings[1], `agents[0].toolsets[1].env["GITHUB_TOKEN"] looks like a literal credential`)
	assert.NotContains(t, rec.all(), token)
}

func TestLiteralCredential(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		key, value string
		want       bool
	}{
		{"Authorization", "Bearer abcdefgh12345", true},
		{"authorization", "${env.TOKEN}", false},
		{"Authorization", "Bearer ${env.TOKEN}", false},
		{"Authorization", "Bearer ${TOKEN}", false},
		{"X-API-KEY", "0123456789abcdef", true},
		{"OPENAI_API_KEY", "op://vault/openai/key", false},
		{"OPENAI_API_KEY", "", false},
		{"LOG_LEVEL", "debug-verbose-mode", false},
		{"ANYTHING", "sk-proj-0123456789abcdef", true},
		{"ANYTHING", "Basic dXNlcjpwYXNzd29yZA==", true},
		{"MY_SECRET", "1", false},
	} {
		assert.Equal(t, tc.want, literalCredential(tc.key, tc.value), "%s=%s", tc.key, tc.value)
	}
}
