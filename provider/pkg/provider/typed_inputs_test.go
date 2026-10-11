// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"encoding/json"
	"testing"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/integration"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// typedInputs uses every kind of typed input the generated schema has: nested objects,
// maps of objects, union variants (thinkingBudgetTokens, deferTools), combined fields
// (skills and inlineSkills, commands and simpleCommands), union map values (top-level
// command and skill groups), keyed hooks and free-form values.
func typedInputs() map[string]any {
	return map[string]any{
		"runtime": map[string]any{
			"sandbox": true, "safety": "restricted", "networkAllowlist": []any{"api.github.com", "registry.npmjs.org:443"},
		},
		"budget":  map[string]any{"maxCost": 2.5, "maxTime": "30m"},
		"budgets": map[string]any{"tight": map[string]any{"maxTokens": 100000}},
		"models": map[string]any{
			"thinker": map[string]any{
				"provider": "anthropic", "model": "claude-sonnet-4-5", "thinkingBudgetTokens": 4096, "taskBudget": 128000,
				"cost": map[string]any{"input": 3, "output": 15}, "providerOpts": map[string]any{"interleaved_thinking": true},
				"routing": []any{map[string]any{"model": "openai/gpt-5-mini", "examples": []any{"say hi"}}},
			},
		},
		"providers": map[string]any{
			"corp": map[string]any{"apiType": "openai_chatcompletions", "baseUrl": "https://llm.example.com/v1", "tokenKey": "CORP_KEY"},
		},
		"mcps": map[string]any{
			"github": map[string]any{
				"remote": map[string]any{
					"url": "https://api.githubcopilot.com/mcp/", "transportType": "streamable",
					"oauth": map[string]any{"clientId": "abc", "scopes": []any{"repo"}, "callbackPort": 8765},
				},
				"deferTools": []any{"search_code"},
				"lifecycle":  map[string]any{"profile": "resilient", "backoff": map[string]any{"initial": "1s", "multiplier": 2}},
			},
		},
		"commands": map[string]any{
			"git": map[string]any{
				"simpleCommands": map[string]any{"status": "Show the git status"},
				"commands":       map[string]any{"review": map[string]any{"description": "Review", "instruction": "Review the diff"}},
			},
		},
		"skills": map[string]any{
			"writing": map[string]any{
				"inlineSkills": []any{map[string]any{"name": "tone", "description": "Tone", "instructions": "Be brief."}},
			},
		},
		"flavors": map[string]any{"fast": map[string]any{"models": map[string]any{"thinker": map[string]any{"model": "claude-haiku-4-5"}}}},
		"agents": []any{map[string]any{
			"name":           "root",
			"model":          "thinker",
			"instruction":    "Help.",
			"safety":         "balanced",
			"skills":         []any{"local"},
			"inlineSkills":   []any{map[string]any{"name": "summarize", "description": "Summaries", "instructions": "Summarize."}},
			"useSkills":      []any{"writing"},
			"simpleCommands": map[string]any{"fix": "Fix the bug"},
			"commands":       map[string]any{"docs": map[string]any{"url": "https://docs.docker.com/ai/docker-agent/"}},
			"useCommands":    []any{"git"},
			"budgets":        []any{"tight"},
			"toolsets": []any{
				map[string]any{"type": "mcp", "ref": "github"},
				map[string]any{"type": "shell", "defer": true},
				map[string]any{"type": "filesystem", "postEdit": []any{map[string]any{"path": "*.go", "cmd": "gofmt -w ${file}"}}},
			},
			"hooks": map[string]any{
				"sessionStart": []any{map[string]any{"type": "builtin", "command": "add_environment_info"}},
				"preToolUse": []any{map[string]any{
					"matcher": "shell", "hooks": []any{map[string]any{"type": "command", "command": "./check.sh", "timeout": 10}},
				}},
			},
			"structuredOutput": map[string]any{
				"name": "answer", "strict": true,
				"schema": map[string]any{"type": "object", "properties": map[string]any{"text": map[string]any{"type": "string"}}},
			},
		}},
	}
}

func TestAgentConfigTypedInputs(t *testing.T) {
	t.Parallel()
	s := server(t)
	resp := check(t, s, agentConfigToken, "typed", typedInputs())
	require.Empty(t, resp.Failures, failureText(resp.Failures))

	integration.LifeCycleTest{
		Resource: agentConfigToken,
		Create: integration.Operation{
			Inputs: pm(typedInputs()),
			Hook: func(_, out property.Map) {
				y := out.Get("yaml").AsString()
				var doc map[string]any
				require.NoError(t, yaml.Unmarshal([]byte(y), &doc), y)
				assert.Equal(t, map[string]any{
					"sandbox": true, "safety": "restricted", "network_allowlist": []any{"api.github.com", "registry.npmjs.org:443"},
				}, doc["runtime"])
				root := doc["agents"].(map[string]any)["root"].(map[string]any)
				assert.Equal(t, []any{"local", map[string]any{"name": "summarize", "description": "Summaries",
					"instructions": "Summarize."}}, root["skills"], "skills and inlineSkills share the skills list")
				assert.Equal(t, map[string]any{"docs": map[string]any{"url": "https://docs.docker.com/ai/docker-agent/"},
					"fix": "Fix the bug"}, root["commands"], "commands and simpleCommands share the commands map")
				thinker := doc["models"].(map[string]any)["thinker"].(map[string]any)
				assert.Equal(t, 4096, thinker["thinking_budget"])
				assert.Equal(t, 128000, thinker["task_budget"])
				gh := doc["mcps"].(map[string]any)["github"].(map[string]any)
				assert.Equal(t, []any{"search_code"}, gh["defer"])
				assert.Equal(t, "abc", gh["remote"].(map[string]any)["oauth"].(map[string]any)["clientId"])
				assert.Equal(t, map[string]any{"review": map[string]any{"description": "Review", "instruction": "Review the diff"},
					"status": "Show the git status"}, doc["commands"].(map[string]any)["git"])
				assert.Contains(t, y, "pre_tool_use:\n")
				assert.Contains(t, y, "post_edit:\n")
				assert.Contains(t, y, "structured_output:\n")
				assert.NotContains(t, y, "extra")
			},
		},
	}.Run(t, s)
}

func TestAgentConfigVariantsAreExclusive(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		set  func(root map[string]any)
		want string
	}{
		"defer": {
			set: func(root map[string]any) {
				root["toolsets"] = []any{map[string]any{"type": "shell", "defer": true, "deferTools": []any{"run"}}}
			},
			want: "`defer` and `deferTools` are mutually exclusive",
		},
		"commands": {
			set:  func(root map[string]any) { root["simpleCommands"] = map[string]any{"docs": "Open the docs"} },
			want: "\"docs\" is set in more than one of `commands`, `simpleCommands`",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			inputs := typedInputs()
			tc.set(inputs["agents"].([]any)[0].(map[string]any))
			resp := check(t, server(t), agentConfigToken, "variants", inputs)
			assert.Contains(t, failureText(resp.Failures), tc.want)
		})
	}
}

// TestSchemaDocumentsEveryConfigInput checks that every type and property of the generated
// configuration types has a description: they become the Registry API docs.
func TestSchemaDocumentsEveryConfigInput(t *testing.T) {
	t.Parallel()
	resp, err := server(t).GetSchema(p.GetSchemaRequest{})
	require.NoError(t, err)
	var schema struct {
		Types map[string]struct {
			Description string                    `json:"description"`
			Properties  map[string]map[string]any `json:"properties"`
			Enum        []any                     `json:"enum"`
		} `json:"types"`
		Resources map[string]struct {
			InputProperties map[string]map[string]any `json:"inputProperties"`
		} `json:"resources"`
	}
	require.NoError(t, json.Unmarshal([]byte(resp.Schema), &schema))
	require.Greater(t, len(schema.Types), 40)
	for token, typ := range schema.Types {
		if typ.Enum != nil {
			continue
		}
		assert.NotEmpty(t, typ.Description, "%s has no description", token)
		for name, prop := range typ.Properties {
			assert.NotEmpty(t, prop["description"], "%s.%s has no description", token, name)
		}
	}
	for name, prop := range schema.Resources[agentConfigToken].InputProperties {
		assert.NotEmpty(t, prop["description"], "AgentConfig.%s has no description", name)
	}
}
