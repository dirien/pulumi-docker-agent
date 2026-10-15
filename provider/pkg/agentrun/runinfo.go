// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package agentrun

import (
	"context"
	"regexp"
	"slices"
	"strings"

	"github.com/docker/docker-agent/pkg/chat"
	"github.com/docker/docker-agent/pkg/chatgpt"
	"github.com/docker/docker-agent/pkg/config"
	"github.com/docker/docker-agent/pkg/config/latest"
	"github.com/docker/docker-agent/pkg/environment"
	"github.com/docker/docker-agent/pkg/modelsdev"
	"github.com/docker/docker-agent/pkg/session"
	"github.com/docker/docker-agent/pkg/team"
)

// What a run child reports about the models of a run: the credential line (D5), a progress
// record at logging status like Claude Code's apiKeySource record, and the credential sources
// on an error the model API rejected for its credentials.

// runModels describes the models of a run: cfg is the configuration (config.Load), t the team
// docker-agent loaded from it. It resolves first_available like the loader (from the run's
// environment, without a token refresh) and takes model auto's model from the team.
type runModels struct {
	uses []modelUse
	view envView
	auto func(modelUse) (modelUse, bool)
}

func newRunModels(ctx context.Context, cfg *latest.Config, req Request, t *team.Team) runModels {
	v := envView{env: req.Env, inherit: req.InheritEnvironment, provider: providerEnvMap()}
	if v.env == nil {
		v.env = map[string]string{}
	}
	prov := v.asProvider()
	// In a run child the chatgpt package reads the run's copy of the login, which exists once
	// the run asked for the login's token.
	if chatgpt.LoggedIn() && slices.ContainsFunc(chatgptUses(ctx, cfg, prov), modelUse.usesChatGPTLogin) {
		prov = environment.NewMultiProvider(prov, loginPresence{})
	}
	selectors := firstAvailableSelectors(cfg)
	_ = config.ResolveFirstAvailableModels(ctx, cfg, "", prov)
	rm := runModels{uses: markFirstAvailable(configModels(cfg), selectors), view: v}
	rm.auto = func(u modelUse) (modelUse, bool) {
		name := strings.TrimSuffix(strings.TrimPrefix(u.loc, "agents."), ".model")
		i := slices.IndexFunc(cfg.Agents, func(a latest.AgentConfig) bool { return a.Name == name })
		if i < 0 || t == nil {
			return modelUse{}, false
		}
		a, err := t.Agent(name)
		if err != nil {
			return modelUse{}, false
		}
		refs := strings.Split(cfg.Agents[i].Model, ",")
		models := a.ConfiguredModels()
		j := slices.IndexFunc(refs, func(r string) bool { return strings.TrimSpace(r) == "auto" })
		if j < 0 || j >= len(models) || len(refs) != len(models) {
			return modelUse{}, false
		}
		r := newModelUse(cfg, models[j].BaseConfig().ModelConfig)
		r.loc = u.loc
		return r, true
	}
	return rm
}

// line is the run's credential line.
func (rm runModels) line(hideNames bool) string {
	return credentialLine(rm.uses, rm.view, rm.auto, hideNames)
}

// chatgptAnswered reports whether a chatgpt model answered during the run (an assistant message
// of the session or a sub-session that a model of provider chatgpt, or of a custom provider of
// it, wrote): that usage is billed to the ChatGPT plan, and docker-agent has no price for it.
func chatgptAnswered(cfg *latest.Config, sess *session.Session) bool {
	if cfg == nil || sess == nil {
		return false
	}
	names := []string{chatgpt.ProviderName}
	for name, p := range cfg.Providers {
		if modelsdev.CanonicalProviderID(p.Provider) == chatgpt.ProviderName {
			names = append(names, name)
		}
	}
	for _, m := range sess.GetAllMessages() {
		if m.Message.Role != chat.MessageRoleAssistant {
			continue
		}
		if p, _, ok := strings.Cut(m.Message.Model, "/"); ok && slices.Contains(names, p) {
			return true
		}
	}
	return false
}

// authFailure matches a model API's rejection of a request's credentials.
var authFailure = regexp.MustCompile(`(?i)\b40[13]\b|\bunauthori[sz]ed\b|\bforbidden\b|invalid[ _-]?(api[ _-]?)?key|` +
	`incorrect api key|authentication[ _]error|failed to authenticate|invalid[ _]token|token (is )?expired`)

// isAuthFailure reports whether an error says that a model API rejected the credentials.
func isAuthFailure(text string) bool { return authFailure.MatchString(text) }

// connectionFailure matches a request that did not reach a model API or reached the wrong one.
var connectionFailure = regexp.MustCompile(`(?i)connection refused|no such host|dial tcp|i/o timeout|` +
	`x509:|tls: |\b404\b|failed to find default credentials|get credentials|no EC2 IMDS role`)

// isConnectionFailure reports whether an error says that a request did not reach its endpoint, or
// found no credentials of the machine.
func isConnectionFailure(text string) bool { return connectionFailure.MatchString(text) }
