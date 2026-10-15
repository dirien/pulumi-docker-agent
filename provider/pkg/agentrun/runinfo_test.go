// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package agentrun

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docker/docker-agent/pkg/config"
	"github.com/docker/docker-agent/pkg/config/latest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// corpModels is a custom provider at the fake.
func corpModels(url string) string {
	return "providers:\n  corp:\n    api_type: openai_chatcompletions\n    base_url: " + url + "/v1\n" +
		"    token_key: FAKE_PROVIDER_KEY\nmodels:\n  m:\n    provider: corp\n    model: my-model\n"
}

// progressRecords runs req and returns its persistent progress records.
func progressRecords(t *testing.T, req Request) ([]string, *Result, error) {
	t.Helper()
	var out []string
	req.Progress = func(p Progress) {
		if p.Persistent && !p.Summary {
			out = append(out, p.Message)
		}
	}
	res, err := Run(t.Context(), req)
	return out, res, err
}

// TestCredentialLine: every run reports its models and where their credentials come from (D5), as
// Claude Code's apiKeySource record does, without a value.
func TestCredentialLine(t *testing.T) {
	isolation := isolateProviderEnv(t)
	f := newFakeModels(t, providerScript)
	env := func(extra map[string]string) map[string]string {
		out := maps.Clone(isolation)
		maps.Copy(out, extra)
		return out
	}
	run := func(t *testing.T, models, agentExtra string, req Request) []string {
		t.Helper()
		req.Name, req.Prompt = "line", "Say hello"
		req.YAML = []byte(providerCase{models: func(string) string { return models }}.yaml(f.URL, agentExtra))
		recs, _, err := progressRecords(t, req)
		require.NoError(t, err)
		for _, r := range recs {
			assert.NotContains(t, r, providerKey)
			assert.NotContains(t, r, inheritedKey)
		}
		return recs
	}

	t.Run("env", func(t *testing.T) {
		recs := run(t, corpModels(f.URL), "", Request{Env: env(map[string]string{"FAKE_PROVIDER_KEY": providerKey})})
		assert.Equal(t, []string{"models: corp/my-model (FAKE_PROVIDER_KEY from env)"}, recs)
	})
	t.Run("inherited", func(t *testing.T) {
		t.Setenv("FAKE_PROVIDER_KEY", inheritedKey)
		recs := run(t, corpModels(f.URL), "", Request{Env: isolation, InheritEnvironment: true})
		assert.Equal(t, []string{"models: corp/my-model (FAKE_PROVIDER_KEY from the environment of the Pulumi CLI)"}, recs)
	})
	t.Run("hidden names", func(t *testing.T) {
		recs := run(t, corpModels(f.URL), "", Request{Env: env(map[string]string{"FAKE_PROVIDER_KEY": providerKey}),
			HideNames: true})
		assert.Equal(t, []string{"models: a model (FAKE_PROVIDER_KEY from env)"}, recs)
	})
	t.Run("fallback", func(t *testing.T) {
		models := corpModels(f.URL) + "  fb:\n    provider: anthropic\n    model: claude-haiku-4-5\n    base_url: " + f.URL + "\n"
		recs := run(t, models, "    fallback:\n      models: [fb]\n",
			Request{Env: env(map[string]string{"FAKE_PROVIDER_KEY": providerKey, "ANTHROPIC_API_KEY": providerKey})})
		assert.Equal(t, []string{"models: corp/my-model (FAKE_PROVIDER_KEY from env); fallback anthropic/claude-haiku-4-5 " +
			"(ANTHROPIC_API_KEY from env)"}, recs)
	})
	t.Run("first_available", func(t *testing.T) {
		models := fmt.Sprintf("models:\n  m:\n    first_available: [claude, gem]\n  claude:\n    provider: anthropic\n"+
			"    model: claude-haiku-4-5\n    base_url: %[1]s\n    token_key: ANTHROPIC_API_KEY\n  gem:\n    provider: google\n"+
			"    model: gemini-2.5-flash\n    base_url: %[1]s/\n    token_key: GOOGLE_API_KEY\n", f.URL)
		recs := run(t, models, "", Request{Env: env(map[string]string{"GOOGLE_API_KEY": providerKey})})
		assert.Equal(t, []string{"models: first_available: google/gemini-2.5-flash (GOOGLE_API_KEY from env)"}, recs)
	})
	t.Run("SDK fallbacks", func(t *testing.T) {
		// An openai model with a base_url and no token_key: openai-go sends OPENAI_API_KEY of the
		// process environment, which with inheritEnvironment is the Pulumi CLI's.
		t.Setenv("OPENAI_API_KEY", inheritedKey)
		recs := run(t, baseURLModel("openai", "gpt-4o-mini", f.URL+"/v1"), "", Request{Env: isolation,
			InheritEnvironment: true})
		assert.Equal(t, []string{"models: openai/gpt-4o-mini (OPENAI_API_KEY from the environment of the Pulumi CLI)"}, recs)
		recs = run(t, baseURLModel("openai", "gpt-4o-mini", f.URL+"/v1"), "", Request{Env: isolation})
		assert.Equal(t, []string{"models: openai/gpt-4o-mini (no key)"}, recs)

		// anthropic-sdk-go adds ANTHROPIC_AUTH_TOKEN when the process environment has no
		// ANTHROPIC_API_KEY.
		t.Setenv("ANTHROPIC_AUTH_TOKEN", inheritedKey)
		models := "providers:\n  corp:\n    provider: anthropic\n    base_url: " + f.URL + "\n    token_key: CORP_KEY\n" +
			"models:\n  m:\n    provider: corp\n    model: claude-haiku-4-5\n"
		recs = run(t, models, "", Request{Env: env(map[string]string{"CORP_KEY": providerKey}), InheritEnvironment: true})
		assert.Equal(t, []string{"models: corp/claude-haiku-4-5 (CORP_KEY from env, and ANTHROPIC_AUTH_TOKEN from the " +
			"environment of the Pulumi CLI as a bearer token)"}, recs)
	})
}

// TestDefaultModelForAuto: DOCKER_AGENT_DEFAULT_MODEL in env selects model auto's model.
func TestDefaultModelForAuto(t *testing.T) {
	isolation := isolateProviderEnv(t)
	f := newFakeModels(t, providerScript)
	env := maps.Clone(isolation)
	maps.Copy(env, map[string]string{"DOCKER_AGENT_DEFAULT_MODEL": "google/gemini-2.5-flash", "GOOGLE_API_KEY": providerKey,
		"GOOGLE_GEMINI_BASE_URL": f.URL + "/", "ANTHROPIC_API_KEY": providerKey})
	recs, res, err := progressRecords(t, Request{Name: "auto", YAML: []byte(providerCase{ref: "auto"}.yaml(f.URL, "")),
		Prompt: "Say hello", Env: env})
	require.NoError(t, err)
	assert.Equal(t, "Hello from the fake: Say hello", res.Output)
	require.Len(t, f.requests(), 1)
	assert.Equal(t, protoGemini, f.requests()[0].Proto)
	assert.Equal(t, "gemini-2.5-flash", f.requests()[0].Model)
	assert.Equal(t, []string{"models: auto: google/gemini-2.5-flash (GOOGLE_API_KEY from env)"}, recs)
}

// TestMissingCredentialsError: docker-agent's preflight error, with AgentRun's advice (D3).
func TestMissingCredentialsError(t *testing.T) {
	isolation := isolateProviderEnv(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".config", "cagent"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".config", "cagent", ".env"),
		[]byte("MISTRAL_API_KEY="+processOnlyKey+"\n"), 0o600))
	t.Setenv("OPENAI_API_KEY", processOnlyKey)
	doc := "models:\n  m:\n    provider: openai\n    model: gpt-4o-mini\n  n:\n    provider: mistral\n" +
		"    model: devstral-small-2507\n" + agentWith("m") + "    fallback:\n      models: [n]\n"
	_, err := Run(t.Context(), Request{Name: "missing", YAML: []byte(doc), Prompt: "Say hello", Env: isolation})
	require.Error(t, err)
	var mce *MissingCredentialsError
	require.True(t, errors.As(err, &mce), "%v", err)
	assert.Equal(t, []string{"OPENAI_API_KEY"}, mce.Variables, "the preflight checks the agent's models")
	msg := err.Error()
	assert.True(t, strings.HasPrefix(msg, "loading the agent configuration: the configuration needs OPENAI_API_KEY, which "+
		"env does not set (inheritEnvironment is false, so the environment of the Pulumi CLI is not used); set it in env"), msg)
	assert.Contains(t, msg, "OPENAI_API_KEY is set in the environment of the Pulumi CLI, but inheritEnvironment: false keeps "+
		"it from the run (since v0.2.0 also from the model SDKs): pass it in env, or set inheritEnvironment: true")
	assert.NotContains(t, msg, "--env-from-file", "no advice for the docker agent CLI")
	assert.NotContains(t, msg, processOnlyKey)

	// A key of docker agent setup's env file.
	_, err = Run(t.Context(), Request{Name: "missing", YAML: []byte(agentWith("mistral/devstral-small-2507")),
		Prompt: "Say hello", Env: isolation, InheritEnvironment: true})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "MISTRAL_API_KEY is in docker agent's env file ("+
		filepath.Join(home, ".config", "cagent", ".env")+"), which AgentRun does not read: set it in env")
	assert.NotContains(t, err.Error(), processOnlyKey)
}

// TestRejectedCredentials: a model API that rejects the credentials gets the run's credential
// sources, and with inheritEnvironment false the SDK variables that no longer reach the model.
func TestRejectedCredentials(t *testing.T) {
	isolation := isolateProviderEnv(t)
	f := newFakeModels(t, func(wireReq) wireReply { return wireReply{Status: 401, Err: "invalid api key"} })
	env := maps.Clone(isolation)
	env["FAKE_PROVIDER_KEY"] = providerKey
	_, err := Run(t.Context(), Request{Name: "rejected", YAML: []byte(providerCase{models: corpModels}.yaml(f.URL, "")),
		Prompt: "Say hello", Env: env})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "; the run's models: corp/my-model (FAKE_PROVIDER_KEY from env)")

	t.Setenv("OPENAI_ORG_ID", "org-of-the-provider")
	env = maps.Clone(isolation)
	env["MISTRAL_API_KEY"] = providerKey
	_, err = Run(t.Context(), Request{Name: "rejected", Prompt: "Say hello", Env: env,
		YAML: []byte(baseURLModel("mistral", "devstral-small-2507", f.URL+"/v1") + agentWith("m"))})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "OPENAI_ORG_ID is set in the environment of the Pulumi CLI, but with inheritEnvironment: "+
		"false it no longer reaches agents.root.model (mistral/devstral-small-2507) since v0.2.0: its requests no longer "+
		"carry the header it sets; set it in env")
}

// TestAnnouncedModelWarnings: a run whose configuration no preview checked (a `reference`, a
// delete prompt) announces CheckModels' warnings.
func TestAnnouncedModelWarnings(t *testing.T) {
	isolation := isolateProviderEnv(t)
	f := newFakeModels(t, providerScript)
	t.Setenv("OPENAI_ORG_ID", "org-of-the-provider")
	env := maps.Clone(isolation)
	env["MISTRAL_API_KEY"] = providerKey
	var warned []string
	_, err := Run(t.Context(), Request{Name: "announce", Prompt: "Say hello", Env: env, AnnounceHarness: true,
		YAML: []byte(baseURLModel("mistral", "devstral-small-2507", f.URL+"/v1") + agentWith("m")),
		Warn: func(msg string) { warned = append(warned, msg) }})
	require.NoError(t, err)
	require.Len(t, warned, 1)
	assert.Contains(t, warned[0], "OPENAI_ORG_ID is set in the environment of the Pulumi CLI, but with inheritEnvironment: "+
		"false it no longer reaches agents.root.model (mistral/devstral-small-2507) since v0.2.0")
}

// TestUnknownProviderAtRunTime: a provider that is neither docker-agent's nor in the configuration
// (a `reference`, which no preview validated).
func TestUnknownProviderAtRunTime(t *testing.T) {
	isolateProviderEnv(t)
	_, err := Run(t.Context(), Request{Name: "unknown", YAML: []byte(agentWith("myendpoint/llama")), Prompt: "Say hello"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), UserProvidersHint)
}

// TestStructuredOutputFence: every agent's JSON answer may come in one Markdown code fence, and a
// native-mode answer that is not JSON names structured_output mode: tool (D4).
func TestStructuredOutputFence(t *testing.T) {
	isolation := isolateProviderEnv(t)
	answer := ""
	f := newFakeModels(t, func(wireReq) wireReply { return wireReply{Text: answer} })
	env := maps.Clone(isolation)
	env["FAKE_PROVIDER_KEY"] = providerKey
	req := Request{Name: "fence", YAML: []byte(providerCase{models: corpModels}.yaml(f.URL, reviewSchema)),
		Prompt: "Review", Env: env}

	answer = "```json\n{\"risk\":\"low\",\"summary\":\"fenced\"}\n```"
	res, err := Run(t.Context(), req)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"risk": "low", "summary": "fenced"}, res.StructuredOutput)
	assert.Equal(t, answer, res.Output, "output stays as the agent wrote it")

	answer = "The risk is low."
	_, err = Run(t.Context(), req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "agent \"root\" declares structured_output but its answer is not valid JSON")
	assert.True(t, strings.HasSuffix(err.Error(), "; set structured_output.mode: tool to have docker-agent enforce the schema"),
		err.Error())
}

// TestCredentialSources covers the sources the run tests do not reach.
func TestCredentialSources(t *testing.T) {
	v := envView{env: map[string]string{"AWS_PROFILE": "dev", "GOOGLE_APPLICATION_CREDENTIALS": "/sa.json"},
		provider: map[string]string{"GH_TOKEN": "x"}, inherit: true}
	for _, tc := range []struct {
		use  modelUse
		want string
	}{
		{modelUse{typ: "dmr", cfg: cfgOf("dmr")}, "no key: Docker Model Runner"},
		{modelUse{typ: "amazon-bedrock", cfg: cfgOf("amazon-bedrock")}, "the AWS profile of AWS_PROFILE from env"},
		{modelUse{typ: "google", cfg: cfgOf("google", "project", "p")},
			"Google application default credentials (GOOGLE_APPLICATION_CREDENTIALS from env)"},
		{modelUse{typ: "openai", cfg: withToken(cfgOf("github-copilot"), "GITHUB_TOKEN")},
			"GH_TOKEN from the environment of the Pulumi CLI"},
		{modelUse{typ: "openai_chatcompletions", custom: true, cfg: cfgOf("corp")},
			"no key (a custom provider without token_key)"},
		{modelUse{typ: "openai", cfg: cfgOf("chatgpt")}, "the ChatGPT login of docker agent setup, billed to the ChatGPT plan"},
		{modelUse{typ: "anthropic", cfg: cfgOf("anthropic")}, "ANTHROPIC_API_KEY (not set)"},
	} {
		assert.Equal(t, tc.want, credentialSource(tc.use, v), tc.use.cfg.Provider)
	}
	empty := envView{env: map[string]string{}}
	assert.Equal(t, "this machine's AWS default credential chain (shared files, SSO, container or instance metadata)",
		credentialSource(modelUse{typ: "amazon-bedrock", cfg: cfgOf("amazon-bedrock")}, empty))
	assert.Equal(t, "this machine's Google application default credentials (the gcloud login, or the metadata server)",
		credentialSource(modelUse{typ: "google", cfg: cfgOf("google", "location", "us")}, empty))
}

func cfgOf(provider string, opts ...string) latest.ModelConfig {
	m := latest.ModelConfig{Provider: provider, Model: "x"}
	for i := 0; i+1 < len(opts); i += 2 {
		if m.ProviderOpts == nil {
			m.ProviderOpts = map[string]any{}
		}
		m.ProviderOpts[opts[i]] = opts[i+1]
	}
	return m
}

func withToken(m latest.ModelConfig, key string) latest.ModelConfig {
	m.TokenKey = key
	return m
}

// TestDescribeAutoError: a model auto that could not be built says what auto chose; only a
// fallback to Docker Model Runner says that no credential was found (phase-2 review #5).
func TestDescribeAutoError(t *testing.T) {
	isolateProviderEnv(t)
	cause := &config.AutoModelFallbackError{Cause: errors.New("failed to load AWS config: profile nope\nmore")}
	describe := func(env map[string]string, hide bool) string {
		req := Request{Env: env, HideNames: hide}
		err := describeAutoError(t.Context(), cause, req, nil, envProvider(req))
		require.Error(t, err)
		return err.Error()
	}
	assert.Equal(t, "loading the agent configuration: model auto resolved to anthropic/claude-haiku-4-5 (selected by "+
		"DOCKER_AGENT_DEFAULT_MODEL from env), which failed: failed to load AWS config: profile nope",
		describe(map[string]string{"DOCKER_AGENT_DEFAULT_MODEL": "anthropic/claude-haiku-4-5"}, false))
	assert.Equal(t, "loading the agent configuration: model auto resolved to google/gemini-3.8-flash (this machine's "+
		"Google application default credentials (the gcloud login, or the metadata server)), which failed: failed to load "+
		"AWS config: profile nope", describe(map[string]string{"GOOGLE_GENAI_USE_VERTEXAI": "true"}, false))
	assert.Equal(t, "loading the agent configuration: model auto resolved to a model (MISTRAL_API_KEY from env), which "+
		"failed: failed to load AWS config: profile nope", describe(map[string]string{"MISTRAL_API_KEY": providerKey}, true))
	assert.Equal(t, "loading the agent configuration: model auto found no provider credential in env (it looks for "+
		"ANTHROPIC_API_KEY, OPENAI_API_KEY, GITHUB_TOKEN, GOOGLE_API_KEY and the other providers' keys) and fell back to "+
		"Docker Model Runner, which failed: failed to load AWS config: profile nope; set a provider's key in env, set "+
		"DOCKER_AGENT_DEFAULT_MODEL (provider/model) in env, or name the model in the configuration", describe(nil, false))
	assert.NoError(t, describeAutoError(t.Context(), errors.New("other"), Request{}, nil, envProvider(Request{})))
}

// TestMissingAlternatives: "X or Y environment variable is required" needs one of them
// (phase-2 review #12).
func TestMissingAlternatives(t *testing.T) {
	err := describeMissingCredentials(errors.New("creating model: GITHUB_TOKEN or GH_TOKEN environment variable is required"),
		false)
	var mce *MissingCredentialsError
	require.ErrorAs(t, err, &mce)
	assert.Equal(t, []string{"GITHUB_TOKEN", "GH_TOKEN"}, mce.Variables)
	assert.Equal(t, "loading the agent configuration: the configuration needs GITHUB_TOKEN or GH_TOKEN, which env does not "+
		"set (inheritEnvironment is false, so the environment of the Pulumi CLI is not used); set one of them in env (for "+
		"example from a secret config value)", err.Error())
	err = describeMissingCredentials(errors.New("OPENAI_API_KEY environment variable is required"), true)
	assert.Contains(t, err.Error(), "needs OPENAI_API_KEY, which neither env nor the environment of the Pulumi CLI sets; "+
		"set it in env")
}

// TestEnvViewPresence: an empty variable of the run's environment is found, as docker-agent's
// providers find it, and hides the sources after it (phase-2 review #12).
func TestEnvViewPresence(t *testing.T) {
	v := envView{env: map[string]string{"CHATGPT_OAUTH_TOKEN": ""}, provider: map[string]string{"EMPTY": "", "SET": "x"},
		inherit: true}
	val, found := v.asProvider().Get(t.Context(), "CHATGPT_OAUTH_TOKEN")
	assert.True(t, found)
	assert.Empty(t, val)
	assert.False(t, v.set("CHATGPT_OAUTH_TOKEN"))
	assert.True(t, v.present("CHATGPT_OAUTH_TOKEN"))
	_, found = v.asProvider().Get(t.Context(), "EMPTY")
	assert.True(t, found, "an inherited empty variable is found too")
	v.inherit = false
	_, found = v.asProvider().Get(t.Context(), "SET")
	assert.False(t, found)
}
