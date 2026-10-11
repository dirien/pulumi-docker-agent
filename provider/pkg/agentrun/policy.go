// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package agentrun

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"sync"

	"github.com/docker/docker-agent/pkg/permissions"
	"github.com/docker/docker-agent/pkg/runtime/toolexec"
	"github.com/docker/docker-agent/pkg/safety"
	"github.com/docker/docker-agent/pkg/session"
	"github.com/docker/docker-agent/pkg/tools"
)

// docker-agent lets hooks approve tool calls: a pre_tool_use hook that answers
// `permission_decision: allow`, or a permission_request hook, runs a call that the safety
// mode would have asked about (and an unattended run would therefore reject). Under
// ToolApprovalDeny such approvals must not count unless Request.AllowHookApprovals is
// set, so every toolset of the run is wrapped in a guard that re-applies docker-agent's
// own approval decision without hooks right before a tool's handler runs:
//
//   - the configuration's `permissions` rules (deny wins, allow runs),
//   - the session's safety mode applied to the call's safety label,
//   - and, in docker-agent's default mode, tools annotated read-only.
//
// A call docker-agent rejected never reaches the guard. Runtime-managed calls
// (transfer_task, handoff, structured output, background agents) do not go through
// toolset handlers and keep docker-agent's rules.

// guardMarker starts the tool error of a call the guard rejected.
const guardMarker = "AgentRun did not run tool"

// approvalPolicy is the run's approval decision. It is bound after the team is loaded
// (toolsets are created during the load) and before the agent runs.
type approvalPolicy struct {
	mu       sync.RWMutex
	enforce  bool
	mode     session.SafetyPolicy
	checkers []toolexec.NamedChecker
}

func (p *approvalPolicy) bind(enforce bool, mode session.SafetyPolicy, perms *permissions.Checker) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.enforce = enforce
	p.mode = mode
	p.checkers = nil
	if perms != nil && !perms.IsEmpty() {
		p.checkers = []toolexec.NamedChecker{{Checker: perms, Source: "permissions configuration", Tier: toolexec.TierTeam}}
	}
}

// permits reports whether docker-agent would run the call without asking anyone.
func (p *approvalPolicy) permits(tc tools.ToolCall, tool tools.Tool) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if !p.enforce {
		return true
	}
	name := tc.Function.Name
	args := toolexec.ParseToolInput(tc.Function.Arguments)
	destructive := tool.Annotations.DestructiveHint != nil && *tool.Annotations.DestructiveHint
	label := safety.LabelToolCall(name, args, tool.Annotations.ReadOnlyHint, destructive)
	d := toolexec.Decide(p.mode, label, p.checkers, name, permissionArgs(name, args))
	switch d.Outcome {
	case toolexec.OutcomeAllow:
		return true
	case toolexec.OutcomeAsk:
		// docker-agent's default mode runs read-only tools without asking, unless an
		// explicit `ask` rule matched.
		return d.Reason == toolexec.ReasonMode && p.mode == "" &&
			label.Class == safety.ClassSafe && label.Origin == safety.OriginAnnotation
	default:
		return false
	}
}

// permissionArgs mirrors docker-agent's permission arguments: for command tools the
// command is also available under "cmd", so a `shell:cmd=...` rule matches either alias.
func permissionArgs(name string, args map[string]any) map[string]any {
	if !safety.IsCommandTool(name) {
		return args
	}
	cmd, ok := safety.CommandArg(args)
	if !ok {
		return args
	}
	out := maps.Clone(args)
	out["cmd"] = cmd
	return out
}

// guardToolSet runs a tool's handler only when the policy permits the call.
type guardToolSet struct {
	tools.ToolSet
	policy *approvalPolicy
}

var (
	_ tools.Instructable = (*guardToolSet)(nil)
	_ tools.Unwrapper    = (*guardToolSet)(nil)
)

func guard(ts tools.ToolSet, policy *approvalPolicy) tools.ToolSet {
	if ts == nil {
		return nil
	}
	return &guardToolSet{ToolSet: ts, policy: policy}
}

// Unwrap implements tools.Unwrapper, so docker-agent finds the inner toolset's
// capabilities (start/stop, instructions, change notifications, ...).
func (g *guardToolSet) Unwrap() tools.ToolSet { return g.ToolSet }

// Instructions implements tools.Instructable.
func (g *guardToolSet) Instructions() string { return tools.GetInstructions(g.ToolSet) }

func (g *guardToolSet) Tools(ctx context.Context) ([]tools.Tool, error) {
	list, err := g.ToolSet.Tools(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]tools.Tool, len(list))
	for i, tool := range list {
		if tool.Handler != nil {
			handler, def := tool.Handler, tool
			tool.Handler = func(ctx context.Context, tc tools.ToolCall, rt tools.Runtime) (*tools.ToolCallResult, error) {
				if !g.policy.permits(tc, def) {
					return tools.ResultError(fmt.Sprintf("%s '%s': it needs approval, and an unattended Pulumi run with "+
						"toolApproval \"deny\" does not accept approvals from hooks. Continue without this tool.",
						guardMarker, tc.Function.Name)), nil
				}
				return handler(ctx, tc, rt)
			}
		}
		out[i] = tool
	}
	return out, nil
}

func isGuardRejection(response string) bool {
	return strings.HasPrefix(response, guardMarker)
}
