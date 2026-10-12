// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package render

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/dirien/pulumi-docker-agent/provider/pkg/validate"
)

// renderValid renders doc, compares it with the golden file and checks that docker-agent
// accepts it.
func renderValid(t *testing.T, name string, doc Document) string {
	t.Helper()
	got, err := Render(doc)
	require.NoError(t, err)
	golden(t, name, got)
	res := validate.Validate(t.Context(), name+".yaml", []byte(got))
	require.True(t, res.Valid, "invalid configuration: %v\n%s", res.Errors, got)
	return got
}

// TestRenderSandbox covers sandbox mode: runtime defaults (sandbox, safety, network
// allowlist), a per-agent safety mode and a read-only toolset.
func TestRenderSandbox(t *testing.T) {
	t.Parallel()
	renderValid(t, "sandbox", Document{
		Runtime: &RuntimeDefaults{
			Sandbox:          ptr(true),
			Safety:           ptr(SafetyModeRestricted),
			NetworkAllowlist: []string{"api.github.com", "registry.npmjs.org:443"},
		},
		Agents: []Agent{{
			Name:        "root",
			Model:       ptr("anthropic/claude-sonnet-4-5"),
			Description: ptr("Works on the repository inside a sandbox"),
			Instruction: ptr("Fix the failing tests. Only talk to GitHub and npm."),
			Safety:      ptr(SafetyModeBalanced),
			Toolsets: []Toolset{
				{Type: "shell"},
				{Type: "filesystem", Readonly: ptr(true)},
				{Type: "fetch", AllowedDomains: []string{"api.github.com"}},
			},
		}},
		Permissions: &Permissions{Ask: []string{"shell"}, Deny: []string{"shell:cmd=rm -rf*"}},
	})
}

// TestRenderHooks covers every hook shape: plain hook lists, tool matchers, builtin, model
// and evaluator hooks, routing hooks and an evaluator.
func TestRenderHooks(t *testing.T) {
	t.Parallel()
	renderValid(t, "hooks", Document{
		Evaluators: map[string]Evaluator{
			"route": {
				Provider: "openai", Model: "gpt-5-mini", Type: "choice",
				Instructions: "Classify the request.",
				Choices:      map[string]string{"simple": "A direct question.", "complex": "Anything else."},
				Timeout:      ptr("30s"),
			},
		},
		Agents: []Agent{
			{
				Name:    "root",
				Model:   ptr("openai/gpt-5-mini"),
				Routing: &AgentRouting{AllowedAgents: []string{"quick", "expert"}, DefaultAgent: ptr("expert")},
				Hooks: &Hooks{
					SessionStart: []HookDefinition{{Type: "builtin", Command: ptr("add_environment_info")}},
					PreToolUse: []HookMatcher{{
						Matcher: ptr("shell"),
						Hooks: []HookDefinition{{
							Type: "command", Command: ptr("./scripts/check.sh"), Args: []string{"--strict"},
							Timeout: ptr(30), OnError: ptr(HookErrorPolicyBlock), Env: map[string]string{"MODE": "ci"},
						}},
					}},
					PostToolUse: []HookMatcher{{Matcher: ptr("*"), Hooks: []HookDefinition{{Type: "command", Command: ptr("echo done")}}}},
					Stop:        []HookDefinition{{Type: "model", Model: ptr("openai/gpt-5-mini"), Prompt: ptr("Was the answer complete?")}},
					BeforeAgentRun: []HookDefinition{{
						Type:      "evaluator",
						Evaluator: ptr("route"),
						RoutingPolicy: &RoutingPolicy{
							Routes:         map[string]string{"simple": "quick", "complex": "expert"},
							MinProbability: 0.7,
						},
					}},
				},
			},
			{Name: "quick", Model: ptr("openai/gpt-5-mini"), Description: ptr("Answers simple questions")},
			{Name: "expert", Model: ptr("openai/gpt-5"), Description: ptr("Handles everything else")},
		},
	})
}

// TestRenderRAG covers a top-level RAG source with two strategies, fusion and reranking,
// used from an agent toolset.
func TestRenderRAG(t *testing.T) {
	t.Parallel()
	renderValid(t, "rag", Document{
		Models: map[string]ModelDef{
			"embedder": {Provider: ptr("openai"), Model: ptr("text-embedding-3-small")},
		},
		Rag: map[string]RagSource{
			"docs": {
				Tool: &RagTool{Name: ptr("search_docs"), Description: ptr("Search the product documentation")},
				Docs: []string{"./docs", "./README.md"},
				Strategies: []RagStrategy{
					{
						Type:             "chunked-embeddings",
						EmbeddingModel:   ptr("embedder"),
						Database:         ptr("./rag/vectors.db"),
						VectorDimensions: ptr(1536),
						SimilarityMetric: ptr("cosine_similarity"),
						Threshold:        ptr(0.5),
						Limit:            ptr(20),
						Chunking:         &RagChunking{Size: ptr(1000), Overlap: ptr(100), RespectWordBoundaries: ptr(true)},
					},
					{Type: "bm25", Database: ptr("./rag/bm25.db"), K1: ptr(1.5), B: ptr(0.75), Limit: ptr(20)},
				},
				Results: &RagResults{
					Limit:       ptr(5),
					Fusion:      &RagFusion{Strategy: ptr(RagFusionStrategyRRF), K: ptr(60)},
					Deduplicate: ptr(true),
				},
			},
		},
		Agents: []Agent{{
			Name:        "root",
			Model:       ptr("openai/gpt-5-mini"),
			Instruction: ptr("Answer questions about the product. Search the documentation first."),
			Toolsets:    []Toolset{{Type: "rag", Ref: ptr("docs")}},
		}},
	})
}
