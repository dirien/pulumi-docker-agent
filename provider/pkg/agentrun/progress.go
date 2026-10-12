// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package agentrun

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/docker/docker-agent/pkg/chat"
	"github.com/docker/docker-agent/pkg/runtime"
	"github.com/docker/docker-agent/pkg/tools/builtin/structuredoutput"
)

// Logging selects the progress a run reports through Request.Progress.
type Logging string

const (
	// LoggingNone reports nothing.
	LoggingNone Logging = "none"
	// LoggingStatus reports status updates while the run is in progress (an agent started,
	// a task was transferred or the conversation handed off to another agent, a tool call
	// was made and approved or rejected, a model failed over to its fallback) and a summary
	// when it succeeds (duration, tool calls, tokens and estimated cost).
	LoggingStatus Logging = "status"
	// LoggingTools also reports each tool call, with its arguments, as a persistent message.
	LoggingTools Logging = "tools"
	// LoggingFull also reports the agents' messages and the tools' results.
	LoggingFull Logging = "full"
)

// Progress is a progress message of a run. Messages are redacted like the run's errors.
type Progress struct {
	Message string
	// Persistent messages are part of the run's record (tool calls, messages, the summary).
	// The others are status updates, only worth showing while they are the latest.
	Persistent bool
	// Summary marks the last message of a successful run: its duration, tool calls, tokens
	// and estimated cost. It is persistent.
	Summary bool
}

// Lengths of the parts of progress messages that quote the run.
const (
	maxArgs    = 300
	maxResult  = 500
	maxMessage = 2000
	maxReason  = 200
)

// progress turns the run's events into progress messages.
type progress struct {
	level   Logging
	emit    func(Progress)
	secrets []string
	// hideNames replaces agent, tool and model names, which are part of a secret
	// configuration, and names hides them in the text the messages quote.
	hideNames bool
	names     *nameHider
	started   time.Time
	sessions  map[string]bool
	calls     map[string]string // tool call ID -> arguments
}

func newProgress(req Request, secrets []string, names *nameHider) *progress {
	level := req.Logging
	if level == "" {
		level = LoggingStatus
	}
	if req.Progress == nil {
		level = LoggingNone
	}
	return &progress{
		level: level, emit: req.Progress, secrets: secrets, hideNames: req.HideNames, names: names,
		started: time.Now(), sessions: map[string]bool{}, calls: map[string]string{},
	}
}

func (p *progress) at(level Logging) bool {
	order := map[Logging]int{LoggingNone: 0, LoggingStatus: 1, LoggingTools: 2, LoggingFull: 3}
	return order[p.level] >= order[level] && p.level != LoggingNone
}

func (p *progress) status(format string, args ...any) {
	p.emit(Progress{Message: redact(fmt.Sprintf(format, args...), p.secrets)})
}

func (p *progress) record(format string, args ...any) {
	p.emit(Progress{Message: redact(fmt.Sprintf(format, args...), p.secrets), Persistent: true})
}

// quote returns s, without hidden names, redacted, on one line and shortened to n runes.
// Redacting first keeps a secret that the shortening would cut from slipping through.
func (p *progress) quote(s string, n int) string {
	return truncate(redact(p.names.hide(s), p.secrets), n)
}

// name returns a name of the configuration, or what it stands for ("an agent") when names are
// hidden.
func (p *progress) name(name, kind string) string {
	if p.hideNames || name == "" {
		return kind
	}
	return name
}

func (p *progress) observe(ev runtime.Event) {
	if !p.at(LoggingStatus) {
		return
	}
	switch e := ev.(type) {
	case *runtime.StreamStartedEvent:
		if !p.sessions[e.SessionID] {
			p.sessions[e.SessionID] = true
			p.status("%s started", p.name(e.AgentName, "an agent"))
		}
	case *runtime.AgentSwitchingEvent:
		if e.Switching {
			p.status("%s -> %s: task transferred", p.name(e.FromAgent, "an agent"), p.name(e.ToAgent, "another agent"))
		} else {
			p.status("%s -> %s: task finished", p.name(e.FromAgent, "an agent"), p.name(e.ToAgent, "the calling agent"))
		}
	case *runtime.ToolCallEvent:
		if e.ToolCall.Function.Name == structuredoutput.ToolName {
			return
		}
		p.calls[e.ToolCall.ID] = e.ToolCall.Function.Arguments
		p.status("%s: calling %s", p.name(e.AgentName, "an agent"), p.name(e.ToolCall.Function.Name, "a tool"))
	case *runtime.ToolCallResponseEvent:
		p.toolResponse(e)
	case *runtime.ModelFallbackEvent:
		p.status("%s: model %s failed (%s), falling back to %s", p.name(e.AgentName, "an agent"),
			p.name(e.FailedModel, "a model"), p.quote(e.Reason, maxReason), p.name(e.FallbackModel, "the next model"))
	case *runtime.MessageAddedEvent:
		if e.Message == nil || e.Message.Implicit || e.Message.Message.Role != chat.MessageRoleAssistant {
			return
		}
		// The arguments of calls that are rejected before they run are only in the message.
		for _, tc := range e.Message.Message.ToolCalls {
			p.calls[tc.ID] = tc.Function.Arguments
		}
		if !p.at(LoggingFull) {
			return
		}
		if text := strings.TrimSpace(chat.VisibleAssistantContent(e.Message.Message.Content)); text != "" {
			p.record("%s: %s", p.name(e.AgentName, "an agent"), p.quote(text, maxMessage))
		}
	}
}

func (p *progress) toolResponse(e *runtime.ToolCallResponseEvent) {
	tool := e.ToolDefinition.Name
	if tool == structuredoutput.ToolName {
		return
	}
	args, known := p.calls[e.ToolCallID]
	delete(p.calls, e.ToolCallID)
	agent, toolName := p.name(e.AgentName, "an agent"), p.name(tool, "a tool")

	outcome := "approved"
	switch {
	case e.Result != nil && e.Result.IsError && isRejection(e.Response):
		outcome = "rejected (" + rejectionReason(e.Response) + ")"
	case e.Result != nil && e.Result.IsError:
		outcome = "approved, failed"
	}
	if tool == "handoff" && outcome == "approved" {
		p.status("%s -> %s: conversation handed off", agent, p.name(argument(args, "agent"), "another agent"))
	} else {
		p.status("%s: %s %s", agent, toolName, outcome)
	}

	if !p.at(LoggingTools) {
		return
	}
	msg := fmt.Sprintf("%s called %s", agent, toolName)
	if known && !p.hideNames {
		msg += " " + p.quote(args, maxArgs)
	}
	msg += ": " + outcome
	if p.at(LoggingFull) && outcome == "approved" && !p.hideNames {
		if result := strings.TrimSpace(e.Response); result != "" {
			msg += ": " + p.quote(result, maxResult)
		}
	}
	p.record("%s", msg)
}

// summary reports a successful run.
func (p *progress) summary(res *Result) {
	if !p.at(LoggingStatus) {
		return
	}
	agent := p.name(res.AgentName, "the agent")
	calls := fmt.Sprintf("%d tool calls", len(res.ToolCalls)+len(res.RejectedToolCalls))
	if len(res.RejectedToolCalls) > 0 {
		calls += fmt.Sprintf(" (%d rejected)", len(res.RejectedToolCalls))
	}
	cost := "no cost estimate (no known pricing for the models)"
	if res.Usage.Cost > 0 {
		cost = fmt.Sprintf("estimated cost $%.4f", res.Usage.Cost)
	}
	took := time.Since(p.started)
	if took < time.Second {
		took = took.Round(time.Millisecond)
	} else {
		took = took.Round(100 * time.Millisecond)
	}
	p.emit(Progress{Message: redact(fmt.Sprintf("agent run finished in %s: %s answered after %s; %d input and %d "+
		"output tokens; %s", took, agent, calls, res.Usage.InputTokens, res.Usage.OutputTokens, cost), p.secrets),
		Persistent: true, Summary: true})
}

// rejectionReason explains a rejected tool call in a few words.
func rejectionReason(response string) string {
	switch {
	case isGuardRejection(response):
		return "a hook approved it, but toolApproval deny does not accept hook approvals"
	case strings.Contains(response, "requires user confirmation"):
		return "it needs approval and toolApproval is deny"
	case strings.Contains(response, "' is denied by"):
		return "denied by the configuration's permissions"
	}
	return "not approved"
}

// argument returns a string argument of a tool call's JSON arguments.
func argument(args, key string) string {
	var m map[string]any
	if json.Unmarshal([]byte(args), &m) != nil {
		return ""
	}
	s, _ := m[key].(string)
	return s
}

// truncate shortens s to at most n runes and puts it on one line.
func truncate(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n]) + "..."
}
