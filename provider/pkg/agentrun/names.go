// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package agentrun

import (
	"slices"
	"strings"
	"sync"

	"github.com/docker/docker-agent/pkg/runtime"
	"gopkg.in/yaml.v3"

	"github.com/dirien/pulumi-docker-agent/provider/pkg/validate"
)

// nameHider keeps the names of a secret configuration (Request.HideNames) out of the text
// docker-agent writes into a run's warnings, errors and logs: the names of its agents,
// models, providers and other named definitions, the models its agents use, and the tools
// the run calls. Messages the provider writes itself use placeholders ("an agent", "a tool")
// instead. A nil nameHider hides nothing.
type nameHider struct {
	mu    sync.Mutex
	names []string // longest first
}

// minHiddenName is the length of the shortest name that is hidden: shorter ones (an agent
// called "a") would hide ordinary words.
const minHiddenName = 2

// newNameHider returns the nameHider of a request, or nil when names may be shown.
func newNameHider(req Request) *nameHider {
	if !req.HideNames {
		return nil
	}
	h := &nameHider{}
	h.add(req.Agent)
	h.add(configNames(req.YAML)...)
	return h
}

// configNames returns the names a configuration defines and the models its agents use.
func configNames(doc []byte) []string {
	var cfg map[string]any
	if yaml.Unmarshal(doc, &cfg) != nil {
		return nil
	}
	var out []string
	addModels := func(v any) {
		if s, ok := v.(string); ok {
			for _, m := range strings.Split(s, ",") {
				out = append(out, strings.TrimSpace(m))
			}
		}
	}
	for section, v := range cfg {
		defs, ok := v.(map[string]any)
		if !ok {
			continue
		}
		for name, def := range defs {
			d, ok := def.(map[string]any)
			if !ok {
				continue // a section of settings, not of named definitions
			}
			out = append(out, name)
			if section == "agents" || section == "models" {
				addModels(d["model"])
			}
		}
	}
	return out
}

// add hides further names.
func (h *nameHider) add(names ...string) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, n := range names {
		if n = strings.TrimSpace(n); len(n) >= minHiddenName && !slices.Contains(h.names, n) {
			h.names = append(h.names, n)
		}
	}
	slices.SortFunc(h.names, func(a, b string) int { return len(b) - len(a) })
}

// observe hides the names of the tools a run calls.
func (h *nameHider) observe(ev runtime.Event) {
	switch e := ev.(type) {
	case *runtime.ToolCallEvent:
		h.add(e.ToolCall.Function.Name, e.AgentName)
	case *runtime.ToolCallResponseEvent:
		h.add(e.ToolDefinition.Name, e.AgentName)
	}
}

// hide replaces every hidden name in s that stands on its own (not inside a longer name)
// with [secret].
func (h *nameHider) hide(s string) string {
	if h == nil || s == "" {
		return s
	}
	h.mu.Lock()
	names := slices.Clone(h.names)
	h.mu.Unlock()
	for _, n := range names {
		s = replaceName(s, n)
	}
	return s
}

// replaceName replaces the occurrences of name in s that are not part of a longer name.
func replaceName(s, name string) string {
	var b strings.Builder
	for {
		i := strings.Index(s, name)
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		end := i + len(name)
		if (i == 0 || !isNameByte(s[i-1])) && (end == len(s) || !isNameByte(s[end])) {
			b.WriteString(s[:i])
			b.WriteString(validate.Redacted)
		} else {
			b.WriteString(s[:end])
		}
		s = s[end:]
	}
}

func isNameByte(c byte) bool {
	return c == '_' || c == '-' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}
