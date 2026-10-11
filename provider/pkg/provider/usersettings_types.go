// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"errors"

	"github.com/pulumi/pulumi-go-provider/infer"
)

// The types in this file are the typed inputs of UserSettings. They mirror docker-agent's
// pkg/userconfig types (and the pkg/config/latest types the user configuration embeds)
// field by field: every field carries the docker-agent YAML key in its `yaml` tag, which
// pkg/usersettings.Encode uses to build the document. usersettings_coverage_test.go fails
// when docker-agent adds a key that is not represented here.

// UserGlobalSettings is the `settings:` block of the user configuration.
type UserGlobalSettings struct {
	HideToolResults       *bool               `pulumi:"hideToolResults,optional"       yaml:"hide_tool_results"`
	ExpandThinking        *bool               `pulumi:"expandThinking,optional"        yaml:"expand_thinking"`
	SplitDiffView         *bool               `pulumi:"splitDiffView,optional"         yaml:"split_diff_view"`
	RenderImages          *bool               `pulumi:"renderImages,optional"          yaml:"render_images"`
	ShowBanner            *bool               `pulumi:"showBanner,optional"            yaml:"show_banner"`
	Theme                 *string             `pulumi:"theme,optional"                 yaml:"theme"`
	ThemeDark             *string             `pulumi:"themeDark,optional"             yaml:"theme_dark"`
	ThemeLight            *string             `pulumi:"themeLight,optional"            yaml:"theme_light"`
	Yolo                  *bool               `pulumi:"yolo,optional"                  yaml:"YOLO"`
	Safety                *string             `pulumi:"safety,optional"                yaml:"safety"`
	Lean                  *bool               `pulumi:"lean,optional"                  yaml:"lean"`
	TabTitleMaxLength     *int                `pulumi:"tabTitleMaxLength,optional"     yaml:"tab_title_max_length"`
	RestoreTabs           *bool               `pulumi:"restoreTabs,optional"           yaml:"restore_tabs"`
	Sound                 *bool               `pulumi:"sound,optional"                 yaml:"sound"`
	SoundThreshold        *int                `pulumi:"soundThreshold,optional"        yaml:"sound_threshold"`
	Snapshot              *bool               `pulumi:"snapshot,optional"              yaml:"snapshot"`
	CacheStablePrompts    *bool               `pulumi:"cacheStablePrompts,optional"    yaml:"cache_stable_prompts"`
	WarnOnCacheMiss       *bool               `pulumi:"warnOnCacheMiss,optional"       yaml:"warn_on_cache_miss"`
	Permissions           *UserPermissions    `pulumi:"permissions,optional"           yaml:"permissions"`
	Hooks                 *UserHooks          `pulumi:"hooks,optional"                 yaml:"hooks"`
	Keybindings           []UserKeybinding    `pulumi:"keybindings,optional"           yaml:"keybindings"`
	Layout                *UserLayoutSettings `pulumi:"layout,optional"                yaml:"layout"`
	BusySendMode          *string             `pulumi:"busySendMode,optional"          yaml:"busy_send_mode"`
	InterruptConfirmation *string             `pulumi:"interruptConfirmation,optional" yaml:"interrupt_confirmation"`
	Extra                 map[string]any      `pulumi:"extra,optional"                 yaml:",inline"`
}

// Annotate describes UserGlobalSettings.
func (s *UserGlobalSettings) Annotate(a infer.Annotator) {
	a.Describe(&s, "The `settings:` block of the docker-agent user configuration: preferences that apply to every "+
		"agent you run. Every field is optional; unset fields use docker-agent's defaults. See "+
		"https://docs.docker.com/ai/docker-agent/configuration/user-settings/.")
	a.Describe(&s.HideToolResults, "Hide tool call results in the TUI by default (`hide_tool_results`, default "+
		"`false`). Mirrors `--hide-tool-results` and the Ctrl+O toggle.")
	a.Describe(&s.ExpandThinking, "Start sessions with thinking and tool blocks expanded (`expand_thinking`, "+
		"default `false`).")
	a.Describe(&s.SplitDiffView, "Render file-edit diffs side by side instead of unified (`split_diff_view`, "+
		"default `true`).")
	a.Describe(&s.RenderImages, "Render images in the TUI with the Kitty graphics protocol (`render_images`, "+
		"default `true`).")
	a.Describe(&s.ShowBanner, "Show the ASCII-art startup banner in the TUI (`show_banner`, default `true`).")
	a.Describe(&s.Theme, "The TUI theme (`theme`, default `default`): a built-in theme or "+
		"`~/.cagent/themes/<name>.yaml`. `auto` follows the terminal background and picks `themeDark` or "+
		"`themeLight`.")
	a.Describe(&s.ThemeDark, "The theme used when `theme` is `auto` on a dark terminal (`theme_dark`, default "+
		"`default`).")
	a.Describe(&s.ThemeLight, "The theme used when `theme` is `auto` on a light terminal (`theme_light`, default "+
		"`default-light`).")
	a.Describe(&s.Yolo, "Auto-approve every tool call globally (`YOLO`, default `false`). Legacy alias for "+
		"`safety: autonomous`; `safety` wins when both are set.")
	a.Describe(&s.Safety, "The default safety mode for new sessions (`safety`): `strict`, `balanced`, "+
		"`restricted` or `autonomous`. It beats the safety defaults declared in agent configurations; explicit "+
		"`--safety`/`--yolo` flags and alias options beat it. Any other value fails docker-agent's config loading, "+
		"so the provider rejects it.")
	a.Describe(&s.Lean, "Make the lean TUI (minimal chrome) the default for interactive runs (`lean`, default "+
		"`false`).")
	a.Describe(&s.TabTitleMaxLength, "Maximum display length of tab titles before they are truncated "+
		"(`tab_title_max_length`, default `20`).")
	a.Describe(&s.RestoreTabs, "Restore previously open tabs when the TUI starts (`restore_tabs`, default "+
		"`false`).")
	a.Describe(&s.Sound, "Play a notification sound when a task succeeds or fails (`sound`, default `false`).")
	a.Describe(&s.SoundThreshold, "Minimum task duration in seconds before a success sound plays "+
		"(`sound_threshold`, default `10`). Failures always play.")
	a.Describe(&s.Snapshot, "Take automatic shadow-git snapshots at turn boundaries for every agent (`snapshot`, "+
		"default `false`).")
	a.Describe(&s.CacheStablePrompts, "Keep changing context (date, environment info, dynamic prompt files) out "+
		"of the frozen system prompt and append chronological updates instead, which improves prompt-cache hit "+
		"rates (`cache_stable_prompts`, default `false`).")
	a.Describe(&s.WarnOnCacheMiss, "Warn when a model call after the first one in a session reports no cached "+
		"input tokens (`warn_on_cache_miss`, default `false`).")
	a.Describe(&s.Permissions, "Global tool permission rules, merged with the permissions of every agent and "+
		"session. A global deny always wins.")
	a.Describe(&s.Hooks, "Global lifecycle hooks that run for every agent, in addition to the agent's own hooks, "+
		"`hooks.d/` drop-ins and `--hook-*` flags.")
	a.Describe(&s.Keybindings, "Remapped TUI keyboard shortcuts. docker-agent ignores unknown actions, malformed "+
		"keys and conflicts with a logged warning; the provider reports them as warnings during preview.")
	a.Describe(&s.Layout, "Sidebar position and section visibility of the TUI.")
	a.Describe(&s.BusySendMode, "What happens to a message sent while the agent works (`busy_send_mode`): "+
		"`steer` (default) injects it into the running turn, `queue` holds it until the turn ends.")
	a.Describe(&s.InterruptConfirmation, "How Esc interrupts a running stream (`interrupt_confirmation`): "+
		"`always` (default) asks for confirmation, `double-tap` requires pressing Esc twice within a second, "+
		"`none` interrupts immediately.")
	a.Describe(&s.Extra, "Additional `settings:` keys that this provider version does not model, for example "+
		"settings added by a newer docker-agent. They are written verbatim (snake_case keys) and must not repeat "+
		"a typed setting.")
}

// UserLayoutSettings is `settings.layout`.
type UserLayoutSettings struct {
	SidebarPosition  *string `pulumi:"sidebarPosition,optional"  yaml:"sidebar_position"`
	SectionSpacing   *string `pulumi:"sectionSpacing,optional"   yaml:"section_spacing"`
	SidebarInfoMode  *string `pulumi:"sidebarInfoMode,optional"  yaml:"sidebar_info_mode"`
	ActiveAgentsOnly *bool   `pulumi:"activeAgentsOnly,optional" yaml:"active_agents_only"`
	HideSessionPath  *bool   `pulumi:"hideSessionPath,optional"  yaml:"hide_session_path"`
	HideUsage        *bool   `pulumi:"hideUsage,optional"        yaml:"hide_usage"`
	HideAgents       *bool   `pulumi:"hideAgents,optional"       yaml:"hide_agents"`
	HideTools        *bool   `pulumi:"hideTools,optional"        yaml:"hide_tools"`
	ShowPlans        *bool   `pulumi:"showPlans,optional"        yaml:"show_plans"`
	HideTodos        *bool   `pulumi:"hideTodos,optional"        yaml:"hide_todos"`
}

// Annotate describes UserLayoutSettings.
func (l *UserLayoutSettings) Annotate(a infer.Annotator) {
	a.Describe(&l, "The TUI layout (`settings.layout`). Unset fields keep the default layout: sidebar on the "+
		"right, every section except Plans visible, normal spacing.")
	a.Describe(&l.SidebarPosition, "Where the sidebar goes: `right` (default), `left`, `top` or `bottom`. "+
		"Top and bottom render a compact horizontal band.")
	a.Describe(&l.SectionSpacing, "Blank space between sidebar sections: `normal` (default), `compact` or "+
		"`relaxed`.")
	a.Describe(&l.SidebarInfoMode, "How the Agents section renders each agent: `compact` (default, two lines) "+
		"or `detailed` (cards with effort, context and cost metrics).")
	a.Describe(&l.ActiveAgentsOnly, "Show only the agents active in the current session in the Agents section.")
	a.Describe(&l.HideSessionPath, "Hide the working directory (session path) line, including its git branch.")
	a.Describe(&l.HideUsage, "Hide the token usage section.")
	a.Describe(&l.HideAgents, "Hide the Agents section.")
	a.Describe(&l.HideTools, "Hide the Tools section.")
	a.Describe(&l.ShowPlans, "Show shared plans in the sidebar (default `false`).")
	a.Describe(&l.HideTodos, "Hide the Todos section.")
}

// UserKeybinding is one entry of `settings.keybindings`.
type UserKeybinding struct {
	Action string   `pulumi:"action" yaml:"action"`
	Keys   []string `pulumi:"keys"   yaml:"keys"`
}

// Annotate describes UserKeybinding.
func (k *UserKeybinding) Annotate(a infer.Annotator) {
	a.Describe(&k, "Binds a TUI action to one or more keys, replacing the action's default keys.")
	a.Describe(&k.Action, "The action to remap: `quit`, `switch_focus`, `commands`, `help`, `toggle_yolo`, "+
		"`toggle_hide_tool_results`, `cycle_agent`, `model_picker`, `clear_queue`, `suspend`, `toggle_sidebar`, "+
		"`edit_external`, `history_search`, `editor_send` or `editor_newline`.")
	a.Describe(&k.Keys, "The key combinations in Bubble Tea format, for example `ctrl+k`, `alt+enter` or `f2`.")
}

// UserPermissions is `settings.permissions`.
type UserPermissions struct {
	Allow []string `pulumi:"allow,optional" yaml:"allow"`
	Ask   []string `pulumi:"ask,optional"   yaml:"ask"`
	Deny  []string `pulumi:"deny,optional"  yaml:"deny"`
}

// Annotate describes UserPermissions.
func (p *UserPermissions) Annotate(a infer.Annotator) {
	a.Describe(&p, "Global tool permission patterns. Patterns use glob syntax, for example `shell`, `read_*`, "+
		"`mcp:github:*` or `shell:cmd=git status*`. Deny is evaluated first, then allow, then ask.")
	a.Describe(&p.Allow, "Tools that run without asking.")
	a.Describe(&p.Ask, "Tools that always ask, even when they are normally auto-approved.")
	a.Describe(&p.Deny, "Tools that are always rejected.")
}

// UserAlias is an entry of `aliases`.
type UserAlias struct {
	Path            string  `pulumi:"path"                     yaml:"path"`
	Yolo            *bool   `pulumi:"yolo,optional"            yaml:"yolo"`
	Safety          *string `pulumi:"safety,optional"          yaml:"safety"`
	Model           *string `pulumi:"model,optional"           yaml:"model"`
	HideToolResults *bool   `pulumi:"hideToolResults,optional" yaml:"hide_tool_results"`
	Sandbox         *bool   `pulumi:"sandbox,optional"         yaml:"sandbox"`
}

// Annotate describes UserAlias.
func (u *UserAlias) Annotate(a infer.Annotator) {
	a.Describe(&u, "An agent alias, as created by `docker agent alias add`: `docker agent run <alias>` runs "+
		"`path` with the alias's defaults. Alias names may contain letters, digits, `-` and `_`, and start "+
		"with a letter or digit.")
	a.Describe(&u.Path, "The agent file path or OCI reference, for example `./agent.yaml` or "+
		"`docker.io/myorg/agent:latest`.")
	a.Describe(&u.Yolo, "Auto-approve every tool call when running the alias. Legacy alias for "+
		"`safety: autonomous`.")
	a.Describe(&u.Safety, "The safety mode used when running the alias without `--safety`/`--yolo`: `strict`, "+
		"`balanced`, `restricted` or `autonomous`.")
	a.Describe(&u.Model, "Override the agent's model, in the format `[agent=]provider/model`.")
	a.Describe(&u.HideToolResults, "Hide tool call results in the TUI when running the alias.")
	a.Describe(&u.Sandbox, "Run the alias inside a Docker sandbox by default.")
}

// UserBoard is the `board:` section.
type UserBoard struct {
	Projects []UserBoardProject `pulumi:"projects,optional" yaml:"projects"`
	Columns  []UserBoardColumn  `pulumi:"columns,optional"  yaml:"columns"`
}

// Annotate describes UserBoard.
func (b *UserBoard) Annotate(a infer.Annotator) {
	a.Describe(&b, "Configuration of the `docker agent board` Kanban TUI.")
	a.Describe(&b.Projects, "The repositories cards can be created against.")
	a.Describe(&b.Columns, "The board pipeline. When a card moves forward into a column, the column's prompt is "+
		"sent to the card's agent. Leave it unset to keep the default pipeline (Dev, Review, Push, Done).")
}

// UserBoardProject is an entry of `board.projects`.
type UserBoardProject struct {
	Name  string  `pulumi:"name"           yaml:"name"`
	Path  string  `pulumi:"path"           yaml:"path"`
	Agent *string `pulumi:"agent,optional" yaml:"agent"`
}

// Annotate describes UserBoardProject.
func (b *UserBoardProject) Annotate(a infer.Annotator) {
	a.Describe(&b, "A repository the board can create cards against.")
	a.Describe(&b.Name, "The display name shown on cards.")
	a.Describe(&b.Path, "The repository's local path.")
	a.Describe(&b.Agent, "The agent reference launched for the project's cards. Defaults to the built-in root "+
		"agent.")
}

// UserBoardColumn is an entry of `board.columns`.
type UserBoardColumn struct {
	ID     *string `pulumi:"id,optional"     yaml:"id"`
	Name   *string `pulumi:"name,optional"   yaml:"name"`
	Emoji  *string `pulumi:"emoji,optional"  yaml:"emoji"`
	Prompt *string `pulumi:"prompt,optional" yaml:"prompt"`
}

// Annotate describes UserBoardColumn.
func (b *UserBoardColumn) Annotate(a infer.Annotator) {
	a.Describe(&b, "A column of the board pipeline. Set `id`, `name` or both.")
	a.Describe(&b.ID, "The column identifier. Derived from `name` when unset; must be unique.")
	a.Describe(&b.Name, "The display name. Defaults to `id`.")
	a.Describe(&b.Emoji, "An emoji shown next to the name.")
	a.Describe(&b.Prompt, "The prompt sent to the card's agent when a card moves into this column.")
}

// UserCredentialHelper is the `credential_helper:` section.
type UserCredentialHelper struct {
	Command string   `pulumi:"command"       yaml:"command"`
	Args    []string `pulumi:"args,optional" yaml:"args"`
}

// Annotate describes UserCredentialHelper.
func (c *UserCredentialHelper) Annotate(a infer.Annotator) {
	a.Describe(&c, "An external command that prints a Docker token (`DOCKER_TOKEN`) on stdout, used instead "+
		"of Docker Desktop's credentials.")
	a.Describe(&c.Command, "The command to run.")
	a.Describe(&c.Args, "The command's arguments.")
}

// UserHooks is `settings.hooks`. Its fields mirror docker-agent's HooksConfig.
type UserHooks struct {
	PromptFileGuard            []UserHook        `pulumi:"promptFileGuard,optional"            yaml:"prompt_file_guard"`
	SkillContentGuard          []UserHook        `pulumi:"skillContentGuard,optional"          yaml:"skill_content_guard"`
	PreToolUse                 []UserHookMatcher `pulumi:"preToolUse,optional"                 yaml:"pre_tool_use"`
	PostToolUse                []UserHookMatcher `pulumi:"postToolUse,optional"                yaml:"post_tool_use"`
	PermissionRequest          []UserHookMatcher `pulumi:"permissionRequest,optional"          yaml:"permission_request"`
	SessionStart               []UserHook        `pulumi:"sessionStart,optional"               yaml:"session_start"`
	UserPromptSubmit           []UserHook        `pulumi:"userPromptSubmit,optional"           yaml:"user_prompt_submit"`
	UserSteeringMessagesSubmit []UserHook        `pulumi:"userSteeringMessagesSubmit,optional" yaml:"user_steering_messages_submit"`
	UserFollowupSubmit         []UserHook        `pulumi:"userFollowupSubmit,optional"         yaml:"user_followup_submit"`
	TurnStart                  []UserHook        `pulumi:"turnStart,optional"                  yaml:"turn_start"`
	TurnEnd                    []UserHook        `pulumi:"turnEnd,optional"                    yaml:"turn_end"`
	BeforeLlmCall              []UserHook        `pulumi:"beforeLlmCall,optional"              yaml:"before_llm_call"`
	AfterLlmCall               []UserHook        `pulumi:"afterLlmCall,optional"               yaml:"after_llm_call"`
	SessionEnd                 []UserHook        `pulumi:"sessionEnd,optional"                 yaml:"session_end"`
	PreCompact                 []UserHook        `pulumi:"preCompact,optional"                 yaml:"pre_compact"`
	SubagentStop               []UserHook        `pulumi:"subagentStop,optional"               yaml:"subagent_stop"`
	OnUserInput                []UserHook        `pulumi:"onUserInput,optional"                yaml:"on_user_input"`
	Stop                       []UserHook        `pulumi:"stop,optional"                       yaml:"stop"`
	Notification               []UserHook        `pulumi:"notification,optional"               yaml:"notification"`
	OnError                    []UserHook        `pulumi:"onError,optional"                    yaml:"on_error"`
	OnMaxIterations            []UserHook        `pulumi:"onMaxIterations,optional"            yaml:"on_max_iterations"`
	OnAgentSwitch              []UserHook        `pulumi:"onAgentSwitch,optional"              yaml:"on_agent_switch"`
	OnSessionResume            []UserHook        `pulumi:"onSessionResume,optional"            yaml:"on_session_resume"`
	OnToolApprovalDecision     []UserHook        `pulumi:"onToolApprovalDecision,optional"     yaml:"on_tool_approval_decision"`
	BeforeCompaction           []UserHook        `pulumi:"beforeCompaction,optional"           yaml:"before_compaction"`
	AfterCompaction            []UserHook        `pulumi:"afterCompaction,optional"            yaml:"after_compaction"`
	ToolResponseTransform      []UserHookMatcher `pulumi:"toolResponseTransform,optional"      yaml:"tool_response_transform"`
	ToolInputTransform         []UserHookMatcher `pulumi:"toolInputTransform,optional"         yaml:"tool_input_transform"`
	ToolGuard                  []UserHookMatcher `pulumi:"toolGuard,optional"                  yaml:"tool_guard"`
	BeforeAgentRun             []UserHook        `pulumi:"beforeAgentRun,optional"             yaml:"before_agent_run"`
	AfterAgentComplete         []UserHook        `pulumi:"afterAgentComplete,optional"         yaml:"after_agent_complete"`
	WorktreeCreate             []UserHook        `pulumi:"worktreeCreate,optional"             yaml:"worktree_create"`
}

// Annotate describes UserHooks.
func (h *UserHooks) Annotate(a infer.Annotator) {
	a.Describe(&h, "Global lifecycle hooks, keyed by event. Tool events (`preToolUse`, `postToolUse`, "+
		"`permissionRequest`, `toolResponseTransform`, `toolInputTransform`, `toolGuard`) take matchers that "+
		"select tools by name; every other event takes a list of hooks. See "+
		"https://docs.docker.com/ai/docker-agent/configuration/hooks/.")
	a.Describe(&h.PromptFileGuard, "Check loaded prompt-file instructions before use (`prompt_file_guard`).")
	a.Describe(&h.SkillContentGuard, "Check raw skill text before it is expanded or delivered "+
		"(`skill_content_guard`).")
	a.Describe(&h.PreToolUse, "Run before a tool executes (`pre_tool_use`). Hooks can block the call or rewrite "+
		"its arguments.")
	a.Describe(&h.PostToolUse, "Run after a tool completes, successfully or not (`post_tool_use`).")
	a.Describe(&h.PermissionRequest, "Run just before the user would be asked to approve a tool call "+
		"(`permission_request`). Hooks can allow or deny without prompting.")
	a.Describe(&h.SessionStart, "Run when a session begins (`session_start`).")
	a.Describe(&h.UserPromptSubmit, "Run once per user message before the first model call of the turn "+
		"(`user_prompt_submit`).")
	a.Describe(&h.UserSteeringMessagesSubmit, "Run when messages sent while the agent was working are added to "+
		"the session (`user_steering_messages_submit`).")
	a.Describe(&h.UserFollowupSubmit, "Run when a queued follow-up message starts a new turn "+
		"(`user_followup_submit`).")
	a.Describe(&h.TurnStart, "Run at the start of every agent turn (`turn_start`). Output is added as transient "+
		"context for that turn.")
	a.Describe(&h.TurnEnd, "Run when an agent turn finishes, for any reason (`turn_end`).")
	a.Describe(&h.BeforeLlmCall, "Run just before each model call (`before_llm_call`).")
	a.Describe(&h.AfterLlmCall, "Run just after each successful model call (`after_llm_call`).")
	a.Describe(&h.SessionEnd, "Run when a session ends (`session_end`).")
	a.Describe(&h.PreCompact, "Run before the session transcript is compacted (`pre_compact`).")
	a.Describe(&h.SubagentStop, "Run when a sub-agent finishes (`subagent_stop`).")
	a.Describe(&h.OnUserInput, "Run when the agent needs user input (`on_user_input`).")
	a.Describe(&h.Stop, "Run when the model finishes responding and hands control back (`stop`).")
	a.Describe(&h.Notification, "Run when the agent sends a notification (`notification`).")
	a.Describe(&h.OnError, "Run when the runtime hits an error during a turn (`on_error`).")
	a.Describe(&h.OnMaxIterations, "Run when the agent reaches its `max_iterations` limit "+
		"(`on_max_iterations`).")
	a.Describe(&h.OnAgentSwitch, "Run whenever the active agent changes (`on_agent_switch`).")
	a.Describe(&h.OnSessionResume, "Run when the user lets the agent continue past `max_iterations` "+
		"(`on_session_resume`).")
	a.Describe(&h.OnToolApprovalDecision, "Run after the approval chain decides on a tool call "+
		"(`on_tool_approval_decision`).")
	a.Describe(&h.BeforeCompaction, "Run immediately before a compaction; hooks can veto it or supply the "+
		"summary (`before_compaction`).")
	a.Describe(&h.AfterCompaction, "Run after a successful compaction (`after_compaction`).")
	a.Describe(&h.ToolResponseTransform, "Rewrite a tool's output before it is recorded "+
		"(`tool_response_transform`).")
	a.Describe(&h.ToolInputTransform, "Rewrite tool arguments before the approval pipeline runs "+
		"(`tool_input_transform`).")
	a.Describe(&h.ToolGuard, "Decide on a tool call before the approval pipeline; the verdict cannot be "+
		"bypassed by any safety mode (`tool_guard`).")
	a.Describe(&h.BeforeAgentRun, "Run once per agent activation and optionally route to another agent "+
		"(`before_agent_run`).")
	a.Describe(&h.AfterAgentComplete, "Run after an agent completes and optionally route to another agent "+
		"(`after_agent_complete`).")
	a.Describe(&h.WorktreeCreate, "Run inside a new git worktree created by `docker agent run --worktree` "+
		"(`worktree_create`).")
}

// UserHookMatcher selects tools for a tool event.
type UserHookMatcher struct {
	Matcher     *string    `pulumi:"matcher,optional"     yaml:"matcher"`
	Hooks       []UserHook `pulumi:"hooks"                yaml:"hooks"`
	PreemptYolo *bool      `pulumi:"preemptYolo,optional" yaml:"preempt_yolo"`
}

// Annotate describes UserHookMatcher.
func (m *UserHookMatcher) Annotate(a infer.Annotator) {
	a.Describe(&m, "Hooks for the tools whose names match `matcher`.")
	a.Describe(&m.Matcher, "A case-sensitive regular expression over tool names, for example `shell|edit_file`. "+
		"`*` matches every tool.")
	a.Describe(&m.Hooks, "The hooks to run when the matcher matches.")
	a.Describe(&m.PreemptYolo, "Run this `preToolUse` entry before the approval pipeline, so its deny or ask "+
		"verdict cannot be bypassed by any safety mode. Only valid on `preToolUse`.")
}

// UserHook mirrors docker-agent's HookDefinition.
type UserHook struct {
	Name            *string                  `pulumi:"name,optional"            yaml:"name"`
	Type            *string                  `pulumi:"type,optional"            yaml:"type"`
	Evaluator       *string                  `pulumi:"evaluator,optional"       yaml:"evaluator"`
	EvaluatorPolicy *UserHookEvaluatorPolicy `pulumi:"evaluatorPolicy,optional" yaml:"evaluator_policy"`
	RoutingPolicy   *UserHookRoutingPolicy   `pulumi:"routingPolicy,optional"   yaml:"routing_policy"`
	Command         *string                  `pulumi:"command,optional"         yaml:"command"`
	Args            []string                 `pulumi:"args,optional"            yaml:"args"`
	Timeout         *int                     `pulumi:"timeout,optional"         yaml:"timeout"`
	Env             map[string]string        `pulumi:"env,optional"             yaml:"env"`
	WorkingDir      *string                  `pulumi:"workingDir,optional"      yaml:"working_dir"`
	OnError         *string                  `pulumi:"onError,optional"         yaml:"on_error"`
	StrictOutput    *bool                    `pulumi:"strictOutput,optional"    yaml:"strict_output"`
	Model           *string                  `pulumi:"model,optional"           yaml:"model"`
	Prompt          *string                  `pulumi:"prompt,optional"          yaml:"prompt"`
	SystemPrompt    *string                  `pulumi:"systemPrompt,optional"    yaml:"system_prompt"`
	Schema          *string                  `pulumi:"schema,optional"          yaml:"schema"`
}

// Annotate describes UserHook.
func (h *UserHook) Annotate(a infer.Annotator) {
	a.Describe(&h, "A hook: a shell command, a built-in function, an evaluator or a model call.")
	a.Describe(&h.Name, "A label for logs and runtime events.")
	a.Describe(&h.Type, "The hook type: `command` (default) runs `command` in a shell, `builtin` runs the "+
		"built-in function named by `command` (for example `add_date` or `redact_secrets`), `evaluator` asks "+
		"a named evaluator, `model` asks an LLM.")
	a.SetDefault(&h.Type, "command")
	a.Describe(&h.Evaluator, "The evaluator an `evaluator` hook uses.")
	a.Describe(&h.EvaluatorPolicy, "How a `toolGuard` evaluator hook turns assessments into decisions.")
	a.Describe(&h.RoutingPolicy, "How a `beforeAgentRun`/`afterAgentComplete` evaluator hook routes to agents.")
	a.Describe(&h.Command, "The shell command (`command` hooks) or the built-in name (`builtin` hooks).")
	a.Describe(&h.Args, "Arguments passed to the hook handler, for example to built-ins.")
	a.Describe(&h.Timeout, "Execution timeout in seconds (default `60`).")
	a.Describe(&h.Env, "Environment variables added for this hook only.")
	a.Describe(&h.WorkingDir, "The working directory of the hook.")
	a.Describe(&h.OnError, "What a hook failure does: `warn` (default), `ignore` or `block`.")
	a.Describe(&h.StrictOutput, "Require JSON output and validate it against the event's capabilities.")
	a.Describe(&h.Model, "The model (`provider/model`) a `model` hook calls.")
	a.Describe(&h.Prompt, "The prompt template of a `model` hook (Go text/template over the hook input).")
	a.Describe(&h.SystemPrompt, "Overrides the default system message of a `model` hook.")
	a.Describe(&h.Schema, "How a `model` hook's reply is interpreted, for example `pre_tool_use_decision` or "+
		"`guard_decision`. Empty returns the reply as additional context.")
}

// UserHookEvaluatorPolicy mirrors docker-agent's EvaluatorPolicy.
type UserHookEvaluatorPolicy struct {
	Decisions      map[string]string `pulumi:"decisions"      yaml:"decisions"`
	MinProbability float64           `pulumi:"minProbability" yaml:"min_probability"`
	Fallback       string            `pulumi:"fallback"       yaml:"fallback"`
}

// Annotate describes UserHookEvaluatorPolicy.
func (p *UserHookEvaluatorPolicy) Annotate(a infer.Annotator) {
	a.Describe(&p, "Maps an evaluator's assessments to tool-guard decisions.")
	a.Describe(&p.Decisions, "Assessment outcome to decision (`allow`, `ask` or `deny`). Boolean evaluators use "+
		"the keys `true` and `false`.")
	a.Describe(&p.MinProbability, "The lowest probability (greater than 0, at most 1) at which a decision "+
		"applies.")
	a.Describe(&p.Fallback, "The decision when the assessment is uncertain: `ask` or `deny`.")
}

// UserHookRoutingPolicy mirrors docker-agent's RoutingPolicy.
type UserHookRoutingPolicy struct {
	Routes         map[string]string `pulumi:"routes"         yaml:"routes"`
	MinProbability float64           `pulumi:"minProbability" yaml:"min_probability"`
}

// Annotate describes UserHookRoutingPolicy.
func (p *UserHookRoutingPolicy) Annotate(a infer.Annotator) {
	a.Describe(&p, "Maps a choice evaluator's outcomes to agents.")
	a.Describe(&p.Routes, "Evaluator choice to agent name.")
	a.Describe(&p.MinProbability, "The lowest probability (greater than 0, at most 1) at which a route applies.")
}

// UserProvider mirrors docker-agent's ProviderConfig (the `providers:` section).
type UserProvider struct {
	Provider          *string             `pulumi:"provider,optional"          yaml:"provider"`
	APIType           *string             `pulumi:"apiType,optional"           yaml:"api_type"`
	BaseURL           *string             `pulumi:"baseUrl,optional"           yaml:"base_url"`
	UnloadAPI         *string             `pulumi:"unloadApi,optional"         yaml:"unload_api"`
	TokenKey          *string             `pulumi:"tokenKey,optional"          yaml:"token_key"`
	Temperature       *float64            `pulumi:"temperature,optional"       yaml:"temperature"`
	MaxTokens         *int                `pulumi:"maxTokens,optional"         yaml:"max_tokens"`
	TopP              *float64            `pulumi:"topP,optional"              yaml:"top_p"`
	FrequencyPenalty  *float64            `pulumi:"frequencyPenalty,optional"  yaml:"frequency_penalty"`
	PresencePenalty   *float64            `pulumi:"presencePenalty,optional"   yaml:"presence_penalty"`
	ParallelToolCalls *bool               `pulumi:"parallelToolCalls,optional" yaml:"parallel_tool_calls"`
	ProviderOpts      map[string]any      `pulumi:"providerOpts,optional"      yaml:"provider_opts"`
	TrackUsage        *bool               `pulumi:"trackUsage,optional"        yaml:"track_usage"`
	ThinkingBudget    *UserThinkingBudget `pulumi:"thinkingBudget,optional"    yaml:"thinking_budget"`
	TaskBudget        *UserTaskBudget     `pulumi:"taskBudget,optional"        yaml:"task_budget"`
	Auth              *UserModelAuth      `pulumi:"auth,optional"              yaml:"auth"`
	CompactionModel   *string             `pulumi:"compactionModel,optional"   yaml:"compaction_model"`
}

// Annotate describes UserProvider.
func (p *UserProvider) Annotate(a infer.Annotator) {
	a.Describe(&p, "A reusable model provider (for example an OpenAI-compatible endpoint), merged into every "+
		"agent configuration docker-agent loads. Definitions in an agent file win on name conflicts.")
	a.Describe(&p.Provider, "The underlying provider type: `openai` (default), `anthropic`, `google`, "+
		"`amazon-bedrock`, `dmr` or a built-in alias.")
	a.Describe(&p.APIType, "The API flavour of OpenAI-compatible providers: `openai_chatcompletions` (default) "+
		"or `openai_responses`.")
	a.Describe(&p.BaseURL, "The API base URL. Required for OpenAI-compatible providers.")
	a.Describe(&p.UnloadAPI, "Path or URL of the provider's model-unload endpoint, used by the `unload` built-in "+
		"hook. Leave unset for cloud providers.")
	a.Describe(&p.TokenKey, "The name of the environment variable that holds the API token (not the token "+
		"itself).")
	a.Describe(&p.Temperature, "Default sampling temperature.")
	a.Describe(&p.MaxTokens, "Default maximum number of output tokens.")
	a.Describe(&p.TopP, "Default top-p sampling parameter.")
	a.Describe(&p.FrequencyPenalty, "Default frequency penalty.")
	a.Describe(&p.PresencePenalty, "Default presence penalty.")
	a.Describe(&p.ParallelToolCalls, "Whether parallel tool calls are enabled by default.")
	a.Describe(&p.ProviderOpts, "Provider-specific options, written verbatim (snake_case keys).")
	a.Describe(&p.TrackUsage, "Whether token usage is tracked by default.")
	a.Describe(&p.ThinkingBudget, "Default reasoning effort or token budget.")
	a.Describe(&p.TaskBudget, "Default total token budget for an agentic task (Anthropic `task_budget`).")
	a.Describe(&p.Auth, "A non-API-key authentication scheme, such as Anthropic workload identity federation.")
	a.Describe(&p.CompactionModel, "Default compaction (summary) model for agents using this provider.")
}

// UserModel mirrors docker-agent's ModelConfig (`default_model`).
type UserModel struct {
	Provider            *string                      `pulumi:"provider,optional"            yaml:"provider"`
	Model               *string                      `pulumi:"model,optional"               yaml:"model"`
	Description         *string                      `pulumi:"description,optional"         yaml:"description"`
	Temperature         *float64                     `pulumi:"temperature,optional"         yaml:"temperature"`
	MaxTokens           *int                         `pulumi:"maxTokens,optional"           yaml:"max_tokens"`
	TopP                *float64                     `pulumi:"topP,optional"                yaml:"top_p"`
	FrequencyPenalty    *float64                     `pulumi:"frequencyPenalty,optional"    yaml:"frequency_penalty"`
	PresencePenalty     *float64                     `pulumi:"presencePenalty,optional"     yaml:"presence_penalty"`
	BaseURL             *string                      `pulumi:"baseUrl,optional"             yaml:"base_url"`
	ParallelToolCalls   *bool                        `pulumi:"parallelToolCalls,optional"   yaml:"parallel_tool_calls"`
	TokenKey            *string                      `pulumi:"tokenKey,optional"            yaml:"token_key"`
	BypassModelsGateway *bool                        `pulumi:"bypassModelsGateway,optional" yaml:"bypass_models_gateway"`
	ProviderOpts        map[string]any               `pulumi:"providerOpts,optional"        yaml:"provider_opts"`
	TrackUsage          *bool                        `pulumi:"trackUsage,optional"          yaml:"track_usage"`
	ThinkingBudget      *UserThinkingBudget          `pulumi:"thinkingBudget,optional"      yaml:"thinking_budget"`
	TaskBudget          *UserTaskBudget              `pulumi:"taskBudget,optional"          yaml:"task_budget"`
	Auth                *UserModelAuth               `pulumi:"auth,optional"                yaml:"auth"`
	Routing             []UserModelRoutingRule       `pulumi:"routing,optional"             yaml:"routing"`
	FirstAvailable      []string                     `pulumi:"firstAvailable,optional"      yaml:"first_available"`
	TitleModel          *string                      `pulumi:"titleModel,optional"          yaml:"title_model"`
	CompactionModel     *string                      `pulumi:"compactionModel,optional"     yaml:"compaction_model"`
	CompactionThreshold *float64                     `pulumi:"compactionThreshold,optional" yaml:"compaction_threshold"`
	Capabilities        *UserModelCapabilities       `pulumi:"capabilities,optional"        yaml:"capabilities"`
	OutputCapabilities  *UserModelOutputCapabilities `pulumi:"outputCapabilities,optional"  yaml:"output_capabilities"`
	Cost                *UserModelCost               `pulumi:"cost,optional"                yaml:"cost"`
}

// Annotate describes UserModel.
func (m *UserModel) Annotate(a infer.Annotator) {
	a.Describe(&m, "A model definition with the same fields as an entry of an agent file's `models:` section. "+
		"A model with only `provider` and `model` is written in the `provider/model` shorthand.")
	a.Describe(&m.Provider, "The provider, for example `openai`, `anthropic`, `google`, `amazon-bedrock`, `dmr` "+
		"or the name of a custom provider.")
	a.Describe(&m.Model, "The model name, for example `gpt-5-mini` or `claude-sonnet-4-5`.")
	a.Describe(&m.Description, "A description of the model.")
	a.Describe(&m.Temperature, "Sampling temperature.")
	a.Describe(&m.MaxTokens, "Maximum number of output tokens.")
	a.Describe(&m.TopP, "Top-p sampling parameter.")
	a.Describe(&m.FrequencyPenalty, "Frequency penalty.")
	a.Describe(&m.PresencePenalty, "Presence penalty.")
	a.Describe(&m.BaseURL, "A custom API base URL. Implies `bypassModelsGateway`.")
	a.Describe(&m.ParallelToolCalls, "Whether parallel tool calls are enabled.")
	a.Describe(&m.TokenKey, "The name of the environment variable that holds the API token.")
	a.Describe(&m.BypassModelsGateway, "Connect directly to the provider even when a models gateway is "+
		"configured.")
	a.Describe(&m.ProviderOpts, "Provider-specific options, written verbatim (snake_case keys), for example "+
		"`context_size`.")
	a.Describe(&m.TrackUsage, "Whether token usage is tracked.")
	a.Describe(&m.ThinkingBudget, "Reasoning effort or token budget.")
	a.Describe(&m.TaskBudget, "Total token budget for an agentic task (Anthropic `task_budget`).")
	a.Describe(&m.Auth, "A non-API-key authentication scheme.")
	a.Describe(&m.Routing, "Rules that route requests to other models based on example prompts.")
	a.Describe(&m.FirstAvailable, "Candidate models; the first usable one is selected. Excludes most other "+
		"fields.")
	a.Describe(&m.TitleModel, "The model used to generate session titles.")
	a.Describe(&m.CompactionModel, "The model used to summarize the session during compaction.")
	a.Describe(&m.CompactionThreshold, "The fraction of the context window (greater than 0, at most 1) at which "+
		"compaction starts.")
	a.Describe(&m.Capabilities, "Input modalities the model supports, overriding the built-in catalog.")
	a.Describe(&m.OutputCapabilities, "Output modalities the model supports.")
	a.Describe(&m.Cost, "Prices per million tokens, used for cost tracking.")
}

// UserThinkingBudget is a thinking budget. docker-agent writes it as a scalar: an effort
// level or a token count.
type UserThinkingBudget struct {
	Effort *string `pulumi:"effort,optional" yaml:"effort"`
	Tokens *int    `pulumi:"tokens,optional" yaml:"tokens"`
}

// Annotate describes UserThinkingBudget.
func (b *UserThinkingBudget) Annotate(a infer.Annotator) {
	a.Describe(&b, "A reasoning budget. Set exactly one of `effort` and `tokens`.")
	a.Describe(&b.Effort, "An effort level: `none`, `minimal`, `low`, `medium`, `high`, `xhigh`, `max`, `adaptive` "+
		"or `adaptive/<effort>`.")
	a.Describe(&b.Tokens, "A token budget. `0` disables thinking, `-1` lets the provider decide.")
}

// YAMLValue returns the scalar docker-agent expects.
func (b UserThinkingBudget) YAMLValue() (any, error) {
	switch {
	case b.Effort != nil && b.Tokens != nil:
		return nil, errors.New("thinkingBudget: set either effort or tokens, not both")
	case b.Effort != nil:
		return *b.Effort, nil
	case b.Tokens != nil:
		return *b.Tokens, nil
	}
	return nil, errors.New("thinkingBudget: set effort or tokens")
}

// UserTaskBudget mirrors docker-agent's TaskBudget.
type UserTaskBudget struct {
	Type  *string `pulumi:"type,optional" yaml:"type"`
	Total int     `pulumi:"total"         yaml:"total"`
}

// Annotate describes UserTaskBudget.
func (b *UserTaskBudget) Annotate(a infer.Annotator) {
	a.Describe(&b, "A total budget for an agentic task. `0` disables it.")
	a.Describe(&b.Type, "The budget kind. Only `tokens` (default) is supported.")
	a.Describe(&b.Total, "The total budget, in tokens.")
}

// UserModelAuth mirrors docker-agent's AuthConfig.
type UserModelAuth struct {
	Type                       string              `pulumi:"type"                                yaml:"type"`
	WorkloadIdentityFederation *UserFederationAuth `pulumi:"workloadIdentityFederation,optional" yaml:"workload_identity_federation"`
}

// Annotate describes UserModelAuth.
func (m *UserModelAuth) Annotate(a infer.Annotator) {
	a.Describe(&m, "A non-API-key authentication scheme.")
	a.Describe(&m.Type, "The scheme. Currently `workload_identity_federation` (Anthropic).")
	a.Describe(&m.WorkloadIdentityFederation, "Workload identity federation settings.")
}

// UserFederationAuth mirrors docker-agent's FederationAuthConfig.
type UserFederationAuth struct {
	FederationRuleID string                   `pulumi:"federationRuleId"          yaml:"federation_rule_id"`
	OrganizationID   string                   `pulumi:"organizationId"            yaml:"organization_id"`
	ServiceAccountID *string                  `pulumi:"serviceAccountId,optional" yaml:"service_account_id"`
	IdentityToken    *UserIdentityTokenSource `pulumi:"identityToken,optional"    yaml:"identity_token"`
}

// Annotate describes UserFederationAuth.
func (f *UserFederationAuth) Annotate(a infer.Annotator) {
	a.Describe(&f, "Exchanges an external identity token for provider credentials.")
	a.Describe(&f.FederationRuleID, "The federation rule identifier.")
	a.Describe(&f.OrganizationID, "The organization identifier.")
	a.Describe(&f.ServiceAccountID, "The service account identifier.")
	a.Describe(&f.IdentityToken, "Where the identity token comes from.")
}

// UserIdentityTokenSource mirrors docker-agent's IdentityTokenSourceConfig.
type UserIdentityTokenSource struct {
	File          *string           `pulumi:"file,optional"          yaml:"file"`
	Env           *string           `pulumi:"env,optional"           yaml:"env"`
	Command       []string          `pulumi:"command,optional"       yaml:"command"`
	URL           *string           `pulumi:"url,optional"           yaml:"url"`
	Headers       map[string]string `pulumi:"headers,optional"       yaml:"headers"`
	ResponseField *string           `pulumi:"responseField,optional" yaml:"response_field"`
}

// Annotate describes UserIdentityTokenSource.
func (s *UserIdentityTokenSource) Annotate(a infer.Annotator) {
	a.Describe(&s, "The source of an identity token. Set one of `file`, `env`, `command` or `url`.")
	a.Describe(&s.File, "Read the token from this file.")
	a.Describe(&s.Env, "Read the token from this environment variable.")
	a.Describe(&s.Command, "Run this command (argv form) and read the token from stdout.")
	a.Describe(&s.URL, "Fetch the token from this URL.")
	a.Describe(&s.Headers, "HTTP headers sent with the `url` request.")
	a.Describe(&s.ResponseField, "The JSON field of the `url` response that holds the token.")
}

// UserModelRoutingRule mirrors docker-agent's RoutingRule.
type UserModelRoutingRule struct {
	Model    string   `pulumi:"model"    yaml:"model"`
	Examples []string `pulumi:"examples" yaml:"examples"`
}

// Annotate describes UserModelRoutingRule.
func (r *UserModelRoutingRule) Annotate(a infer.Annotator) {
	a.Describe(&r, "Routes prompts similar to the examples to another model.")
	a.Describe(&r.Model, "The model to route to.")
	a.Describe(&r.Examples, "Example prompts for this route.")
}

// UserModelCapabilities mirrors docker-agent's CapabilitiesConfig.
type UserModelCapabilities struct {
	Image *bool `pulumi:"image,optional" yaml:"image"`
	Pdf   *bool `pulumi:"pdf,optional"   yaml:"pdf"`
	Audio *bool `pulumi:"audio,optional" yaml:"audio"`
	Video *bool `pulumi:"video,optional" yaml:"video"`
}

// Annotate describes UserModelCapabilities.
func (c *UserModelCapabilities) Annotate(a infer.Annotator) {
	a.Describe(&c, "Input modalities the model accepts.")
	a.Describe(&c.Image, "Accepts images.")
	a.Describe(&c.Pdf, "Accepts PDF documents.")
	a.Describe(&c.Audio, "Accepts audio.")
	a.Describe(&c.Video, "Accepts video.")
}

// UserModelOutputCapabilities mirrors docker-agent's OutputCapabilitiesConfig.
type UserModelOutputCapabilities struct {
	Image *bool `pulumi:"image,optional" yaml:"image"`
}

// Annotate describes UserModelOutputCapabilities.
func (c *UserModelOutputCapabilities) Annotate(a infer.Annotator) {
	a.Describe(&c, "Output modalities the model produces.")
	a.Describe(&c.Image, "Produces images.")
}

// UserModelCost mirrors docker-agent's CostConfig.
type UserModelCost struct {
	Input      *float64 `pulumi:"input,optional"      yaml:"input"`
	Output     *float64 `pulumi:"output,optional"     yaml:"output"`
	CacheRead  *float64 `pulumi:"cacheRead,optional"  yaml:"cache_read"`
	CacheWrite *float64 `pulumi:"cacheWrite,optional" yaml:"cache_write"`
}

// Annotate describes UserModelCost.
func (c *UserModelCost) Annotate(a infer.Annotator) {
	a.Describe(&c, "Prices in USD per million tokens.")
	a.Describe(&c.Input, "Price of input tokens.")
	a.Describe(&c.Output, "Price of output tokens.")
	a.Describe(&c.CacheRead, "Price of cache-read tokens.")
	a.Describe(&c.CacheWrite, "Price of cache-write tokens.")
}
