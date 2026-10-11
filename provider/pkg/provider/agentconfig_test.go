// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAgentConfigCheckFailureProperty is the regression test for render failures that were
// always attributed to `agents`, and for a top-level `extra.agents` that silently replaced the
// typed agents (while `agentNames` kept the typed names).
func TestAgentConfigCheckFailureProperty(t *testing.T) {
	t.Parallel()
	root := map[string]any{"name": "root", "model": "m", "instruction": "Help."}
	for name, tc := range map[string]struct {
		inputs   map[string]any
		property string
		reason   string
	}{
		"model variants": {
			inputs: map[string]any{
				"agents": []any{root},
				"models": map[string]any{"m": map[string]any{
					"provider": "openai", "model": "gpt-5", "thinkingBudget": "low", "thinkingBudgetTokens": 1024,
				}},
			},
			property: "models",
			reason:   "`thinkingBudget` and `thinkingBudgetTokens` are mutually exclusive",
		},
		"typed key in extra": {
			inputs: map[string]any{
				"agents": []any{root},
				"models": map[string]any{"m": map[string]any{"provider": "openai", "model": "gpt-5"}},
				"extra":  map[string]any{"agents": map[string]any{"other": map[string]any{"model": "openai/gpt-5"}}},
			},
			property: "extra",
			reason:   "\"agents\" is a typed input; set `agents` instead of extra.agents",
		},
		"agent toolset": {
			inputs: map[string]any{
				"agents": []any{map[string]any{"name": "root", "model": "openai/gpt-5", "toolsets": []any{
					map[string]any{"type": "shell", "defer": true, "deferTools": []any{"x"}},
				}}},
			},
			property: "agents",
			reason:   "`defer` and `deferTools` are mutually exclusive",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			resp := check(t, server(t), agentConfigToken, "failures", tc.inputs)
			require.Len(t, resp.Failures, 1, failureText(resp.Failures))
			assert.Equal(t, tc.property, resp.Failures[0].Property)
			assert.Contains(t, resp.Failures[0].Reason, tc.reason)
		})
	}
}
