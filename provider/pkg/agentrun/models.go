// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package agentrun

import (
	"cmp"
	"fmt"
	"maps"
	"net"
	"net/url"
	"strings"

	"github.com/docker/docker-agent/pkg/chatgpt"
	"github.com/docker/docker-agent/pkg/config"
	"github.com/docker/docker-agent/pkg/config/latest"
	"github.com/docker/docker-agent/pkg/model/provider"
	"github.com/docker/docker-agent/pkg/modelsdev"
)

// The models of a configuration as docker-agent builds them. The preview checks
// (check_models.go), the credential line of a run (credentials.go) and the ChatGPT login
// (chatgpt.go) need to know which providers the agents' models use, how they authenticate
// and which variables their SDKs read.

// Provider types (provider.ResolveType) and SDK names of modelUse.sdk.
const (
	typeOpenAI    = "openai"
	typeAnthropic = "anthropic"
	typeGoogle    = "google"
	typeDMR       = "dmr"
	typeBedrock   = "amazon-bedrock"
	sdkBedrock    = "bedrock"
)

// modelUse is a model a configuration's agents (not coding-harness agents) send requests to.
type modelUse struct {
	// loc names where the configuration uses it first, for example "agents.root.model" or
	// "models.m.routing[0].model".
	loc string
	// role says how an agent uses it: "" (the agent's model), "fallback", "routed",
	// "compaction", "title" or "tool".
	role string
	// ref is the reference: a key of `models`, an inline provider/model, or auto.
	ref string
	// name is the provider as the configuration names it (a custom provider's key, an alias,
	// a core type), and model the model ID.
	name, model string
	// cfg is the model with the defaults docker-agent applies (pkg/model/provider/defaults.go):
	// a custom provider's base_url, token_key, auth, provider_opts and api_type, or an alias's
	// base_url and token variable; Provider is the effective provider (for a custom provider
	// its `provider`, for example chatgpt).
	cfg latest.ModelConfig
	// typ is the provider type that serves the model (provider.ResolveType): openai,
	// openai_chatcompletions, openai_responses, anthropic, google, dmr or amazon-bedrock.
	typ string
	// custom reports a custom provider of `providers` (docker-agent pins its api_type and sends
	// no key without token_key).
	custom bool
	// auto is model auto (cfg is empty until resolved); firstAvailable a candidate of a
	// first_available selector.
	auto, firstAvailable bool
}

// configModels returns the models the configuration's agents can send requests to, each once:
// their models (several for an alloy), fallbacks, compaction models and the models of their
// toolsets and forked skills, with the targets of routing models, the title and compaction
// models of models, and every candidate of a first_available selector. model auto is included
// unresolved. cfg is a configuration config.Load returned (inline references are entries of
// cfg.Models).
func configModels(cfg *latest.Config) []modelUse {
	var out []modelUse
	seen := map[string]bool{}
	var visit func(ref, loc, role string, firstAvailable bool)
	visit = func(ref, loc, role string, firstAvailable bool) {
		ref = strings.TrimSpace(ref)
		if ref == "" || seen[ref] {
			return
		}
		seen[ref] = true
		if ref == "auto" {
			out = append(out, modelUse{loc: loc, role: role, ref: ref, auto: true})
			return
		}
		m, named := cfg.Models[ref]
		if !named {
			inline, err := latest.ParseModelRef(ref)
			if err != nil {
				return
			}
			m = inline
		}
		if m.IsFirstAvailable() {
			for i, c := range m.FirstAvailable {
				visit(c, fmt.Sprintf("models.%s.first_available[%d]", ref, i), role, true)
			}
			return
		}
		u := newModelUse(cfg, m)
		u.loc, u.role, u.ref, u.firstAvailable = loc, role, ref, firstAvailable
		out = append(out, u)
		for i, r := range m.Routing {
			visit(r.Model, fmt.Sprintf("models.%s.routing[%d].model", ref, i), "routed", false)
		}
		if m.CompactionModel != "" {
			visit(m.CompactionModel, "models."+ref+".compaction_model", "compaction", false)
		}
		if m.TitleModel != "" {
			visit(m.TitleModel, "models."+ref+".title_model", "title", false)
		}
	}
	for i := range cfg.Agents {
		a := &cfg.Agents[i]
		if a.Harness != nil {
			continue
		}
		loc := "agents." + a.Name
		for ref := range strings.SplitSeq(a.Model, ",") {
			visit(ref, loc+".model", "", false)
		}
		for j, ref := range a.GetFallbackModels() {
			visit(ref, fmt.Sprintf("%s.fallback.models[%d]", loc, j), "fallback", false)
		}
		if ref := config.EffectiveCompactionModelRef(cfg, a); ref != "" {
			visit(ref, loc+".compaction_model", "compaction", false)
		}
		for j, ts := range a.Toolsets {
			visit(ts.Model, fmt.Sprintf("%s.toolsets[%d].model", loc, j), "tool", false)
		}
		for j, s := range a.Skills.Inline {
			if s.Context == "fork" {
				visit(s.Model, fmt.Sprintf("%s.skills.inline[%d].model", loc, j), "tool", false)
			}
		}
	}
	return out
}

// newModelUse applies the defaults docker-agent applies to a model (applyProviderDefaults in
// pkg/model/provider/defaults.go): a custom provider's settings, or an alias's base_url and
// token variable.
func newModelUse(cfg *latest.Config, m latest.ModelConfig) modelUse {
	u := modelUse{name: m.Provider, model: m.Model, typ: provider.ResolveType(&m, cfg.Providers)}
	eff := m
	eff.ProviderOpts = maps.Clone(m.ProviderOpts)
	if p, ok := cfg.Providers[m.Provider]; ok {
		u.custom = true
		if p.Provider != "" {
			eff.Provider = modelsdev.CanonicalProviderID(p.Provider)
		}
		eff.BaseURL = cmp.Or(eff.BaseURL, p.BaseURL)
		eff.TokenKey = cmp.Or(eff.TokenKey, p.TokenKey)
		if eff.Auth == nil {
			eff.Auth = p.Auth
		}
		for k, v := range p.ProviderOpts {
			if _, set := eff.ProviderOpts[k]; !set {
				if eff.ProviderOpts == nil {
					eff.ProviderOpts = map[string]any{}
				}
				eff.ProviderOpts[k] = v
			}
		}
	} else {
		eff.Provider = modelsdev.CanonicalProviderID(eff.Provider)
		if alias, ok := provider.LookupAlias(eff.Provider); ok {
			eff.BaseURL = cmp.Or(eff.BaseURL, alias.BaseURL)
			eff.TokenKey = cmp.Or(eff.TokenKey, alias.TokenEnvVar)
		}
	}
	u.cfg = eff
	return u
}

// display names the model for messages: provider/model, for model auto "auto", or kind when
// names are hidden.
func (u modelUse) display(hideNames bool, kind string) string {
	switch {
	case hideNames:
		return kind
	case u.auto:
		return "auto"
	}
	return u.name + "/" + u.model
}

// sdk names the client library that sends the model's requests: "openai" (openai-go: the
// OpenAI protocol, its aliases, custom providers and Docker Model Runner), "anthropic"
// (anthropic-sdk-go), "google" (genai), "bedrock" (the AWS SDK), or "" for model auto.
func (u modelUse) sdk() string {
	switch {
	case u.auto:
		return ""
	case u.typ == typeAnthropic:
		return typeAnthropic
	case u.typ == typeGoogle:
		return typeGoogle
	case u.typ == typeBedrock:
		return sdkBedrock
	}
	return typeOpenAI
}

// isChatGPT reports whether the model uses a ChatGPT subscription (provider chatgpt, also
// through a custom provider).
func (u modelUse) isChatGPT() bool { return !u.auto && u.cfg.Provider == chatgpt.ProviderName }

// isVertex reports whether a google model uses Vertex AI (gemini/client.go): a project or
// location in provider_opts, GOOGLE_GENAI_USE_VERTEXAI in the run's environment, or a
// publisher other than google (Model Garden).
func (u modelUse) isVertex(vertexEnv bool) bool {
	if u.typ != typeGoogle {
		return false
	}
	if u.cfg.ProviderOpts["project"] != nil || u.cfg.ProviderOpts["location"] != nil || vertexEnv {
		return true
	}
	publisher, _ := u.cfg.ProviderOpts["publisher"].(string)
	return publisher != "" && !strings.EqualFold(publisher, typeGoogle)
}

// usesADC reports whether a Vertex AI model gets Google's application default credentials: the
// genai SDK skips them for a base_url without a project or location (genai client.go, skipADC).
func (u modelUse) usesADC() bool {
	return u.cfg.BaseURL == "" || u.cfg.ProviderOpts["project"] != nil || u.cfg.ProviderOpts["location"] != nil
}

// namesChatGPT reports whether the configuration names a chatgpt model anywhere an agent can
// use it (also as a fallback, routing target or first_available candidate). model auto does not
// count: AgentRun never offers the ChatGPT login to auto.
func namesChatGPT(cfg *latest.Config) bool {
	for _, u := range configModels(cfg) {
		if u.isChatGPT() {
			return true
		}
	}
	return false
}

// isLoopbackURL reports whether a URL's host is this machine.
func isLoopbackURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// hostOf returns the scheme and host of a URL, without a path, userinfo or query.
func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "the configured endpoint"
	}
	return u.Scheme + "://" + u.Host
}
