// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package render

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDocsExamplesRoundTrip runs every complete YAML example of docs.docker.com
// (testdata/docs/all) through decode -> Render -> load and compares the loaded
// configurations.
func TestDocsExamplesRoundTrip(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob(filepath.Join("testdata", "docs", "all", "*.yaml"))
	require.NoError(t, err)
	require.NotEmpty(t, files)
	for _, file := range files {
		name := filepath.Base(file)
		t.Run(strings.TrimSuffix(name, ".yaml"), func(t *testing.T) {
			t.Parallel()
			orig, err := os.ReadFile(file)
			require.NoError(t, err)
			want, err := loadJSON(t, name, orig)
			if err != nil {
				t.Skipf("the docs example does not load with docker-agent v1.150.0: %v", err)
			}
			doc, untyped, err := decodeYAML(orig)
			require.NoError(t, err)
			assert.Empty(t, untyped, "keys without a typed input")
			rendered, err := Render(doc)
			require.NoError(t, err)
			got, err := loadJSON(t, name, []byte(rendered))
			require.NoError(t, err, "the rendered configuration does not load:\n%s", rendered)
			assert.JSONEq(t, want, got, "rendered:\n%s", rendered)
		})
	}
}

// docsCase is one example of a docs.docker.com page, written as typed inputs.
type docsCase struct {
	page string // docs page URL
	file string // testdata/docs/<file>: the docs example (verbatim, or a fragment wrapped in a minimal agent)
	doc  Document
	skip string
}

// toolAgent is the minimal agent the toolset fragments of the tool pages are wrapped in.
func toolAgent(ts ...Toolset) []Agent {
	return []Agent{{Name: "root", Model: ptr("openai/gpt-5-mini"), Toolsets: ts}}
}

const docsBase = "https://docs.docker.com/ai/docker-agent/"

// docsCases has at least one example per page of the Docker Agent configuration docs
// (configuration/*, providers/*) and per tool page (tools/*), expressed with typed inputs.
// See research/schema-coverage.md for the page -> schema -> input table.
var docsCases = []docsCase{
	// ---------------------------------------------------------------- configuration
	{page: "configuration/overview/", file: "configuration-overview-1.yaml", doc: Document{Agents: []Agent{{
		Name: "root", Model: ptr("openai/gpt-5"), Description: ptr("A helpful assistant"),
		Instruction: ptr("You are a helpful assistant."),
	}}}},
	{page: "configuration/agents/", file: "configuration-agents-4.yaml", doc: Document{Agents: []Agent{{
		Name: "root", Model: ptr("anthropic/claude-sonnet-4-5"), Description: ptr("A helpful coding assistant"),
		Instruction: ptr("You are an expert software developer."), AddPromptFiles: []string{"AGENTS.md"},
	}}}},
	{page: "configuration/models/", file: "configuration-models-9.yaml", doc: Document{
		Models: map[string]ModelDef{"smart": {FirstAvailable: []string{
			"anthropic/claude-sonnet-4-6", "openai/gpt-5", "google/gemini-3.5-flash", "dmr/ai/qwen3",
		}}},
		Agents: []Agent{{Name: "root", Model: ptr("smart"), Instruction: ptr("You are a helpful assistant.")}},
	}},
	{page: "configuration/tools/", file: "configuration-tools-11.yaml", doc: Document{
		Models: map[string]ModelDef{
			"primary": {Provider: ptr("anthropic"), Model: ptr("claude-sonnet-4-5")},
			"fast":    {Provider: ptr("anthropic"), Model: ptr("claude-haiku-4-5")},
		},
		Agents: []Agent{{Name: "root", Model: ptr("primary"), Toolsets: []Toolset{
			{Type: "filesystem", Model: ptr("fast")},
			{Type: "shell", Model: ptr("fast")},
			{Type: "mcp", Ref: ptr("docker:github-official"), Model: ptr("openai/gpt-4o-mini")},
		}}},
	}},
	// Commands: the object form with URLs, agent switching, and top-level groups with the
	// bare-string shorthand plus use_commands.
	{page: "configuration/commands/", file: "configuration-commands-4.yaml", doc: Document{Agents: []Agent{{
		Name: "root", Model: ptr("anthropic/claude-sonnet-4-5"), Description: ptr("An agent with handy URL shortcuts."),
		Instruction: ptr("You are a helpful assistant."),
		Commands: map[string]Command{
			"feedback": {Description: ptr("Open the feedback site for this session"), URL: ptr("https://example.com/feedback?session={{session_id}}")},
			"docs":     {Description: ptr("Open the documentation"), URL: ptr("https://docs.docker.com/")},
			"desktop":  {Description: ptr("Open this session in Docker Desktop"), URL: ptr("docker-desktop://dashboard/session/{{session_id}}")},
		},
	}}}},
	{page: "configuration/commands/", file: "configuration-commands-5.yaml", doc: Document{Agents: []Agent{
		{
			Name: "root", Model: ptr("anthropic/claude-sonnet-4-5"), Description: ptr("Main assistant"),
			Instruction: ptr("You are a project coordinator."), SubAgents: []string{"planner", "reviewer"},
			Commands: map[string]Command{
				"plan":   {Agent: ptr("planner"), Instruction: ptr("Create a detailed plan for: ${args.join(' ')}")},
				"review": {Agent: ptr("reviewer")},
			},
		},
		{Name: "planner", Model: ptr("anthropic/claude-sonnet-4-5"), Description: ptr("Planning specialist"),
			Instruction: ptr("You create detailed project plans.")},
		{Name: "reviewer", Model: ptr("anthropic/claude-sonnet-4-5"), Description: ptr("Code review specialist"),
			Instruction: ptr("You review code and suggest improvements.")},
	}}},
	{page: "configuration/commands/", file: "configuration-commands-6.yaml", doc: Document{
		Commands: map[string]CommandGroup{"ci": {SimpleCommands: map[string]string{
			"deploy": "Deploy the application", "test": "Run the test suite",
		}}},
		Agents: []Agent{
			{Name: "root", Model: ptr("anthropic/claude-sonnet-4-5"), Description: ptr("Lead developer"),
				Instruction: ptr("You are the lead developer. Coordinate the team."), UseCommands: []string{"ci"},
				SimpleCommands: map[string]string{"lint": "Run the linter"}},
			{Name: "docs-writer", Model: ptr("anthropic/claude-sonnet-4-5"), Description: ptr("Documentation writer"),
				Instruction: ptr("You write and maintain the project documentation."), UseCommands: []string{"ci"}},
		},
	}},
	{page: "configuration/hooks/", file: "configuration-hooks-13.yaml", doc: Document{
		Agents: []Agent{
			{Name: "coder", Model: ptr("qwen3-large"), Handoffs: []string{"reviewer"},
				Hooks: &Hooks{OnAgentSwitch: []HookDefinition{{Type: "builtin", Command: ptr("unload")}}}},
			{Name: "reviewer", Model: ptr("qwen3-coder"), Handoffs: []string{"coder"},
				Hooks: &Hooks{OnAgentSwitch: []HookDefinition{{Type: "builtin", Command: ptr("unload")}}}},
		},
		Models: map[string]ModelDef{
			"qwen3-large": {Provider: ptr("dmr"), Model: ptr("ai/qwen3-large")},
			"qwen3-coder": {Provider: ptr("dmr"), Model: ptr("ai/qwen3-coder")},
		},
	}},
	{page: "configuration/permissions/", file: "configuration-permissions-1.yaml", doc: Document{
		Runtime: &RuntimeDefaults{Safety: ptr("balanced")},
		Agents:  []Agent{{Name: "root", Safety: ptr("strict")}},
	}},
	{page: "configuration/budget/", file: "configuration-budget-1.yaml", doc: Document{
		Agents: []Agent{{Name: "root", Model: ptr("openai/gpt-4o-mini"), Description: ptr("An agent on a short leash."),
			Instruction: ptr("You are a helpful assistant.")}},
		Budget: &Budget{MaxCost: ptr(0.50), MaxTokens: ptr(100000), MaxTime: ptr("10m")},
	}},
	{page: "configuration/sandbox/", file: "configuration-sandbox-1.yaml", doc: Document{
		Runtime: &RuntimeDefaults{Sandbox: ptr(true)},
		Agents: []Agent{{Name: "root", Model: ptr("openai/gpt-4o"), Description: ptr("A helpful assistant"),
			Instruction: ptr("You are a helpful assistant."), Toolsets: []Toolset{{Type: "shell"}}}},
	}},
	{page: "configuration/sandbox/", file: "configuration-sandbox-2.yaml", doc: Document{
		Runtime: &RuntimeDefaults{
			Sandbox:          ptr(true),
			NetworkAllowlist: []string{"api.example.com", "registry.npmjs.org"},
			Safety:           ptr("restricted"),
		},
		Agents: []Agent{{Name: "root", Model: ptr("openai/gpt-4o"), Description: ptr("Agent with sandboxed shell"),
			Instruction: ptr("You are a helpful assistant."), Toolsets: []Toolset{{Type: "shell"}}}},
	}},
	{page: "configuration/structured-output/", file: "configuration-structured-output-7.yaml", doc: Document{Agents: []Agent{{
		Name: "classifier", Model: ptr("anthropic/claude-sonnet-4-5"), Description: ptr("Classify support tickets"),
		Instruction: ptr("Classify the support ticket into the appropriate category\nand priority level based on its content.\n"),
		StructuredOutput: &StructuredOutput{Name: "ticket_classification", Strict: ptr(true), Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"category":   map[string]any{"type": "string", "enum": []any{"billing", "technical", "account", "feature_request", "other"}},
				"priority":   map[string]any{"type": "string", "enum": []any{"low", "medium", "high", "urgent"}},
				"confidence": map[string]any{"type": "number", "minimum": 0, "maximum": 1, "description": "Confidence score between 0 and 1"},
				"reasoning":  map[string]any{"type": "string", "description": "Brief explanation for the classification"},
			},
			"required": []any{"category", "priority", "confidence"},
		}},
	}}}},
	{page: "configuration/flavors/", file: "configuration-flavors-4.yaml", doc: Document{
		Agents: []Agent{{Name: "root", Toolsets: []Toolset{{Type: "think"}}}},
		Flavors: map[string]map[string]any{"with-shell": {
			"agents": map[string]any{"root": map[string]any{"toolsets+": []any{map[string]any{"type": "shell"}}}},
		}},
	}},
	{page: "configuration/routing/", file: "configuration-routing-1.yaml", doc: Document{
		Models: map[string]ModelDef{"smart_router": {Provider: ptr("openai"), Model: ptr("gpt-5-mini"), Routing: []RoutingRule{
			{Model: "anthropic/claude-sonnet-4-5", Examples: []string{"Write a detailed technical document",
				"Help me architect this system", "Review this code for security issues", "Explain this complex algorithm"}},
			{Model: "openai/gpt-5", Examples: []string{"Generate some creative ideas", "Write a story about",
				"Help me brainstorm", "Come up with names for"}},
			{Model: "openai/gpt-5-mini", Examples: []string{"What time is it", "Convert this to JSON",
				"Simple math calculation", "Translate this word"}},
		}}},
		Agents: []Agent{{Name: "root", Model: ptr("smart_router"), Description: ptr("Assistant with intelligent model routing"),
			Instruction: ptr("You are a helpful assistant.")}},
	}},
	{page: "providers/custom/", file: "providers-custom-1.yaml", doc: Document{
		Providers: map[string]ProviderDef{"my_gateway": {BaseURL: ptr("https://api.example.com/v1"), TokenKey: ptr("MY_API_KEY")}},
		Models:    map[string]ModelDef{"my_model": {Provider: ptr("my_gateway"), Model: ptr("gpt-4o")}},
		Agents:    []Agent{{Name: "root", Model: ptr("my_model"), Instruction: ptr("You are a helpful assistant.")}},
	}},
	{page: "providers/overview/", file: "providers-overview-1.yaml", doc: Document{
		Models: map[string]ModelDef{
			"claude": {Provider: ptr("anthropic"), Model: ptr("claude-sonnet-4-5"), MaxTokens: ptr(64000)},
			"gpt":    {Provider: ptr("openai"), Model: ptr("gpt-5")},
			"local":  {Provider: ptr("dmr"), Model: ptr("ai/qwen3")},
		},
		Agents: []Agent{
			{Name: "root", Model: ptr("claude"), SubAgents: []string{"coder", "helper"}},
			{Name: "coder", Model: ptr("gpt")},
			{Name: "helper", Model: ptr("local")},
		},
	}},

	// ---------------------------------------------------------------- tools
	{page: "tools/a2a/", file: "tools-a2a-1.yaml", doc: Document{Agents: toolAgent(Toolset{
		Type: "a2a", URL: ptr("http://localhost:8080/a2a"), Name: ptr("research_agent"),
		Headers: map[string]string{"Authorization": "Bearer ${env.A2A_TOKEN}", "X-Tenant": "acme"},
	})}},
	{page: "tools/api/", file: "tools-api-1.yaml", doc: Document{Agents: []Agent{{
		Name: "assistant", Model: ptr("openai/gpt-4o"), Description: ptr("Assistant with API access"),
		Instruction: ptr("You can look up weather information."),
		Toolsets: []Toolset{{Type: "api", APIConfig: &ApiConfig{
			Name: "get_weather", Method: "GET", Endpoint: "https://api.weather.example/v1/current?city=${city}",
			Instruction: ptr("Get current weather for a city"),
			Args:        map[string]ApiArg{"city": {Type: ptr("string"), Description: ptr("City name to get weather for")}},
			Required:    []string{"city"},
			Headers:     map[string]string{"Authorization": "Bearer ${env.WEATHER_API_KEY}"},
		}}},
	}}}},
	{page: "tools/background-agents/", file: "tools-background-agents-2.yaml", doc: Document{Agents: []Agent{
		{Name: "coordinator", Model: ptr("openai/gpt-4o"), Description: ptr("Orchestrates parallel research"),
			Instruction: ptr("Fan out research tasks and synthesize results."), SubAgents: []string{"researcher"},
			Toolsets: []Toolset{{Type: "background_agents"}, {Type: "think"}}},
		{Name: "researcher", Model: ptr("openai/gpt-4o"), Description: ptr("Web researcher"),
			Instruction: ptr("Research topics thoroughly."), Toolsets: []Toolset{{Type: "mcp", Ref: ptr("docker:duckduckgo")}}},
	}}},
	{page: "tools/background-jobs/", file: "tools-background-jobs-2.yaml", doc: Document{Agents: toolAgent(Toolset{
		Type: "background_jobs", Env: map[string]string{"MY_VAR": "value", "PATH": "${env.PATH}:/custom/bin"},
	})}},
	{page: "tools/fetch/", file: "tools-fetch-3.yaml", doc: Document{Agents: toolAgent(Toolset{
		Type: "fetch", AllowedDomains: []string{"docs.internal.example.com"},
		Headers: map[string]string{"Authorization": "Bearer ${env.INTERNAL_DOCS_TOKEN}", "X-Internal-Client": "docker-agent"},
	})}},
	{page: "tools/filesystem/", file: "tools-filesystem-2.yaml", doc: Document{Agents: toolAgent(Toolset{
		Type: "filesystem", AllowList: []string{".", "~"}, DenyList: []string{"~/.ssh", "~/.aws"},
	})}},
	{page: "tools/git/", file: "tools-git-2.yaml", doc: Document{Agents: []Agent{{
		Name: "root", Model: ptr("openai/gpt-5-mini"), Description: ptr("A code review assistant"),
		Instruction: ptr("Review the working changes: check git_status, then git_show the latest\ncommit, and summarize what changed.\n"),
		Toolsets:    []Toolset{{Type: "git"}, {Type: "filesystem"}},
	}}}},
	{page: "tools/handoff/", file: "tools-handoff-1.yaml", doc: Document{Agents: []Agent{
		{Name: "router", Model: ptr("openai/gpt-4o"), Description: ptr("Routes questions to the right specialist"),
			Instruction: ptr("Classify the user's question and hand off to the most appropriate\n" +
				"specialist. If unsure, ask a clarifying question first.\n"),
			Handoffs: []string{"billing", "support"}},
		{Name: "billing", Model: ptr("openai/gpt-4o"), Description: ptr("Billing specialist"), Instruction: ptr("Answer billing questions.")},
		{Name: "support", Model: ptr("openai/gpt-4o"), Description: ptr("Technical support specialist"),
			Instruction: ptr("Help with technical issues.")},
	}}},
	{page: "tools/lsp/", file: "tools-lsp-1.yaml", doc: Document{Agents: []Agent{{
		Name: "developer", Model: ptr("anthropic/claude-sonnet-4-5"), Description: ptr("Code developer with LSP support"),
		Instruction: ptr("You are a software developer."),
		Toolsets:    []Toolset{{Type: "lsp", Command: ptr("gopls"), FileTypes: []string{".go"}}, {Type: "filesystem"}, {Type: "shell"}},
	}}}},
	{page: "tools/mcp-catalog/", file: "tools-mcp-catalog-3.yaml", doc: Document{Agents: []Agent{{
		Name: "root", Model: ptr("anthropic/claude-sonnet-4-5"),
		Description: ptr("Agent that can on-demand connect to remote MCP servers from the Docker MCP Catalog."),
		Instruction: ptr("You can discover and activate remote MCP servers on demand.\n" +
			"Use search_remote_mcp_servers to find a server matching the\n" +
			"user's intent, then enable_remote_mcp_server to activate it.\n" +
			"Be conservative: enable only the servers you actually need for\n" +
			"the task at hand. Disable a server with disable_remote_mcp_server\n" +
			"once you are done with it.\n"),
		Toolsets: []Toolset{{Type: "mcp_catalog"}},
	}}}},
	{page: "tools/mcp/", file: "tools-mcp-4.yaml", doc: Document{
		Mcps: map[string]McpDefinition{
			"github":     {Remote: &Remote{URL: "https://api.githubcopilot.com/mcp", TransportType: ptr("sse")}},
			"playwright": {Command: ptr("npx"), Args: []string{"-y", "@modelcontextprotocol/server-playwright"}},
		},
		Agents: []Agent{{Name: "root", Model: ptr("openai/gpt-5"), Toolsets: []Toolset{
			{Type: "mcp", Ref: ptr("github")}, {Type: "mcp", Ref: ptr("playwright")},
		}}},
	}},
	{page: "tools/mcp/", file: "tools-mcp-3.yaml", doc: Document{Agents: toolAgent(Toolset{
		Type: "mcp",
		Remote: &Remote{URL: "https://mcp.linear.app/mcp", TransportType: ptr("streamable"),
			Headers: map[string]string{"Authorization": "Bearer ${env.LINEAR_TOKEN}"}},
		AllowPrivateIPs: ptr(false),
		Tools:           []string{"search_issues", "create_issue"},
	})}},
	{page: "tools/memory/", file: "tools-memory-2.yaml", doc: Document{Agents: toolAgent(Toolset{
		Type: "memory", Path: ptr("./agent_memory.db"),
	})}},
	{page: "tools/model-picker/", file: "tools-model-picker-2.yaml", doc: Document{Agents: []Agent{{
		Name: "root", Model: ptr("openai/gpt-5-mini"),
		Instruction: ptr("You are a helpful assistant. For simple questions, use gpt-5-mini.\n" +
			"For complex reasoning or coding tasks, switch to claude-sonnet-4-5 or gpt-5.\n"),
		Toolsets: []Toolset{{Type: "model_picker", Models: []string{"openai/gpt-5-mini", "anthropic/claude-sonnet-4-5", "openai/gpt-5"}}},
	}}}},
	{page: "tools/open-url/", file: "tools-open-url-1.yaml", doc: Document{Agents: []Agent{{
		Name: "assistant", Model: ptr("openai/gpt-4o"), Description: ptr("Assistant that can open the dashboard"),
		Instruction: ptr("When the user asks to see the dashboard, call open_dashboard."),
		Toolsets:    []Toolset{{Type: "open_url", Name: ptr("open_dashboard"), URL: ptr("https://example.com/dashboard")}},
	}}}},
	{page: "tools/openapi/", file: "tools-openapi-2.yaml", doc: Document{Agents: toolAgent(Toolset{
		Type: "openapi", URL: ptr("https://api.example.com/openapi.json"),
		Headers: map[string]string{"Authorization": "Bearer ${env.API_TOKEN}", "X-Custom-Header": "my-value"},
	})}},
	{page: "tools/plan/", file: "tools-plan-1.yaml", doc: Document{Agents: toolAgent(Toolset{Type: "plan"})}},
	{page: "tools/rag/", file: "tools-rag-1.yaml", doc: Document{
		Rag: map[string]RagSource{"my_docs": {
			Tool: &RagTool{Description: ptr("Technical documentation")},
			Docs: []string{"./documents", "./some-doc.md"},
			Strategies: []RagStrategy{{Type: "chunked-embeddings", EmbeddingModel: ptr("openai/text-embedding-3-small"),
				Database: ptr("./docs.db"), VectorDimensions: ptr(1536)}},
		}},
		Agents: []Agent{{Name: "root", Model: ptr("openai/gpt-4o"),
			Instruction: ptr("You have access to a knowledge base. Use it to answer questions.\n"),
			Toolsets:    []Toolset{{Type: "rag", Ref: ptr("my_docs")}}}},
	}},
	{page: "tools/scheduler/", file: "tools-scheduler-2.yaml", doc: Document{Agents: []Agent{{
		Name: "root", Model: ptr("openai/gpt-5-mini"), Description: ptr("A monitoring assistant"),
		Instruction: ptr("Every 15 minutes, run `git fetch` and tell me if origin/main moved.\n"),
		Toolsets:    []Toolset{{Type: "scheduler"}, {Type: "shell"}},
	}}}},
	{page: "tools/script/", file: "tools-script-2.yaml", doc: Document{Agents: toolAgent(Toolset{
		Type: "script",
		Shell: map[string]ScriptShellTool{"deploy": {
			Cmd: ptr("./scripts/deploy.sh ${env}"), Description: ptr("Deploy to an environment"),
			Args:     map[string]any{"env": map[string]any{"type": "string", "enum": []any{"staging", "production"}}},
			Required: []string{"env"},
		}},
	})}},
	{page: "tools/session_context/", file: "tools-session_context-2.yaml", doc: Document{Agents: toolAgent(Toolset{
		Type: "session_context", Tools: []string{"list_sessions"},
	})}},
	{page: "tools/session_plan/", file: "tools-session_plan-2.yaml",
		skip: "the session_plan toolset type was removed upstream (docker/docker-agent#4199) and v1.150.0 rejects it; the docs page is stale"},
	{page: "tools/shell/", file: "tools-shell-2.yaml", doc: Document{Agents: toolAgent(Toolset{
		Type: "shell", Env: map[string]string{"MY_VAR": "value", "PATH": "${env.PATH}:/custom/bin"},
	})}},
	{page: "tools/tasks/", file: "tools-tasks-2.yaml", doc: Document{Agents: []Agent{{
		Name: "root", Model: ptr("openai/gpt-4o"), Toolsets: []Toolset{{Type: "tasks", Path: ptr("./project-tasks.json")}},
	}}}},
	{page: "tools/think/", file: "tools-think-1.yaml", doc: Document{Agents: toolAgent(Toolset{Type: "think"})}},
	{page: "tools/todo/", file: "tools-todo-2.yaml", doc: Document{Agents: toolAgent(Toolset{Type: "todo", Shared: ptr(true)})}},
	{page: "tools/transfer-task/", file: "tools-transfer-task-1.yaml", doc: Document{Agents: []Agent{
		{Name: "coordinator", Model: ptr("openai/gpt-4o"), Description: ptr("Coordinates work across specialists"),
			Instruction: ptr("Analyze requests and delegate to the right specialist."), SubAgents: []string{"developer", "researcher"}},
		{Name: "developer", Model: ptr("anthropic/claude-sonnet-4-5"), Description: ptr("Expert software developer"),
			Instruction: ptr("Write clean, production-ready code."), Toolsets: []Toolset{{Type: "filesystem"}, {Type: "shell"}}},
		{Name: "researcher", Model: ptr("openai/gpt-4o"), Description: ptr("Web researcher"),
			Instruction: ptr("Search for information online."), Toolsets: []Toolset{{Type: "mcp", Ref: ptr("docker:duckduckgo")}}},
	}}},
	{page: "tools/user-prompt/", file: "tools-user-prompt-1.yaml", doc: Document{Agents: []Agent{{
		Name: "assistant", Model: ptr("openai/gpt-4o"), Description: ptr("Interactive assistant"),
		Instruction: ptr("You are a helpful assistant. When you need information\nfrom the user, use the user_prompt tool to ask them.\n"),
		Toolsets:    []Toolset{{Type: "user_prompt"}, {Type: "filesystem"}, {Type: "shell"}},
	}}}},
	{page: "tools/webhook/", file: "tools-webhook-7.yaml", doc: Document{Agents: []Agent{{
		Name: "root", Model: ptr("openai/gpt-5-mini"), Instruction: ptr("If a check fails, notify the team with send_webhook."),
		Toolsets: []Toolset{{Type: "webhook", WebhookConfig: &WebhookConfig{Provider: ptr("slack"), URL: "${env.SLACK_WEBHOOK_URL}"}}},
	}}}},
}

// TestDocsExamplesTyped renders a typed Document for one or more examples of every docs page
// and checks that docker-agent loads the same configuration as from the docs YAML. It also
// checks that every key of the docs YAML has a typed input.
func TestDocsExamplesTyped(t *testing.T) {
	t.Parallel()
	for _, tc := range docsCases {
		t.Run(strings.TrimSuffix(tc.file, ".yaml"), func(t *testing.T) {
			t.Parallel()
			if tc.skip != "" {
				t.Skip(tc.skip)
			}
			orig, err := os.ReadFile(filepath.Join("testdata", "docs", tc.file))
			require.NoError(t, err)
			require.True(t, strings.HasPrefix(string(orig), "# "), "testdata/docs/%s must start with its source comment", tc.file)
			require.Contains(t, string(orig), docsBase+tc.page)
			want, err := loadJSON(t, tc.file, orig)
			require.NoError(t, err, "the docs example does not load")

			_, untyped, err := decodeYAML(orig)
			require.NoError(t, err)
			assert.Empty(t, untyped, "keys of the docs example without a typed input")

			rendered, err := Render(tc.doc)
			require.NoError(t, err)
			got, err := loadJSON(t, tc.file, []byte(rendered))
			require.NoError(t, err, "the typed configuration does not load:\n%s", rendered)
			assert.JSONEq(t, want, got, "rendered:\n%s", rendered)
		})
	}
}

// TestDocsExamplesCoverEveryPage fails when a page of the configuration docs has no typed
// example.
func TestDocsExamplesCoverEveryPage(t *testing.T) {
	t.Parallel()
	pages := map[string]bool{}
	for _, tc := range docsCases {
		pages[tc.page] = true
	}
	for _, p := range []string{
		"configuration/overview/", "configuration/agents/", "configuration/models/", "configuration/tools/",
		"configuration/commands/", "configuration/hooks/", "configuration/permissions/", "configuration/budget/",
		"configuration/sandbox/", "configuration/structured-output/", "configuration/flavors/", "configuration/routing/",
		"providers/custom/", "providers/overview/",
		"tools/a2a/", "tools/api/", "tools/background-agents/", "tools/background-jobs/", "tools/fetch/", "tools/filesystem/",
		"tools/git/", "tools/handoff/", "tools/lsp/", "tools/mcp-catalog/", "tools/mcp/", "tools/memory/",
		"tools/model-picker/", "tools/open-url/", "tools/openapi/", "tools/plan/", "tools/rag/", "tools/scheduler/",
		"tools/script/", "tools/session_context/", "tools/session_plan/", "tools/shell/", "tools/tasks/", "tools/think/",
		"tools/todo/", "tools/transfer-task/", "tools/user-prompt/", "tools/webhook/",
	} {
		assert.True(t, pages[p], "no typed docs example for %s%s", docsBase, p)
	}
}
