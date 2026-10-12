// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package main

// This file holds every decision the generator cannot derive from agent-schema.json on
// its own: Pulumi type names, union representations, free-form values and field order.
// Schema paths use the notation of the coverage test and the schema diff:
// `Config.<prop>` for root properties, `<Definition>.<prop>`, `<path>[]` for array items,
// `<path>{}` for map values and `<path>|<i>` for branch i of a oneOf/anyOf/allOf.
//
// When docker-agent adds a definition, an inline object or a union, the generator stops
// with an error that names the path to add here. New plain properties need no change.

// typeNames maps schema definitions and inline object paths to Pulumi type names. The names
// must not collide with resource or function names (`AgentConfig` would clash with the
// resource's `AgentConfigArgs` in the Python, Go and .NET SDKs, `Provider` with the
// provider resource) and a type must not have a property with its own name (a C# class
// cannot have a member named like the class, which is why ModelConfig is `ModelDef`).
const (
	goAny    = "any"
	goAnyMap = "map[string]any"
	hookDef  = "HookDefinition"
)

var typeNames = map[string]string{
	"AgentConfig":                     "Agent",
	"ApiConfig":                       "ApiConfig",
	"ApiConfig.args{}":                "ApiArg",
	"AgentConfig.structured_output":   "StructuredOutput",
	"AgentRouting":                    "AgentRouting",
	"AuthConfig":                      "ModelAuth",
	"BudgetConfig":                    "Budget",
	"CacheConfig":                     "ResponseCache",
	"CapabilitiesConfig":              "ModelCapabilities",
	"CommandConfig":                   "Command",
	"Commands":                        "CommandGroup",
	"CostConfig":                      "Cost",
	"EvaluatorConfig":                 "Evaluator",
	"EvaluatorPolicy":                 "EvaluatorPolicy",
	"FallbackConfig":                  "Fallback",
	"FederationAuthConfig":            "WorkloadIdentityFederation",
	"HarnessConfig":                   "Harness",
	hookDef:                           hookDef,
	"HookMatcherConfig":               "HookMatcher",
	"HooksConfig":                     "Hooks",
	"IdentityTokenSourceConfig":       "IdentityTokenSource",
	"InlineSkill":                     "InlineSkill",
	"Lifecycle":                       "Lifecycle",
	"Lifecycle.backoff":               "LifecycleBackoff",
	"MCPToolset":                      "McpDefinition",
	"Metadata":                        "Metadata",
	"ModelConfig":                     "ModelDef",
	"OutputCapabilitiesConfig":        "ModelOutputCapabilities",
	"PermissionsConfig":               "Permissions",
	"PostEditConfig":                  "PostEdit",
	"ProviderConfig":                  "ProviderDef",
	"RAGConfig":                       "RagConfig",
	"RAGConfig.results":               "RagResults",
	"RAGConfig.results.fusion":        "RagFusion",
	"RAGConfig.results.reranking":     "RagReranking",
	"RAGConfig.strategies[]":          "RagStrategy",
	"RAGConfig.strategies[].chunking": "RagChunking",
	"RAGConfig.tool":                  "RagTool",
	"RAGToolset":                      "RagSource",
	"Remote":                          "Remote",
	"RemoteOAuthConfig":               "RemoteOAuth",
	"RoutingPolicy":                   "RoutingPolicy",
	"RoutingRule":                     "RoutingRule",
	"RuntimeDefaults":                 "RuntimeDefaults",
	"ScriptShellToolConfig":           "ScriptShellTool",
	"SkillsConfig":                    "SkillSet",
	"Toolset":                         "Toolset",
	"WebhookConfig":                   "WebhookConfig",
}

// aliases are definitions that only add constraints to another definition
// (`allOf: [{$ref: X}, {...constraints}]`). They are represented by X's type; docker-agent's
// loader enforces the constraints.
var aliases = map[string]string{
	"NonEvaluatorHookDefinition":    hookDef,
	"NonEvaluatorHookMatcherConfig": "HookMatcherConfig",
	"RoutingHookDefinition":         hookDef,
}

// mergedAllOf are definitions whose allOf branches are flattened into one object.
var mergedAllOf = map[string]bool{
	// A top-level `rag` source: the RAGConfig fields plus instruction, tools, name and defer.
	"RAGToolset": true,
}

// freeForm are values that are free-form by design. They are typed `any` or
// `map[string]any` and rendered verbatim (map keys sorted). The coverage test has the same
// list with the reasons.
var freeForm = map[string]string{
	"Config.flavors":                       "map[string]map[string]any",
	"MCPToolset.config":                    goAny,
	"Toolset.config":                       goAny,
	"ModelConfig.provider_opts":            goAnyMap,
	"ProviderConfig.provider_opts":         goAnyMap,
	"AgentConfig.structured_output.schema": goAnyMap,
	"ApiConfig.output_schema":              goAnyMap,
	"ScriptShellToolConfig.args":           goAnyMap,
}

// fieldSpec is a hand-written field for a union property.
type fieldSpec struct {
	pulumi string
	goType string
	// concat: several fields share the YAML key and are combined (lists are concatenated,
	// maps merged). Without it the fields sharing a key are mutually exclusive variants.
	concat bool
	desc   string
}

// unionFields maps union-typed properties (oneOf/anyOf with different branch types) to
// their typed representation. Folded branches (a branch that is expressed through another
// branch with the same meaning) are listed with a reason in the coverage test.
var unionFields = map[string][]fieldSpec{
	// string | list of strings. docker-agent joins a list with a blank line, so one string
	// is equivalent.
	"AgentConfig.instruction": {{pulumi: "instruction", goType: "*string"}},
	// string | list of strings. A list of one is equivalent to the string form.
	"AgentConfig.instruction_file": {{pulumi: "instructionFile", goType: "[]string"}},
	// map | list of single-entry maps (merged in order by docker-agent), and each value is
	// a string (the instruction) or an object. Both value forms are typed.
	"AgentConfig.commands": {
		{pulumi: "commands", goType: "map[string]Command", concat: true,
			desc: "Named commands (`/name`), in the object form: `description`, `instruction`, `agent` or `url`."},
		{pulumi: "simpleCommands", goType: "map[string]string", concat: true,
			desc: "Named commands in the short form `name: instruction`: the string is the prompt sent to the " +
				"agent. Rendered into the same `commands` map as `commands`; a name must not appear in both."},
	},
	// true | false | list of sources, skill names and inline skills. `true` is the same as
	// `["local"]` and `false` as an empty list, so the list form covers the boolean.
	"AgentConfig.skills": {
		{pulumi: "skills", goType: "[]string", concat: true,
			desc: "Skill sources and names: `local` (skills discovered on the local filesystem; `[\"local\"]` is " +
				"the same as `skills: true`), HTTP/HTTPS URLs of remote skill sources, or names of skills to " +
				"include. Rendered into the `skills` list together with `inlineSkills`."},
		{pulumi: "inlineSkills", goType: "[]InlineSkill", concat: true,
			desc: "Skills defined inline in the configuration. Rendered into the `skills` list after the " +
				"entries of `skills`."},
	},
	"ModelConfig.thinking_budget":    thinkingBudget,
	"ProviderConfig.thinking_budget": thinkingBudget,
	"ModelConfig.task_budget":        taskBudget,
	"ProviderConfig.task_budget":     taskBudget,
	"MCPToolset.defer":               deferFields,
	"Toolset.defer":                  deferFields,
	"RAGToolset|1.defer":             deferFields,
	// duration string | integer number of seconds.
	"EvaluatorConfig.timeout": {{pulumi: "timeout", goType: "*string",
		desc: "Timeout for one evaluator call, as a duration such as `30s` or `2m` (a number of seconds " +
			"`N` is written as `Ns`)."}},
}

var thinkingBudget = []fieldSpec{
	{pulumi: "thinkingBudget", goType: "*string",
		desc: "Reasoning effort (`thinking_budget`) as a level: `none`, `minimal`, `low`, `medium`, `high`, " +
			"`xhigh`, `max`, `adaptive` or `adaptive/<level>`. Mutually exclusive with `thinkingBudgetTokens`."},
	{pulumi: "thinkingBudgetTokens", goType: "*int",
		desc: "Reasoning budget (`thinking_budget`) as a number of tokens. Mutually exclusive with " +
			"`thinkingBudget`."},
}

var taskBudget = []fieldSpec{{pulumi: "taskBudget", goType: "*int",
	desc: "Total-token budget for an agentic task (`task_budget`), forwarded to Anthropic as " +
		"`output_config.task_budget`. The object form `{type: tokens, total: N}` is the same as `N`; " +
		"`tokens` is the only type."}}

var deferFields = []fieldSpec{
	{pulumi: "defer", goType: "*bool",
		desc: "Defer loading all tools of this toolset until the model searches for them (`defer: true`). " +
			"Mutually exclusive with `deferTools`."},
	{pulumi: "deferTools", goType: "[]string",
		desc: "Defer loading only these tools until the model searches for them (`defer: [names]`). " +
			"Mutually exclusive with `defer`."},
}

// unionTypes are definitions that are unions as a whole. They become object types whose
// fields render into the value itself instead of into a mapping.
var unionTypes = map[string][]fieldSpec{
	// The value of a top-level `commands` group: the same two forms as an agent's commands.
	"Commands": {
		{pulumi: "commands", goType: "map[string]Command", concat: true,
			desc: "Commands in the object form: `description`, `instruction`, `agent` or `url`."},
		{pulumi: "simpleCommands", goType: "map[string]string", concat: true,
			desc: "Commands in the short form `name: instruction`. A name must not appear in both maps."},
	},
	// The value of a top-level `skills` group: the list form of an agent's skills.
	"SkillsConfig": {
		{pulumi: "skills", goType: "[]string", concat: true,
			desc: "Skill sources and names: `local`, HTTP/HTTPS URLs or names of skills to include. " +
				"`[\"local\"]` is the same as `true`."},
		{pulumi: "inlineSkills", goType: "[]InlineSkill", concat: true,
			desc: "Skills defined inline. Rendered after the entries of `skills`."},
	},
}

// enumTypes names the Pulumi enums generated from the `enum` of a string property (see
// enumValuesOf for which enums qualify). Every value becomes a typed constant in each SDK
// (`dockeragent.SafetyModeBalanced` in Go, `SafetyMode.Balanced` in TypeScript and C#,
// `SafetyMode.BALANCED` in Python), while TypeScript, Python, YAML and HCL programs can keep
// passing the string.
//
// Paths with the same name share one type; they must have the same values. Should
// docker-agent ever give them different values, the shared type gets all of them (so
// docker-agent's own validation catches a value that does not fit the path) and
// `schemagen generate` prints a note.
//
// A new enum without an entry here still gets a deterministic name, its owner type and the
// property name (`HookDefinition.on_error` would be HookDefinitionOnError), so a docker-agent
// upgrade never stops on it. Add an entry before a release ships the default name: renaming a
// published enum type breaks Go, .NET and Java programs.
var enumTypes = map[string]string{
	"AgentConfig.safety":                 "SafetyMode",
	"RuntimeDefaults.safety":             "SafetyMode",
	"AgentConfig.structured_output.mode": "StructuredOutputMode",
	"HookDefinition.type":                "HookType",
	"HookDefinition.on_error":            "HookErrorPolicy",
	"Lifecycle.profile":                  "LifecycleProfile",
	"Lifecycle.restart":                  "RestartPolicy",
	"ProviderConfig.api_type":            "ApiType",
	"HarnessConfig.type":                 "HarnessType",
	"HarnessConfig.effort":               "HarnessEffort",
	"ApiConfig.method":                   "HttpMethod",
	"RAGConfig.strategies[].type":        "RagStrategyType",
	"RAGConfig.results.fusion.strategy":  "RagFusionStrategy",
	"EvaluatorConfig.type":               "EvaluatorType",
	"EvaluatorPolicy.decisions{}":        "EvaluatorDecision",
	"EvaluatorPolicy.fallback":           "EvaluatorFallback",
}

// volatileEnums stay plain strings even when agent-schema.json gives them an `enum`: their
// description lists the allowed values instead. A Pulumi enum is closed: the C# and Java
// enums cannot hold any other value, and Check rejects other values in every language
// unless `skipValidation` is set. That is wrong for sets that grow with most docker-agent
// releases (a program would need a provider upgrade for every new value) and for values
// that may name user-defined things.
var volatileEnums = map[string]string{
	"Toolset.type": "about 30 toolset types, and most docker-agent releases add one; the provider checks " +
		"them against the pinned docker-agent's registered toolsets instead (validate.ToolsetTypes)",
	"WebhookConfig.provider": "payload shapes of chat and automation services; new integrations are " +
		"added regularly and `generic` covers the rest",
	// The schema has no enum for the model providers today. They stay strings if it gets one.
	"ModelConfig.provider": "custom provider names from the `providers` section and built-in aliases " +
		"are allowed",
	"ProviderConfig.provider":  "built-in aliases are allowed, and the list grows with docker-agent",
	"EvaluatorConfig.provider": "a named provider from the `providers` section is allowed",
}

// enumValueNames override the PascalCase name of an enum value (see enumValueName).
var enumValueNames = map[string]string{
	"bm25":                   "BM25",
	"openai_chatcompletions": "OpenAIChatCompletions",
	"openai_responses":       "OpenAIResponses",
	"opencode":               "OpenCode",
	"rrf":                    "RRF",
	"xhigh":                  "XHigh",
}

// enumValueDocs document enum values, by enum name and value. They replace the descriptions
// valueDocs extracts from the schema text (`'value' does ...`), which do not cover every
// value.
var enumValueDocs = map[string]map[string]string{
	// From docker-agent's latest.SafetyMode constants.
	"SafetyMode": {
		"strict":   "Prompt for every tool call.",
		"balanced": "Auto-approve the tool calls docker-agent's classifier considers safe; prompt for the others.",
		"restricted": "Auto-approve the calls the classifier considers safe and deny every other unmatched call " +
			"without prompting: the fail-closed profile for unattended runs. Custom permission rules still win.",
		"autonomous": "Auto-approve every tool call (the legacy YOLO mode). Review configurations from URLs or " +
			"OCI registries before running them.",
	},
}

// requiredOverrides marks properties as required Pulumi inputs although the schema does
// not list them in `required` (docker-agent's loader requires them).
var requiredOverrides = map[string]bool{
	"Toolset.type": true,
}

// fieldOrder fixes the leading YAML key order of a type. The remaining properties follow
// in schema order. The orders of the types that existed before the generator keep the
// rendered YAML of existing programs byte-for-byte identical.
var fieldOrder = map[string][]string{
	"Config": {"version", "metadata", "agents", "models", "providers", "permissions", "runtime", "budget",
		"budgets", "mcps", "rag", "toolsets", "commands", "skills", "evaluators", "flavors"},
	"AgentConfig": {"model", "description", "welcome_message", "instruction", "instruction_file", "harness",
		"sub_agents", "handoffs", "force_handoff", "routing", "fallback", "toolsets", "use_toolsets", "add_date",
		"add_environment_info", "max_iterations", "num_history_items", "skills", "use_skills", "commands",
		"use_commands", "structured_output", "hooks"},
	"ModelConfig": {"provider", "model", "description", "base_url", "token_key", "temperature", "max_tokens",
		"top_p", "parallel_tool_calls", "thinking_budget", "provider_opts"},
	"ProviderConfig": {"provider", "api_type", "base_url", "token_key"},
	"Toolset": {"type", "ref", "name", "command", "args", "env", "remote", "url", "headers", "path", "shared",
		"tools", "instruction", "config"},
	"Remote":            {"url", "transport_type", "headers"},
	"Metadata":          {"author", "license", "description", "readme", "version", "tags"},
	"PermissionsConfig": {"allow", "ask", "deny"},
	"FallbackConfig":    {"models", "retries", "cooldown"},
	hookDef:             {"type", "name"},
	"HookMatcherConfig": {"matcher", "hooks"},
}

// descriptions replace the schema description of a path.
var descriptions = map[string]string{
	"Config.agents": "The agents, in order. At least one agent is required. The agent named `root`, or else " +
		"the first agent, is the default agent. Rendered as the `agents` map keyed by each agent's `name`.",
	"Config.version": "The docker-agent configuration schema version.",
	"AgentConfig.instruction": "Instructions for the agent (the system prompt). docker-agent's YAML list form is " +
		"the same as one string with the parts joined by a blank line.",
	"SkillsConfig": "A named group of skills (a value of the top-level `skills` map), merged into the agents " +
		"that list the group in `useSkills`.",
	"Commands": "A named group of commands (a value of the top-level `commands` map), merged into the " +
		"agents that list the group in `useCommands`.",
}

// fallbackDescriptions document paths that have no description in agent-schema.json.
// Paths missing from this list get a generic description.
var fallbackDescriptions = map[string]string{
	"EvaluatorConfig.model":        "The evaluator model of the provider.",
	"EvaluatorConfig.instructions": "The instructions the evaluator assesses the input with.",
	"EvaluatorConfig.type":         "The kind of assessment: a yes/no decision, one of `choices`, or one of the score `levels`.",
	"EvaluatorConfig.choices": "For `type: choice`: the possible outcomes, keyed by name, with a description of " +
		"when each applies (2 to 255).",
	"EvaluatorConfig.levels": "For `type: score`: the ordered score levels.",
	"EvaluatorPolicy": "Maps an evaluator's decisions to tool approval outcomes (`allow`, `ask`, `deny`) for " +
		"`tool_guard` hooks.",
}

// descriptionNotes are appended to the schema description of a path.
var descriptionNotes = map[string]string{ //nolint:gosec // documentation, not credentials
	"AgentConfig.instruction_file": "The provider validates the configuration in memory, where relative files " +
		"cannot be read, so `instructionFile` needs `skipValidation: true`. Prefer reading the file in your " +
		"program and passing its contents as `instruction`.",
	"ModelConfig.token_key":     "Never put the secret itself in the configuration.",
	"ProviderConfig.token_key":  "Never put the secret itself in the configuration.",
	"EvaluatorConfig.token_key": "Never put the secret itself in the configuration.",
	"Config.flavors": "Each flavor is a free-form YAML patch with the same structure as the configuration " +
		"(snake_case keys), rendered verbatim.",
	"Toolset.config":    "Free-form, rendered verbatim.",
	"MCPToolset.config": "Free-form, rendered verbatim.",
}

// agentNameDoc documents the synthetic `name` input of Agent (the key of the `agents` map).
const agentNameDoc = "The agent name. It becomes the key under `agents:` and is how other agents reference " +
	"it in `subAgents`, `handoffs` and `forceHandoff`."

// extraDoc documents the `extra` escape hatch of every object type.
const extraDoc = "Additional fields that are not modelled by this provider version, merged verbatim into " +
	"this object's YAML mapping (snake_case keys). A key that is also set by a typed property overrides it."

// loaderOnly are properties docker-agent's loader accepts (and keeps) although
// agent-schema.json does not list them. They are typed too, so every key the loader
// understands can be set; the coverage test lists them as loader-only.
type loaderOnlyProp struct {
	key    string
	from   string // schema path to copy the property schema from
	schema string // or an inline JSON schema
}

var loaderOnly = map[string][]loaderOnlyProp{
	// `mcps` entries are decoded as complete toolsets of type mcp, so these toolset fields
	// work there as well (checked with `docker agent debug config`).
	"MCPToolset": {
		{key: "toon", from: "Toolset.toon"},
		{key: "readonly", from: "Toolset.readonly"},
		{key: "model", from: "Toolset.model"},
		{key: "timeout", from: "Toolset.timeout"},
		{key: "allow_private_ips", from: "Toolset.allow_private_ips"},
	},
	// API tool arguments are JSON schema properties (docker-agent keeps them as a free-form
	// map); the documentation uses `enum` to restrict a string argument.
	"ApiConfig.args{}": {
		{key: "enum", schema: `{"type": "array", "items": {"type": "string"}, "description": "Allowed values of the argument."}`},
	},
}

const loaderOnlyNote = "Accepted by docker-agent's loader, although agent-schema.json does not list it here."
