// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package agentrun

import (
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
//   - the values of Request.Env and Request.Secrets (secret Pulumi inputs);
//   - the values of the provider's environment variables whose names look like
//     credentials (API keys, tokens, secrets, passwords), inherited or not, because the
//     processes a configuration starts can see them;
//   - the values of the variables the configuration names as `token_key` (models and
//     providers), from Request.Env or, when inherited, the provider's environment.

// credentialName matches environment variable names that hold credentials.
var credentialName = regexp.MustCompile(`(?i)(API_?KEY|TOKEN|SECRET|PASSWORD|PASSWD|PASSPHRASE|CREDENTIAL|ACCESS_?KEY|PRIVATE_?KEY)`)

// minInheritedSecret is the shortest inherited value that is redacted: short values of
// credential-named variables are flags ("1", "true"), not credentials.
const minInheritedSecret = 8

func redactionSet(req Request) []string {
	out := slices.Clone(req.Secrets)
	for _, v := range req.Env {
		out = append(out, v)
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
