// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/property"

	"github.com/dirien/pulumi-docker-agent/provider/pkg/agentrun"
	"github.com/dirien/pulumi-docker-agent/provider/pkg/ociagent"
	"github.com/dirien/pulumi-docker-agent/provider/pkg/validate"
)

// AgentRun runs a prompt against an agent configuration during `pulumi up`, in process,
// with the docker-agent Go SDK.
type AgentRun struct{}

const agentRunDoc = `Runs a prompt against a [Docker Agent](https://docs.docker.com/ai/docker-agent/) configuration during
` + "`pulumi up`" + ` and exposes the agent's answer as outputs, for example to generate content, a review or a
structured decision that other resources consume.

The agent runs inside the provider process with the docker-agent Go SDK (the same runtime as
` + "`docker agent run --exec`" + `), headless and unattended. It never runs during ` + "`pulumi preview`" + `: the
outputs are unknown until the update.

**When the agent runs.** Creating the resource sends ` + "`prompt`" + `. A change of ` + "`yaml`" + `,
` + "`reference`" + `, ` + "`agent`" + `, ` + "`prompt`" + `, ` + "`env`" + `, ` + "`inheritEnvironment`" + `,
` + "`workingDir`" + `, ` + "`toolApproval`" + `, ` + "`allowHookApprovals`" + ` or ` + "`triggers`" + ` (including a value
that is unknown during the preview) runs the agent again: with ` + "`prompt`" + ` when ` + "`prompt`" + ` changed,
otherwise with ` + "`updatePrompt`" + ` (which defaults to ` + "`prompt`" + `). With ` + "`rerun: always`" + ` every
` + "`pulumi up`" + ` that includes the resource runs it again; the preview then shows it as an update, and the
resources that consume its outputs change too. A change of ` + "`updatePrompt`" + `, ` + "`deletePrompt`" + `,
` + "`allowHarnessAgents`" + `, ` + "`timeoutSeconds`" + `, ` + "`outputSecret`" + `, ` + "`logging`" + ` or ` + "`rerun`" + `
alone only updates the state (unless ` + "`rerun`" + ` becomes ` + "`always`" + `). Without changes, ` + "`pulumi up`" + ` reuses
the stored answer (unless ` + "`rerun: always`" + `), and ` + "`pulumi refresh`" + ` always does; importing is not supported.
A retry through the ` + "`hooks.onError`" + ` resource option runs the whole agent again.

**Deleting the resource** (` + "`pulumi destroy`" + `, removing it from the program, or the old resource of a
replacement) runs ` + "`deletePrompt`" + ` when one is set, with the inputs stored in the state and the environment of
the deleting command; its answer is not kept. When it fails, the delete fails and the resource stays in the state, and
for a removed or replaced resource every later ` + "`pulumi up`" + ` tries the delete again. To delete without running
it, set ` + "`DOCKER_AGENT_SKIP_DELETE_PROMPTS=true`" + ` for that command. It is skipped with a warning when
` + "`workingDir`" + ` no longer exists or is no longer a directory, and when another AgentRun of the same provider ran
in the same ` + "`workingDir`" + ` during the same command (also while the delete prompt waited for it): a replacement
that bypasses the provider's diff (` + "`replacementTrigger`" + `, ` + "`--replace`" + `, ` + "`replaceWith`" + `,
` + "`deletedWith`" + `) and a rename without ` + "`aliases`" + ` create the new run before they delete the old one. A
change of the provider replaces no resource: a changed setting (also only through ` + "`DOCKER_AGENT_SKIP_VALIDATION`" + `
or ` + "`DOCKER_AGENT_FORCE_HTTPS`" + ` in the environment of one command), a new provider version and a move to another
provider instance update the provider in place. Use the ` + "`deleteBeforeReplace`" + ` resource option (AgentRun asks
for it when a ` + "`deletePrompt`" + ` is stored, which covers ` + "`replaceOnChanges`" + `), ` + "`aliases`" + `, and one
` + "`workingDir`" + ` per AgentRun. With ` + "`retainOnDelete`" + `, or when the ` + "`deletedWith`" + ` resource is
deleted in the same operation, Delete is not called and nothing runs.

**Tools run on the machine executing ` + "`pulumi up`" + `.** With the default ` + "`toolApproval: deny`" + `, a tool
call only runs when docker-agent would run it without asking: tools annotated read-only (for example
` + "`think`" + `, ` + "`todo`" + `, ` + "`memory`" + ` reads, the filesystem toolset's read and search tools,
` + "`api`" + ` tools and MCP tools whose server declares them read-only), calls an ` + "`allow`" + ` rule of the
configuration's ` + "`permissions`" + ` matches, calls the configuration's own ` + "`safety`" + ` mode
(` + "`strict`" + `, ` + "`balanced`" + ` or ` + "`restricted`" + `) approves, and agent delegation
(` + "`transfer_task`" + `, ` + "`handoff`" + `). Approvals from the configuration's hooks (` + "`pre_tool_use`" + `
or ` + "`permission_request`" + `) are ignored unless ` + "`allowHookApprovals`" + ` is set. Every other call (shell
commands, ` + "`script`" + ` tools, file writes, ` + "`fetch`" + `, MCP tools without a read-only annotation, ...) is
rejected, and the model is told why. ` + "`toolApproval: allow`" + ` runs every tool call, like
` + "`docker agent run --yolo`" + `.

In both modes, these run without approval because they are part of the configuration: command hooks, MCP and LSP
servers started with ` + "`command`" + `, ` + "`docker:`" + ` MCP servers (through the Docker MCP Gateway), and
filesystem ` + "`post_edit`" + ` commands after an approved write. Read-only tools can read any file the provider can
read unless the toolset restricts them (` + "`allow_list`" + `). Review configurations pulled by ` + "`reference`" + `
before running them.

**Coding-harness agents** (` + "`harness: {type: claude-code}`" + `) run only with ` + "`allowHarnessAgents: true`" + `,
on Linux and macOS; codex, pi and opencode harnesses are not supported yet. Such an agent starts the Claude Code CLI
(` + "`claude`" + `, found on the ` + "`PATH`" + ` of its environment) on the machine that runs ` + "`pulumi up`" + `, which
runs its own tools without approval: ` + "`toolApproval`" + `, ` + "`allowHookApprovals`" + ` and the configuration's
` + "`permissions`" + ` do not apply to it. The CLI runs in ` + "`workingDir`" + ` (or a temporary directory) with the
prompt on stdin, the environment described for ` + "`inheritEnvironment`" + ` plus ` + "`env`" + `, and its user's
Claude login and settings: the login of ` + "`claude`" + ` on this machine, or ` + "`CLAUDE_CODE_OAUTH_TOKEN`" + ` (from
` + "`claude setup-token`" + `) in ` + "`env`" + `. An ` + "`ANTHROPIC_API_KEY`" + ` or ` + "`ANTHROPIC_AUTH_TOKEN`" + ` in its
environment bills API usage instead of the subscription; when ` + "`env`" + ` sets ` + "`CLAUDE_CODE_OAUTH_TOKEN`" + `,
inherited ones are not passed to the CLI. ` + "`output`" + ` is Claude Code's final result (when that is empty, the
text it wrote during the run, with a warning), the agent's ` + "`instruction`" + ` is not sent to it, and
` + "`structured_output`" + ` is decoded from the result but not enforced: an answer that is not JSON fails the run.
When the run times out or is cancelled,
AgentRun sends SIGTERM to the CLI's process group and SIGKILL after 10 seconds; when Pulumi stops the provider, no new
CLI starts and the running ones get SIGTERM and SIGKILL after 2 seconds. Claude Code runs each shell command in a
session of its own, so processes it does not stop itself can keep running.

Credentials and ` + "`${env.NAME}`" + ` references resolve from ` + "`env`" + ` and, unless
` + "`inheritEnvironment`" + ` is false, the environment of the Pulumi CLI. With
` + "`inheritEnvironment: false`" + `, command hooks, ` + "`shell`" + `, ` + "`script`" + ` and
` + "`background_jobs`" + ` tools, MCP and LSP servers started with ` + "`command`" + ` and the Claude Code CLI also lose
that environment: they see the system variables (` + "`PATH`" + `, ` + "`HOME`" + `, ` + "`USER`" + `, ` + "`SHELL`" + `,
` + "`LANG`" + `, ` + "`TMPDIR`" + `, ` + "`TZ`" + `, ...) plus their own ` + "`env`" + ` (hooks and the Claude Code CLI also
get ` + "`env`" + `). ` + "`docker:`" + ` MCP servers, filesystem ` + "`post_edit`" + ` commands, the
` + "`open_url`" + ` launcher and docker-agent's on-demand tool installation always inherit it.
Errors, warnings and logs are redacted: the values of
` + "`env`" + `, secret inputs, credential-named variables of the environment (` + "`*_API_KEY`" + `,
` + "`*_TOKEN`" + `, ` + "`*SECRET*`" + `, ` + "`*PASSWORD*`" + `, ...) and the variables the configuration names as
` + "`token_key`" + `. When the answer contains one of these values, ` + "`output`" + ` and
` + "`structuredOutput`" + ` are stored as secrets.

Each run is isolated from the docker-agent CLI on the same machine: docker-agent's data, config and cache directories
point to a private temporary directory that is deleted afterwards, so ` + "`~/.cagent`" + ` and the user settings,
aliases, global providers and credentials in ` + "`~/.config/cagent`" + ` are neither read nor written. The agent
works in a fresh temporary directory unless ` + "`workingDir`" + ` is set. Runs of one provider process execute one at
a time.

` + "`logging`" + ` controls what ` + "`pulumi up`" + ` shows while the agent runs. ` + "`status`" + ` (the default) shows the
run's progress as status messages that the next one replaces (an agent started, a task was transferred or the
conversation handed off, each tool call and whether it was approved or rejected, and why, a model that fell back to
the next one) and ends with one summary line: duration, tool calls, input and output tokens and the estimated cost.
` + "`tools`" + ` also logs every tool call with its arguments, and ` + "`full`" + ` also logs the agents' messages and
the tools' results. ` + "`none`" + ` shows nothing. Messages are redacted like errors. When the answer is secret
(` + "`outputSecret`" + `, or a secret ` + "`prompt`" + `, ` + "`updatePrompt`" + ` or ` + "`yaml`" + `), ` + "`tools`" + ` and
` + "`full`" + ` log no arguments, messages or results, and with a secret ` + "`yaml`" + ` or ` + "`agent`" + ` the messages
name no agents, tools or models.

An AgentRun has two time limits. ` + "`timeoutSeconds`" + ` (default 300) limits the agent run itself: when it runs
out, the run is cancelled and the create or update fails with "the agent did not finish within ... (timeoutSeconds)".
The ` + "`customTimeouts`" + ` resource option (` + "`create`" + `, ` + "`update`" + `, ` + "`delete`" + `) limits the whole
provider operation, including pulling a ` + "`reference`" + ` and waiting for another AgentRun of the same provider
process to finish; when it runs out, Pulumi cancels the operation and the run fails with "the operation's time limit
(customTimeouts) ran out before the agent finished". The shorter limit wins. Use ` + "`customTimeouts`" + ` to bound the
deployment step as a whole and keep ` + "`timeoutSeconds`" + ` as the per-run safety limit.`

const agentRunImportDoc = `An AgentRun cannot be imported: a run has nothing to read back. Declare the resource instead; the
next ` + "`pulumi up`" + ` runs the agent.`

// Annotate describes the resource.
func (r *AgentRun) Annotate(a infer.Annotator) {
	a.Describe(&r, describe(agentRunDoc, agentRunExamples, agentRunImportDoc))
}

// ToolApproval decides what happens to tool calls that docker-agent would ask a user to
// confirm.
type ToolApproval string

const (
	// ToolApprovalDeny rejects tool calls that need approval.
	ToolApprovalDeny ToolApproval = "deny"
	// ToolApprovalAllow approves every tool call.
	ToolApprovalAllow ToolApproval = "allow"
)

// Values lists the enum values.
func (ToolApproval) Values() []infer.EnumValue[ToolApproval] {
	return []infer.EnumValue[ToolApproval]{
		{Name: "Deny", Value: ToolApprovalDeny, Description: "Run only the tool calls docker-agent approves without " +
			"asking (read-only tools, `permissions` allow rules, the configuration's own safety mode); reject the rest."},
		{Name: "Allow", Value: ToolApprovalAllow, Description: "Approve every tool call, like `docker agent run --yolo`. " +
			"Tools run on the machine executing `pulumi up`."},
	}
}

// Logging selects what AgentRun reports while the agent runs.
type Logging string

const (
	// LoggingNone reports nothing.
	LoggingNone Logging = "none"
	// LoggingStatus reports status updates and a summary.
	LoggingStatus Logging = "status"
	// LoggingTools also reports each tool call with its arguments.
	LoggingTools Logging = "tools"
	// LoggingFull also reports the agents' messages and the tools' results.
	LoggingFull Logging = "full"
)

// Values lists the enum values.
func (Logging) Values() []infer.EnumValue[Logging] {
	return []infer.EnumValue[Logging]{
		{Name: "None", Value: LoggingNone, Description: "Report nothing while the agent runs, and no summary."},
		{Name: "Status", Value: LoggingStatus, Description: "Show the run's progress as status messages that are " +
			"replaced by the next one (an agent started, a task was transferred or the conversation handed off, a " +
			"tool call was approved or rejected, a model fell back to the next one), and log a summary of the run " +
			"(duration, tool calls, tokens and estimated cost)."},
		{Name: "Tools", Value: LoggingTools, Description: "`status`, and also log every tool call with its arguments " +
			"and whether it was approved."},
		{Name: "Full", Value: LoggingFull, Description: "`tools`, and also log the agents' messages and the tools' " +
			"results."},
	}
}

// RerunPolicy decides when `pulumi up` runs the agent again after it first ran.
type RerunPolicy string

const (
	// RerunOnChange runs the agent again when an input that affects the run changes.
	RerunOnChange RerunPolicy = "onChange"
	// RerunAlways runs the agent again on every `pulumi up` that includes the resource.
	RerunAlways RerunPolicy = "always"
)

// Values lists the enum values.
func (RerunPolicy) Values() []infer.EnumValue[RerunPolicy] {
	return []infer.EnumValue[RerunPolicy]{
		{Name: "OnChange", Value: RerunOnChange, Description: "Run the agent again only when an input that affects the " +
			"run changes, including `triggers` (default)."},
		{Name: "Always", Value: RerunAlways, Description: "Run the agent again on every `pulumi up` that includes this " +
			"resource. Previews show it as an update and its run outputs as unknown; `pulumi refresh` does not."},
	}
}

// AgentRunArgs are the inputs of AgentRun.
type AgentRunArgs struct {
	YAML               *string           `pulumi:"yaml,optional"`
	Reference          *string           `pulumi:"reference,optional"`
	Agent              *string           `pulumi:"agent,optional"`
	Prompt             string            `pulumi:"prompt"`
	UpdatePrompt       *string           `pulumi:"updatePrompt,optional"`
	DeletePrompt       *string           `pulumi:"deletePrompt,optional"`
	Env                map[string]string `pulumi:"env,optional" provider:"secret"`
	InheritEnvironment *bool             `pulumi:"inheritEnvironment,optional"`
	WorkingDir         *string           `pulumi:"workingDir,optional"`
	ToolApproval       *ToolApproval     `pulumi:"toolApproval,optional"`
	TimeoutSeconds     *int              `pulumi:"timeoutSeconds,optional"`
	Triggers           []any             `pulumi:"triggers,optional"`
	Rerun              *RerunPolicy      `pulumi:"rerun,optional"`
	OutputSecret       *bool             `pulumi:"outputSecret,optional"`
	AllowHookApprovals *bool             `pulumi:"allowHookApprovals,optional"`
	AllowHarnessAgents *bool             `pulumi:"allowHarnessAgents,optional"`
	Logging            *Logging          `pulumi:"logging,optional"`
}

// Annotate describes the inputs.
func (a *AgentRunArgs) Annotate(an infer.Annotator) {
	an.Describe(&a.YAML, "The docker-agent configuration to run, for example `AgentConfig.yaml` or the "+
		"contents of an agent file. Exactly one of `yaml` and `reference` is required.")
	an.Describe(&a.Reference, "An agent shared through an OCI registry (`registry/repository:tag` or "+
		"`@sha256:...`), pulled like `getAgent` with the provider's `registryAuth`. Pin it by digest, or use "+
		"`triggers`, to control when a moved tag reruns the agent: only input changes do.")
	an.Describe(&a.Agent, "The agent to run. Defaults to `root`, or else the first agent of the configuration.")
	an.Describe(&a.Prompt, "The user message sent when the resource is created, and on reruns caused by a change of "+
		"`prompt`.")
	an.Describe(&a.UpdatePrompt, "The user message sent when the agent runs again while `prompt` is unchanged: after "+
		"a change of `triggers`, `yaml`, `reference`, `agent`, `env`, `inheritEnvironment`, `workingDir`, `toolApproval` "+
		"or `allowHookApprovals`, or on every `pulumi up` with `rerun: always`. When `prompt` changes, the rerun sends "+
		"`prompt`. Defaults to `prompt`. Use it to tell the agent that its earlier work exists, for example files in "+
		"`workingDir`. Changing it updates the state without running the agent; the next rerun uses it.")
	an.Describe(&a.DeletePrompt, "A user message the agent runs when the resource is deleted: by `pulumi destroy`, by "+
		"removing it from the program, or by a replacement. It runs with the configuration and inputs stored in the "+
		"state, and the environment of the deleting command, for example to clean up what earlier runs created in "+
		"`workingDir`. Its answer is not kept. When it fails, the delete fails and the resource stays in the state. For a "+
		"resource that was removed or replaced, that fails every later `pulumi up` as well. To delete without running "+
		"it, set `DOCKER_AGENT_SKIP_DELETE_PROMPTS=true` for that command. It is skipped with a warning when "+
		"`workingDir` no longer exists or is no longer a directory, and when an AgentRun of the same provider already "+
		"ran in that `workingDir` during the same `pulumi` command. That happens when a replacement or a rename "+
		"creates the new run first; use the `deleteBeforeReplace` resource option or `aliases`. It does not run with "+
		"the `retainOnDelete` resource option, or when the `deletedWith` resource is deleted in the same operation. "+
		"Changing it updates the state without running the agent.")
	an.Describe(&a.Env, "Environment variables for model credentials (for example `OPENAI_API_KEY` or the "+
		"`tokenKey` of a custom provider) and `${env.NAME}` references in the configuration. They take precedence "+
		"over the inherited environment, are stored as secrets, are redacted from errors and logs, and also reach "+
		"command hooks and the Claude Code CLI of a coding-harness agent (for example `CLAUDE_CODE_OAUTH_TOKEN`). "+
		"When `env` sets `CLAUDE_CODE_OAUTH_TOKEN`, an inherited `ANTHROPIC_API_KEY` or `ANTHROPIC_AUTH_TOKEN` is not "+
		"passed to the CLI. Other processes get them only through their toolset's own `env` (`${env.NAME}`).")
	an.Describe(&a.InheritEnvironment, "Also resolve credentials and `${env.NAME}` references from the "+
		"environment of the Pulumi CLI, and let the processes the configuration starts inherit that environment. "+
		"With `false`, command hooks, `shell`, `script` and `background_jobs` tools, MCP and LSP servers "+
		"started with `command`, and the Claude Code CLI of a coding-harness agent only see the system variables "+
		"(`PATH`, `HOME`, `USER`, `SHELL`, `LANG`, `TMPDIR`, `TZ`, ...) plus their own `env` (hooks and the Claude Code "+
		"CLI also get `env`); the Claude Code CLI also keeps the Claude configuration locations (`CLAUDE_CONFIG_DIR`, "+
		"`CLAUDE_SECURESTORAGE_CONFIG_DIR`, `XDG_*_HOME`) and `IS_SANDBOX`. `docker:` MCP servers, filesystem "+
		"`post_edit` commands, the `open_url` launcher and docker-agent's on-demand tool installation still inherit "+
		"everything. "+
		"docker-agent's other credential sources "+
		"(its `.env` file, credential helper, Docker Desktop) are never used.")
	an.SetDefault(&a.InheritEnvironment, true)
	an.Describe(&a.WorkingDir, "The agent's working directory, used by the shell, filesystem and other "+
		"local tools, and the Claude Code CLI of a coding-harness agent. Defaults to a fresh temporary directory that "+
		"is deleted after the run. Relative paths are resolved against the directory of the Pulumi program.")
	an.Describe(&a.ToolApproval, "What happens to tool calls that docker-agent would ask a user to confirm. "+
		"`deny` rejects them and tells the model; `allow` approves every tool call. Tools run on the machine "+
		"executing `pulumi up`.")
	an.SetDefault(&a.ToolApproval, ToolApprovalDeny)
	an.Describe(&a.TimeoutSeconds, "How long the run may take, in seconds, before it fails. When it runs out, "+
		"AgentRun stops a Claude Code CLI: SIGTERM to its process group, and SIGKILL to the group after 10 s. Changing it "+
		"updates "+
		"the state without running the agent again.")
	an.SetDefault(&a.TimeoutSeconds, int(agentrun.DefaultTimeout/time.Second))
	an.Describe(&a.Triggers, "Arbitrary values; any change runs the agent again, like a change of any other "+
		"input.")
	an.Describe(&a.Rerun, "When `pulumi up` runs the agent again after it first ran. `onChange` (default) runs it "+
		"when an input that affects the run changes, including `triggers`. `always` runs it on every `pulumi up` that "+
		"includes this resource. The preview then shows the resource as an update (`[diff: ~rerun]`) with unknown "+
		"outputs, so a stack with such an AgentRun never previews without changes, and resources that consume its "+
		"outputs change too. `pulumi refresh` reports no drift for it. Changing `rerun` alone updates the state, unless "+
		"the new value is `always`.")
	an.SetDefault(&a.Rerun, RerunOnChange)
	an.Describe(&a.OutputSecret, "Mark `output` and `structuredOutput` as secret. They are also secret when "+
		"`prompt`, `updatePrompt` or `yaml` is secret, and when the answer contains the value of a secret (an `env` value, a secret "+
		"input, or a credential of the environment such as an API key the agent was given through `${env.NAME}`). "+
		"A secret `env` alone does not make them secret. Changing it updates the state without running the agent "+
		"again.")
	an.SetDefault(&a.OutputSecret, false)
	an.Describe(&a.AllowHookApprovals, "Let the configuration's hooks approve tool calls under `toolApproval: deny`. "+
		"docker-agent runs a tool call that a `pre_tool_use` hook (`permission_decision: allow`) or a "+
		"`permission_request` hook approves; by default AgentRun ignores such approvals and rejects the call, so "+
		"`deny` only runs what docker-agent approves by its rules. Hook denials always apply. It has no effect with "+
		"`toolApproval: allow`.")
	an.SetDefault(&a.AllowHookApprovals, false)
	an.Describe(&a.AllowHarnessAgents, "Run coding-harness agents: agents with `harness`. AgentRun supports "+
		"`harness.type: claude-code` on Linux and macOS. Such an agent starts the Claude Code CLI (`claude`) on the "+
		"machine that runs `pulumi up`. The CLI runs its own tools (shell commands, file edits, web requests) without "+
		"approval: `toolApproval`, `allowHookApprovals` and the configuration's `permissions` do not apply to it. The "+
		"CLI runs in `workingDir`, with the environment described for `inheritEnvironment` plus `env`. It uses its "+
		"user's Claude login and settings: the login of `claude` on this machine, or `CLAUDE_CODE_OAUTH_TOKEN` in "+
		"`env`. Without it, a coding-harness agent fails the run, and a `yaml` configuration (that the provider's "+
		"docker-agent version can load) already fails the preview. Changing it updates "+
		"the state without running the agent again.")
	an.SetDefault(&a.AllowHarnessAgents, false)
	an.Describe(&a.Logging, "What `pulumi up` shows while the agent runs. `status` (default) shows the run's "+
		"progress as status messages that the next one replaces (an agent started, a task was transferred or the "+
		"conversation handed off, each tool call and whether it was approved or rejected, a model that fell back to "+
		"the next one) and logs one summary (duration, tool calls, input and output tokens, estimated cost). "+
		"`tools` also logs every tool call with its arguments, `full` also the agents' messages and the tools' "+
		"results; `none` shows nothing. Messages are redacted like errors (`env` values, secret inputs, "+
		"credentials of the environment and `tokenKey` variables). When the answer is secret (`outputSecret`, or a "+
		"secret `prompt`, `updatePrompt` or `yaml`; for a delete run also a secret `deletePrompt`), `tools` and `full` "+
		"log no arguments, messages or results, and with a secret "+
		"`yaml` or `agent` the messages name no agents, tools or models. Changing it updates the state without "+
		"running the agent again.")
	an.SetDefault(&a.Logging, LoggingStatus)
}

// AgentRunUsage is the token usage and cost of a run.
type AgentRunUsage struct {
	InputTokens  int     `pulumi:"inputTokens"`
	OutputTokens int     `pulumi:"outputTokens"`
	Cost         float64 `pulumi:"cost"`
}

// Annotate describes AgentRunUsage.
func (u *AgentRunUsage) Annotate(a infer.Annotator) {
	a.Describe(&u, "Token usage and cost of an agent run, including sub-agents.")
	a.Describe(&u.InputTokens, "Prompt tokens, including cached input tokens.")
	a.Describe(&u.OutputTokens, "Completion tokens.")
	a.Describe(&u.Cost, "Estimated cost in US dollars, from the model pricing of the models.dev catalog bundled "+
		"with docker-agent "+DockerAgentVersion+". 0 for models without known pricing, such as custom providers. For a "+
		"coding-harness agent, the cost Claude Code reports (`total_cost_usd`), an API-price estimate also under a "+
		"subscription.")
}

// AgentRunState is the state of AgentRun.
type AgentRunState struct {
	AgentRunArgs
	Output           string        `pulumi:"output"`
	StructuredOutput any           `pulumi:"structuredOutput,optional"`
	AgentName        string        `pulumi:"agentName"`
	Usage            AgentRunUsage `pulumi:"usage"`
	ToolCalls        []string      `pulumi:"toolCalls"`
	RanAt            string        `pulumi:"ranAt"`
}

// Annotate describes the outputs.
func (s *AgentRunState) Annotate(an infer.Annotator) {
	an.Describe(&s.Output, "The agent's final answer. For a coding-harness agent, Claude Code's final result, or, "+
		"when that is empty, the text Claude Code wrote during the run.")
	an.Describe(&s.StructuredOutput, "The final answer decoded as JSON, when the answering agent declares "+
		"`structured_output`.")
	an.Describe(&s.AgentName, "The agent that gave the final answer: the selected agent, or the agent it "+
		"handed the conversation off to.")
	an.Describe(&s.Usage, "Token usage and estimated cost of the run.")
	an.Describe(&s.ToolCalls, "The names of the tools that ran, in order, including those of sub-agents the "+
		"agent transferred tasks to and the tools Claude Code reports it ran. Rejected tool calls are reported as "+
		"warnings instead.")
	an.Describe(&s.RanAt, "When the run finished (RFC 3339).")
}

// agentRunOutputs are the outputs computed by a run.
var agentRunOutputs = []string{"output", "structuredOutput", "agentName", "usage", "toolCalls", "ranAt"}

// Check validates the inputs and, when it is known, the configuration.
func (*AgentRun) Check(ctx context.Context, req infer.CheckRequest) (infer.CheckResponse[AgentRunArgs], error) {
	args, failures, err := infer.DefaultCheck[AgentRunArgs](ctx, req.NewInputs)
	if err != nil {
		return infer.CheckResponse[AgentRunArgs]{Inputs: args, Failures: failures}, err
	}
	in := req.NewInputs
	hasYAML, hasRef := isSet(in.Get("yaml")), isSet(in.Get("reference"))
	switch {
	case hasYAML && hasRef:
		failures = append(failures, p.CheckFailure{Property: "reference", Reason: "set either `yaml` or `reference`, not both"})
	case !hasYAML && !hasRef:
		failures = append(failures, p.CheckFailure{Property: "yaml", Reason: "one of `yaml` and `reference` is required"})
	}
	if v := in.Get("prompt"); v.IsString() && strings.TrimSpace(v.AsString()) == "" {
		failures = append(failures, p.CheckFailure{Property: "prompt", Reason: "`prompt` must not be empty"})
	}
	if v := in.Get("updatePrompt"); v.IsString() && strings.TrimSpace(v.AsString()) == "" {
		failures = append(failures, p.CheckFailure{Property: "updatePrompt",
			Reason: "`updatePrompt` must not be empty; omit it to rerun with `prompt`"})
	}
	if v := in.Get("deletePrompt"); v.IsString() && strings.TrimSpace(v.AsString()) == "" {
		failures = append(failures, p.CheckFailure{Property: "deletePrompt", Reason: "`deletePrompt` must not be empty"})
	}
	if args.Rerun != nil && *args.Rerun != RerunOnChange && *args.Rerun != RerunAlways {
		failures = append(failures, p.CheckFailure{Property: "rerun",
			Reason: fmt.Sprintf("invalid value %q: must be %q or %q", *args.Rerun, RerunOnChange, RerunAlways)})
	}
	if args.ToolApproval != nil && *args.ToolApproval != ToolApprovalDeny && *args.ToolApproval != ToolApprovalAllow {
		failures = append(failures, p.CheckFailure{Property: "toolApproval",
			Reason: fmt.Sprintf("invalid value %q: must be %q or %q", *args.ToolApproval, ToolApprovalDeny, ToolApprovalAllow)})
	}
	if v := in.Get("timeoutSeconds"); v.IsNumber() && v.AsNumber() <= 0 {
		failures = append(failures, p.CheckFailure{Property: "timeoutSeconds", Reason: "`timeoutSeconds` must be positive"})
	}

	if y := in.Get("yaml"); hasYAML && !hasRef && y.IsString() {
		secrets := secretStrings(in)
		doc := []byte(y.AsString())
		valid := true
		if !skipValidation(ctx, nil) {
			res := validate.Validate(ctx, req.Name+".yaml", doc)
			for _, e := range res.Errors {
				failures = append(failures, p.CheckFailure{Property: "yaml",
					Reason: "invalid docker-agent configuration: " + validate.Redact(e, secrets)})
			}
			valid = res.Valid
		}
		// AgentRun's own rules for coding-harness agents are not docker-agent's validation:
		// with skipValidation they still apply when the pinned docker-agent loads the
		// configuration (CheckConfig reports nothing for one it cannot load).
		if valid {
			problems, warnings := agentrun.CheckConfig(ctx, req.Name+".yaml", doc, checkOptions(in, args))
			for _, e := range problems {
				failures = append(failures, p.CheckFailure{Property: "yaml", Reason: validate.Redact(e, secrets)})
			}
			for _, w := range warnings {
				userLog(ctx).Warning(validate.Redact(w, secrets))
			}
		}
		if a := in.Get("agent"); a.IsString() && a.AsString() != "" {
			if names := validate.AgentNames(doc); names != nil && !slices.Contains(names, a.AsString()) {
				failures = append(failures, p.CheckFailure{Property: "agent",
					Reason: fmt.Sprintf("agent %q is not defined in the configuration (agents: %s)",
						a.AsString(), strings.Join(names, ", "))})
			}
		}
	}
	return infer.CheckResponse[AgentRunArgs]{Inputs: args, Failures: failures}, nil
}

// checkOptions are the inputs agentrun.CheckConfig needs, from the raw and the checked inputs.
func checkOptions(in property.Map, args AgentRunArgs) agentrun.CheckOptions {
	opts := agentrun.CheckOptions{
		// An opt-in that is unknown during a preview (an output of another resource) fails
		// nothing yet; the run enforces it.
		AllowHarnessAgents: in.Get("allowHarnessAgents").IsComputed() || runBool(args.AllowHarnessAgents),
		// An inheritEnvironment that is unknown during a preview counts as true, its default: the
		// warnings about inherited credentials apply, and none claims that it is false.
		InheritEnvironment: in.Get("inheritEnvironment").IsComputed() || inheritEnvironment(args),
		DeletePrompt:       isSet(in.Get("deletePrompt")),
		ToolApprovalDeny:   toolApproval(args) == ToolApprovalDeny,
		HideNames:          in.Get("yaml").Secret() || in.Get("agent").Secret(),
	}
	if env := in.Get("env"); env.IsComputed() {
		opts.EnvUnknown = true
	} else if env.IsMap() {
		opts.EnvKeys, opts.EnvValues = map[string]bool{}, map[string]string{}
		for k, v := range env.AsMap().All {
			opts.EnvKeys[k] = v.IsComputed() || v.IsString() && v.AsString() != ""
			if v.IsString() {
				opts.EnvValues[k] = v.AsString()
			}
		}
	}
	return opts
}

// isSet reports whether a raw input is present: a non-empty string or an unknown value.
func isSet(v property.Value) bool {
	if v.IsComputed() {
		return true
	}
	return v.IsString() && v.AsString() != ""
}

// Create runs the agent with `prompt`. Previews never run it. The ID is the resource name
// with a random suffix, so that a resource and its replacement (or a pending delete) have
// different IDs; resources created by v0.1.1 keep their name as ID.
func (*AgentRun) Create(ctx context.Context, req infer.CreateRequest[AgentRunArgs]) (infer.CreateResponse[AgentRunState], error) {
	if req.DryRun {
		return infer.CreateResponse[AgentRunState]{ID: req.Name, Output: AgentRunState{AgentRunArgs: req.Inputs}}, nil
	}
	state, err := runAgent(ctx, req.Name, req.Inputs, req.Inputs.Prompt, false)
	if err != nil {
		return infer.CreateResponse[AgentRunState]{}, err
	}
	id, err := resource.NewUniqueHex(req.Name+"-", 8, 0)
	if err != nil {
		return infer.CreateResponse[AgentRunState]{}, err
	}
	return infer.CreateResponse[AgentRunState]{ID: id, Output: state}, nil
}

// Update runs the agent again when an input that affects the run changed, or with `rerun:
// always`. Changes of `updatePrompt`, `deletePrompt`, `allowHarnessAgents`, `rerun`,
// `outputSecret`, `timeoutSeconds` or `logging` alone only update the state.
func (*AgentRun) Update(
	ctx context.Context, req infer.UpdateRequest[AgentRunArgs, AgentRunState],
) (infer.UpdateResponse[AgentRunState], error) {
	rc := runContextFrom(ctx)
	if !rerunNeeded(req.State.AgentRunArgs, req.Inputs) {
		state := req.State
		state.AgentRunArgs = req.Inputs
		if state.ToolCalls == nil {
			// The decoded empty list is nil, which would be dropped from the state (see
			// withToolCalls).
			state.ToolCalls = []string{}
		}
		if rc != nil && state.Output != "" {
			rc.answerSecret = agentrun.AnswerContainsSecret(state.Output, agentrun.Request{
				YAML: []byte(runString(req.Inputs.YAML)), Env: req.Inputs.Env,
				InheritEnvironment: inheritEnvironment(req.Inputs), Secrets: rc.secrets,
			})
		}
		return infer.UpdateResponse[AgentRunState]{Output: state}, nil
	}
	if rc != nil {
		rc.rerun = true
	}
	if req.DryRun {
		return infer.UpdateResponse[AgentRunState]{Output: AgentRunState{AgentRunArgs: req.Inputs}}, nil
	}
	state, err := runAgent(ctx, req.ID, req.Inputs, rerunPrompt(req.State.AgentRunArgs, req.Inputs), false)
	if err != nil {
		return infer.UpdateResponse[AgentRunState]{}, err
	}
	return infer.UpdateResponse[AgentRunState]{Output: state}, nil
}

// rerunNeeded reports whether an update runs the agent again: an input that affects the run
// changed (everything except the state-only inputs), or `rerun` is `always`. Update is only
// called after Diff reported a change, and Diff reports `rerun: always` only after a Check
// of the same `pulumi up` (see agentRunResource), so the `always` term never reruns the
// agent in a refresh.
func rerunNeeded(olds, news AgentRunArgs) bool {
	return len(runInputChanges(olds, news)) > 0 || rerunOf(news) == RerunAlways
}

// rerunPrompt is the user message of a rerun: `prompt` when it changed, otherwise
// `updatePrompt`, which defaults to `prompt`.
func rerunPrompt(olds, news AgentRunArgs) string {
	if olds.Prompt != news.Prompt || runString(news.UpdatePrompt) == "" {
		return news.Prompt
	}
	return *news.UpdatePrompt
}

// runInputChanges lists the changed inputs that affect the run.
func runInputChanges(olds, news AgentRunArgs) []string {
	var changed []string
	add := func(name string, before, after any) {
		if !reflect.DeepEqual(before, after) {
			changed = append(changed, name)
		}
	}
	add("yaml", runString(olds.YAML), runString(news.YAML))
	add("reference", runString(olds.Reference), runString(news.Reference))
	add("agent", runString(olds.Agent), runString(news.Agent))
	add("prompt", olds.Prompt, news.Prompt)
	if !maps.Equal(olds.Env, news.Env) {
		changed = append(changed, "env")
	}
	add("inheritEnvironment", inheritEnvironment(olds), inheritEnvironment(news))
	add("workingDir", runString(olds.WorkingDir), runString(news.WorkingDir))
	add("toolApproval", toolApproval(olds), toolApproval(news))
	add("allowHookApprovals", runBool(olds.AllowHookApprovals), runBool(news.AllowHookApprovals))
	if len(olds.Triggers) != 0 || len(news.Triggers) != 0 {
		add("triggers", olds.Triggers, news.Triggers)
	}
	return changed
}

// Diff reports every input change as an update. Inputs that affect the run make the
// update run the agent again (see Update); unknown inputs and `rerun: always` are handled by
// agentRunResource. Absent values of a v0.1.1 state equal their defaults. A replacement asks
// to delete first when a delete prompt is stored, so that the old resource's cleanup cannot
// undo the new run.
func (*AgentRun) Diff(
	_ context.Context, req infer.DiffRequest[AgentRunArgs, AgentRunState],
) (infer.DiffResponse, error) {
	olds, news := req.State.AgentRunArgs, req.Inputs
	diff := map[string]p.PropertyDiff{}
	for _, name := range runInputChanges(olds, news) {
		diff[name] = p.PropertyDiff{Kind: p.Update, InputDiff: true}
	}
	if timeoutOf(olds) != timeoutOf(news) {
		diff["timeoutSeconds"] = p.PropertyDiff{Kind: p.Update, InputDiff: true}
	}
	if runBool(olds.OutputSecret) != runBool(news.OutputSecret) {
		diff["outputSecret"] = p.PropertyDiff{Kind: p.Update, InputDiff: true}
	}
	if loggingOf(olds) != loggingOf(news) {
		diff["logging"] = p.PropertyDiff{Kind: p.Update, InputDiff: true}
	}
	stateOnly := map[string]bool{
		"updatePrompt":       runString(olds.UpdatePrompt) != runString(news.UpdatePrompt),
		"deletePrompt":       runString(olds.DeletePrompt) != runString(news.DeletePrompt),
		"allowHarnessAgents": runBool(olds.AllowHarnessAgents) != runBool(news.AllowHarnessAgents),
		"rerun":              rerunOf(olds) != rerunOf(news),
	}
	for name, changed := range stateOnly {
		if changed {
			diff[name] = p.PropertyDiff{Kind: p.Update, InputDiff: true}
		}
	}
	return infer.DiffResponse{
		HasChanges:          len(diff) > 0,
		DetailedDiff:        diff,
		DeleteBeforeReplace: runString(olds.DeletePrompt) != "",
	}, nil
}

// Read keeps the stored answer: a run cannot be observed after the fact. There is nothing
// to import.
func (*AgentRun) Read(
	_ context.Context, req infer.ReadRequest[AgentRunArgs, AgentRunState],
) (infer.ReadResponse[AgentRunArgs, AgentRunState], error) {
	if req.State.RanAt == "" {
		return infer.ReadResponse[AgentRunArgs, AgentRunState]{}, errAgentRunImport
	}
	return infer.ReadResponse[AgentRunArgs, AgentRunState](req), nil
}

var errAgentRunImport = errors.New("importing an AgentRun is not supported: a run has nothing to import; " +
	"declare the resource in your program and `pulumi up` runs the agent")

// skipDeletePromptsEnv names the environment variable that skips every delete prompt of a
// `pulumi` command. Delete reads it from the provider process, which gets the environment
// of the command: a provider configuration value would come from the stored state in
// `pulumi destroy`.
const skipDeletePromptsEnv = "DOCKER_AGENT_SKIP_DELETE_PROMPTS"

// Delete runs `deletePrompt`, when one is stored, with the stored inputs. A failure fails the
// delete, unless one of deletePromptSkip's reasons applies. They are checked before the run
// and again when it holds the run slot: another AgentRun of the provider process can run in
// the same workingDir while this one waits for it.
func (*AgentRun) Delete(ctx context.Context, req infer.DeleteRequest[AgentRunState]) (infer.DeleteResponse, error) {
	prompt := runString(req.State.DeletePrompt)
	if strings.TrimSpace(prompt) == "" {
		return infer.DeleteResponse{}, nil
	}
	// The reason can name a secret workingDir.
	var secrets []string
	if rc := runContextFrom(ctx); rc != nil {
		secrets = rc.secrets
	}
	skipped := func(why string) (infer.DeleteResponse, error) {
		userLog(ctx).Warning("deletePrompt not run: " + validate.Redact(why, secrets))
		return infer.DeleteResponse{}, nil
	}
	if why := deletePromptSkip(req.State.AgentRunArgs); why != "" {
		return skipped(why)
	}
	if _, err := runAgent(ctx, req.ID, req.State.AgentRunArgs, prompt, true); err != nil {
		if skip := (deleteSkipped{}); errors.As(err, &skip) {
			return skipped(skip.why)
		}
		return infer.DeleteResponse{}, fmt.Errorf("deletePrompt failed: %w; to delete without running it, set "+
			"%s=true for this command (for example `%s=true pulumi destroy`)", err, skipDeletePromptsEnv,
			skipDeletePromptsEnv)
	}
	return infer.DeleteResponse{}, nil
}

// deleteSkipped ends a delete prompt's run before it starts, for deletePromptSkip's reason.
type deleteSkipped struct{ why string }

func (d deleteSkipped) Error() string { return "deletePrompt not run: " + d.why }

// deletePromptSkip returns why a delete prompt is not run, or "":
//
//   - DOCKER_AGENT_SKIP_DELETE_PROMPTS is true in the environment of the `pulumi` command;
//   - the stored workingDir no longer exists, or is no longer a directory: there is nothing
//     left to clean up there. Another error (a directory that cannot be read) does not skip
//     the prompt: its run fails and says why;
//   - an AgentRun of this provider process started in the same workingDir: a replacement
//     or a rename without aliases created the new run before deleting the old one, whose
//     cleanup would undo the new run's work.
func deletePromptSkip(args AgentRunArgs) string {
	if skip, err := strconv.ParseBool(os.Getenv(skipDeletePromptsEnv)); err == nil && skip {
		return skipDeletePromptsEnv + " is set"
	}
	wd := runString(args.WorkingDir)
	if wd == "" {
		return ""
	}
	switch fi, err := os.Stat(wd); {
	case errors.Is(err, fs.ErrNotExist):
		return fmt.Sprintf("workingDir %q no longer exists", wd)
	case err == nil && !fi.IsDir():
		return fmt.Sprintf("workingDir %q is no longer a directory", wd)
	}
	if dir, ok := resolvedDir(wd); ok {
		if _, used := usedDirs.Load(dir); used {
			return fmt.Sprintf("an AgentRun already ran in workingDir %q during this pulumi command (a replacement, or "+
				"a rename without aliases, creates the new run before it deletes the old one), so this delete prompt "+
				"would undo its work; use the deleteBeforeReplace resource option or aliases", wd)
		}
	}
	return ""
}

// Process-local state of AgentRun. The engine starts a provider process for each Pulumi
// operation (a preview, an update, a refresh or a destroy), so in production these sets
// live for one operation. A provider process that the engine attaches to
// (PULUMI_DEBUG_PROVIDERS) serves several commands: withCommandScopedState forgets the
// sets when another engine connects.
var (
	// alwaysChecked holds the URNs of the AgentRuns with `rerun: always` that Check saw (see
	// agentRunResource).
	alwaysChecked sync.Map
	// usedDirs holds the resolved working directories in which a Create or an Update started
	// the agent (see deletePromptSkip).
	usedDirs sync.Map
)

// engineAddress is the address of the engine whose command the process-local state belongs to.
var engineAddress struct {
	sync.Mutex
	addr string
}

// withCommandScopedState forgets the process-local AgentRun state when a provider process
// is handshaken by another engine than before, which is another Pulumi command. Every
// provider instance of one command handshakes with the same engine address.
func withCommandScopedState(prov p.Provider) p.Provider {
	inner := prov.Handshake
	prov.Handshake = func(ctx context.Context, req p.HandshakeRequest) (p.HandshakeResponse, error) {
		engineAddress.Lock()
		if req.EngineAddress != "" && req.EngineAddress != engineAddress.addr {
			if engineAddress.addr != "" {
				alwaysChecked.Clear()
				usedDirs.Clear()
			}
			engineAddress.addr = req.EngineAddress
		}
		engineAddress.Unlock()
		if inner == nil {
			return p.HandshakeResponse{}, nil
		}
		return inner(ctx, req)
	}
	return prov
}

// resolvedDir resolves a workingDir like agentrun.Run does, and its symbolic links.
func resolvedDir(wd string) (string, bool) {
	abs, err := filepath.Abs(wd)
	if err != nil {
		return "", false
	}
	dir, err := filepath.EvalSymlinks(abs)
	return dir, err == nil
}

// WireDependencies declares secretness. `env` never makes an output secret. `output` and
// `structuredOutput` are secret with `outputSecret`, or when `prompt`, `updatePrompt` or
// `yaml` is secret, since the answer can repeat them (agentRunResource also marks them secret when the
// answer contains a secret value). The run's outputs are only unknown when the agent runs
// again, which agentRunResource decides, so they are declared always known here; the
// inputs stored in the state keep their own secretness and computedness.
func (*AgentRun) WireDependencies(f infer.FieldSelector, args *AgentRunArgs, state *AgentRunState) {
	prompt, yaml, agent := f.InputField(&args.Prompt).Secret(), f.InputField(&args.YAML).Secret(), f.InputField(&args.Agent).Secret()
	updatePrompt := f.InputField(&args.UpdatePrompt).Secret()
	for _, out := range []any{&state.Output, &state.StructuredOutput} {
		f.OutputField(out).AlwaysKnown()
		if args.OutputSecret != nil && *args.OutputSecret {
			f.OutputField(out).AlwaysSecret()
		} else {
			f.OutputField(out).DependsOn(prompt, updatePrompt, yaml)
		}
	}
	f.OutputField(&state.AgentName).AlwaysKnown()
	f.OutputField(&state.AgentName).DependsOn(agent, yaml)
	f.OutputField(&state.ToolCalls).AlwaysKnown()
	f.OutputField(&state.ToolCalls).DependsOn(yaml)
	for _, out := range []any{&state.Usage, &state.RanAt} {
		f.OutputField(out).AlwaysKnown()
		f.OutputField(out).NeverSecret()
	}

	f.OutputField(&state.YAML).DependsOn(f.InputField(&args.YAML))
	f.OutputField(&state.Reference).DependsOn(f.InputField(&args.Reference))
	f.OutputField(&state.Agent).DependsOn(f.InputField(&args.Agent))
	f.OutputField(&state.Prompt).DependsOn(f.InputField(&args.Prompt))
	f.OutputField(&state.UpdatePrompt).DependsOn(f.InputField(&args.UpdatePrompt))
	f.OutputField(&state.DeletePrompt).DependsOn(f.InputField(&args.DeletePrompt))
	f.OutputField(&state.Env).AlwaysSecret()
	f.OutputField(&state.Env).DependsOn(f.InputField(&args.Env).Computed())
	f.OutputField(&state.InheritEnvironment).DependsOn(f.InputField(&args.InheritEnvironment))
	f.OutputField(&state.WorkingDir).DependsOn(f.InputField(&args.WorkingDir))
	f.OutputField(&state.ToolApproval).DependsOn(f.InputField(&args.ToolApproval))
	f.OutputField(&state.TimeoutSeconds).DependsOn(f.InputField(&args.TimeoutSeconds))
	f.OutputField(&state.Triggers).DependsOn(f.InputField(&args.Triggers))
	f.OutputField(&state.Rerun).DependsOn(f.InputField(&args.Rerun))
	f.OutputField(&state.OutputSecret).DependsOn(f.InputField(&args.OutputSecret))
	f.OutputField(&state.AllowHookApprovals).DependsOn(f.InputField(&args.AllowHookApprovals))
	f.OutputField(&state.AllowHarnessAgents).DependsOn(f.InputField(&args.AllowHarnessAgents))
	f.OutputField(&state.Logging).DependsOn(f.InputField(&args.Logging))
}

// runAgent runs the agent with a prompt. A delete run (deleting) announces the
// coding-harness warnings that no Check of the operation reported, and its answer is
// discarded by the caller.
func runAgent(ctx context.Context, name string, args AgentRunArgs, prompt string, deleting bool) (AgentRunState, error) {
	state := AgentRunState{AgentRunArgs: args}
	doc, err := agentDocument(ctx, args)
	if err != nil {
		return state, err
	}
	// Secret inputs (env values, a secret yaml or prompt) never appear in errors,
	// warnings or logs. agentRunResource collects them from the raw inputs.
	rc := runContextFrom(ctx)
	var secrets []string
	if rc != nil {
		secrets = rc.secrets
		if rc.name != "" {
			name = rc.name
		}
	}
	// docker-agent's own log records: ERROR records of a successful run (a model that
	// fell back, a toolset that failed to start) become warnings, everything else is
	// debug output. A failed run reports its error instead.
	var logs []runLogRecord
	var logsMu sync.Mutex
	// Progress is shown while the agent runs; the summary comes after the warnings.
	logger := userLog(ctx)
	var summary string
	progress := func(pr agentrun.Progress) {
		switch {
		case pr.Summary:
			summary = pr.Message
		case pr.Persistent:
			logger.Info(pr.Message)
		default:
			logger.InfoStatus(pr.Message)
		}
	}
	res, err := agentrun.Run(ctx, agentrun.Request{
		Name:               name,
		YAML:               doc,
		Agent:              runString(args.Agent),
		Prompt:             prompt,
		Env:                args.Env,
		InheritEnvironment: inheritEnvironment(args),
		WorkingDir:         runString(args.WorkingDir),
		ToolApproval:       agentrun.ToolApproval(toolApproval(args)),
		AllowHookApprovals: runBool(args.AllowHookApprovals),
		Timeout:            time.Duration(timeoutOf(args)) * time.Second,
		Secrets:            secrets,
		Log: func(level slog.Level, msg string) {
			logsMu.Lock()
			defer logsMu.Unlock()
			logs = append(logs, runLogRecord{level, msg})
		},
		Progress:           progress,
		Logging:            runLogging(args, rc),
		HideNames:          rc != nil && rc.namesSecret,
		AllowHarnessAgents: runBool(args.AllowHarnessAgents),
		// Check validated only a `yaml` configuration, and no Check runs in a destroy.
		AnnounceHarness: deleting || runString(args.YAML) == "",
		Warn:            logger.Warning,
		Start:           runStart(args, deleting),
	})
	logsMu.Lock()
	defer logsMu.Unlock()
	for _, l := range logs {
		if err == nil && l.level >= slog.LevelError {
			logger.Warning("docker-agent: " + l.msg)
		} else {
			logger.Debug("docker-agent: " + l.msg)
		}
	}
	if err != nil {
		return state, err
	}
	for _, w := range res.Warnings {
		logger.Warning("docker-agent: " + w)
	}
	if summary != "" {
		logger.Info(summary)
	}
	state.Output = res.Output
	if rc != nil {
		rc.answerSecret = res.OutputContainsSecret
	}
	if res.HasStructuredOutput {
		state.StructuredOutput = res.StructuredOutput
	}
	state.AgentName = res.AgentName
	state.Usage = AgentRunUsage{
		InputTokens:  int(res.Usage.InputTokens),
		OutputTokens: int(res.Usage.OutputTokens),
		Cost:         res.Usage.Cost,
	}
	state.ToolCalls = res.ToolCalls
	state.RanAt = time.Now().UTC().Format(time.RFC3339)
	return state, nil
}

// runStart is called when a run holds the run slot. A Create or an Update marks its
// workingDir as used, before the agent can change it, so that a delete prompt that waited
// for the slot sees it. A delete prompt checks deletePromptSkip again.
func runStart(args AgentRunArgs, deleting bool) func() error {
	return func() error {
		if deleting {
			if why := deletePromptSkip(args); why != "" {
				return deleteSkipped{why}
			}
			return nil
		}
		if wd := runString(args.WorkingDir); wd != "" {
			if dir, ok := resolvedDir(wd); ok {
				usedDirs.Store(dir, true)
			}
		}
		return nil
	}
}

// agentDocument returns the configuration to run: `yaml`, or the artifact `reference`
// points to.
func agentDocument(ctx context.Context, args AgentRunArgs) ([]byte, error) {
	if y := runString(args.YAML); y != "" {
		return []byte(y), nil
	}
	ref := runString(args.Reference)
	if ref == "" {
		return nil, fmt.Errorf("one of `yaml` and `reference` is required")
	}
	pulled, err := ociagent.Pull(ctx, ref, registryOptions(ctx, nil))
	if err != nil {
		return nil, fmt.Errorf("pulling %s: %w", ref, err)
	}
	return pulled.YAML, nil
}

type runLogRecord struct {
	level slog.Level
	msg   string
}

func runString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func runBool(b *bool) bool { return b != nil && *b }

func inheritEnvironment(a AgentRunArgs) bool {
	return a.InheritEnvironment == nil || *a.InheritEnvironment
}

func toolApproval(a AgentRunArgs) ToolApproval {
	if a.ToolApproval == nil || *a.ToolApproval == "" {
		return ToolApprovalDeny
	}
	return *a.ToolApproval
}

func rerunOf(a AgentRunArgs) RerunPolicy {
	if a.Rerun == nil || *a.Rerun == "" {
		return RerunOnChange
	}
	return *a.Rerun
}

func loggingOf(a AgentRunArgs) Logging {
	if a.Logging == nil || *a.Logging == "" {
		return LoggingStatus
	}
	return *a.Logging
}

// runLogging returns the progress a run reports: `logging`, without the arguments, messages
// and results of `tools` and `full` when the answer is secret.
func runLogging(args AgentRunArgs, rc *runContext) agentrun.Logging {
	level := loggingOf(args)
	if rc != nil && rc.contentSecret && (level == LoggingTools || level == LoggingFull) {
		level = LoggingStatus
	}
	return agentrun.Logging(level)
}

func timeoutOf(a AgentRunArgs) int {
	if a.TimeoutSeconds == nil || *a.TimeoutSeconds <= 0 {
		return int(agentrun.DefaultTimeout / time.Second)
	}
	return *a.TimeoutSeconds
}

// NewAgentRunResource returns the AgentRun resource for the provider builder. It wraps
// the inferred resource:
//
//   - previews report every output of a (re)run as unknown: pulumi-go-provider only marks
//     an output unknown when an input it depends on changed, and misses inputs that were
//     absent before (for example newly added `triggers`);
//   - `rerun: always` is a change only in a Diff that follows a Check of the same resource
//     in this provider process. The engine calls Check only while the program registers a
//     resource of the operation, never in `pulumi refresh`, and in `pulumi up --refresh`
//     the refresh (Read, then Diff) of a resource comes before its Check. Read clears the
//     mark, so the refresh Diff, which compares the stored inputs with themselves, reports
//     no drift;
//   - the requests carry the resource name (the IDs of new resources have a suffix) and,
//     for Delete, the secrets of the stored state.
func NewAgentRunResource() infer.InferredResource {
	return agentRunResource{infer.Resource(&AgentRun{})}
}

type agentRunResource struct {
	infer.InferredResource
}

// runContext passes per-call information between agentRunResource and the typed
// resource: the secret input strings (redacted from errors and logs) in, whether the
// answer contains a secret and whether the agent runs out.
type runContext struct {
	// name is the resource name, which labels the configuration in docker-agent messages.
	name         string
	secrets      []string
	answerSecret bool
	rerun        bool
	// contentSecret and namesSecret keep secret content out of the progress messages: the
	// answer is secret (outputSecret, a secret prompt or yaml), or the configuration's
	// names are (a secret yaml or agent).
	contentSecret, namesSecret bool
}

func newRunContext(urn resource.URN, inputs property.Map) *runContext {
	outputSecret := inputs.Get("outputSecret")
	yaml := inputs.Get("yaml").Secret()
	return &runContext{
		name:    urn.Name(),
		secrets: secretStrings(inputs),
		contentSecret: (outputSecret.IsBool() && outputSecret.AsBool()) || outputSecret.Secret() || yaml ||
			inputs.Get("prompt").Secret() || inputs.Get("updatePrompt").Secret(),
		namesSecret: yaml || inputs.Get("agent").Secret(),
	}
}

type runContextKey struct{}

func runContextFrom(ctx context.Context) *runContext {
	rc, _ := ctx.Value(runContextKey{}).(*runContext)
	return rc
}

// withToolCalls returns a stored state with `toolCalls`. v0.1.1 dropped an empty list from
// the state of an update that did not run the agent, and a state without the required
// output fails to decode, which failed every later Diff, Update and Delete.
func withToolCalls(state property.Map) property.Map {
	if _, ran := state.GetOk("ranAt"); !ran {
		return state
	}
	if v, ok := state.GetOk("toolCalls"); !ok || v.IsNull() {
		return state.Set("toolCalls", property.New([]property.Value{}))
	}
	return state
}

// Check marks a resource with a known `rerun: always` for the Diff that follows.
func (r agentRunResource) Check(ctx context.Context, req p.CheckRequest) (p.CheckResponse, error) {
	resp, err := r.InferredResource.Check(ctx, req)
	if err != nil {
		return resp, err
	}
	if v := resp.Inputs.Get("rerun"); !req.Inputs.Get("rerun").IsComputed() && v.IsString() &&
		v.AsString() == string(RerunAlways) {
		alwaysChecked.Store(req.Urn, true)
	} else {
		alwaysChecked.Delete(req.Urn)
	}
	return resp, nil
}

// Read clears the mark of Check: the Diff of a refresh follows it.
func (r agentRunResource) Read(ctx context.Context, req p.ReadRequest) (p.ReadResponse, error) {
	alwaysChecked.Delete(req.Urn)
	req.Properties = withToolCalls(req.Properties)
	return r.InferredResource.Read(ctx, req)
}

// Delete passes the secrets of the stored state to the delete prompt's run.
func (r agentRunResource) Delete(ctx context.Context, req p.DeleteRequest) error {
	req.Properties = withToolCalls(req.Properties)
	rc := newRunContext(req.Urn, req.Properties)
	rc.contentSecret = rc.contentSecret || req.Properties.Get("deletePrompt").Secret()
	return r.InferredResource.Delete(context.WithValue(ctx, runContextKey{}, rc), req)
}

func (r agentRunResource) Create(ctx context.Context, req p.CreateRequest) (p.CreateResponse, error) {
	rc := newRunContext(req.Urn, req.Properties)
	resp, err := r.InferredResource.Create(context.WithValue(ctx, runContextKey{}, rc), req)
	if err != nil {
		return resp, err
	}
	if req.DryRun {
		resp.Properties = agentRunPreview(req.Properties, resp.Properties)
	} else if rc.answerSecret {
		resp.Properties = markSecret(resp.Properties, "output", "structuredOutput")
	}
	return resp, nil
}

func (r agentRunResource) Update(ctx context.Context, req p.UpdateRequest) (p.UpdateResponse, error) {
	req.State = withToolCalls(req.State)
	rc := newRunContext(req.Urn, req.Inputs)
	resp, err := r.InferredResource.Update(context.WithValue(ctx, runContextKey{}, rc), req)
	if err != nil {
		return resp, err
	}
	switch {
	case req.DryRun && (rc.rerun || req.Inputs.HasComputed()):
		resp.Properties = agentRunPreview(req.Inputs, resp.Properties)
	case rc.answerSecret:
		resp.Properties = markSecret(resp.Properties, "output", "structuredOutput")
	}
	return resp, nil
}

// Diff reports an update for every input that is unknown during a preview (the typed Diff
// sees unknown values as zero values, which equal an absent old value), and for `rerun:
// always` after a Check of this resource.
func (r agentRunResource) Diff(ctx context.Context, req p.DiffRequest) (p.DiffResponse, error) {
	req.State = withToolCalls(req.State)
	resp, err := r.InferredResource.Diff(ctx, req)
	if err != nil {
		return resp, err
	}
	change := func(k string) {
		if resp.DetailedDiff == nil {
			resp.DetailedDiff = map[string]p.PropertyDiff{}
		}
		resp.DetailedDiff[k] = p.PropertyDiff{Kind: p.Update, InputDiff: true}
		resp.HasChanges = true
	}
	for k, v := range req.Inputs.All {
		if v.HasComputed() {
			change(k)
		}
	}
	if v := req.Inputs.Get("rerun"); v.IsString() && v.AsString() == string(RerunAlways) {
		if _, checked := alwaysChecked.Load(req.Urn); checked {
			change("rerun")
		}
	}
	return resp, nil
}

// agentRunPreview marks every run output unknown, keeping the secretness the real outputs
// will have.
func agentRunPreview(inputs, out property.Map) property.Map {
	outputSecret := inputs.Get("outputSecret")
	answerSecret := (outputSecret.IsBool() && outputSecret.AsBool()) || outputSecret.Secret() ||
		inputs.Get("prompt").Secret() || inputs.Get("updatePrompt").Secret() || inputs.Get("yaml").Secret()
	for _, k := range agentRunOutputs {
		secret := false
		switch k {
		case "output", "structuredOutput":
			secret = answerSecret
		case "agentName":
			secret = inputs.Get("agent").Secret() || inputs.Get("yaml").Secret()
		case "toolCalls":
			secret = inputs.Get("yaml").Secret()
		}
		out = out.Set(k, property.New(property.Computed).WithSecret(secret))
	}
	return out
}
