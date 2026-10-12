// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package render

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/dirien/pulumi-docker-agent/provider/pkg/validate"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata/")

func ptr[T any](v T) *T { return &v }

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden.yaml")
	if *update {
		require.NoError(t, os.MkdirAll("testdata", 0o755))
		require.NoError(t, os.WriteFile(path, []byte(got), 0o600))
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err, "golden file missing; run go test ./pkg/render -update")
	assert.Equal(t, string(want), got)
}

func TestRenderMinimal(t *testing.T) {
	t.Parallel()
	got, err := Render(Document{Agents: []Agent{{
		Name:        "root",
		Model:       ptr("openai/gpt-5-mini"),
		Instruction: ptr("You are a helpful assistant."),
	}}})
	require.NoError(t, err)
	golden(t, "minimal", got)
}

// TestRenderFull covers every typed field, snake_case keys, sorted map keys, preserved
// agent order and multi-line strings.
func TestRenderFull(t *testing.T) {
	t.Parallel()
	doc := Document{
		Version: ptr("16"),
		Metadata: &Metadata{
			Author:      ptr("Jane Doe <jane@example.com>"),
			License:     ptr("Apache-2.0"),
			Description: ptr("A research team"),
			Readme:      ptr("# Team\n\nResearches things.\n"),
			Version:     ptr("1.2.3"),
			Tags:        []string{"research", "demo"},
		},
		// Deliberately not in alphabetical order: the order must be preserved.
		Agents: []Agent{
			{
				Name:               "zeta",
				Model:              ptr("fast"),
				Description:        ptr("Coordinates the team"),
				WelcomeMessage:     ptr("Hi!"),
				Instruction:        ptr("Line one.\nLine two."),
				SubAgents:          []string{"alpha"},
				Handoffs:           []string{"alpha"},
				AddDate:            ptr(true),
				AddEnvironmentInfo: ptr(false),
				MaxIterations:      ptr(20),
				NumHistoryItems:    ptr(30),
				Skills:             []string{"local"},
				SimpleCommands:     map[string]string{"fix": "Fix the bug"},
				Commands:           map[string]Command{"deploy": {Instruction: ptr("Deploy it")}},
				StructuredOutput: &StructuredOutput{
					Name:   "answer",
					Schema: map[string]any{"type": "object", "properties": map[string]any{"text": map[string]any{"type": "string"}}},
				},
				Hooks: &Hooks{
					SessionStart: []HookDefinition{{Type: "builtin", Command: ptr("add_date")}},
				},
				Fallback: &Fallback{Models: []string{"openai/gpt-5-mini"}, Retries: ptr(2), Cooldown: ptr("1m")},
				Toolsets: []Toolset{
					{Type: "think"},
					{
						Type:    "mcp",
						Command: ptr("npx"),
						Args:    []string{"-y", "@modelcontextprotocol/server-everything"},
						Env:     map[string]string{"Z_VAR": "z", "A_VAR": "${env.A_VAR}"},
						Tools:   []string{"echo"},
						Config:  map[string]any{"b": 2.0, "a": []any{"x", 1.5, true}},
					},
					{
						Type:   "mcp",
						Remote: &Remote{URL: "https://mcp.example.com/sse", TransportType: ptr("sse"), Headers: map[string]string{"X-B": "b", "X-A": "a"}},
					},
					{Type: "fetch", Headers: map[string]string{"User-Agent": "agent"}},
					{Type: "memory", Path: ptr("./memory.db")},
					{Type: "todo", Shared: ptr(true)},
					{Type: "openapi", URL: ptr("https://api.example.com/openapi.json"), Name: ptr("petstore"), Instruction: ptr("Use the pet store.")},
					{Type: "mcp", Ref: ptr("docker:duckduckgo")},
				},
			},
			{Name: "alpha", Model: ptr("anthropic/claude-sonnet-4-5"), Description: ptr("Does the work")},
		},
		Models: map[string]ModelDef{
			"fast": {
				Provider:          ptr("openai"),
				Model:             ptr("gpt-5-mini"),
				Description:       ptr("Fast model"),
				Temperature:       ptr(1.0),
				MaxTokens:         ptr(8192),
				TopP:              ptr(0.9),
				BaseURL:           ptr("https://api.openai.com/v1"),
				TokenKey:          ptr("MY_OPENAI_KEY"),
				ParallelToolCalls: ptr(true),
				ThinkingBudget:    ptr("low"),
				ProviderOpts:      map[string]any{"seed": 42.0},
			},
			"another": {Provider: ptr("dmr"), Model: ptr("ai/qwen3")},
		},
		Providers: map[string]ProviderDef{
			"corp": {APIType: ptr(ApiTypeOpenAIChatCompletions), BaseURL: ptr("https://llm.corp.example/v1"), TokenKey: ptr("CORP_KEY")},
		},
		Permissions: &Permissions{Allow: []string{"think"}, Ask: []string{"shell"}, Deny: []string{"mcp:danger:*"}},
	}
	got, err := Render(doc)
	require.NoError(t, err)
	golden(t, "full", got)

	// Agent order is preserved.
	assert.Less(t, strings.Index(got, "  zeta:"), strings.Index(got, "  alpha:"))
	// The output is valid YAML with snake_case keys.
	var parsed map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(got), &parsed))
	zeta := parsed["agents"].(map[string]any)["zeta"].(map[string]any)
	assert.Contains(t, zeta, "welcome_message")
	assert.Contains(t, zeta, "sub_agents")
	assert.Contains(t, zeta, "add_environment_info")
	assert.Equal(t, "Line one.\nLine two.", zeta["instruction"])
	fast := parsed["models"].(map[string]any)["fast"].(map[string]any)
	assert.Equal(t, 1.0, fast["temperature"]) // stays a float
	assert.Equal(t, 42, fast["provider_opts"].(map[string]any)["seed"])
}

func TestRenderExtraMerge(t *testing.T) {
	t.Parallel()
	got, err := Render(Document{
		Agents: []Agent{{
			Name:        "root",
			Model:       ptr("openai/gpt-5-mini"),
			Description: ptr("typed"),
			Extra: map[string]any{
				"description":                "overridden by extra",
				"max_consecutive_tool_calls": 10.0,
				"code_mode_tools":            true,
			},
			Toolsets: []Toolset{{Type: "filesystem", Extra: map[string]any{"ignore_vcs": false, "post_edit": []any{map[string]any{"path": "*.go", "cmd": "gofmt -w ${file}"}}}}},
		}},
		Models: map[string]ModelDef{"m": {Provider: ptr("openai"), Model: ptr("gpt-5"), Extra: map[string]any{"cost": map[string]any{"input": 1.25, "output": 10.0}}}},
		// Top-level extra is for sections newer than the pinned docker-agent.
		Extra: map[string]any{
			"future_section": map[string]any{"search": map[string]any{"ref": "docker:duckduckgo"}},
			"future_limit":   2.5,
		},
	})
	require.NoError(t, err)
	golden(t, "extra", got)
	assert.True(t, strings.HasPrefix(got, "version: \"16\"\n"), got)
	assert.Contains(t, got, "description: overridden by extra")
	assert.NotContains(t, got, "description: typed")
}

// TestRenderRejectsTypedTopLevelExtra is the regression test for `extra.agents`: it replaced the
// typed agents in the YAML while `agentNames` and `defaultAgent` still reported the typed ones.
// Top-level extra keys that are typed inputs are now an error, attributed to `extra`.
func TestRenderRejectsTypedTopLevelExtra(t *testing.T) {
	t.Parallel()
	agents := []Agent{{Name: "root", Model: ptr("openai/gpt-5-mini")}}
	for key, input := range map[string]string{"agents": "agents", "version": "version", "mcps": "mcps", "runtime": "runtime"} {
		_, err := Render(Document{Agents: agents, Extra: map[string]any{key: map[string]any{"other": map[string]any{}}}})
		require.ErrorContains(t, err, "is a typed input; set `"+input+"` instead of extra."+key)
		assert.Equal(t, "extra", ErrorProperty(err))
	}
	// A nil value is skipped by the merge, so it is not a conflict.
	_, err := Render(Document{Agents: agents, Extra: map[string]any{"agents": nil}})
	require.NoError(t, err)
}

// TestErrorProperty checks that rendering errors name the top-level input they are about, so
// Check failures point at the right property.
func TestErrorProperty(t *testing.T) {
	t.Parallel()
	agents := []Agent{{Name: "root", Model: ptr("m")}}
	_, err := Render(Document{Agents: agents, Models: map[string]ModelDef{
		"m": {Provider: ptr("openai"), Model: ptr("gpt-5"), ThinkingBudget: ptr("low"), ThinkingBudgetTokens: ptr(10)},
	}})
	require.ErrorContains(t, err, `models["m"]: `+"`thinkingBudget` and `thinkingBudgetTokens` are mutually exclusive")
	assert.Equal(t, "models", ErrorProperty(err))

	_, err = Render(Document{Agents: []Agent{{Name: "root", Toolsets: []Toolset{{}}}}})
	assert.Equal(t, "agents", ErrorProperty(err))
	_, err = Render(Document{})
	assert.Equal(t, "agents", ErrorProperty(err))
	_, err = Render(Document{Agents: []Agent{{Name: "a"}, {Name: "a"}}})
	assert.Equal(t, "agents", ErrorProperty(err))
	assert.Empty(t, ErrorProperty(errors.New("plain")))
}

func TestRenderDeterministic(t *testing.T) {
	t.Parallel()
	mk := func() Document {
		return Document{
			Agents: []Agent{{Name: "root", Model: ptr("m1"), Toolsets: []Toolset{{Type: "shell", Env: map[string]string{"B": "2", "A": "1", "C": "3"}}}}},
			Models: map[string]ModelDef{"m1": {Provider: ptr("openai"), Model: ptr("gpt-5")}, "m0": {Provider: ptr("anthropic"), Model: ptr("claude")}},
			Extra:  map[string]any{"z": 1.0, "a": map[string]any{"y": 1.0, "b": 2.0}},
		}
	}
	first, err := Render(mk())
	require.NoError(t, err)
	for range 50 {
		again, err := Render(mk())
		require.NoError(t, err)
		require.Equal(t, first, again)
	}
	assert.Equal(t, SHA256(first), SHA256(first))
	assert.Len(t, SHA256(first), 64)
}

func TestRenderQuotesAmbiguousStrings(t *testing.T) {
	t.Parallel()
	got, err := Render(Document{Agents: []Agent{{
		Name:        "root",
		Model:       ptr("openai/gpt-5-mini"),
		Description: ptr("yes"),
		Instruction: ptr("123"),
	}}})
	require.NoError(t, err)
	var parsed struct {
		Agents map[string]map[string]any `yaml:"agents"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(got), &parsed))
	assert.Equal(t, "yes", parsed.Agents["root"]["description"])
	assert.Equal(t, "123", parsed.Agents["root"]["instruction"])
}

func TestRenderErrors(t *testing.T) {
	t.Parallel()
	_, err := Render(Document{})
	require.ErrorContains(t, err, "at least one agent")

	_, err = Render(Document{Agents: []Agent{{Name: "a"}, {Name: "a"}}})
	require.ErrorContains(t, err, `duplicate agent name "a"`)

	_, err = Render(Document{Agents: []Agent{{Name: ""}}})
	require.ErrorContains(t, err, "name must not be empty")

	_, err = Render(Document{Agents: []Agent{{Name: "a", Toolsets: []Toolset{{}}}}})
	require.ErrorContains(t, err, `agents["a"].toolsets[0].type: must not be empty`)
}

func TestDefaultAgent(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "root", DefaultAgent([]string{"a", "root"}))
	assert.Equal(t, "a", DefaultAgent([]string{"a", "b"}))
	assert.Equal(t, "", DefaultAgent(nil))
}

// TestRenderWhitespaceOnlyMultilineStrings is the regression test for "\n": yaml.v3 renders
// it as an empty literal block ("|2+"), which docker-agent's YAML parser rejects.
func TestRenderWhitespaceOnlyMultilineStrings(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"\n", "\n\n", " \n ", "\t\n", "\r\n"} {
		got, err := Render(Document{Agents: []Agent{{
			Name:        "root",
			Model:       ptr("openai/gpt-5-mini"),
			Description: ptr(s),
			Instruction: ptr("Line one.\nLine two.\n"),
		}}})
		require.NoError(t, err)
		res := validate.Validate(t.Context(), "", []byte(got))
		require.True(t, res.Valid, "%q rendered as\n%s\nerrors: %v", s, got, res.Errors)

		var parsed struct {
			Agents map[string]map[string]string `yaml:"agents"`
		}
		require.NoError(t, yaml.Unmarshal([]byte(got), &parsed))
		assert.Equal(t, s, parsed.Agents["root"]["description"])
		assert.Contains(t, got, "instruction: |", "multi-line text still uses a literal block")
	}
}
