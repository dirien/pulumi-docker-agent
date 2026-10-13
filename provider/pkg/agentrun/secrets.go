// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package agentrun

import (
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/dirien/pulumi-docker-agent/provider/pkg/validate"
)

// A run's errors, warnings and logs (and the decision whether its answer is secret) use
// a redaction set of:
//
//   - the values of Request.Env, except those that are configuration, not credentials
//     (NonSecretEnvValue), and of Request.Secrets (secret Pulumi inputs);
//   - the values of the provider's environment variables whose names look like
//     credentials (API keys, tokens, secrets, passwords), inherited or not, because the
//     processes a configuration starts can see them;
//   - the values of the variables the configuration names as `token_key` (models and
//     providers), from Request.Env or, when inherited, the provider's environment.
//
// A run child (child.go) computes the set from its own environment, which without
// Request.InheritEnvironment holds none of the provider's credentials; the provider redacts
// everything the child sends again with its own set.

// credentialName matches environment variable names that hold credentials.
var credentialName = regexp.MustCompile(`(?i)(API_?KEY|TOKEN|SECRET|PASSWORD|PASSWD|PASSPHRASE|CREDENTIAL|ACCESS_?KEY|PRIVATE_?KEY)`)

// minInheritedSecret is the shortest inherited value that is redacted: short values of
// credential-named variables are flags ("1", "true"), not credentials.
const minInheritedSecret = 8

func redactionSet(req Request) []string {
	out := slices.Clone(req.Secrets)
	for k, v := range req.Env {
		if !NonSecretEnvValue(k, v) {
			out = append(out, v)
		}
	}
	for _, kv := range os.Environ() {
		k, v, ok := strings.Cut(kv, "=")
		if ok && credentialName.MatchString(k) && len(strings.TrimSpace(v)) >= minInheritedSecret {
			out = append(out, v)
		}
	}
	for _, key := range tokenKeys(req.YAML) {
		if v, ok := req.Env[key]; ok {
			out = append(out, v)
		} else if v, ok := os.LookupEnv(key); ok && req.InheritEnvironment {
			out = append(out, v)
		}
	}
	return out
}

// nonSecretEnvNames are `env` variables whose values are configuration, not credentials (a
// region, a profile or project name, a routing flag, model auto's model), by exact name.
var nonSecretEnvNames = []string{
	"AWS_REGION", "AWS_DEFAULT_REGION", "AWS_PROFILE",
	"GOOGLE_CLOUD_PROJECT", "GOOGLE_CLOUD_LOCATION", "GOOGLE_CLOUD_REGION", "GOOGLE_GENAI_USE_VERTEXAI",
	"CLAUDE_CODE_USE_BEDROCK", "CLAUDE_CODE_USE_VERTEX", "CLAUDE_CODE_USE_FOUNDRY",
	"DOCKER_AGENT_DEFAULT_MODEL", "CAGENT_DEFAULT_MODEL",
}

// nonSecretEnvURLs are `env` variables whose values are endpoints: not credentials unless the URL
// carries some (userinfo, a query, a fragment).
var nonSecretEnvURLs = []string{
	"OPENAI_BASE_URL", "ANTHROPIC_BASE_URL", "GOOGLE_GEMINI_BASE_URL", "GOOGLE_VERTEX_BASE_URL", "MODEL_RUNNER_HOST",
	"OLLAMA_HOST",
}

// NonSecretEnvValue reports whether the value of an `env` variable is configuration, not a
// credential, and is therefore neither redacted nor a reason to store an answer as a secret: a
// boolean (true, false, yes, no, on, off), the value of a variable of nonSecretEnvNames, or a
// URL without userinfo, query or fragment in a variable of nonSecretEnvURLs. A variable whose
// name looks like a credential (credentialName) never qualifies. Pulumi SDKs mark the whole
// `env` map secret, so the provider cannot tell a value the program marked secret itself.
func NonSecretEnvValue(name, value string) bool {
	if credentialName.MatchString(name) || value == "" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "false", "yes", "no", "on", "off":
		return true
	}
	if slices.Contains(nonSecretEnvNames, name) {
		return true
	}
	return slices.Contains(nonSecretEnvURLs, name) && plainURL(value)
}

// plainURL reports whether s is an http(s) URL with a host and without userinfo, query or
// fragment, which could carry credentials.
func plainURL(s string) bool {
	u, err := url.Parse(strings.TrimSpace(s))
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil &&
		u.RawQuery == "" && !u.ForceQuery && u.Fragment == "" && !strings.ContainsAny(s, "@?#")
}

// tokenKeys returns the `token_key` names of a configuration's models and providers.
func tokenKeys(doc []byte) []string {
	var cfg struct {
		Models map[string]struct {
			TokenKey string `yaml:"token_key"`
		} `yaml:"models"`
		Providers map[string]struct {
			TokenKey string `yaml:"token_key"`
		} `yaml:"providers"`
	}
	if yaml.Unmarshal(doc, &cfg) != nil {
		return nil
	}
	var keys []string
	for _, m := range cfg.Models {
		if m.TokenKey != "" {
			keys = append(keys, m.TokenKey)
		}
	}
	for _, p := range cfg.Providers {
		if p.TokenKey != "" {
			keys = append(keys, p.TokenKey)
		}
	}
	slices.Sort(keys)
	return slices.Compact(keys)
}

// redact replaces secret values (and the lines and `key: value` values of multi-line
// secrets, such as a secret YAML document) with [secret].
func redact(msg string, secrets []string) string {
	return validate.Redact(msg, secrets)
}

// containsSecret reports whether msg contains one of the secrets.
func containsSecret(msg string, secrets []string) bool {
	return msg != "" && redact(msg, secrets) != msg
}

// AnswerContainsSecret reports whether an answer contains a value of the request's
// redaction set. The provider uses it to keep a stored answer secret when it updates the
// state without running the agent again.
func AnswerContainsSecret(answer string, req Request) bool {
	return containsSecret(answer, redactionSet(req))
}
