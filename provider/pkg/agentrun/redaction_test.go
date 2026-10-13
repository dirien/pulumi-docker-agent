// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package agentrun

import (
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNonSecretEnvValue: the `env` values that are configuration, not credentials (D6).
func TestNonSecretEnvValue(t *testing.T) {
	for _, tc := range []struct {
		name, value string
		want        bool
	}{
		{"AWS_REGION", "us-east-1", true},
		{"GOOGLE_CLOUD_LOCATION", "global", true},
		{"GOOGLE_CLOUD_PROJECT", "my-project", true},
		{"GOOGLE_CLOUD_REGION", "europe-west4", true},
		{"AWS_PROFILE", "dev", true},
		{"DOCKER_AGENT_DEFAULT_MODEL", "openai/gpt-5-mini", true},
		{"GOOGLE_GENAI_USE_VERTEXAI", "true", true},
		{"ANY_FLAG", "TRUE", true},
		{"ANY_FLAG", "off", true},
		{"OPENAI_BASE_URL", "https://gateway.example/v1", true},
		{"MODEL_RUNNER_HOST", "http://localhost:12434", true},
		// URLs that can carry credentials.
		{"OPENAI_BASE_URL", "https://user:token@gateway.example/v1", false},
		{"OPENAI_BASE_URL", "https://gateway.example/v1?key=abcdefgh", false},
		{"ANTHROPIC_BASE_URL", "https://gateway.example/#token", false},
		{"OPENAI_BASE_URL", "gateway.example/v1", false},
		// No wildcard, no number rule, no credential names.
		{"CORP_BASE_URL", "https://gateway.example/v1", false},
		{"PIN", "12345678", false},
		{"AWS_SECRET_ACCESS_KEY", "true", false},
		{"MY_TOKEN", "us-east-1", false},
		{"ANTHROPIC_MODEL", "claude-sonnet-5", false},
		{"AWS_REGION", "", false},
	} {
		assert.Equal(t, tc.want, NonSecretEnvValue(tc.name, tc.value), "%s=%s", tc.name, tc.value)
	}
}

func TestRedactionSetSkipsConfiguration(t *testing.T) {
	isolateProviderEnv(t)
	set := redactionSet(Request{Env: map[string]string{"AWS_REGION": "us-east-1", "GOOGLE_GENAI_USE_VERTEXAI": "true",
		"OPENAI_API_KEY": providerKey, "OPENAI_BASE_URL": "https://u:pw@gw.example"}})
	assert.False(t, slices.Contains(set, "us-east-1"))
	assert.False(t, slices.Contains(set, "true"))
	assert.True(t, slices.Contains(set, providerKey))
	assert.True(t, slices.Contains(set, "https://u:pw@gw.example"))
}

// TestAnswerWithConfigurationIsNotSecret: an answer that repeats a region or a flag of `env` is
// not stored as a secret, one that repeats a credential is.
func TestAnswerWithConfigurationIsNotSecret(t *testing.T) {
	isolation := isolateProviderEnv(t)
	answer := ""
	f := newFakeModels(t, func(wireReq) wireReply { return wireReply{Text: answer} })
	env := maps.Clone(isolation)
	maps.Copy(env, map[string]string{"FAKE_PROVIDER_KEY": providerKey, "AWS_REGION": "us-east-1",
		"GOOGLE_GENAI_USE_VERTEXAI": "true"})
	req := Request{Name: "d6", YAML: []byte(providerCase{models: corpModels}.yaml(f.URL, "")), Prompt: "Where?", Env: env}

	answer = "deployed to us-east-1: true"
	res, err := Run(t.Context(), req)
	require.NoError(t, err)
	assert.False(t, res.OutputContainsSecret)
	assert.Empty(t, res.Warnings)
	assert.True(t, AnswerContainsSecret("key "+providerKey, req))
	assert.False(t, AnswerContainsSecret(answer, req))

	answer = "the key is " + providerKey
	res, err = Run(t.Context(), req)
	require.NoError(t, err)
	assert.True(t, res.OutputContainsSecret)
}
