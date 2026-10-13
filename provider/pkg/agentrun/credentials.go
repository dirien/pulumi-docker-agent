// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package agentrun

import (
	"bufio"
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/docker/docker-agent/pkg/chatgpt"
	"github.com/docker/docker-agent/pkg/config"
	"github.com/docker/docker-agent/pkg/config/latest"
	"github.com/docker/docker-agent/pkg/environment"
)

// Where a run's credentials come from, for the credential line of a run (D5: the counterpart
// of Claude Code's apiKeySource record), the preview checks (check_models.go) and the errors
// of a run whose credentials are missing or rejected. Every message names variables and their
// source, never a value.

// The sources of a variable.
const (
	fromEnv       = "from env"
	fromInherited = "from the environment of the Pulumi CLI"
)

// envView is the environment a run resolves variables from: Request.Env, then, with
// Request.InheritEnvironment, the provider's environment (envProvider). The model SDKs read
// the same (childenv.go).
type envView struct {
	// env is Request.Env. In a preview, a value that is not known yet is a placeholder.
	env map[string]string
	// inherit is Request.InheritEnvironment; provider is the provider's environment (the
	// Pulumi CLI's), inherited or not.
	inherit  bool
	provider map[string]string
}

// lookup returns a variable's value and where it comes from ("" when it is not set).
func (v envView) lookup(name string) (string, string) {
	if val, ok := v.env[name]; ok {
		if val == "" {
			return "", ""
		}
		return val, fromEnv
	}
	if val := v.provider[name]; v.inherit && val != "" {
		return val, fromInherited
	}
	return "", ""
}

// source returns "NAME from env" or "NAME from the environment of the Pulumi CLI", or "".
func (v envView) source(name string) string {
	if _, from := v.lookup(name); from != "" {
		return name + " " + from
	}
	return ""
}

// set reports whether a variable is set.
func (v envView) set(name string) bool {
	_, from := v.lookup(name)
	return from != ""
}

// asProvider returns the view as an environment.Provider.
func (v envView) asProvider() environment.Provider {
	return envViewProvider{v}
}

type envViewProvider struct{ v envView }

func (p envViewProvider) Get(_ context.Context, name string) (string, bool) {
	val, from := p.v.lookup(name)
	return val, from != ""
}

// credentialSource says how a model authenticates with the environment v, as docker-agent and
// its SDKs resolve it (including the SDKs' own fallbacks to the process environment, which in
// a run child is v). It names variables, never values.
func credentialSource(u modelUse, v envView) string {
	m := u.cfg
	key := func(name string) string {
		if s := v.source(name); s != "" {
			return s
		}
		return name + " (not set)"
	}
	switch {
	case u.isChatGPT():
		if s := v.source(cmp.Or(m.TokenKey, chatgpt.TokenEnvVar)); s != "" {
			return s
		}
		return "the ChatGPT login of docker agent setup, billed to the ChatGPT plan"
	case latest.EffectiveAuth(m, nil) != nil:
		return "workload identity federation"
	}
	switch u.typ {
	case typeDMR:
		return "no key: Docker Model Runner"
	case typeAnthropic:
		s := key(cmp.Or(m.TokenKey, "ANTHROPIC_API_KEY"))
		// anthropic-sdk-go adds ANTHROPIC_AUTH_TOKEN of the process environment as a bearer token
		// when that environment has no ANTHROPIC_API_KEY (client.go DefaultClientOptions).
		if !v.set("ANTHROPIC_API_KEY") && v.set("ANTHROPIC_AUTH_TOKEN") {
			s += ", and " + v.source("ANTHROPIC_AUTH_TOKEN") + " as a bearer token"
		}
		return s
	case typeGoogle:
		if u.isVertex(isTrueEnv(v, "GOOGLE_GENAI_USE_VERTEXAI")) {
			switch {
			case v.set("GOOGLE_APPLICATION_CREDENTIALS"):
				return "Google application default credentials (" + v.source("GOOGLE_APPLICATION_CREDENTIALS") + ")"
			case !u.usesADC():
				return "no credentials (a Vertex AI base_url without a project)"
			}
			return "this machine's Google application default credentials (the gcloud login, or the metadata server)"
		}
		if m.TokenKey != "" {
			return key(m.TokenKey)
		}
		if s := cmp.Or(v.source("GOOGLE_API_KEY"), v.source("GEMINI_API_KEY")); s != "" {
			return s
		}
		return "GOOGLE_API_KEY or GEMINI_API_KEY (not set)"
	case typeBedrock:
		if s := v.source(cmp.Or(m.TokenKey, "AWS_BEARER_TOKEN_BEDROCK")); s != "" {
			return s + " (a Bedrock API key)"
		}
		if s := v.source("AWS_ACCESS_KEY_ID"); s != "" {
			return "AWS access keys (" + s + ")"
		}
		if p, _ := m.ProviderOpts["profile"].(string); p != "" {
			return "the AWS profile of provider_opts.profile"
		}
		if s := v.source("AWS_PROFILE"); s != "" {
			return "the AWS profile of " + s
		}
		return "this machine's AWS default credential chain (shared files, SSO, container or instance metadata)"
	}
	// The OpenAI protocol (openai/client.go NewClient).
	switch {
	case m.TokenKey != "":
		if m.Provider == "github-copilot" && m.TokenKey == "GITHUB_TOKEN" && !v.set("GITHUB_TOKEN") && v.set("GH_TOKEN") {
			return v.source("GH_TOKEN")
		}
		return key(m.TokenKey)
	case u.custom:
		return "no key (a custom provider without token_key)"
	case v.set("OPENAI_API_KEY"):
		return v.source("OPENAI_API_KEY")
	}
	return "no key"
}

// isTrueEnv reports whether a variable of v is set to a true value (strconv.ParseBool).
func isTrueEnv(v envView, name string) bool {
	val, _ := v.lookup(name)
	b, _ := strconv.ParseBool(val)
	return b
}

// credentialLine is the credential line of a run: every model the agents can send requests
// to, how it was chosen, and its credential source, for example "models: openai/gpt-5-mini
// (OPENAI_API_KEY from env); fallback anthropic/claude-haiku-4-5 (ANTHROPIC_API_KEY from the
// environment of the Pulumi CLI)". auto resolves model auto (model auto of an agent of the
// configuration, as the run resolved it). Title models are left out (AgentRun sets the
// session's title) and so are models of toolsets and skills. It returns "" without models.
func credentialLine(uses []modelUse, v envView, auto func(modelUse) (modelUse, bool), hideNames bool) string {
	var entries []string
	for _, u := range uses {
		if u.role == "title" || u.role == "tool" {
			continue
		}
		how := ""
		switch {
		case u.auto:
			r, ok := auto(u)
			if !ok {
				continue
			}
			r.role = u.role
			u, how = r, "auto: "
		case u.firstAvailable:
			how = "first_available: "
		}
		entry := how
		if u.role != "" {
			entry = u.role + " " + how
		}
		entry += u.display(hideNames, "a model") + " (" + credentialSource(u, v) + ")"
		if !slices.Contains(entries, entry) {
			entries = append(entries, entry)
		}
	}
	if len(entries) == 0 {
		return ""
	}
	return "models: " + strings.Join(entries, "; ")
}

// --- errors of a run whose credentials are missing ---------------------------------------------

// MissingCredentialsError is the error of a run whose configuration needs environment variables
// (model credentials, or variables its models or toolsets reference) that the run's environment
// does not set: neither env nor, with inheritEnvironment, the environment of the Pulumi CLI.
type MissingCredentialsError struct {
	// Variables are the missing variables.
	Variables []string
	msg       string
}

func (e *MissingCredentialsError) Error() string { return e.msg }

// modelCredentialsError is docker-agent's unexported error of a first_available selector without
// a candidate whose credentials are set.
type modelCredentialsError interface {
	error
	MissingModelCredentials() bool
}

// requiredVar matches docker-agent's errors for a credential a model client did not find
// (openai, anthropic and gemini client.go).
var requiredVar = regexp.MustCompile(`\b([A-Z][A-Z0-9_]*)(?: or ([A-Z][A-Z0-9_]*))? environment variable is required`)

// missingVariables returns the variables a load error of docker-agent says are missing, if it is
// about missing variables: the preflight check's RequiredEnvError, a first_available selector
// without a candidate whose credentials are set, and a model client without its credential.
func missingVariables(err error) []string {
	var re *environment.RequiredEnvError
	if errors.As(err, &re) {
		return slices.Clone(re.Missing)
	}
	// first_available's error type is unexported: " - candidate: VAR, VAR" lines.
	if fa, ok := errors.AsType[modelCredentialsError](err); ok && fa.MissingModelCredentials() {
		// The candidate lines come first; docker-agent's advice for its CLI follows a blank line.
		var vars []string
		sc := bufio.NewScanner(strings.NewReader(err.Error()))
		for sc.Scan() {
			if strings.TrimSpace(sc.Text()) == "" && len(vars) > 0 {
				break
			}
			line, ok := strings.CutPrefix(sc.Text(), " - ")
			if !ok {
				continue
			}
			if _, list, ok := strings.Cut(line, ": "); ok {
				for v := range strings.SplitSeq(list, ", ") {
					if v = strings.TrimSpace(v); v != "" && !slices.Contains(vars, v) {
						vars = append(vars, v)
					}
				}
			}
		}
		return vars
	}
	if m := requiredVar.FindStringSubmatch(err.Error()); m != nil {
		return slices.DeleteFunc([]string{m[1], m[2]}, func(s string) bool { return s == "" })
	}
	return nil
}

// missingCredentialsMessage explains missing variables of a run, without docker-agent's advice
// for its CLI (export, --env-from-file, docker agent setup's env file), which AgentRun does not
// read.
func missingCredentialsMessage(vars []string, inherit, firstAvailable bool) string {
	list := strings.Join(vars, ", ")
	msg := "the configuration needs " + list
	if firstAvailable {
		msg = "no first_available candidate has its credentials (" + list + ")"
	}
	if inherit {
		msg += ", which neither env nor the environment of the Pulumi CLI sets"
	} else {
		msg += ", which env does not set (inheritEnvironment is false, so the environment of the Pulumi CLI is not used)"
	}
	switch {
	case firstAvailable:
		msg += "; set the credentials of one candidate in env (for example from a secret config value)"
	case len(vars) > 1:
		msg += "; set them in env (for example from secret config values)"
	default:
		msg += "; set it in env (for example from a secret config value)"
	}
	if slices.Contains(vars, chatgpt.TokenEnvVar) {
		msg += "; for " + chatgpt.TokenEnvVar + ", sign in with `docker agent setup` on the machine that runs pulumi up " +
			"(AgentRun uses that ChatGPT login for configurations that name a chatgpt model), or pass a ChatGPT access token " +
			"in env"
	}
	return msg
}

// credentialHints are the parent's hints for missing variables: a variable that the
// environment of the Pulumi CLI sets but inheritEnvironment: false keeps from the run, and a
// variable that docker agent's own env file (~/.config/cagent/.env, written by docker agent
// setup) holds, which AgentRun does not read. Only the file's variable names are read.
func credentialHints(vars []string, inherit bool, providerEnv map[string]string) []string {
	var hints []string
	stored := envFileNames(environment.ConfigEnvFilePath())
	for _, v := range vars {
		switch {
		case !inherit && providerEnv[v] != "":
			hints = append(hints, fmt.Sprintf("%s is set in the environment of the Pulumi CLI, but inheritEnvironment: false "+
				"keeps it from the run (since v0.2.0 also from the model SDKs): pass it in env, or set inheritEnvironment: true",
				v))
		case slices.Contains(stored, v):
			hints = append(hints, fmt.Sprintf("%s is in docker agent's env file (%s), which AgentRun does not read: set it "+
				"in env", v, environment.ConfigEnvFilePath()))
		}
	}
	return hints
}

// envFileNames returns the variable names of an env file (NAME=value lines, optionally with
// export), never the values.
func envFileNames(path string) []string {
	f, err := os.Open(path) //nolint:gosec // docker agent's own env file
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	var names []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		if name, _, ok := strings.Cut(line, "="); ok && strings.TrimSpace(name) != "" {
			names = append(names, strings.TrimSpace(name))
		}
	}
	return names
}

// describeMissingCredentials returns the error of a load that failed for missing variables, or
// nil when err is about something else.
func describeMissingCredentials(err error, inherit bool) error {
	vars := missingVariables(err)
	if len(vars) == 0 {
		return nil
	}
	_, firstAvailable := errors.AsType[modelCredentialsError](err)
	var re *environment.RequiredEnvError
	firstAvailable = firstAvailable && !errors.As(err, &re)
	return &MissingCredentialsError{Variables: vars, msg: "loading the agent configuration: " +
		missingCredentialsMessage(vars, inherit, firstAvailable)}
}

// describeAutoError rewrites docker-agent's error for a model auto that found no provider
// (config.AutoModelFallbackError), whose advice is for the docker agent CLI.
func describeAutoError(err error, inherit bool) error {
	var ae *config.AutoModelFallbackError
	if !errors.As(err, &ae) {
		return nil
	}
	where := "env"
	if inherit {
		where = "env and the environment of the Pulumi CLI"
	}
	msg := "loading the agent configuration: model auto found no provider credential in " + where +
		" (it looks for ANTHROPIC_API_KEY, OPENAI_API_KEY, GITHUB_TOKEN, GOOGLE_API_KEY and the other providers' keys) " +
		"and fell back to Docker Model Runner"
	if ae.Cause != nil {
		msg += ", which failed: " + strings.TrimSpace(firstLine(ae.Cause.Error()))
	}
	return errors.New(msg + "; set a provider's key in env, set DOCKER_AGENT_DEFAULT_MODEL (provider/model) in env, " +
		"or name the model in the configuration")
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}

// parentErrorHints adds to the error of a run child what only the parent knows: the environment
// of the Pulumi CLI (a missing variable it sets but inheritEnvironment: false keeps from the
// run, the model SDK variables that no longer reach the models since v0.2.0 when a request was
// rejected or did not connect), docker agent's env file, and the ChatGPT login.
func parentErrorHints(ctx context.Context, req Request, msg string, missing []string, login *chatgptLogin) string {
	var hints []string
	if len(missing) > 0 {
		hints = append(hints, credentialHints(missing, req.InheritEnvironment, providerEnvMap())...)
	}
	if h := login.errorHint(msg); h != "" {
		hints = append(hints, h)
	}
	if isAuthFailure(msg) || isConnectionFailure(msg) {
		hints = append(hints, connectionNotes(ctx, req)...)
	}
	return joinHints(msg, hints)
}
