// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package validate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUpstreamExamples validates example configurations shipped with docker-agent.
func TestUpstreamExamples(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob(filepath.Join("testdata", "upstream", "*.yaml"))
	require.NoError(t, err)
	require.NotEmpty(t, files)
	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			t.Parallel()
			b, err := os.ReadFile(f)
			require.NoError(t, err)
			res := Validate(t.Context(), filepath.Base(f), b)
			assert.True(t, res.Valid, "errors: %v", res.Errors)
			assert.Empty(t, res.Errors)
			assert.NotEmpty(t, res.AgentNames)
			assert.NotEmpty(t, res.DefaultAgent)
		})
	}
}

func TestValidConfig(t *testing.T) {
	t.Parallel()
	doc := `version: "16"
metadata:
  author: pulumi
agents:
  helper:
    model: fast
    instruction: Help.
  root:
    model: anthropic/claude-sonnet-4-5
    description: Root agent
    instruction: You are helpful.
    sub_agents: [helper]
    toolsets:
      - type: filesystem
      - type: mcp
        ref: docker:duckduckgo
models:
  fast:
    provider: openai
    model: gpt-5-mini
  custom:
    provider: openai
    model: gpt-5
    token_key: MY_KEY
`
	res := Validate(t.Context(), "agent.yaml", []byte(doc))
	require.True(t, res.Valid, "errors: %v", res.Errors)
	assert.Equal(t, []string{"helper", "root"}, res.AgentNames)
	assert.Equal(t, "root", res.DefaultAgent)
	// Only models used by agents count; the unused custom model's MY_KEY is not required.
	assert.Equal(t, []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY"}, res.RequiredEnvVars)
	require.NoError(t, res.Error())
}

func TestDefaultAgentIsFirstWithoutRoot(t *testing.T) {
	t.Parallel()
	res := Validate(t.Context(), "", []byte("agents:\n  zed:\n    model: openai/gpt-5-mini\n  amy:\n    model: openai/gpt-5-mini\n"))
	require.True(t, res.Valid, "errors: %v", res.Errors)
	assert.Equal(t, []string{"zed", "amy"}, res.AgentNames)
	assert.Equal(t, "zed", res.DefaultAgent)
}

func TestTokenKeyIsRequired(t *testing.T) {
	t.Parallel()
	res := Validate(t.Context(), "", []byte(`agents:
  root:
    model: corp
models:
  corp:
    provider: openai
    model: gpt-5
    token_key: CORP_OPENAI_KEY
`))
	require.True(t, res.Valid, "errors: %v", res.Errors)
	assert.Equal(t, []string{"CORP_OPENAI_KEY"}, res.RequiredEnvVars)
}

func TestInvalidConfigs(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		doc  string
		want []string // substrings of the error
	}{
		"yaml syntax": {
			doc:  "agents:\n  root:\n    model: [openai\n",
			want: []string{"looking for version"},
		},
		"unknown field": {
			doc:  "agents:\n  root:\n    model: openai/gpt-5-mini\n    instructions: typo\n",
			want: []string{`unknown field "instructions"`},
		},
		"missing sub-agent": {
			doc:  "agents:\n  root:\n    model: openai/gpt-5-mini\n    sub_agents: [nope]\n",
			want: []string{"non-existent sub-agent 'nope'"},
		},
		"no agents": {
			doc:  "version: \"16\"\nmodels: {}\n",
			want: []string{"at least one agent"},
		},
		"mcp without command": {
			doc:  "agents:\n  root:\n    model: openai/gpt-5-mini\n    toolsets:\n      - type: mcp\n",
			want: []string{"either command, remote or ref must be set"},
		},
		"unknown model": {
			doc:  "agents:\n  root:\n    model: notamodel\n",
			want: []string{"non-existent model 'notamodel'"},
		},
		"bad version": {
			doc:  "version: \"99\"\nagents:\n  root:\n    model: openai/gpt-5-mini\n",
			want: []string{"unsupported config version: 99"},
		},
		"unknown toolset type": {
			doc:  "agents:\n  root:\n    model: openai/gpt-5-mini\n    toolsets:\n      - type: nosuch\n",
			want: []string{`unknown toolset type "nosuch" at agents.root.toolsets[0]`, "valid types are a2a, api"},
		},
		"unknown provider": {
			doc:  "agents:\n  root:\n    model: totally-unknown/some-model\n",
			want: []string{`unknown model provider "totally-unknown"`},
		},
		"instruction_file": {
			doc:  "agents:\n  root:\n    model: openai/gpt-5-mini\n    instruction_file: prompt.md\n",
			want: []string{"instruction_file"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			res := Validate(t.Context(), "agent.yaml", []byte(tc.doc))
			require.False(t, res.Valid)
			require.NotEmpty(t, res.Errors)
			msg := strings.Join(res.Errors, "\n")
			for _, w := range tc.want {
				assert.Contains(t, msg, w)
			}
			assert.NotContains(t, msg, "\x1b[", "ANSI codes must be stripped")
			require.Error(t, res.Error())
		})
	}
}

func TestInvalidConfigStillReportsAgentNames(t *testing.T) {
	t.Parallel()
	res := Validate(t.Context(), "", []byte("agents:\n  root:\n    model: notamodel\n  other:\n    model: x\n"))
	require.False(t, res.Valid)
	assert.Equal(t, []string{"root", "other"}, res.AgentNames)
	assert.Equal(t, "root", res.DefaultAgent)
	assert.Empty(t, res.RequiredEnvVars)
}

func TestKnownProvidersAndAliases(t *testing.T) {
	t.Parallel()
	for _, model := range []string{
		"openai/gpt-5-mini", "anthropic/claude-sonnet-4-5", "google/gemini-2.5-flash", "dmr/ai/qwen3",
		"amazon-bedrock/anthropic.claude-sonnet-4", "mistral/mistral-large-latest", "openrouter/x/y",
		"ollama/llama3", "xai/grok-4", "github-copilot/gpt-5",
	} {
		res := Validate(t.Context(), "", []byte("agents:\n  root:\n    model: "+model+"\n"))
		assert.True(t, res.Valid, "%s: %v", model, res.Errors)
	}
}

func TestAllToolsetTypesAccepted(t *testing.T) {
	t.Parallel()
	// Types with mandatory fields get the minimum they need.
	extra := map[string]string{
		"a2a":          "url: https://a2a.example.com",
		"api":          "api_config: {name: get, endpoint: 'https://api.example.com', method: GET, instruction: x}",
		"lsp":          "command: gopls",
		"mcp":          "ref: docker:duckduckgo",
		"model_picker": "models: [openai/gpt-5-mini]",
		"open_url":     "url: https://example.com",
		"openapi":      "url: https://example.com/openapi.json",
		"rag":          "ref: docs",
		"webhook":      "webhook_config: {provider: generic, url: 'https://hooks.example.com'}",
	}
	var b strings.Builder
	b.WriteString("rag:\n  docs:\n    docs: [./docs]\n    strategies:\n      - type: chunked-embeddings\n        embedding_model: openai/text-embedding-3-small\n")
	b.WriteString("agents:\n  root:\n    model: openai/gpt-5-mini\n    toolsets:\n")
	for _, typ := range ToolsetTypes {
		b.WriteString("      - type: " + typ + "\n")
		if e, ok := extra[typ]; ok {
			b.WriteString("        " + e + "\n")
		}
	}
	res := Validate(t.Context(), "", []byte(b.String()))
	assert.True(t, res.Valid, "errors: %v\n%s", res.Errors, b.String())
}

func TestClean(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "parsing config file\n[1:1] unknown field", Clean("parsing config file  \n\x1b[31m[1:1] unknown field\x1b[0m\n"))
}

func TestAgentNames(t *testing.T) {
	t.Parallel()
	assert.Equal(t, []string{"b", "a"}, AgentNames([]byte("agents:\n  b: {}\n  a: {}\n")))
	assert.Nil(t, AgentNames([]byte(":::")))
	assert.Nil(t, AgentNames([]byte("agents: [1, 2]")))
}

// TestErrorsNeverEchoYAML is the regression test for secrets leaking through docker-agent's
// YAML source excerpts: the error keeps "[line:col] message" but no source lines.
func TestErrorsNeverEchoYAML(t *testing.T) {
	t.Parallel()
	docs := map[string]string{
		"unknown field next to a secret header": "version: \"16\"\nagents:\n  root:\n    model: openai/gpt-5-mini\n" +
			"    toolsets:\n      - type: fetch\n        headers:\n          Authorization: Bearer SUPERSECRET-TOKEN-123\n" +
			"        tolls: [x]\n",
		"type error next to a secret": "agents:\n  root:\n    model: openai/gpt-5-mini\n" +
			"    instruction: SUPERSECRET-TOKEN-123\n    max_iterations: [1, 2]\n",
		"syntax error next to a secret": "agents:\n  root:\n    model: openai/gpt-5-mini\n" +
			"    description: SUPERSECRET-TOKEN-123\n    instruction: [unclosed\n",
	}
	for name, doc := range docs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			res := Validate(t.Context(), "agent.yaml", []byte(doc))
			require.False(t, res.Valid)
			msg := strings.Join(res.Errors, "\n")
			assert.NotContains(t, msg, "SUPERSECRET", "error must not echo YAML content:\n%s", msg)
			assert.NotRegexp(t, `(?m)^\s*>?\s*\d+\s*\|`, msg, "no source excerpt lines")
			assert.Regexp(t, `\[\d+:\d+\]`, msg, "line:col is kept")
		})
	}
	res := Validate(t.Context(), "agent.yaml", []byte(docs["unknown field next to a secret header"]))
	assert.Contains(t, strings.Join(res.Errors, "\n"), `unknown field "tolls"`)
}

func TestCleanStripsExcerpt(t *testing.T) {
	t.Parallel()
	in := "parsing config file\n[5:3] unknown field \"tolls\"\n   2 | toolsets:\n   3 | - headers:\n" +
		"   4 |     Authorization: Bearer X\n>  5 |   tolls:\n         ^\n"
	assert.Equal(t, "parsing config file\n[5:3] unknown field \"tolls\"", Clean(in))
}

func TestRedact(t *testing.T) {
	t.Parallel()
	doc := "agents:\n  root:\n    instruction: the launch code is 0000-1111\n"
	msg := "agent root: instruction 'the launch code is 0000-1111' token Bearer abcdef"
	got := Redact(msg, []string{doc, "Bearer abcdef", "ab"})
	assert.NotContains(t, got, "0000-1111")
	assert.NotContains(t, got, "Bearer abcdef")
	assert.Contains(t, got, "agent root", "short values are not redacted")
	assert.Equal(t, "nothing", Redact("nothing", nil))
}

// TestWarningsAreCollected checks that docker-agent's slog warnings are returned instead of
// being written to the process's stderr.
func TestWarningsAreCollected(t *testing.T) {
	t.Parallel()
	res := Validate(t.Context(), "", []byte(`agents:
  root:
    model: big
models:
  big:
    provider: openai
    model: gpt-5
    max_tokens: 500000
`))
	require.True(t, res.Valid, "errors: %v", res.Errors)
	require.NotEmpty(t, res.Warnings)
	assert.Contains(t, strings.Join(res.Warnings, "\n"), "max_tokens")

	clean := Validate(t.Context(), "", []byte("agents:\n  root:\n    model: openai/gpt-5-mini\n"))
	assert.Empty(t, clean.Warnings)
}
