// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package validate

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// located is a problem as the tests expect it: its path (dotted) and a part of its message.
type located struct {
	path, msg string
}

func locatedProblems(t *testing.T, doc string) []located {
	t.Helper()
	res := Validate(t.Context(), "agent.yaml", []byte(doc))
	require.False(t, res.Valid)
	require.Len(t, res.Errors, len(res.Problems))
	out := make([]located, 0, len(res.Problems))
	for i, p := range res.Problems {
		assert.Equal(t, p.Message, res.Errors[i])
		assert.NotContains(t, p.Message, "parsing config file")
		out = append(out, located{strings.Join(p.Path, "."), p.Message})
	}
	return out
}

func assertProblems(t *testing.T, want []located, got []located) {
	t.Helper()
	require.Len(t, got, len(want), "%v", got)
	for i := range want {
		assert.Equal(t, want[i].path, got[i].path, "problem %d: %s", i, got[i].msg)
		assert.Contains(t, got[i].msg, want[i].msg, "problem %d", i)
	}
}

// TestProblemsAreLocated checks where each kind of docker-agent message is located: at the
// location it starts with (refined to the key it names), at the agent it is about, or at
// the smallest value that fails on its own.
func TestProblemsAreLocated(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		doc  string
		want []located
	}{
		"sub-agent reference": {
			doc:  "agents:\n  root:\n    model: openai/gpt-5\n    sub_agents: [missing]\n",
			want: []located{{"agents.root.sub_agents", "agent 'root' references non-existent sub-agent 'missing'"}},
		},
		"model reference": {
			doc:  "agents:\n  root:\n    model: nomodel\n",
			want: []located{{"agents.root.model", "references non-existent model 'nomodel'"}},
		},
		"location prefix": {
			doc:  "runtime:\n  safety: yolo\nagents:\n  root:\n    model: openai/gpt-5\n",
			want: []located{{"runtime.safety", `runtime.safety: invalid safety mode "yolo"`}},
		},
		"refined to the key the message starts with": {
			doc: "models:\n  claude:\n    provider: anthropic\n    model: c\n    compaction_threshold: 7\n" +
				"agents:\n  root:\n    model: claude\n",
			want: []located{{"models.claude.compaction_threshold", "compaction_threshold must be greater than 0"}},
		},
		"toolset found by unmarshalling": {
			doc:  "agents:\n  root:\n    model: openai/gpt-5\n    toolsets:\n      - type: think\n      - type: think\n      - type: shell\n        ref: foo\n",
			want: []located{{"agents.root.toolsets.2.ref", "ref can only be used with type 'mcp' or 'rag'"}},
		},
		"relative to an agent": {
			doc: "agents:\n  root:\n    model: openai/gpt-5\n    hooks:\n      pre_tool_use:\n" +
				"        - matcher: x\n          hooks:\n            - type: bogus\n",
			// Removing the hook leaves the matcher without hooks: that is not reported.
			want: []located{{"agents.root.hooks.pre_tool_use.0.hooks.0.type", "unsupported hook type 'bogus'"}},
		},
		"unknown key": {
			doc:  "agents:\n  root:\n    model: openai/gpt-5\n    tolls: [x]\n",
			want: []located{{"agents.root.tolls", `unknown field "tolls"`}},
		},
		"every unknown toolset type and inline model provider": {
			doc: "agents:\n  root:\n    model: zzz/gpt-5\n    toolsets:\n      - type: bogus\n      - type: think\n" +
				"      - type: bogus2\n",
			want: []located{
				{"agents.root.model", `unknown model provider "zzz"`},
				{"agents.root.toolsets.0", `unknown toolset type "bogus" at agents.root.toolsets[0]`},
				{"agents.root.toolsets.2", `unknown toolset type "bogus2" at agents.root.toolsets[2]`},
			},
		},
		"each reference of a list": {
			doc: "agents:\n  root:\n    model: openai/gpt-5\n    sub_agents: [missing, other]\n    budgets: [b]\n",
			want: []located{
				{"agents.root.sub_agents", "non-existent sub-agent 'missing'"},
				{"agents.root.sub_agents", "non-existent sub-agent 'other'"},
				{"agents.root.budgets", `unknown budget "b"`},
			},
		},
		"version": {
			doc:  "version: \"99\"\nagents:\n  root:\n    model: openai/gpt-5\n",
			want: []located{{"version", "unsupported config version: 99"}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertProblems(t, tc.want, locatedProblems(t, tc.doc))
		})
	}
}

// TestIndependentProblemsAreAllReported is the regression test for G4: docker-agent stops at
// the first problem, Validate reports every independent one, in document order.
func TestIndependentProblemsAreAllReported(t *testing.T) {
	t.Parallel()
	doc := `models:
  claude:
    provider: anthropic
    model: claude-sonnet-4-5
    compaction_threshold: 7
agents:
  root:
    model: claude
    safety: yolo
    sub_agents: [missing]
    toolsets:
      - type: think
      - type: think
      - type: shell
        ref: foo
  helper:
    model: openai/gpt-5
    handoffs: [nobody]
`
	assertProblems(t, []located{
		{"models.claude.compaction_threshold", "compaction_threshold must be greater than 0 and at most 1, got 7"},
		{"agents.root.safety", `invalid safety mode "yolo"`},
		{"agents.root.sub_agents", "agent 'root' references non-existent sub-agent 'missing'"},
		{"agents.root.toolsets.2.ref", "ref can only be used with type 'mcp' or 'rag'"},
		{"agents.helper.handoffs", "agent 'helper' references non-existent handoff agent 'nobody'"},
	}, locatedProblems(t, doc))
}

// TestProblemsKeepOriginalIndexes checks that a problem found after a list item was removed
// is reported at the item's index in the validated document.
func TestProblemsKeepOriginalIndexes(t *testing.T) {
	t.Parallel()
	doc := "agents:\n  root:\n    model: openai/gpt-5\n    toolsets:\n      - type: mcp\n      - type: think\n" +
		"      - type: mcp\n"
	assertProblems(t, []located{
		{"agents.root.toolsets.0", "either command, remote or ref must be set"},
		{"agents.root.toolsets.2", "either command, remote or ref must be set"},
	}, locatedProblems(t, doc))
}

// TestDefinitionsAreNotRemoved checks that a problem with a whole definition stops the search:
// removing the definition would turn the references to it into problems too.
func TestDefinitionsAreNotRemoved(t *testing.T) {
	t.Parallel()
	doc := "providers:\n  p:\n    api_type: openai_chatcompletions\nagents:\n  root:\n    model: p/m\n" +
		"    sub_agents: [missing]\n"
	assertProblems(t, []located{{"providers.p", "provider 'p': base_url is required"}}, locatedProblems(t, doc))
}

// TestNamedDefinitionProblems checks messages about a named definition.
func TestNamedDefinitionProblems(t *testing.T) {
	t.Parallel()
	doc := "providers:\n  p:\n    api_type: bogus\n    base_url: http://localhost:1\n" +
		"models:\n  m:\n    provider: openai\n    model: gpt-5\n    cost: {input: -1}\n" +
		"agents:\n  root:\n    model: m\n    toolsets:\n      - type: mcp\n        ref: undefined\n"
	assertProblems(t, []located{
		{"providers.p.api_type", "provider 'p': invalid api_type 'bogus'"},
		{"models.m.cost.input", "models.m: cost.input must not be negative"},
		{"agents.root.toolsets.0.ref", "agent 'root' references non-existent MCP definition 'undefined'"},
	}, locatedProblems(t, doc))
}

func TestUnlocatedProblem(t *testing.T) {
	t.Parallel()
	res := Validate(t.Context(), "agent.yaml", []byte("agents: [\n"))
	require.False(t, res.Valid)
	require.Len(t, res.Problems, 1)
	assert.Nil(t, res.Problems[0].Path)
}

// TestProblemMessagesKeepOriginalIndexes checks that the locations in the messages of
// problems found after earlier ones were removed name the validated document's indexes, like
// Path: docker-agent reports them for the reduced document.
func TestProblemMessagesKeepOriginalIndexes(t *testing.T) {
	t.Parallel()
	hooks := "agents:\n  root:\n    model: openai/gpt-5\n    hooks:\n      pre_tool_use:\n        - matcher: \"*\"\n" +
		"          hooks:\n            - type: command\n            - type: command\n            - type: command\n"
	got := locatedProblems(t, hooks)
	assertProblems(t, []located{
		{"agents.root.hooks.pre_tool_use.0.hooks.0", "hooks.pre_tool_use[0].hooks[0]: command is required"},
		{"agents.root.hooks.pre_tool_use.0.hooks.1", "hooks.pre_tool_use[0].hooks[1]: command is required"},
		{"agents.root.hooks.pre_tool_use.0.hooks.2", "hooks.pre_tool_use[0].hooks[2]: command is required"},
	}, got)

	// A requirement problem found once the earlier problems are removed.
	toolsets := "agents:\n  root:\n    model: openai/gpt-5\n    toolsets:\n      - type: mcp\n        ref: missing1\n" +
		"      - type: bogus\n"
	assertProblems(t, []located{
		{"agents.root.toolsets.0.ref", "agent 'root' references non-existent MCP definition 'missing1'"},
		{"agents.root.toolsets.1", `unknown toolset type "bogus" at agents.root.toolsets[1]: valid types are`},
	}, locatedProblems(t, toolsets))
	for _, p := range locatedProblems(t, toolsets) {
		assert.NotContains(t, p.msg, "toolsets[0]:", p.msg)
	}
}

// TestRelocateMessage checks the rewriting of locations: only locations of the current
// document that resolve to exactly one value change, to the original indexes.
func TestRelocateMessage(t *testing.T) {
	t.Parallel()
	tr, ok := parseTree([]byte("agents:\n  a:\n    toolsets: [{type: x}, {type: y}, {type: z}]\n" +
		"  b:\n    toolsets: [{type: x}, {type: y}]\n"))
	require.True(t, ok)
	assert.Equal(t, "agents.a.toolsets[1]: bad", tr.relocateMessage("agents.a.toolsets[1]: bad"), "nothing removed")
	_, removed := tr.remove([][]string{{agentsKey, "a", "toolsets", "0"}})
	require.True(t, removed)
	assert.Equal(t, "agents.a.toolsets[2]: bad, see agents.b.toolsets[1]",
		tr.relocateMessage("agents.a.toolsets[1]: bad, see agents.b.toolsets[1]"))
	assert.Equal(t, "toolsets[1]: ambiguous", tr.relocateMessage("toolsets[1]: ambiguous"),
		"a location relative to an agent that matches several agents is kept")
	assert.Equal(t, "agents.a.toolsets[7]: unknown", tr.relocateMessage("agents.a.toolsets[7]: unknown"))
}
