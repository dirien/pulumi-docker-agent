// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package agentrun

import (
	"context"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/docker/docker-agent/pkg/codingharness"
	"github.com/docker/docker-agent/pkg/config"
	"github.com/docker/docker-agent/pkg/config/latest"
)

// CheckOptions are the inputs of an AgentRun that CheckConfig needs.
type CheckOptions struct {
	// AllowHarnessAgents is Request.AllowHarnessAgents. The provider passes true when the
	// input is unknown during a preview, so that only a known false fails the preview.
	AllowHarnessAgents bool
	// InheritEnvironment is Request.InheritEnvironment.
	InheritEnvironment bool
	// EnvKeys are the keys of Request.Env, true when the value is non-empty or unknown.
	EnvKeys map[string]bool
	// EnvUnknown reports that Request.Env is unknown during the preview, so its keys are not
	// known either.
	EnvUnknown bool
	// EnvValues are the values of Request.Env that are known. CheckConfig reads only those of
	// the flags whose value decides a warning (CLAUDE_CODE_USE_BEDROCK, CLAUDE_CODE_USE_VERTEX,
	// CLAUDE_CODE_USE_FOUNDRY, IS_SANDBOX) and never shows a value.
	EnvValues map[string]string
	// DeletePrompt reports that the AgentRun has a delete prompt.
	DeletePrompt bool
	// ToolApprovalDeny reports that Request.ToolApproval is ToolApprovalDeny.
	ToolApprovalDeny bool
	// HideNames keeps the agent and model names out of the messages (a secret yaml or agent).
	HideNames bool
}

// CheckConfig reports what keeps a valid configuration from running in AgentRun, and what
// a user should know before it runs, so that both surface during a preview:
//
//   - problems: coding-harness agents without Request.AllowHarnessAgents (H1), of a type
//     other than claude-code and codex (H2), or on an operating system other than Linux and
//     macOS (H3), and for codex agents what would keep codex from starting or make it delete
//     its login (check_codex.go). Run enforces them too;
//   - warnings: for claude-code agents that may run, that they run without tool approval
//     (W1), the credentials Claude Code would bill (W2), a provider running as root (W3),
//     an instruction (W4) or structured_output (W5) the harness does not get, and a model
//     the harness inherits from the agent (W6), and a CLAUDE_CODE_OAUTH_TOKEN of the Pulumi
//     CLI that inheritEnvironment: false keeps from the CLI (W8); for codex agents the same
//     kinds of warnings in Codex's terms (check_codex.go); for configurations without
//     harness agents, a delete prompt that toolApproval deny can hardly carry out (W7).
//
// A configuration that does not load is reported by the validator, not here.
func CheckConfig(ctx context.Context, name string, yamlDoc []byte, opts CheckOptions) (problems, warnings []string) {
	cfg, err := config.Load(ctx, config.NewBytesSource(name, yamlDoc))
	if err != nil {
		return nil, nil
	}
	agents := harnessAgentsOf(cfg)
	if problems := agents.problems(opts.AllowHarnessAgents, opts.HideNames); len(problems) > 0 {
		return problems, nil
	}
	if len(agents) == 0 {
		if opts.DeletePrompt && opts.ToolApprovalDeny {
			warnings = append(warnings, "deletePrompt runs with toolApproval deny: the agent can only run read-only tools "+
				"and calls that permissions allow, which is rarely enough to clean up; add permissions allow rules, set "+
				"toolApproval: allow, or use a coding-harness agent")
		}
		return nil, warnings
	}

	// The warnings depend on which variables are set, and for flags on their value.
	env := map[string]string{}
	for k, set := range opts.EnvKeys {
		v, known := opts.EnvValues[k]
		switch {
		case !set:
			env[k] = ""
		case known && isFlagEnvVar(k):
			env[k] = v
		case k == "IS_SANDBOX":
			env[k] = "1" // an unknown IS_SANDBOX is taken as the opt-out of the root check
		default:
			env[k] = "set"
		}
	}
	ce := newClaudeEnvironment(os.Environ(), env, opts.InheritEnvironment, "")
	if opts.EnvUnknown {
		ce.withheldToken = false // env may set the token
	}
	codexProblems, codexWarnings := codexChecks(agents, opts)
	if len(codexProblems) > 0 {
		return codexProblems, nil
	}
	warnings = agents.runWarnings(ce, opts.HideNames)
	if geteuid() == 0 && !ce.sandboxed() && len(agents.claudeCode()) > 0 {
		warnings = append(warnings, "the provider runs as root, and Claude Code refuses --dangerously-skip-permissions as "+
			"root: run pulumi up as a non-root user, or, in an isolated container, set IS_SANDBOX=1 in env")
	}
	for _, a := range agents.claudeCode() {
		if a.instruction {
			warnings = append(warnings, agentRef(a.name, "instruction", opts.HideNames)+" is not sent to the coding harness: "+
				"docker-agent sends only the prompt (to a sub-agent: its task); put the instructions into the prompt or into "+
				"a CLAUDE.md in workingDir")
		}
		if a.structuredOutput {
			warnings = append(warnings, agentRef(a.name, "structured_output", opts.HideNames)+" is not enforced for coding "+
				"harnesses: AgentRun decodes Claude Code's final result as JSON (a surrounding Markdown code fence is "+
				"removed), so ask for JSON in the prompt")
		}
		if a.inheritsModel {
			model := fmt.Sprintf(" %q", a.model)
			if opts.HideNames {
				model = ""
			}
			warnings = append(warnings, fmt.Sprintf("%s has no model, so docker-agent passes the agent's model%s to claude "+
				"--model; set harness.model (for example sonnet)", agentRef(a.name, "harness", opts.HideNames), model))
		}
	}
	return nil, append(warnings, codexWarnings...)
}

// harnessAgent is an agent of a configuration that delegates to a coding harness.
type harnessAgent struct {
	name, typ, model string
	instruction      bool
	structuredOutput bool
	// strict reports structured_output.strict.
	strict bool
	// inheritsModel reports that harness.model is empty and the agent's model is not a model
	// ID of the harness: a key of `models:`, several models, or (for claude-code) another
	// provider's model, (for codex) any provider-qualified model.
	inheritsModel bool
}

type harnessAgents []harnessAgent

// harnessAgentsOf returns the coding-harness agents of a loaded configuration.
func harnessAgentsOf(cfg *latest.Config) harnessAgents {
	if len(config.Requires(cfg).Features[config.FeatureHarness]) == 0 {
		return nil
	}
	var out harnessAgents
	for _, a := range cfg.Agents {
		if a.Harness == nil {
			continue
		}
		h := harnessAgent{
			name: a.Name, typ: a.Harness.Type, model: a.Model,
			instruction:      strings.TrimSpace(a.Instruction) != "",
			structuredOutput: a.StructuredOutput != nil,
			strict:           a.StructuredOutput != nil && a.StructuredOutput.Strict,
		}
		if a.Harness.Model == "" && a.Model != "" {
			_, isKey := cfg.Models[a.Model]
			prefix, _, qualified := strings.Cut(a.Model, "/")
			h.inheritsModel = isKey || strings.Contains(a.Model, ",") ||
				qualified && (prefix != "anthropic" || a.Harness.Type == codingharness.TypeCodex)
		}
		out = append(out, h)
	}
	return out
}

// claudeCode returns the claude-code agents.
func (h harnessAgents) claudeCode() harnessAgents {
	return slices.DeleteFunc(slices.Clone(h), func(a harnessAgent) bool { return a.typ != codingharness.TypeClaudeCode })
}

// problems returns the reasons the agents cannot run: H2 for every unsupported harness type,
// then for claude-code and codex agents H3 on an unsupported operating system or else H1
// without the opt-in.
func (h harnessAgents) problems(allowed, hideNames bool) []string {
	byType := map[string][]string{}
	for _, a := range h {
		if !isHarnessSupported(a.typ) {
			byType[a.typ] = append(byType[a.typ], agentRef(a.name, "harness", hideNames))
		}
	}
	var out []string
	for _, typ := range slices.Sorted(maps.Keys(byType)) {
		out = append(out, unsupportedHarnessMessage(typ, strings.Join(slices.Compact(byType[typ]), ", ")))
	}
	if supported := h.supported(); len(supported) > 0 {
		switch {
		case !harnessOSSupported():
			out = append(out, harnessOSMessage())
		case !allowed:
			out = append(out, fmt.Sprintf("coding-harness agents (%s) run only with allowHarnessAgents: true: they start "+
				"an external coding agent CLI that runs its own tools without docker-agent's tool approval",
				supported.list(hideNames)))
		}
	}
	return out
}

// runWarnings are the warnings about running the claude-code agents with an environment:
// W1, W2 with the information that inherited API credentials are dropped, and W8.
func (h harnessAgents) runWarnings(ce claudeEnvironment, hideNames bool) []string {
	claude := h.claudeCode()
	if len(claude) == 0 {
		return nil
	}
	out := []string{fmt.Sprintf("coding-harness agents (%s) run without tool approval: the claude CLI runs its own tools "+
		"(shell commands, file edits, web requests) on the machine that runs pulumi up, in workingDir or a temporary "+
		"directory, with its user's Claude login and settings and any .claude/ settings or .mcp.json in workingDir "+
		"(without a trust prompt); toolApproval and permissions do not apply to it", claude.list(hideNames))}
	return append(out, ce.billingWarnings()...)
}

// list names the agents' harness settings and types, for example
// "agents.root.harness: claude-code".
func (h harnessAgents) list(hideNames bool) string {
	refs := make([]string, 0, len(h))
	for _, a := range h {
		refs = append(refs, agentRef(a.name, "harness", hideNames)+": "+a.typ)
	}
	return strings.Join(slices.Compact(refs), ", ")
}

// agentRef names a setting of an agent: "agents.root.harness", or "an agent's harness" when
// names are hidden.
func agentRef(agent, field string, hideNames bool) string {
	if hideNames {
		return "an agent's " + field
	}
	return "agents." + agent + "." + field
}
