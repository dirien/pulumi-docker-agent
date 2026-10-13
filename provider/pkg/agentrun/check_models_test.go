// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package agentrun

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docker/docker-agent/pkg/chatgpt"
	"github.com/docker/docker-agent/pkg/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// checkModels runs CheckModels for a configuration with env (all values known).
func checkModels(t *testing.T, doc string, env map[string]string, inherit bool) (warnings, infos []string) {
	t.Helper()
	opts := CheckOptions{InheritEnvironment: inherit, EnvKeys: map[string]bool{}, EnvValues: map[string]string{}}
	for k, v := range env {
		opts.EnvKeys[k], opts.EnvValues[k] = v != "", v
	}
	return CheckModels(t.Context(), "models.yaml", []byte(doc), opts)
}

func agentWith(model string) string {
	return "agents:\n  root:\n    model: " + model + "\n    instruction: hi\n"
}

func joined(s []string) string { return strings.Join(s, "\n") }

// TestCheckModelsMissingCredentials: docker-agent's own preflight (the variables the models
// need), with AgentRun's advice instead of the docker agent CLI's.
func TestCheckModelsMissingCredentials(t *testing.T) {
	isolateProviderEnv(t)
	t.Setenv("HOME", t.TempDir())
	doc := "models:\n  m:\n    provider: openai\n    model: gpt-4o-mini\n" + agentWith("m")

	warnings, _ := checkModels(t, doc, nil, false)
	require.Len(t, warnings, 1)
	assert.Equal(t, "the configuration needs OPENAI_API_KEY, which env does not set (inheritEnvironment is false, so the "+
		"environment of the Pulumi CLI is not used); set it in env (for example from a secret config value)", warnings[0])

	warnings, _ = checkModels(t, doc, nil, true)
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "OPENAI_API_KEY, which neither env nor the environment of the Pulumi CLI sets")

	// The environment of the Pulumi CLI has it, but inheritEnvironment: false keeps it from the run.
	t.Setenv("OPENAI_API_KEY", processOnlyKey)
	warnings, _ = checkModels(t, doc, nil, false)
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "; OPENAI_API_KEY is set in the environment of the Pulumi CLI, but inheritEnvironment: "+
		"false keeps it from the run (since v0.2.0 also from the model SDKs): pass it in env, or set inheritEnvironment: true")
	assert.NotContains(t, warnings[0], processOnlyKey)
	warnings, _ = checkModels(t, doc, nil, true)
	assert.Empty(t, warnings, "inherited")
	warnings, _ = checkModels(t, doc, map[string]string{"OPENAI_API_KEY": providerKey}, false)
	assert.Empty(t, warnings, "in env")

	// A value that is unknown during the preview counts as set; an unknown env skips the check.
	opts := CheckOptions{EnvKeys: map[string]bool{"OPENAI_API_KEY": true}}
	warnings, _ = CheckModels(t.Context(), "models.yaml", []byte(doc), opts)
	assert.Empty(t, warnings)
	warnings, _ = CheckModels(t.Context(), "models.yaml", []byte(doc), CheckOptions{EnvUnknown: true})
	assert.Empty(t, warnings)
}

// TestCheckModelsEnvFile: docker agent setup stores keys in ~/.config/cagent/.env, which AgentRun
// does not read; the warning names the variable and the file, never the value.
func TestCheckModelsEnvFile(t *testing.T) {
	isolateProviderEnv(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".config", "cagent")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".env"), []byte("# docker agent setup\nexport MISTRAL_API_KEY="+
		processOnlyKey+"\n"), 0o600))
	warnings, _ := checkModels(t, agentWith("mistral/devstral-small-2507"), nil, true)
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "MISTRAL_API_KEY is in docker agent's env file ("+filepath.Join(dir, ".env")+"), which "+
		"AgentRun does not read: set it in env")
	assert.NotContains(t, warnings[0], processOnlyKey)
}

func TestCheckModelsFirstAvailable(t *testing.T) {
	isolateProviderEnv(t)
	t.Setenv("HOME", t.TempDir())
	doc := "models:\n  m:\n    first_available: [anthropic/claude-haiku-4-5, google/gemini-2.5-flash]\n" + agentWith("m")
	warnings, _ := checkModels(t, doc, nil, false)
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "no first_available candidate has its credentials (ANTHROPIC_API_KEY, GOOGLE_API_KEY)")
	assert.Contains(t, warnings[0], "set the credentials of one candidate in env")
	warnings, _ = checkModels(t, doc, map[string]string{"GOOGLE_API_KEY": providerKey}, false)
	assert.Empty(t, warnings)
}

// TestCheckModelsSDKVariables: the variables the model SDKs read themselves, for every provider:
// with inheritEnvironment false those that no longer reach the model (generalized W8), with true
// those that reach it although docker-agent does not ask for them (W2 for in-process models).
func TestCheckModelsSDKVariables(t *testing.T) {
	openai := "models:\n  m:\n    provider: openai\n    model: gpt-4o-mini\n" + agentWith("m")
	key := map[string]string{"OPENAI_API_KEY": providerKey}
	t.Run("base URLs first, inheritEnvironment false", func(t *testing.T) {
		isolateProviderEnv(t)
		t.Setenv("OPENAI_ORG_ID", "org-of-the-cli")
		t.Setenv("OPENAI_BASE_URL", "https://gateway.example/v1")
		warnings, _ := checkModels(t, openai, key, false)
		require.Len(t, warnings, 2, joined(warnings))
		assert.Equal(t, "OPENAI_BASE_URL is set in the environment of the Pulumi CLI, but with inheritEnvironment: false it "+
			"no longer reaches agents.root.model (openai/gpt-4o-mini) since v0.2.0: its requests go to "+
			"https://api.openai.com/v1; set it in env, or set the model's base_url", warnings[0])
		assert.Equal(t, "OPENAI_ORG_ID is set in the environment of the Pulumi CLI, but with inheritEnvironment: false it "+
			"no longer reaches agents.root.model (openai/gpt-4o-mini) since v0.2.0: its requests no longer carry the header "+
			"it sets; set it in env", warnings[1])
		assert.NotContains(t, joined(warnings), "gateway.example", "no value")

		// Set in env: no warning, also when it is blanked.
		env := map[string]string{"OPENAI_API_KEY": providerKey, "OPENAI_BASE_URL": "", "OPENAI_ORG_ID": "org"}
		warnings, _ = checkModels(t, openai, env, false)
		assert.Empty(t, warnings)
	})
	t.Run("inherited base URL", func(t *testing.T) {
		isolateProviderEnv(t)
		t.Setenv("OPENAI_BASE_URL", "https://gateway.example/v1")
		t.Setenv("OPENAI_ORG_ID", "org-of-the-cli")
		warnings, _ := checkModels(t, openai, key, true)
		require.Len(t, warnings, 1, "the organization goes to OpenAI's own API: %s", joined(warnings))
		assert.Equal(t, "OPENAI_BASE_URL (inherited from the environment of the Pulumi CLI) sends the requests and the key of "+
			"agents.root.model (openai/gpt-4o-mini) to OPENAI_BASE_URL instead of https://api.openai.com/v1, as with docker "+
			"agent run; set inheritEnvironment: false, or set the model's base_url", warnings[0])
	})
	t.Run("OPENAI_API_KEY to another endpoint", func(t *testing.T) {
		isolateProviderEnv(t)
		t.Setenv("OPENAI_API_KEY", processOnlyKey)
		cloud := "models:\n  m:\n    provider: ollama\n    model: gpt-oss:120b\n    base_url: https://ollama.com/v1\n" +
			agentWith("m")
		warnings, _ := checkModels(t, cloud, nil, true)
		require.Len(t, warnings, 1, joined(warnings))
		assert.Equal(t, "OPENAI_API_KEY (inherited from the environment of the Pulumi CLI) is sent as the key of "+
			"agents.root.model (ollama/gpt-oss:120b) to https://ollama.com (the model has a base_url without token_key, so "+
			"the OpenAI SDK reads OPENAI_API_KEY itself), as with docker agent run; set token_key to the variable of that "+
			"endpoint's key, or blank it with env: {OPENAI_API_KEY: \"\"}", warnings[0])
		warnings, _ = checkModels(t, cloud, nil, false)
		require.Len(t, warnings, 1)
		assert.Contains(t, warnings[0], "OPENAI_API_KEY is set in the environment of the Pulumi CLI, but with "+
			"inheritEnvironment: false it no longer reaches agents.root.model (ollama/gpt-oss:120b) since v0.2.0: its "+
			"requests to https://ollama.com carry no key (it has a base_url without token_key); set it in env, or set the "+
			"model's token_key")

		// Ollama on this machine: the key goes nowhere.
		local := "models:\n  m:\n    provider: ollama\n    model: llama3.2\n" + agentWith("m")
		warnings, _ = checkModels(t, local, nil, true)
		assert.Empty(t, warnings)
		warnings, _ = checkModels(t, local, nil, false)
		assert.Empty(t, warnings)
	})
	t.Run("OpenAI headers to an alias", func(t *testing.T) {
		isolateProviderEnv(t)
		t.Setenv("OPENAI_PROJECT_ID", "proj-of-the-cli")
		mistral := agentWith("mistral/devstral-small-2507")
		warnings, _ := checkModels(t, mistral, map[string]string{"MISTRAL_API_KEY": providerKey}, true)
		require.Len(t, warnings, 1, joined(warnings))
		assert.Equal(t, "OPENAI_PROJECT_ID (inherited from the environment of the Pulumi CLI) is sent with the requests of "+
			"agents.root.model (mistral/devstral-small-2507) to https://api.mistral.ai (the OpenAI SDK adds it to every "+
			"OpenAI-protocol model), as with docker agent run; set inheritEnvironment: false to keep it from the model",
			warnings[0])
	})
	t.Run("ANTHROPIC_AUTH_TOKEN", func(t *testing.T) {
		isolateProviderEnv(t)
		t.Setenv("ANTHROPIC_AUTH_TOKEN", "fake-anthropic-auth-token-of-the-cli")
		corp := "providers:\n  corp:\n    provider: anthropic\n    base_url: https://llm.example\n    token_key: CORP_KEY\n" +
			"models:\n  m:\n    provider: corp\n    model: claude-haiku-4-5\n" + agentWith("m")
		warnings, _ := checkModels(t, corp, map[string]string{"CORP_KEY": providerKey}, true)
		require.Len(t, warnings, 1, joined(warnings))
		assert.Equal(t, "ANTHROPIC_AUTH_TOKEN (inherited from the environment of the Pulumi CLI) is sent as a bearer token, "+
			"next to the key of agents.root.model (corp/claude-haiku-4-5), to https://llm.example, as with docker agent run; "+
			"set inheritEnvironment: false, or blank it with env: {ANTHROPIC_AUTH_TOKEN: \"\"}", warnings[0])
		// ANTHROPIC_API_KEY in the run's environment: the SDK adds no token.
		direct := "models:\n  m:\n    provider: anthropic\n    model: claude-haiku-4-5\n    base_url: https://llm.example\n" +
			agentWith("m")
		warnings, _ = checkModels(t, direct, map[string]string{"ANTHROPIC_API_KEY": providerKey}, true)
		assert.Empty(t, warnings)
	})
	t.Run("Bedrock and Vertex AI use the machine's credentials", func(t *testing.T) {
		isolateProviderEnv(t)
		bedrock := "models:\n  m:\n    provider: amazon-bedrock\n    model: amazon.nova-micro-v1:0\n    provider_opts:\n" +
			"      region: us-east-1\n" + agentWith("m")
		warnings, _ := checkModels(t, bedrock, nil, false)
		require.Len(t, warnings, 1, joined(warnings))
		assert.Equal(t, "with inheritEnvironment: false, agents.root.model (amazon-bedrock/amazon.nova-micro-v1:0) gets "+
			"no AWS credentials from env, so it uses this machine's AWS default chain (shared files such as "+
			"~/.aws/credentials, SSO, container or instance metadata), which may be another account; pass the credentials "+
			"in env (AWS_BEARER_TOKEN_BEDROCK, or AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY)", warnings[0])
		t.Setenv("AWS_ACCESS_KEY_ID", "AKIAFAKEPROCESSKEY01")
		t.Setenv("AWS_SECRET_ACCESS_KEY", "fake-process-secret-0123456789")
		warnings, _ = checkModels(t, bedrock, nil, false)
		require.Len(t, warnings, 1, joined(warnings))
		assert.True(t, strings.HasPrefix(warnings[0], "AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY are set in the environment "+
			"of the Pulumi CLI, but with inheritEnvironment: false they no longer reach agents.root.model "+
			"(amazon-bedrock/amazon.nova-micro-v1:0) since v0.2.0, which gets no AWS credentials from env"), warnings[0])
		warnings, _ = checkModels(t, bedrock, map[string]string{"AWS_BEARER_TOKEN_BEDROCK": providerKey}, false)
		assert.Empty(t, warnings)
		warnings, _ = checkModels(t, bedrock, nil, true)
		assert.Empty(t, warnings, "inherited AWS credentials are the point of inheritEnvironment")

		vertex := "models:\n  m:\n    provider: google\n    model: gemini-2.5-flash\n    provider_opts:\n" +
			"      project: p\n      location: us-central1\n" + agentWith("m")
		warnings, _ = checkModels(t, vertex, nil, false)
		require.Len(t, warnings, 1, joined(warnings))
		assert.Contains(t, warnings[0], "gets no GOOGLE_APPLICATION_CREDENTIALS from env, so it uses this machine's "+
			"Google application default credentials (the gcloud login, or the metadata server)")
		warnings, _ = checkModels(t, vertex, map[string]string{"GOOGLE_APPLICATION_CREDENTIALS": "/sa.json"}, false)
		assert.Empty(t, warnings)
		proxy := "models:\n  m:\n    provider: google\n    model: gemini-2.5-flash\n    base_url: https://vertex-proxy.example/\n" +
			agentWith("m")
		warnings, _ = checkModels(t, proxy, map[string]string{"GOOGLE_GENAI_USE_VERTEXAI": "true"}, false)
		assert.Empty(t, warnings, "a Vertex AI base_url without a project gets no ADC")
	})
	t.Run("hidden names", func(t *testing.T) {
		isolateProviderEnv(t)
		t.Setenv("OPENAI_BASE_URL", "https://gateway.example/v1")
		opts := CheckOptions{EnvKeys: map[string]bool{"OPENAI_API_KEY": true}, EnvValues: key, HideNames: true}
		warnings, _ := CheckModels(t.Context(), "models.yaml", []byte(openai), opts)
		require.Len(t, warnings, 1)
		assert.Contains(t, warnings[0], "no longer reaches a model since v0.2.0")
		assert.NotContains(t, warnings[0], "gpt-4o-mini")
		assert.NotContains(t, warnings[0], "root")

		// Neither the host of a base_url nor a model key (phase-2 review #6).
		t.Setenv("OPENAI_API_KEY", processOnlyKey)
		secret := "models:\n  secretmodel:\n    provider: ollama\n    model: gpt-oss:120b\n    base_url: " +
			"https://internal.secret-host.example/v1\n    bypass_models_gateway: true\n" + agentWith("secretmodel")
		for _, inherit := range []bool{true, false} {
			opts := CheckOptions{InheritEnvironment: inherit, EnvKeys: map[string]bool{}, HideNames: true}
			warnings, _ := CheckModels(t.Context(), "models.yaml", []byte(secret), opts)
			require.Len(t, warnings, 2, joined(warnings))
			assert.NotContains(t, joined(warnings), "secret-host")
			assert.NotContains(t, joined(warnings), "secretmodel")
			assert.Contains(t, joined(warnings), "bypass_models_gateway (a model) has no effect")
			assert.Contains(t, joined(warnings), "its endpoint")
		}
	})
	t.Run("ANTHROPIC_CUSTOM_HEADERS only where anthropic-sdk-go sends them", func(t *testing.T) {
		isolateProviderEnv(t)
		t.Setenv("ANTHROPIC_CONFIG_DIR", t.TempDir())
		t.Setenv("ANTHROPIC_CUSTOM_HEADERS", "X-Gateway: yes")
		direct := "models:\n  m:\n    provider: anthropic\n    model: claude-haiku-4-5\n" + agentWith("m")
		corp := "providers:\n  corp:\n    provider: anthropic\n    base_url: https://llm.example\n    token_key: CORP_KEY\n" +
			"models:\n  m:\n    provider: corp\n    model: claude-haiku-4-5\n" + agentWith("m")
		withKey := map[string]string{"ANTHROPIC_API_KEY": providerKey}
		corpKey := map[string]string{"CORP_KEY": providerKey}

		// The SDK returns before it reads the headers when its environment has a key.
		warnings, _ := checkModels(t, direct, withKey, true)
		assert.Empty(t, warnings)
		t.Setenv("ANTHROPIC_API_KEY", inheritedKey)
		warnings, _ = checkModels(t, direct, nil, true)
		assert.Empty(t, warnings, "the usual setup: ANTHROPIC_API_KEY and ANTHROPIC_CUSTOM_HEADERS exported")
		warnings, _ = checkModels(t, corp, corpKey, false)
		assert.Empty(t, warnings, "before v0.2.0 the provider's ANTHROPIC_API_KEY kept the headers off too")
		require.NoError(t, os.Unsetenv("ANTHROPIC_API_KEY"))

		// Without a key of the SDK's own they are sent (inherited) and were sent before v0.2.0.
		warnings, _ = checkModels(t, corp, corpKey, true)
		assert.Equal(t, []string{"ANTHROPIC_CUSTOM_HEADERS (inherited from the environment of the Pulumi CLI) adds its " +
			"headers to the requests of agents.root.model (corp/claude-haiku-4-5) to https://llm.example, as with docker " +
			"agent run; set inheritEnvironment: false, or blank it with env: {ANTHROPIC_CUSTOM_HEADERS: \"\"}"}, warnings)
		warnings, _ = checkModels(t, corp, corpKey, false)
		assert.Equal(t, []string{"ANTHROPIC_CUSTOM_HEADERS is set in the environment of the Pulumi CLI, but with " +
			"inheritEnvironment: false it no longer reaches agents.root.model (corp/claude-haiku-4-5) since v0.2.0: its " +
			"requests no longer carry the header it sets; set it in env"}, warnings)

		// ANTHROPIC_PROFILE, env federation and a profile file are credentials of the SDK too.
		for _, extra := range []map[string]string{{"ANTHROPIC_PROFILE": "work"}, {"ANTHROPIC_FEDERATION_RULE_ID": "r",
			"ANTHROPIC_ORGANIZATION_ID": "o", "ANTHROPIC_IDENTITY_TOKEN": "fake-identity-token-0123456789"}} {
			env := maps.Clone(corpKey)
			maps.Copy(env, extra)
			warnings, _ = checkModels(t, corp, env, true)
			assert.Empty(t, warnings, "%v", extra)
		}
		dir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "configs"), 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "configs", "default.json"), []byte(`{}`), 0o600))
		t.Setenv("ANTHROPIC_CONFIG_DIR", dir)
		warnings, _ = checkModels(t, corp, corpKey, true)
		assert.Empty(t, warnings, "a fallback profile")
	})
	t.Run("a realistic shell environment", func(t *testing.T) {
		isolateProviderEnv(t)
		t.Setenv("ANTHROPIC_CONFIG_DIR", t.TempDir())
		for k, v := range map[string]string{"ANTHROPIC_API_KEY": inheritedKey, "OPENAI_API_KEY": processOnlyKey,
			"OPENAI_ORG_ID": "org-of-the-cli", "ANTHROPIC_CUSTOM_HEADERS": "X-Gateway: yes", "AWS_PROFILE": "dev"} {
			t.Setenv(k, v)
		}
		doc := "models:\n  o:\n    provider: openai\n    model: gpt-4o-mini\n  a:\n    provider: anthropic\n" +
			"    model: claude-haiku-4-5\n" + agentWith("o") + "    fallback:\n      models: [a]\n"
		warnings, infos := checkModels(t, doc, nil, true)
		assert.Empty(t, warnings, "the organization goes to OpenAI's own API, the headers are not sent")
		assert.Empty(t, infos)
		warnings, _ = checkModels(t, doc, map[string]string{"OPENAI_API_KEY": providerKey, "ANTHROPIC_API_KEY": providerKey},
			false)
		assert.Equal(t, []string{"OPENAI_ORG_ID is set in the environment of the Pulumi CLI, but with inheritEnvironment: " +
			"false it no longer reaches agents.root.model (openai/gpt-4o-mini) since v0.2.0: its requests no longer carry " +
			"the header it sets; set it in env"}, warnings)
	})
}

// TestCheckModelsBedrockStructuredOutput: Bedrock gets no schema in native mode (D4).
func TestCheckModelsBedrockStructuredOutput(t *testing.T) {
	isolateProviderEnv(t)
	bedrock := "models:\n  m:\n    provider: amazon-bedrock\n    model: amazon.nova-micro-v1:0\n" + agentWith("m")
	env := map[string]string{"AWS_BEARER_TOKEN_BEDROCK": providerKey}
	warnings, _ := checkModels(t, bedrock+reviewSchema, env, false)
	require.Len(t, warnings, 1, joined(warnings))
	assert.Equal(t, "agents.root.structured_output is not enforced by Amazon Bedrock (agents.root.model "+
		"(amazon-bedrock/amazon.nova-micro-v1:0)): docker-agent sends Bedrock models no schema, so the answer may not be "+
		"JSON and the run then fails; set structured_output.mode: tool to have docker-agent enforce the schema", warnings[0])
	warnings, _ = checkModels(t, bedrock+structuredBlock("tool"), env, false)
	assert.Empty(t, warnings)
	// A fallback on Bedrock answers too.
	fallback := "models:\n  m:\n    provider: openai\n    model: gpt-4o-mini\n  b:\n    provider: amazon-bedrock\n" +
		"    model: amazon.nova-micro-v1:0\n" + agentWith("m") + "    fallback:\n      models: [b]\n"
	env["OPENAI_API_KEY"] = providerKey
	warnings, _ = checkModels(t, fallback+reviewSchema, env, false)
	require.Len(t, warnings, 1, joined(warnings))
	assert.Contains(t, warnings[0], "agents.root.fallback.models[0] (amazon-bedrock/amazon.nova-micro-v1:0)")
}

// TestCheckModelsGateway: AgentRun never uses a Docker models gateway.
func TestCheckModelsGateway(t *testing.T) {
	isolateProviderEnv(t)
	doc := "models:\n  m:\n    provider: openai\n    model: gpt-4o-mini\n    bypass_models_gateway: true\n" + agentWith("m")
	warnings, _ := checkModels(t, doc, map[string]string{"OPENAI_API_KEY": providerKey,
		"DOCKER_AGENT_MODELS_GATEWAY": "https://gateway.docker.com"}, false)
	assert.Equal(t, []string{
		"AgentRun does not use a Docker models gateway: DOCKER_AGENT_MODELS_GATEWAY from env has no effect, and the models " +
			"call their providers directly, with their own credentials",
		"bypass_models_gateway (models.m) has no effect: AgentRun never uses a Docker models gateway",
	}, warnings)
}

// TestCheckModelsAuto: the preview says what model auto resolves to, with the run's environment,
// and DOCKER_AGENT_DEFAULT_MODEL selects it (as with docker agent run).
func TestCheckModelsAuto(t *testing.T) {
	isolateProviderEnv(t)
	doc := agentWith("auto")
	_, infos := checkModels(t, doc, map[string]string{"GOOGLE_API_KEY": providerKey}, false)
	assert.Equal(t, []string{"agents.root.model auto resolves to google/gemini-3.8-flash (GOOGLE_API_KEY from env)"}, infos)

	t.Setenv("GITHUB_TOKEN", inheritedKey)
	_, infos = checkModels(t, doc, nil, true)
	assert.Equal(t, []string{"agents.root.model auto resolves to github-copilot/gpt-5.6-sol (GITHUB_TOKEN from the " +
		"environment of the Pulumi CLI)"}, infos, "a CI token picks Copilot")

	_, infos = checkModels(t, doc, map[string]string{"DOCKER_AGENT_DEFAULT_MODEL": "anthropic/claude-haiku-4-5",
		"ANTHROPIC_API_KEY": providerKey, "OPENAI_API_KEY": providerKey}, false)
	assert.Equal(t, []string{"agents.root.model auto resolves to anthropic/claude-haiku-4-5, selected by " +
		"DOCKER_AGENT_DEFAULT_MODEL from env (ANTHROPIC_API_KEY from env)"}, infos)

	_, infos = checkModels(t, doc, nil, false)
	require.Len(t, infos, 1)
	assert.Contains(t, infos[0], "agents.root.model auto finds no provider credential in the run's environment and "+
		"resolves to Docker Model Runner (dmr/ai/qwen3:latest")

	// A selected model whose credential is not set fails the run: a warning (phase-2 review #12).
	warnings, infos := checkModels(t, doc, map[string]string{"DOCKER_AGENT_DEFAULT_MODEL": "anthropic/claude-haiku-4-5"},
		false)
	assert.Empty(t, infos)
	assert.Equal(t, []string{"agents.root.model auto resolves to anthropic/claude-haiku-4-5, selected by " +
		"DOCKER_AGENT_DEFAULT_MODEL from env, but the run's environment does not set its credential (ANTHROPIC_API_KEY " +
		"(not set)), so the run fails: set it in env (for example from a secret config value), or select another model " +
		"with DOCKER_AGENT_DEFAULT_MODEL"}, warnings)
}

// TestCheckModelsUnknownVertexFlag: a GOOGLE_GENAI_USE_VERTEXAI that is not known yet may select
// the Gemini API or Vertex AI; only what both need is missing.
func TestCheckModelsUnknownVertexFlag(t *testing.T) {
	isolateProviderEnv(t)
	doc := agentWith("google/gemini-2.5-flash")
	opts := CheckOptions{EnvKeys: map[string]bool{"GOOGLE_GENAI_USE_VERTEXAI": true}, EnvValues: map[string]string{}}
	warnings, _ := CheckModels(t.Context(), "models.yaml", []byte(doc), opts)
	assert.Empty(t, warnings)
	warnings, _ = checkModels(t, doc, map[string]string{"GOOGLE_GENAI_USE_VERTEXAI": "false"}, false)
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "the configuration needs GOOGLE_API_KEY")
}

// fakeChatGPTLogin writes a ChatGPT login whose access token is valid for an hour and points the
// chatgpt package of this process and AgentRun's login path at it (under go test the package reads
// no login otherwise). It returns the login's path.
func fakeChatGPTLogin(t *testing.T, accessToken string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cagent", chatgptAuthFile)
	writeChatGPTLogin(t, path, accessToken, "fake-chatgpt-refresh-token-0123456789")
	t.Cleanup(chatgpt.SetCredentialsPathForTests(path))
	prev := chatgptLoginPath
	chatgptLoginPath = func() string { return path }
	t.Cleanup(func() { chatgptLoginPath = prev })
	return path
}

// TestCheckModelsChatGPT: a chatgpt model needs the ChatGPT login of this machine or
// CHATGPT_OAUTH_TOKEN; model auto never gets the login.
func TestCheckModelsChatGPT(t *testing.T) {
	isolateProviderEnv(t)
	doc := agentWith("chatgpt/gpt-5.6")
	t.Cleanup(chatgpt.SetCredentialsPathForTests(filepath.Join(t.TempDir(), "none.json")))
	prev := chatgptLoginPath
	chatgptLoginPath = func() string { return "/home/u/.config/cagent/chatgpt-auth.json" }
	t.Cleanup(func() { chatgptLoginPath = prev })
	warnings, infos := checkModels(t, doc, nil, false)
	assert.Equal(t, []string{"agents.root.model (chatgpt/gpt-5.6) uses a ChatGPT subscription, but this machine has no " +
		"ChatGPT login and env does not set CHATGPT_OAUTH_TOKEN: sign in with `docker agent setup` on the machine that " +
		"runs pulumi up (it stores the login in /home/u/.config/cagent/chatgpt-auth.json)"}, warnings)
	assert.Empty(t, infos)
	warnings, infos = checkModels(t, doc, map[string]string{"CHATGPT_OAUTH_TOKEN": "fake-access"}, false)
	assert.Empty(t, warnings)
	assert.Empty(t, infos)

	path := fakeChatGPTLogin(t, "fake-chatgpt-access-token-0123456789")
	warnings, infos = checkModels(t, doc, nil, false)
	assert.Empty(t, warnings)
	assert.Equal(t, []string{"agents.root.model (chatgpt/gpt-5.6) uses the ChatGPT login of docker agent setup on this " +
		"machine (" + path + "), billed to the ChatGPT plan: AgentRun reads that file, and writes a token it refreshed " +
		"back to it"}, infos)

	// model auto does not see the login (U#2): with only GOOGLE_API_KEY it stays with Google.
	_, infos = checkModels(t, agentWith("auto"), map[string]string{"GOOGLE_API_KEY": providerKey}, false)
	assert.Equal(t, []string{"agents.root.model auto resolves to google/gemini-3.8-flash (GOOGLE_API_KEY from env)"}, infos)

	// DOCKER_AGENT_DEFAULT_MODEL selects a chatgpt model for auto: it uses the login.
	dm := map[string]string{"DOCKER_AGENT_DEFAULT_MODEL": "chatgpt/gpt-5.6"}
	warnings, infos = checkModels(t, agentWith("auto"), dm, false)
	assert.Empty(t, warnings)
	assert.Equal(t, []string{"agents.root.model (auto: chatgpt/gpt-5.6) uses the ChatGPT login of docker agent setup on " +
		"this machine (" + path + "), billed to the ChatGPT plan: AgentRun reads that file, and writes a token it " +
		"refreshed back to it", "agents.root.model auto resolves to chatgpt/gpt-5.6, selected by DOCKER_AGENT_DEFAULT_MODEL " +
		"from env (the ChatGPT login of docker agent setup, billed to the ChatGPT plan)"}, infos)

	// An empty CHATGPT_OAUTH_TOKEN hides the login, as at run time (phase-2 review #12).
	warnings, infos = checkModels(t, doc, map[string]string{"CHATGPT_OAUTH_TOKEN": ""}, false)
	assert.Empty(t, infos)
	assert.Equal(t, []string{"agents.root.model (chatgpt/gpt-5.6) uses a ChatGPT subscription, but env sets " +
		"CHATGPT_OAUTH_TOKEN to an empty value, which hides the ChatGPT login of docker agent setup from the run, so the " +
		"run fails: remove it from env to use the login, or pass a ChatGPT access token in env"}, warnings)

	// A first_available candidate that is not chosen does not use the login.
	fa := "models:\n  pick:\n    first_available: [openai/gpt-5-mini, chatgpt/gpt-5.6]\n" + agentWith("pick")
	warnings, infos = checkModels(t, fa, map[string]string{"OPENAI_API_KEY": providerKey}, false)
	assert.Empty(t, warnings)
	assert.Empty(t, infos)

	// Not signed in.
	t.Cleanup(chatgpt.SetCredentialsPathForTests(filepath.Join(t.TempDir(), "none.json")))
	warnings, infos = checkModels(t, agentWith("auto"), dm, false)
	assert.Empty(t, infos)
	assert.Equal(t, []string{"agents.root.model (auto: chatgpt/gpt-5.6) uses a ChatGPT subscription, but this machine " +
		"has no ChatGPT login and env does not set CHATGPT_OAUTH_TOKEN: sign in with `docker agent setup` on the machine " +
		"that runs pulumi up (it stores the login in " + path + ")"}, warnings)
}

// TestCheckModelsProviderCases: the provider matrix's configurations (with their credentials in
// env) preview without a model warning, with and without inheritEnvironment.
func TestCheckModelsProviderCases(t *testing.T) {
	for _, c := range providerCases() {
		t.Run(c.name, func(t *testing.T) {
			isolation := isolateProviderEnv(t)
			env := c.env()
			maps.Copy(env, c.sdkVars(t, "http://127.0.0.1:1"))
			maps.Copy(env, isolation)
			doc := c.yaml("http://127.0.0.1:1", fsToolset)
			for _, inherit := range []bool{false, true} {
				warnings, infos := checkModels(t, doc, env, inherit)
				assert.Empty(t, warnings, "inheritEnvironment %v", inherit)
				assert.Empty(t, infos, "inheritEnvironment %v", inherit)
			}
		})
	}
}

// TestConfigModels: every place a configuration names a model, and whether it names chatgpt.
func TestConfigModels(t *testing.T) {
	load := func(doc string) []modelUse {
		cfg, err := config.Load(t.Context(), config.NewBytesSource("m.yaml", []byte(doc)))
		require.NoError(t, err)
		return configModels(cfg)
	}
	uses := load(`providers:
  sub:
    provider: chatgpt
models:
  router:
    provider: openai
    model: gpt-4o-mini
    routing:
      - model: anthropic/claude-haiku-4-5
        examples: [poem]
  pick:
    first_available: [sub/gpt-5.6, google/gemini-2.5-flash]
agents:
  root:
    model: router,pick
    instruction: hi
    fallback:
      models: [mistral/devstral-small-2507]
  coder:
    model: openai/gpt-5-mini
    harness:
      type: claude-code
`)
	var got []string
	for _, u := range uses {
		got = append(got, u.loc+" "+u.role+" "+u.name+"/"+u.model+" "+u.typ)
	}
	assert.Equal(t, []string{
		"agents.root.model  openai/gpt-4o-mini openai",
		"models.router.routing[0].model routed anthropic/claude-haiku-4-5 anthropic",
		"models.pick.first_available[0]  sub/gpt-5.6 openai_responses",
		"models.pick.first_available[1]  google/gemini-2.5-flash google",
		"agents.root.fallback.models[0] fallback mistral/devstral-small-2507 openai",
	}, got, "harness agents are left out")
	assert.True(t, uses[2].isChatGPT() && uses[2].custom && uses[2].firstAvailable)

	// Hooks, RAG and model_picker models are model uses too; a router's own requests use only
	// its provider/model (phase-2 review #9).
	uses = load(`models:
  router:
    provider: openai
    model: gpt-4o-mini
    base_url: https://router.example/v1
    token_key: ROUTER_KEY
    routing:
      - model: anthropic/claude-haiku-4-5
        examples: [poem]
agents:
  root:
    model: router
    instruction: hi
    hooks:
      pre_tool_use:
        - matcher: "*"
          hooks:
            - type: model
              model: chatgpt/gpt-5.6
              prompt: judge
    toolsets:
      - type: model_picker
        models: [mistral/devstral-small-2507]
      - type: rag
        ref: docs
rag:
  docs:
    docs: [README.md]
    strategies:
      - type: chunked-embeddings
        embedding_model: openai/text-embedding-3-small
        vector_dimensions: 1536
        database: ./rag.db
    results:
      reranking:
        model: dmr/ai/qwen3-reranker
`)
	got = nil
	for _, u := range uses {
		got = append(got, u.role+" "+u.name+"/"+u.model)
	}
	assert.ElementsMatch(t, []string{" openai/gpt-4o-mini", "routed anthropic/claude-haiku-4-5",
		"tool mistral/devstral-small-2507", "tool openai/text-embedding-3-small", "tool dmr/ai/qwen3-reranker",
		"tool chatgpt/gpt-5.6"}, got)
	assert.Empty(t, uses[0].cfg.BaseURL, "the router's fallback is rebuilt from provider/model")
	assert.Empty(t, uses[0].cfg.TokenKey)

	for doc, want := range map[string]bool{
		agentWith("chatgpt/gpt-5.6"): true,
		agentWith("auto"):            false,
		agentWith("openai/gpt-5-mini") + "    fallback:\n      models: [chatgpt/gpt-5.6]\n": true,
		"models:\n  m:\n    provider: openai\n    model: x\n    routing:\n      - model: chatgpt/gpt-5.6\n" +
			"        examples: [a]\n" + agentWith("m"): true,
		agentWith("openai/gpt-5-mini"): false,
	} {
		cfg, err := config.Load(t.Context(), config.NewBytesSource("m.yaml", []byte(doc)))
		require.NoError(t, err)
		assert.Equal(t, want, usesChatGPT(t.Context(), cfg, envView{}.asProvider()), doc)
	}
}
