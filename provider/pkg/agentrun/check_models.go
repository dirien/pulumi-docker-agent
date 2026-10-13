// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package agentrun

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/docker/docker-agent/pkg/chatgpt"
	"github.com/docker/docker-agent/pkg/config"
	"github.com/docker/docker-agent/pkg/config/latest"
	"github.com/docker/docker-agent/pkg/environment"
)

// CheckModels reports what a user should know before the models of a valid configuration run in
// AgentRun, for every model provider alike (CheckConfig covers coding-harness agents):
//
//   - warnings: variables the models need that the run's environment does not set (docker-agent's
//     own preflight: config.ResolveFirstAvailableModels, config.GatherEnvVarsForModels and
//     config.GatherEnvVarsForEvaluators), a chatgpt model without a ChatGPT login; variables
//     of the model SDKs that the environment of the Pulumi CLI sets: with inheritEnvironment
//     false the ones that no longer reach the models since v0.2.0 (base URLs first), with
//     inheritEnvironment true the ones that reach them as with docker agent run although
//     docker-agent does not ask for them (an OPENAI_API_KEY sent to another endpoint, OpenAI
//     and Anthropic headers, ANTHROPIC_AUTH_TOKEN, base URLs); Amazon Bedrock and Vertex AI
//     models that use this machine's default AWS or Google credentials; structured_output that
//     an Amazon Bedrock model does not enforce; a Docker models gateway, which AgentRun does
//     not use;
//   - infos: what model auto resolves to, and that a chatgpt model uses the ChatGPT login of
//     this machine.
//
// It reads the environment of this process (the Pulumi CLI's) and the names of docker agent's
// env file, and does no network I/O: no MCP catalog, no `docker model` command, no token
// refresh. The provider calls it only when the operation runs the agent. Without known env
// values (EnvUnknown), only the checks that do not depend on them run.
func CheckModels(ctx context.Context, name string, yamlDoc []byte, opts CheckOptions) (warnings, infos []string) {
	cfg, err := config.Load(ctx, config.NewBytesSource(name, yamlDoc))
	if err != nil {
		return nil, nil
	}
	v := previewView(opts)
	hide := opts.HideNames
	warnings = append(warnings, structuredOutputWarnings(cfg, hide)...)
	warnings = append(warnings, gatewayWarnings(cfg, v, opts.EnvUnknown, hide)...)
	if opts.EnvUnknown {
		return warnings, nil
	}

	prov := v.asProvider()
	loggedIn := chatgpt.LoggedIn()
	if loggedIn && slices.ContainsFunc(chatgptUses(ctx, cfg, prov), modelUse.usesChatGPTLogin) {
		// Load reads the login without refreshing it. An empty CHATGPT_OAUTH_TOKEN of the run's
		// environment hides it, as at run time.
		prov = environment.NewMultiProvider(prov, loginPresence{})
	}
	selectors := firstAvailableSelectors(cfg)
	if err := config.ResolveFirstAvailableModels(ctx, cfg, "", prov); err != nil {
		if vars := missingVariables(err); len(vars) > 0 {
			warnings = append(warnings, joinHints(missingCredentialsMessage(vars, v.inherit, true),
				credentialHints(vars, v.inherit, v.provider)))
		}
	}
	required := slices.Concat(requiredModelVars(ctx, cfg, v, prov), config.GatherEnvVarsForEvaluators(cfg))
	var missing []string
	for _, r := range required {
		if val, _ := prov.Get(ctx, r); r != "" && r != chatgpt.TokenEnvVar && val == "" && !slices.Contains(missing, r) {
			missing = append(missing, r)
		}
	}
	slices.Sort(missing)
	if len(missing) > 0 {
		warnings = append(warnings, joinHints(missingCredentialsMessage(missing, v.inherit, false),
			credentialHints(missing, v.inherit, v.provider)))
	}

	uses := slices.Concat(markFirstAvailable(configModels(cfg), selectors), autoDefaultUses(ctx, cfg, prov))
	for _, u := range uses {
		if !u.usesChatGPTLogin() {
			continue
		}
		model := modelRef(u, hide)
		token := chatgpt.TokenEnvVar
		switch {
		case v.set(token):
		case v.present(token):
			where, remedy := "env sets", "remove it from env to use the login, or pass a ChatGPT access token in env"
			if _, inEnv := v.env[token]; !inEnv {
				where, remedy = "the environment of the Pulumi CLI sets", "unset it there, or set inheritEnvironment: false"
			}
			warnings = append(warnings, fmt.Sprintf("%s uses a ChatGPT subscription, but %s %s to an empty value, which "+
				"hides the ChatGPT login of docker agent setup from the run, so the run fails: %s", model, where, token,
				remedy))
		case loggedIn:
			infos = append(infos, fmt.Sprintf("%s uses the ChatGPT login of docker agent setup on this machine (%s), billed "+
				"to the ChatGPT plan: AgentRun reads that file, and writes a token it refreshed back to it", model,
				chatgptLoginPath()))
		default:
			warnings = append(warnings, fmt.Sprintf("%s uses a ChatGPT subscription, but this machine has no ChatGPT login "+
				"and env does not set %s: sign in with `docker agent setup` on the machine that runs pulumi up (it stores the "+
				"login in %s)", model, token, chatgptLoginPath()))
		}
	}
	warnings = append(warnings, sdkVariableWarnings(uses, v, hide)...)
	autoWarnings, autoInfos := autoChecks(ctx, cfg, v, prov, hide)
	return append(warnings, autoWarnings...), append(infos, autoInfos...)
}

// requiredModelVars are the variables the models need (config.GatherEnvVarsForModels). When a
// preview does not know the value of GOOGLE_GENAI_USE_VERTEXAI yet, which decides between the
// Gemini API and Vertex AI, only the variables both need are required.
func requiredModelVars(ctx context.Context, cfg *latest.Config, v envView, prov environment.Provider) []string {
	const vertex = "GOOGLE_GENAI_USE_VERTEXAI"
	if !v.unknown[vertex] {
		return config.GatherEnvVarsForModels(ctx, cfg, prov)
	}
	gemini := config.GatherEnvVarsForModels(ctx, cfg, environment.NewMultiProvider(fixedVar{vertex, "false"}, prov))
	vertexAI := config.GatherEnvVarsForModels(ctx, cfg, environment.NewMultiProvider(fixedVar{vertex, "true"}, prov))
	return slices.DeleteFunc(gemini, func(name string) bool { return !slices.Contains(vertexAI, name) })
}

// fixedVar serves one variable with a fixed value.
type fixedVar struct{ name, value string }

func (f fixedVar) Get(_ context.Context, name string) (string, bool) {
	if name == f.name {
		return f.value, true
	}
	return "", false
}

// previewView is the environment of a preview: the known values of env, a placeholder for those
// that are not known yet, and this process's environment (the Pulumi CLI's).
func previewView(opts CheckOptions) envView {
	v := envView{env: map[string]string{}, inherit: opts.InheritEnvironment, provider: map[string]string{},
		unknown: map[string]bool{}}
	for k, set := range opts.EnvKeys {
		switch val, known := opts.EnvValues[k]; {
		case !set:
			v.env[k] = ""
		case known:
			v.env[k] = val
		default:
			v.env[k] = "unknown"
			v.unknown[k] = true
		}
	}
	for _, kv := range os.Environ() {
		if k, val, ok := strings.Cut(kv, "="); ok && k != "" {
			v.provider[k] = val
		}
	}
	return v
}

// loginPresence serves CHATGPT_OAUTH_TOKEN, as the ChatGPT login does, without a token: a preview
// only needs to know that the login exists.
type loginPresence struct{}

func (loginPresence) Get(_ context.Context, name string) (string, bool) {
	if name == chatgpt.TokenEnvVar {
		return "the ChatGPT login", true
	}
	return "", false
}

// firstAvailableSelectors returns the names of the first_available models.
func firstAvailableSelectors(cfg *latest.Config) map[string]bool {
	out := map[string]bool{}
	for name, m := range cfg.Models {
		if m.IsFirstAvailable() {
			out[name] = true
		}
	}
	return out
}

// markFirstAvailable marks the models that a resolved first_available selector chose.
func markFirstAvailable(uses []modelUse, selectors map[string]bool) []modelUse {
	for i := range uses {
		if selectors[uses[i].ref] {
			uses[i].firstAvailable = true
		}
	}
	return uses
}

func joinHints(msg string, hints []string) string {
	if len(hints) == 0 {
		return msg
	}
	return msg + "; " + strings.Join(hints, "; ")
}

// modelRef names a model in a message: "agents.root.model (openai/gpt-5-mini)", "agents.root.model
// (auto: chatgpt/gpt-5.5)" for the model DOCKER_AGENT_DEFAULT_MODEL selects for auto, or "a
// model".
func modelRef(u modelUse, hideNames bool) string {
	switch {
	case hideNames:
		return "a model"
	case u.auto:
		return u.loc + " (auto)"
	case u.byDefault != "":
		return u.loc + " (auto: " + u.name + "/" + u.model + ")"
	}
	return u.loc + " (" + u.name + "/" + u.model + ")"
}

// sdkVar is a variable a model SDK reads from the process environment itself.
type sdkVar struct {
	name string
	// kind orders the warnings: base URLs first, then keys, credentials and headers.
	kind int
	// reads reports whether the SDK reads it for the model.
	reads func(u modelUse, v envView) bool
	// lost says what the model loses when the variable does not reach it; endpoint names where
	// its requests go (endpointOf, or "its endpoint" when names are hidden).
	lost func(u modelUse, endpoint string) string
	// leak says what the model gets when it inherits the variable and how to avoid it, or ""
	// when that is what the variable is for (no warning); nil means never.
	leak func(u modelUse, v envView, model, endpoint string) (what, remedy string)
}

const (
	kindBaseURL = iota
	kindKey
	kindCredential
	kindHeader
)

// openAIDefault is the endpoint of the OpenAI protocol without a base URL.
const openAIDefault = "https://api.openai.com/v1"

// endpointOf returns the scheme and host a model's requests go to.
func endpointOf(u modelUse) string {
	if u.cfg.BaseURL != "" {
		return hostOf(u.cfg.BaseURL)
	}
	switch {
	case u.typ == typeDMR:
		return "Docker Model Runner"
	case u.sdk() == typeAnthropic:
		return "https://api.anthropic.com"
	case u.sdk() == typeGoogle:
		return "https://generativelanguage.googleapis.com"
	}
	return hostOf(openAIDefault)
}

// usesOpenAIKey reports whether docker-agent leaves the key of an OpenAI-protocol model to the
// SDK's OPENAI_API_KEY (openai/client.go NewClient, the default branch).
func usesOpenAIKey(u modelUse) bool {
	return u.sdk() == typeOpenAI && u.typ != typeDMR && !u.isChatGPT() && !u.custom && u.cfg.TokenKey == "" &&
		latest.EffectiveAuth(u.cfg, nil) == nil
}

var sdkVars = []sdkVar{
	{name: "OPENAI_BASE_URL", kind: kindBaseURL,
		reads: func(u modelUse, _ envView) bool {
			return u.sdk() == typeOpenAI && u.typ != typeDMR && u.cfg.BaseURL == ""
		},
		lost: func(modelUse, string) string { return "its requests go to " + openAIDefault },
		leak: func(_ modelUse, _ envView, model, _ string) (string, string) {
			return "sends the requests and the key of " + model + " to OPENAI_BASE_URL instead of " + openAIDefault,
				"set inheritEnvironment: false, or set the model's base_url"
		}},
	{name: "ANTHROPIC_BASE_URL", kind: kindBaseURL,
		reads: func(u modelUse, _ envView) bool { return u.sdk() == typeAnthropic && u.cfg.BaseURL == "" },
		lost:  func(modelUse, string) string { return "its requests go to https://api.anthropic.com" },
		leak: func(_ modelUse, _ envView, model, _ string) (string, string) {
			return "sends the requests and the key of " + model + " to ANTHROPIC_BASE_URL instead of https://api.anthropic.com",
				"set inheritEnvironment: false, or set the model's base_url"
		}},
	{name: "GOOGLE_GEMINI_BASE_URL", kind: kindBaseURL,
		reads: func(u modelUse, v envView) bool {
			return u.typ == typeGoogle && !u.isVertex(isTrueEnv(v, "GOOGLE_GENAI_USE_VERTEXAI")) && u.cfg.BaseURL == ""
		},
		lost: func(modelUse, string) string { return "its requests go to https://generativelanguage.googleapis.com" },
		leak: func(_ modelUse, _ envView, model, _ string) (string, string) {
			return "sends the requests and the key of " + model + " to GOOGLE_GEMINI_BASE_URL instead of " +
					"https://generativelanguage.googleapis.com",
				"set inheritEnvironment: false, or set the model's base_url"
		}},
	{name: "GOOGLE_VERTEX_BASE_URL", kind: kindBaseURL,
		reads: func(u modelUse, v envView) bool {
			return u.isVertex(isTrueEnv(v, "GOOGLE_GENAI_USE_VERTEXAI")) && u.cfg.BaseURL == ""
		},
		lost: func(modelUse, string) string { return "its requests go to the Vertex AI endpoint of its location" },
		leak: func(_ modelUse, _ envView, model, _ string) (string, string) {
			return "sends the requests and the credentials of " + model + " to GOOGLE_VERTEX_BASE_URL instead of the Vertex " +
					"AI endpoint",
				"set inheritEnvironment: false, or set the model's base_url"
		}},
	{name: "MODEL_RUNNER_HOST", kind: kindBaseURL,
		reads: func(u modelUse, _ envView) bool { return u.typ == typeDMR && u.cfg.BaseURL == "" },
		lost: func(modelUse, string) string {
			return "it uses the Docker Model Runner endpoint that `docker model status` reports"
		}},
	{name: "AWS_ENDPOINT_URL_BEDROCK_RUNTIME", kind: kindBaseURL,
		reads: func(u modelUse, _ envView) bool {
			return u.sdk() == sdkBedrock && u.cfg.ProviderOpts["endpoint_url"] == nil
		},
		lost: func(modelUse, string) string { return "its requests go to the Bedrock endpoint of its region" }},
	{name: "OPENAI_API_KEY", kind: kindKey,
		// Without a base_url docker-agent's preflight asks for OPENAI_API_KEY itself; a key sent to
		// this machine (Ollama, a local server) goes nowhere.
		reads: func(u modelUse, _ envView) bool {
			return usesOpenAIKey(u) && u.cfg.BaseURL != "" && !isLoopbackURL(u.cfg.BaseURL)
		},
		lost: func(_ modelUse, endpoint string) string {
			return "its requests to " + endpoint + " carry no key (it has a base_url without token_key)"
		},
		leak: func(_ modelUse, _ envView, model, endpoint string) (string, string) {
			return "is sent as the key of " + model + " to " + endpoint + " (the model has a base_url without " +
					"token_key, so the OpenAI SDK reads OPENAI_API_KEY itself)",
				"set token_key to the variable of that endpoint's key, or blank it with env: {OPENAI_API_KEY: \"\"}"
		}},
	{name: "AWS_BEARER_TOKEN_BEDROCK", kind: kindCredential,
		reads: func(u modelUse, _ envView) bool { return u.sdk() == sdkBedrock && u.cfg.TokenKey == "" }},
	{name: "AWS_ACCESS_KEY_ID", kind: kindCredential, reads: readsAWSChain},
	{name: "AWS_SECRET_ACCESS_KEY", kind: kindCredential, reads: readsAWSChain},
	{name: "AWS_SESSION_TOKEN", kind: kindCredential, reads: readsAWSChain},
	{name: "AWS_PROFILE", kind: kindCredential, reads: readsAWSChain},
	{name: "AWS_ROLE_ARN", kind: kindCredential, reads: readsAWSChain},
	{name: "AWS_WEB_IDENTITY_TOKEN_FILE", kind: kindCredential, reads: readsAWSChain},
	{name: "GOOGLE_APPLICATION_CREDENTIALS", kind: kindCredential,
		reads: func(u modelUse, v envView) bool {
			return u.isVertex(isTrueEnv(v, "GOOGLE_GENAI_USE_VERTEXAI")) && u.usesADC()
		}},
	{name: "OPENAI_ORG_ID", kind: kindHeader, reads: readsOpenAIHeaders, lost: lostHeader, leak: leakOpenAIHeader},
	{name: "OPENAI_PROJECT_ID", kind: kindHeader, reads: readsOpenAIHeaders, lost: lostHeader, leak: leakOpenAIHeader},
	{name: "OPENAI_CUSTOM_HEADERS", kind: kindHeader, reads: readsOpenAIHeaders, lost: lostHeader, leak: leakOpenAIHeader},
	{name: "ANTHROPIC_AUTH_TOKEN", kind: kindHeader,
		// anthropic-sdk-go adds it when the process environment has no ANTHROPIC_API_KEY: in a run
		// child the run's environment, before v0.2.0 the provider's own.
		reads: func(u modelUse, v envView) bool {
			if !v.inherit {
				return u.sdk() == typeAnthropic && v.provider["ANTHROPIC_API_KEY"] == ""
			}
			return u.sdk() == typeAnthropic && !v.set("ANTHROPIC_API_KEY")
		},
		lost: func(modelUse, string) string { return "its requests no longer carry it as a bearer token" },
		leak: func(_ modelUse, _ envView, model, endpoint string) (string, string) {
			return "is sent as a bearer token, next to the key of " + model + ", to " + endpoint,
				"set inheritEnvironment: false, or blank it with env: {ANTHROPIC_AUTH_TOKEN: \"\"}"
		}},
	{name: "ANTHROPIC_CUSTOM_HEADERS", kind: kindHeader,
		// anthropic-sdk-go applies it only when it finds no credential of its own: in a run child
		// in the run's environment, before v0.2.0 in the provider's.
		reads: func(u modelUse, v envView) bool {
			if !v.inherit {
				return u.sdk() == typeAnthropic && anthropicHeadersApply(v.providerLookup)
			}
			return u.sdk() == typeAnthropic && anthropicHeadersApply(v.runLookup)
		},
		lost: lostHeader,
		leak: func(_ modelUse, _ envView, model, endpoint string) (string, string) {
			return "adds its headers to the requests of " + model + " to " + endpoint,
				"set inheritEnvironment: false, or blank it with env: {ANTHROPIC_CUSTOM_HEADERS: \"\"}"
		}},
}

func readsAWSChain(u modelUse, v envView) bool {
	return u.sdk() == sdkBedrock && !v.set(orDefault(u.cfg.TokenKey, "AWS_BEARER_TOKEN_BEDROCK"))
}

// readsOpenAIHeaders: openai-go adds OPENAI_ORG_ID, OPENAI_PROJECT_ID and OPENAI_CUSTOM_HEADERS
// to every client (DefaultClientOptions), also those of aliases, custom providers and Docker
// Model Runner.
func readsOpenAIHeaders(u modelUse, _ envView) bool { return u.sdk() == typeOpenAI }

func lostHeader(modelUse, string) string { return "its requests no longer carry the header it sets" }

func leakOpenAIHeader(u modelUse, _ envView, model, endpoint string) (string, string) {
	if u.cfg.Provider == typeOpenAI && u.cfg.BaseURL == "" || u.typ == typeDMR || isLoopbackURL(u.cfg.BaseURL) {
		return "", "" // OpenAI's own API, or this machine
	}
	return "is sent with the requests of " + model + " to " + endpoint + " (the OpenAI SDK adds it to every " +
			"OpenAI-protocol model)",
		"set inheritEnvironment: false to keep it from the model"
}

// anthropicHeadersApply reports whether anthropic-sdk-go (v1.79.1, DefaultClientOptions) adds
// ANTHROPIC_CUSTOM_HEADERS of the process environment that lookup reads to a client's
// requests: only when that environment has no credential of the SDK's own (a non-empty
// ANTHROPIC_API_KEY, ANTHROPIC_AUTH_TOKEN or ANTHROPIC_PROFILE, a complete env federation) and
// the SDK finds no fallback profile file; otherwise it returns before it reads the headers.
func anthropicHeadersApply(lookup func(string) (string, bool)) bool {
	nonEmpty := func(name string) bool {
		val, ok := lookup(name)
		return ok && val != ""
	}
	present := func(name string) bool {
		_, ok := lookup(name)
		return ok
	}
	switch {
	case nonEmpty("ANTHROPIC_API_KEY"), nonEmpty("ANTHROPIC_AUTH_TOKEN"), nonEmpty("ANTHROPIC_PROFILE"):
		return false
	case present("ANTHROPIC_FEDERATION_RULE_ID") && present("ANTHROPIC_ORGANIZATION_ID") &&
		(nonEmpty("ANTHROPIC_IDENTITY_TOKEN_FILE") || nonEmpty("ANTHROPIC_IDENTITY_TOKEN")):
		return false
	}
	return !anthropicFallbackProfile(lookup)
}

// anthropicFallbackProfile reports whether anthropic-sdk-go finds its fallback profile file:
// configs/<profile>.json in its configuration directory (ANTHROPIC_CONFIG_DIR, else
// XDG_CONFIG_HOME/anthropic or ~/.config/anthropic, %APPDATA%\Anthropic on Windows), the
// profile named by its active_config file or "default".
func anthropicFallbackProfile(lookup func(string) (string, bool)) bool {
	dir, ok := lookup("ANTHROPIC_CONFIG_DIR")
	if !ok {
		get := func(name string) string { val, _ := lookup(name); return val }
		switch {
		case runtime.GOOS == "windows":
			if get("APPDATA") == "" {
				return false
			}
			dir = filepath.Join(get("APPDATA"), "Anthropic")
		case get("XDG_CONFIG_HOME") != "":
			dir = filepath.Join(get("XDG_CONFIG_HOME"), "anthropic")
		default:
			home := get("HOME")
			if home == "" {
				home, _ = os.UserHomeDir()
			}
			if home == "" {
				return false
			}
			dir = filepath.Join(home, ".config", "anthropic")
		}
	}
	profile := "default"
	if b, err := os.ReadFile(filepath.Join(dir, "active_config")); err == nil { //nolint:gosec // the SDK's own file
		if name := strings.TrimSpace(string(b)); name != "" {
			profile = name
		}
	}
	_, err := os.Stat(filepath.Join(dir, "configs", profile+".json")) //nolint:gosec // the SDK's own file, as it reads it
	return err == nil
}

// sdkVariableWarnings warns about the variables of the model SDKs that the environment of the
// Pulumi CLI sets and env does not: with inheritEnvironment false they no longer reach the models
// (the run child's environment, since v0.2.0), with inheritEnvironment true they reach them as
// they reach docker agent run, also where docker-agent does not ask for them. Without a value.
func sdkVariableWarnings(uses []modelUse, v envView, hideNames bool) []string {
	type warning struct {
		kind int
		msg  string
	}
	var out []warning
	chains := map[string]bool{}
	for _, u := range uses {
		if u.auto {
			continue
		}
		model := modelRef(u, hideNames)
		endpoint := endpointOf(u)
		if hideNames {
			endpoint = "its endpoint"
		}
		var withheldCreds []string
		for _, s := range sdkVars {
			if _, inEnv := v.env[s.name]; inEnv || v.provider[s.name] == "" || !s.reads(u, v) {
				continue
			}
			switch {
			case !v.inherit && s.kind == kindCredential:
				withheldCreds = append(withheldCreds, s.name)
			case !v.inherit && s.lost != nil:
				out = append(out, warning{s.kind, fmt.Sprintf("%s is set in the environment of the Pulumi CLI, but with "+
					"inheritEnvironment: false it no longer reaches %s since v0.2.0: %s; set it in env%s", s.name, model,
					s.lost(u, endpoint), remedyFor(s))})
			case v.inherit && s.leak != nil:
				if what, remedy := s.leak(u, v, model, endpoint); what != "" {
					out = append(out, warning{s.kind, fmt.Sprintf("%s (inherited from the environment of the Pulumi CLI) %s, "+
						"as with docker agent run; %s", s.name, what, remedy)})
				}
			}
		}
		if v.inherit {
			continue
		}
		// Machine credentials: the AWS and Google SDKs fall back to this machine's files and
		// metadata services, which inheritEnvironment: false does not keep from them.
		var chain string
		switch {
		case u.sdk() == sdkBedrock && !bedrockCredentialsInEnv(u, v):
			chain = "gets no AWS credentials from env, so it uses this machine's AWS default chain (shared files such " +
				"as ~/.aws/credentials, SSO, container or instance metadata), which may be another account; pass the " +
				"credentials in env (AWS_BEARER_TOKEN_BEDROCK, or AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY)"
		case u.isVertex(isTrueEnv(v, "GOOGLE_GENAI_USE_VERTEXAI")) && u.usesADC() && !v.set("GOOGLE_APPLICATION_CREDENTIALS"):
			chain = "gets no GOOGLE_APPLICATION_CREDENTIALS from env, so it uses this machine's Google application " +
				"default credentials (the gcloud login, or the metadata server), which may be another account; pass a " +
				"service account key file as GOOGLE_APPLICATION_CREDENTIALS in env"
		}
		if chain == "" || chains[model] {
			continue
		}
		chains[model] = true
		msg := "with inheritEnvironment: false, " + model + " " + chain
		if len(withheldCreds) > 0 {
			verb, pron, reach := "is", "it", "reaches"
			if len(withheldCreds) > 1 {
				verb, pron, reach = "are", "they", "reach"
			}
			msg = fmt.Sprintf("%s %s set in the environment of the Pulumi CLI, but with inheritEnvironment: false %s no "+
				"longer %s %s since v0.2.0, which %s", strings.Join(withheldCreds, ", "), verb, pron, reach, model, chain)
		}
		out = append(out, warning{kindCredential, msg})
	}
	slices.SortStableFunc(out, func(a, b warning) int { return a.kind - b.kind })
	msgs := make([]string, 0, len(out))
	for _, w := range out {
		if !slices.Contains(msgs, w.msg) {
			msgs = append(msgs, w.msg)
		}
	}
	return msgs
}

// remedyFor is the alternative to setting a variable in env.
func remedyFor(s sdkVar) string {
	switch s.kind {
	case kindBaseURL:
		if s.name == "AWS_ENDPOINT_URL_BEDROCK_RUNTIME" {
			return ", or set the model's provider_opts.endpoint_url"
		}
		return ", or set the model's base_url"
	case kindKey:
		return ", or set the model's token_key"
	}
	return ""
}

// bedrockCredentialsInEnv reports whether an Amazon Bedrock model gets its credentials from the
// run's environment or its own settings: a bearer token, access keys, a profile or a web identity.
func bedrockCredentialsInEnv(u modelUse, v envView) bool {
	if p, _ := u.cfg.ProviderOpts["profile"].(string); p != "" {
		return true
	}
	for _, name := range []string{orDefault(u.cfg.TokenKey, "AWS_BEARER_TOKEN_BEDROCK"), "AWS_ACCESS_KEY_ID", "AWS_PROFILE",
		"AWS_WEB_IDENTITY_TOKEN_FILE", "AWS_CONTAINER_CREDENTIALS_FULL_URI", "AWS_CONTAINER_CREDENTIALS_RELATIVE_URI"} {
		if v.set(name) {
			return true
		}
	}
	return false
}

// structuredOutputWarnings warns about agents whose structured_output (native mode) an Amazon
// Bedrock model would answer: docker-agent's Bedrock client sends no schema.
func structuredOutputWarnings(cfg *latest.Config, hideNames bool) []string {
	var out []string
	for _, a := range cfg.Agents {
		if a.Harness != nil || a.StructuredOutput == nil || a.StructuredOutput.ToolMode() {
			continue
		}
		one := *cfg
		one.Agents = []latest.AgentConfig{a}
		var bedrock []string
		for _, u := range configModels(&one) {
			if u.typ == typeBedrock && (u.role == "" || u.role == "fallback" || u.role == "routed") {
				bedrock = append(bedrock, modelRef(u, hideNames))
			}
		}
		if len(bedrock) == 0 {
			continue
		}
		out = append(out, fmt.Sprintf("%s is not enforced by Amazon Bedrock (%s): docker-agent sends Bedrock models no "+
			"schema, so the answer may not be JSON and the run then fails; set structured_output.mode: tool to have "+
			"docker-agent enforce the schema", agentRef(a.Name, "structured_output", hideNames),
			strings.Join(slices.Compact(bedrock), ", ")))
	}
	return out
}

// gatewayVars select a Docker models gateway for docker agent run.
var gatewayVars = []string{"DOCKER_AGENT_MODELS_GATEWAY", "CAGENT_MODELS_GATEWAY"}

// gatewayWarnings warns that AgentRun does not use a Docker models gateway: a gateway variable
// of the run's environment, or a model's bypass_models_gateway, has no effect. Hidden names
// count the models instead of naming them.
func gatewayWarnings(cfg *latest.Config, v envView, envUnknown, hideNames bool) []string {
	var out []string
	for _, name := range gatewayVars {
		s := v.source(name)
		if envUnknown && !v.inherit {
			s = ""
		}
		if s != "" {
			out = append(out, fmt.Sprintf("AgentRun does not use a Docker models gateway: %s has no effect, and the models "+
				"call their providers directly, with their own credentials", s))
		}
	}
	var bypass []string
	for _, name := range slices.Sorted(maps.Keys(cfg.Models)) {
		if cfg.Models[name].BypassModelsGateway {
			bypass = append(bypass, "models."+name)
		}
	}
	if len(bypass) > 0 {
		which := strings.Join(bypass, ", ")
		switch {
		case hideNames && len(bypass) == 1:
			which = "a model"
		case hideNames:
			which = fmt.Sprintf("%d models", len(bypass))
		}
		out = append(out, fmt.Sprintf("bypass_models_gateway (%s) has no effect: AgentRun never uses a Docker models "+
			"gateway", which))
	}
	return out
}

// Variables that select the model of model auto for docker agent run (cmd/root/flags.go).
var defaultModelVars = []string{"DOCKER_AGENT_DEFAULT_MODEL", "CAGENT_DEFAULT_MODEL"}

// defaultModel returns the model DOCKER_AGENT_DEFAULT_MODEL (or CAGENT_DEFAULT_MODEL) of the
// run's environment selects for model auto ("provider/model"), and the variable's source.
func defaultModel(ctx context.Context, env environment.Provider) (*latest.ModelConfig, string) {
	for _, name := range defaultModelVars {
		val, _ := env.Get(ctx, name)
		if val == "" {
			continue
		}
		m, err := latest.ParseModelRef(strings.TrimSpace(val))
		if err != nil {
			return nil, ""
		}
		return &m, name
	}
	return nil, ""
}

// autoChecks says what model auto resolves to, with the run's environment, and with which
// credential: the first provider whose variable is set, as docker agent run picks it
// (config.AutoModelConfig, without listing Docker Model Runner's models), or the model
// DOCKER_AGENT_DEFAULT_MODEL selects. A selected model whose credential the environment does not
// set is a warning: the run fails. A selected chatgpt model without a token is CheckModels'.
func autoChecks(ctx context.Context, cfg *latest.Config, v envView, prov environment.Provider, hideNames bool,
) (warnings, infos []string) {
	for _, a := range cfg.Agents {
		if a.Harness != nil || !slices.Contains(strings.Split(a.Model, ","), "auto") {
			continue
		}
		dm, dmVar := defaultModel(ctx, prov)
		m := config.AutoModelConfig(ctx, "", prov, dm, nil)
		u := newModelUse(cfg, m)
		where := agentRef(a.Name, "model", hideNames) + " auto"
		model := u.display(hideNames, "a model")
		switch {
		case dm != nil && u.usesChatGPTLogin() && !v.set(chatgpt.TokenEnvVar):
			if v.present(chatgpt.TokenEnvVar) || !chatgpt.LoggedIn() {
				continue // CheckModels warns
			}
			infos = append(infos, fmt.Sprintf("%s resolves to %s, selected by %s (%s)", where, model, v.source(dmVar),
				credentialSource(u, v)))
		case dm != nil:
			source := credentialSource(u, v)
			if strings.HasSuffix(source, "(not set)") {
				warnings = append(warnings, fmt.Sprintf("%s resolves to %s, selected by %s, but the run's environment does "+
					"not set its credential (%s), so the run fails: set it in env (for example from a secret config value), "+
					"or select another model with %s", where, model, v.source(dmVar), source, dmVar))
				continue
			}
			infos = append(infos, fmt.Sprintf("%s resolves to %s, selected by %s (%s)", where, model, v.source(dmVar), source))
		case m.Provider == typeDMR:
			infos = append(infos, fmt.Sprintf("%s finds no provider credential in the run's environment and resolves to Docker "+
				"Model Runner (%s, or a model this machine already pulled; a missing model is pulled within "+
				"timeoutSeconds)", where, model))
		case u.isChatGPT():
			// docker-agent's default ChatGPT model, which the Codex backend does not serve to
			// every plan.
			infos = append(infos, fmt.Sprintf("%s resolves to %s (%s), docker-agent's default ChatGPT model, which not "+
				"every ChatGPT plan offers: when the run fails with \"model is not supported when using Codex with a "+
				"ChatGPT account\", select one the plan offers with DOCKER_AGENT_DEFAULT_MODEL: chatgpt/<model> in env (the "+
				"models codex lists for this account: run codex, then /model)", where, model, credentialSource(u, v)))
		default:
			infos = append(infos, fmt.Sprintf("%s resolves to %s (%s)", where, model, credentialSource(u, v)))
		}
	}
	return warnings, infos
}

// modelRunWarnings are CheckModels' warnings for a run whose configuration no preview checked
// (Request.AnnounceHarness: a `reference`, or a delete prompt), from the request and the
// environment of this process (the parent of the run child), without those about missing
// variables: the run fails with them.
func modelRunWarnings(ctx context.Context, req Request) []string {
	opts := CheckOptions{InheritEnvironment: req.InheritEnvironment, EnvKeys: map[string]bool{},
		EnvValues: map[string]string{}, HideNames: req.HideNames}
	for k, val := range req.Env {
		opts.EnvKeys[k] = val != ""
		opts.EnvValues[k] = val
	}
	warnings, _ := CheckModels(ctx, orDefault(req.Name, "agent")+".yaml", req.YAML, opts)
	// Missing variables fail the run, with the same message.
	return slices.DeleteFunc(warnings, func(w string) bool {
		return strings.HasPrefix(w, "the configuration needs ") || strings.HasPrefix(w, "no first_available candidate ")
	})
}

// connectionNotes are the warnings of sdkVariableWarnings that concern a run's endpoints and
// credentials with inheritEnvironment false, for the error of a run whose model requests were
// rejected or could not connect (the parent adds them).
func connectionNotes(ctx context.Context, req Request) []string {
	if req.InheritEnvironment {
		return nil
	}
	cfg, err := config.Load(ctx, config.NewBytesSource(orDefault(req.Name, "agent")+".yaml", req.YAML))
	if err != nil {
		return nil
	}
	v := envView{env: req.Env, provider: providerEnvMap()}
	if v.env == nil {
		v.env = map[string]string{}
	}
	_ = config.ResolveFirstAvailableModels(ctx, cfg, "", v.asProvider())
	return sdkVariableWarnings(configModels(cfg), v, req.HideNames)
}

func orDefault(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
