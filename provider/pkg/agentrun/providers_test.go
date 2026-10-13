// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package agentrun

// Provider coverage of AgentRun's runs (not coding harnesses): every model provider docker-agent
// v1.150.0 implements, pointed at the multi-protocol fake of providers_fake_test.go through the
// endpoint override docker-agent offers for it (a model's or custom provider's base_url,
// Bedrock's provider_opts.endpoint_url, or the SDK's own base URL variable in `env`), with:
//
//   - a plain answer, a tool-call round trip (the filesystem toolset's read-only read_file, which
//     toolApproval deny runs), and structured_output;
//   - AgentRun's outputs: output, toolCalls, usage tokens and cost (checked against the
//     models.dev catalog embedded in docker-agent);
//   - the preview checks (docker-agent's validation and CheckConfig);
//   - credentials from `env` without inheritEnvironment, from the inherited environment, and
//     that the process environment is not used without inheritEnvironment;
//   - the boundary of the run child's environment (childenv.go): the variables the SDKs read
//     themselves come from `env` and, only with inheritEnvironment, from the provider's
//     environment, as with `docker agent run` in that environment.

import (
	"cmp"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	modelprovider "github.com/docker/docker-agent/pkg/model/provider"
	"github.com/docker/docker-agent/pkg/modelsdev"
	"github.com/docker/docker-agent/pkg/tools/builtin/structuredoutput"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dirien/pulumi-docker-agent/provider/pkg/agentrun/fakeopenai"
	"github.com/dirien/pulumi-docker-agent/provider/pkg/validate"
)

const (
	providerKey     = "fake-provider-credential-0123456789"
	inheritedKey    = "fake-inherited-credential-987654321"
	processOnlyKey  = "fake-process-only-credential-55555"
	googleToken     = "ya29.fake-google-access-token"
	testInstruction = "You are a provider test agent."
)

// providerCase is a configuration whose model reaches the fake.
type providerCase struct {
	name  string
	proto wireProto
	// models returns the `providers:` and `models:` sections for the fake's URL. The agent uses
	// model ref (default "m").
	models func(url string) string
	ref    string
	// credVar holds the credential ("" when the endpoint needs none).
	credVar string
	// extraEnv are other variables of `env` (not credentials).
	extraEnv map[string]string
	// sdkEnv are variables an SDK reads from the process environment itself (an endpoint, ADC).
	// The runs pass them in `env`, which the run child sets in its process environment; with
	// inheritEnvironment they work from the provider's environment too.
	sdkEnv func(t *testing.T, url string) map[string]string
	// wireModel is the model the request must name.
	wireModel string
	// wantCred is the credential the fake must see when credVar is empty.
	wantCred string
	// pathContains must be part of the request path.
	pathContains string
	// costID is the models.dev entry that prices the run; empty means unpriced.
	costID modelsdev.ID
	// costPerM is a `cost:` override of the configuration (USD per 1M input and output tokens).
	costPerM *[2]float64
	// structured is "native" when the protocol carries the schema, "tool" when the case uses
	// structured_output mode: tool.
	structured string
	// check runs extra checks on the first request.
	check func(t *testing.T, r wireReq)
	// tls serves the fake over HTTPS.
	tls bool
}

func (c providerCase) yaml(url, agentExtra string) string {
	models := ""
	if c.models != nil {
		models = c.models(url)
	}
	return models + fmt.Sprintf("agents:\n  root:\n    model: %s\n    description: Provider test agent\n    instruction: %s\n%s",
		cmp.Or(c.ref, "m"), testInstruction, agentExtra)
}

func (c providerCase) env() map[string]string {
	env := map[string]string{}
	if c.credVar != "" {
		env[c.credVar] = providerKey
	}
	maps.Copy(env, c.extraEnv)
	return env
}

func (c providerCase) expectedCred() string {
	if c.credVar != "" {
		return providerKey
	}
	return c.wantCred
}

// processEnvVars are the variables docker-agent and its SDKs read for model endpoints and
// credentials. The tests start without them so that the machine running them does not matter.
var processEnvVars = []string{
	"OPENAI_API_KEY", "OPENAI_BASE_URL", "OPENAI_ORG_ID", "OPENAI_PROJECT_ID", "OPENAI_CUSTOM_HEADERS",
	"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_BASE_URL", "ANTHROPIC_PROFILE", "ANTHROPIC_CUSTOM_HEADERS",
	"ANTHROPIC_FEDERATION_RULE_ID", "ANTHROPIC_ORGANIZATION_ID", "ANTHROPIC_IDENTITY_TOKEN_FILE", "ANTHROPIC_IDENTITY_TOKEN",
	"GOOGLE_API_KEY", "GEMINI_API_KEY", "GOOGLE_GENAI_USE_VERTEXAI", "GOOGLE_GENAI_USE_ENTERPRISE", "GOOGLE_CLOUD_PROJECT",
	"GOOGLE_CLOUD_LOCATION", "GOOGLE_CLOUD_REGION", "GOOGLE_GEMINI_BASE_URL", "GOOGLE_VERTEX_BASE_URL",
	"GOOGLE_APPLICATION_CREDENTIALS", "CLOUDSDK_CONFIG",
	"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN", "AWS_PROFILE", "AWS_DEFAULT_PROFILE", "AWS_REGION",
	"AWS_DEFAULT_REGION", "AWS_BEARER_TOKEN_BEDROCK", "AWS_ROLE_ARN", "AWS_WEB_IDENTITY_TOKEN_FILE", "AWS_ENDPOINT_URL",
	"AWS_ENDPOINT_URL_BEDROCK_RUNTIME", "AWS_CONTAINER_CREDENTIALS_FULL_URI", "AWS_CONTAINER_CREDENTIALS_RELATIVE_URI",
	"MODEL_RUNNER_HOST", "CHATGPT_OAUTH_TOKEN", "GITHUB_TOKEN", "GH_TOKEN", "CLOUDFLARE_ACCOUNT_ID", "CLOUDFLARE_GATEWAY_ID",
	"FAKE_PROVIDER_KEY", "CORP_KEY",
}

// isolateProviderEnv unsets the provider variables of the test process (restored afterwards) and
// keeps the AWS SDK away from the user's files and the instance metadata service, and Google's
// SDK away from gcloud's files. It returns those settings for `env`: a run without
// inheritEnvironment does not get them from the test process.
func isolateProviderEnv(t *testing.T) map[string]string {
	t.Helper()
	keys := slices.Clone(processEnvVars)
	for _, a := range modelprovider.Aliases {
		if a.TokenEnvVar != "" {
			keys = append(keys, a.TokenEnvVar)
		}
	}
	for _, k := range keys {
		t.Setenv(k, "")
		require.NoError(t, os.Unsetenv(k))
	}
	dir := t.TempDir()
	isolation := map[string]string{
		"AWS_CONFIG_FILE":             filepath.Join(dir, "aws-config"),
		"AWS_SHARED_CREDENTIALS_FILE": filepath.Join(dir, "aws-credentials"),
		"AWS_EC2_METADATA_DISABLED":   "true",
		"CLOUDSDK_CONFIG":             filepath.Join(dir, "gcloud"),
	}
	for k, v := range isolation {
		t.Setenv(k, v)
	}
	return isolation
}

// providerScript answers the scenarios: structured output when the request carries a schema or
// the structured output tool, a read_file call when the filesystem tools are offered (and the
// answer with its result afterwards), else a greeting.
func providerScript(r wireReq) wireReply {
	results := r.toolResults()
	switch {
	case r.Schema != nil:
		b, _ := json.Marshal(fakeopenai.Sample(r.Schema))
		return wireReply{Text: string(b)}
	case r.hasTool(structuredoutput.ToolName) && len(results) == 0:
		return wireReply{Calls: []wireCall{{Name: structuredoutput.ToolName, Args: `{"risk":"high","summary":"tool mode"}`}}}
	case r.hasTool("read_file") && len(results) == 0:
		return wireReply{Calls: []wireCall{{Name: "read_file", Args: `{"path":"marker.txt"}`}}}
	case len(results) > 0:
		return wireReply{Text: "file says: " + r.toolResultText()}
	}
	return wireReply{Text: "Hello from the fake: " + r.lastUser()}
}

// expectedCost prices calls requests of 12 input and 3 output tokens like docker-agent does.
func expectedCost(t *testing.T, c providerCase, calls int) float64 {
	t.Helper()
	const in, out = 12, 3
	if c.costPerM != nil {
		return float64(calls) * (in*c.costPerM[0] + out*c.costPerM[1]) / 1e6
	}
	if c.costID.Provider == "" {
		return 0
	}
	m, err := modelsdev.NewDatabaseStore(modelsdev.EmbeddedSnapshot()).GetModel(context.Background(), c.costID)
	require.NoError(t, err, "test setup: %s is not in the embedded models.dev catalog", c.costID)
	require.NotNil(t, m.Cost, "test setup: %s has no pricing", c.costID)
	rates := m.Cost.RatesFor(in)
	return float64(calls) * (in*rates.Input + out*rates.Output) / 1e6
}

const fsToolset = "    toolsets:\n      - type: filesystem\n"

func structuredBlock(mode string) string {
	if mode == "tool" {
		return strings.Replace(reviewSchema, "      name: review\n", "      name: review\n      mode: tool\n", 1)
	}
	return reviewSchema
}

func baseURLModel(provider, model, baseURL string, extra ...string) string {
	s := fmt.Sprintf("models:\n  m:\n    provider: %s\n    model: %s\n    base_url: %s\n", provider, model, baseURL)
	for _, e := range extra {
		s += "    " + e + "\n"
	}
	return s
}

// aliasModels are the built-in OpenAI-compatible aliases with a model of the catalog.
var aliasModels = []struct{ alias, model string }{
	{"requesty", "qwen3.5-2b"},
	{"azure", "phi-4-mini"},
	{"xai", "grok-4.20-0309-non-reasoning"},
	{"nebius", "Qwen/Qwen3-30B-A3B-Instruct-2507"},
	{"nvidia", "deepseek-ai/deepseek-v4-flash"},
	{"openrouter", "openai/gpt-oss-20b"},
	{"mistral", "devstral-small-2507"},
	{"ollama", "llama3.2"},
	{"minimax", "MiniMax-M2.5"},
	{"baseten", "deepseek-ai/DeepSeek-V4-Flash-0731"},
	{"ovhcloud", "gpt-oss-20b"},
	{"daoxe", "kimi-k2.5"},
	{"groq", "llama-3.1-8b-instant"},
	{"fireworks-ai", "accounts/fireworks/models/gpt-oss-120b"},
	{"deepseek", "deepseek-v4-flash"},
	{"cerebras", "gpt-oss-120b"},
	{"togetherai", "openai/gpt-oss-120b"},
	{"huggingface", "google/gemma-3-12b-it"},
	{"moonshotai", "kimi-k2.6"},
	{"vercel", "openai/gpt-oss-20b"},
	{"cloudflare-workers-ai", "@cf/qwen/qwen3-30b-a3b-fp8"},
	{"cloudflare-ai-gateway", "openai/gpt-4o-mini"},
	{"github-copilot", "mai-code-1.1-flash"},
	{"opencode-go", "deepseek-v4-flash"},
	{"opencode", "deepseek-v4-flash"},
}

func providerCases() []providerCase {
	v1 := func(url string) string { return url + "/v1" }
	cases := []providerCase{
		// --- OpenAI Chat Completions ------------------------------------------------------------
		{
			name: "openai/chat/base_url", proto: protoChat, credVar: "OPENAI_API_KEY", wireModel: "gpt-4o-mini",
			models:       func(u string) string { return baseURLModel("openai", "gpt-4o-mini", v1(u)) },
			pathContains: "/v1/chat/completions", costID: modelsdev.NewID("openai", "gpt-4o-mini"), structured: "native",
		},
		{
			name: "openai/chat/OPENAI_BASE_URL", proto: protoChat, credVar: "OPENAI_API_KEY", wireModel: "gpt-4o-mini",
			models: func(string) string { return "models:\n  m:\n    provider: openai\n    model: gpt-4o-mini\n" },
			sdkEnv: func(_ *testing.T, u string) map[string]string { return map[string]string{"OPENAI_BASE_URL": v1(u)} },
			costID: modelsdev.NewID("openai", "gpt-4o-mini"), structured: "native",
		},
		{
			name: "openai/chat/inline-ref", proto: protoChat, credVar: "OPENAI_API_KEY", wireModel: "gpt-4o-mini",
			ref:    "openai/gpt-4o-mini",
			sdkEnv: func(_ *testing.T, u string) map[string]string { return map[string]string{"OPENAI_BASE_URL": v1(u)} },
			costID: modelsdev.NewID("openai", "gpt-4o-mini"), structured: "native",
		},
		{
			name: "custom/openai_chatcompletions", proto: protoChat, credVar: "FAKE_PROVIDER_KEY", wireModel: "my-model",
			models: func(u string) string {
				return "providers:\n  corp:\n    api_type: openai_chatcompletions\n    base_url: " + v1(u) +
					"\n    token_key: FAKE_PROVIDER_KEY\nmodels:\n  m:\n    provider: corp\n    model: my-model\n"
			},
			structured: "native",
		},
		{
			name: "custom/openai_chatcompletions+cost", proto: protoChat, credVar: "FAKE_PROVIDER_KEY", wireModel: "my-model",
			models: func(u string) string {
				return "providers:\n  corp:\n    api_type: openai_chatcompletions\n    base_url: " + v1(u) +
					"\n    token_key: FAKE_PROVIDER_KEY\nmodels:\n  m:\n    provider: corp\n    model: my-model\n" +
					"    cost:\n      input: 1.5\n      output: 6\n"
			},
			costPerM: &[2]float64{1.5, 6}, structured: "native",
		},
		{
			name: "custom/no-api_type/chat", proto: protoChat, credVar: "FAKE_PROVIDER_KEY", wireModel: "llama-3.3-70b",
			models: func(u string) string {
				return "providers:\n  corp:\n    base_url: " + v1(u) + "\n    token_key: FAKE_PROVIDER_KEY\n" +
					"models:\n  m:\n    provider: corp\n    model: llama-3.3-70b\n"
			},
			structured: "native",
		},
		{
			name: "custom/provider:mistral", proto: protoChat, credVar: "CORP_KEY", wireModel: "devstral-small-2507",
			models: func(u string) string {
				return "providers:\n  corp:\n    provider: mistral\n    base_url: " + v1(u) + "\n    token_key: CORP_KEY\n" +
					"models:\n  m:\n    provider: corp\n    model: devstral-small-2507\n"
			},
			costID: modelsdev.NewID("mistral", "devstral-small-2507"), structured: "native",
		},
		{
			name: "dmr/base_url", proto: protoChat, wireModel: "ai/qwen3", pathContains: "/v1/chat/completions",
			models: func(u string) string { return baseURLModel("dmr", "ai/qwen3", v1(u)) }, structured: "native",
		},
		{
			name: "dmr/MODEL_RUNNER_HOST", proto: protoChat, wireModel: "ai/qwen3", pathContains: "/engines/v1/chat/completions",
			models:     func(string) string { return "models:\n  m:\n    provider: dmr\n    model: ai/qwen3\n" },
			sdkEnv:     func(_ *testing.T, u string) map[string]string { return map[string]string{"MODEL_RUNNER_HOST": u} },
			structured: "native",
		},
		{
			name: "azure/api_version", proto: protoChat, credVar: "AZURE_API_KEY", wireModel: "phi-4-mini",
			models: func(u string) string {
				return baseURLModel("azure", "phi-4-mini", v1(u), "provider_opts:", "  api_version: \"2024-10-21\"")
			},
			costID: modelsdev.NewID("azure", "phi-4-mini"), structured: "native",
			check: func(t *testing.T, r wireReq) { assert.Equal(t, "2024-10-21", r.Query.Get("api-version")) },
		},
		{
			name: "cloudflare-workers-ai/templated-base_url", proto: protoChat, credVar: "CLOUDFLARE_API_TOKEN",
			wireModel: "@cf/qwen/qwen3-30b-a3b-fp8", extraEnv: map[string]string{"CLOUDFLARE_ACCOUNT_ID": "acct-1234"},
			models: func(u string) string {
				return baseURLModel("cloudflare-workers-ai", "\"@cf/qwen/qwen3-30b-a3b-fp8\"",
					u+"/client/v4/accounts/${CLOUDFLARE_ACCOUNT_ID}/ai/v1")
			},
			pathContains: "/accounts/acct-1234/ai/v1/chat/completions",
			costID:       modelsdev.NewID("cloudflare-workers-ai", "@cf/qwen/qwen3-30b-a3b-fp8"), structured: "native",
		},

		// --- OpenAI Responses ---------------------------------------------------------------------
		{
			name: "openai/responses/base_url", proto: protoResponses, credVar: "OPENAI_API_KEY", wireModel: "gpt-5-mini",
			models:       func(u string) string { return baseURLModel("openai", "gpt-5-mini", v1(u)) },
			pathContains: "/v1/responses", costID: modelsdev.NewID("openai", "gpt-5-mini"), structured: "native",
		},
		{
			name: "openai/responses/OPENAI_BASE_URL", proto: protoResponses, credVar: "OPENAI_API_KEY", wireModel: "gpt-5-mini",
			models: func(string) string { return "models:\n  m:\n    provider: openai\n    model: gpt-5-mini\n" },
			sdkEnv: func(_ *testing.T, u string) map[string]string { return map[string]string{"OPENAI_BASE_URL": v1(u)} },
			costID: modelsdev.NewID("openai", "gpt-5-mini"), structured: "native",
		},
		{
			name: "custom/openai_responses", proto: protoResponses, credVar: "FAKE_PROVIDER_KEY", wireModel: "my-model",
			models: func(u string) string {
				return "providers:\n  corp:\n    api_type: openai_responses\n    base_url: " + v1(u) +
					"\n    token_key: FAKE_PROVIDER_KEY\nmodels:\n  m:\n    provider: corp\n    model: my-model\n"
			},
			structured: "native",
		},
		{
			name: "custom/no-api_type/gpt-5", proto: protoResponses, credVar: "FAKE_PROVIDER_KEY", wireModel: "gpt-5-mini",
			models: func(u string) string {
				return "providers:\n  corp:\n    base_url: " + v1(u) + "\n    token_key: FAKE_PROVIDER_KEY\n" +
					"models:\n  m:\n    provider: corp\n    model: gpt-5-mini\n"
			},
			structured: "native",
		},
		{
			name: "chatgpt/base_url", proto: protoResponses, credVar: "CHATGPT_OAUTH_TOKEN", wireModel: "gpt-5.5",
			models:       func(u string) string { return baseURLModel("chatgpt", "gpt-5.5", v1(u)) },
			pathContains: "/v1/responses", structured: "native",
			check: func(t *testing.T, r wireReq) {
				assert.Equal(t, "codex_cli_rs", r.Header.Get("Originator"), "Codex originator header")
				assert.Equal(t, "responses=experimental", r.Header.Get("Openai-Beta"))
				assert.Equal(t, false, r.Body["store"], "the ChatGPT backend requires store: false")
				assert.Contains(t, str(r.Body["instructions"]), testInstruction, "system messages move to instructions")
			},
		},
		{
			name: "github-copilot/responses", proto: protoResponses, credVar: "GITHUB_TOKEN", wireModel: "gpt-6-luna",
			models: func(u string) string { return baseURLModel("github-copilot", "gpt-6-luna", v1(u)) },
			costID: modelsdev.NewID("github-copilot", "gpt-6-luna"), structured: "native",
			check: func(t *testing.T, r wireReq) {
				assert.Equal(t, "copilot-developer-cli", r.Header.Get("Copilot-Integration-Id"))
			},
		},
		{
			name: "opencode/responses", proto: protoResponses, credVar: "OPENCODE_API_KEY", wireModel: "gpt-6-luna",
			models: func(u string) string { return baseURLModel("opencode", "gpt-6-luna", v1(u)) },
			costID: modelsdev.NewID("opencode", "gpt-6-luna"), structured: "native",
		},

		// --- Anthropic Messages -------------------------------------------------------------------
		{
			name: "anthropic/base_url", proto: protoAnthropic, credVar: "ANTHROPIC_API_KEY", wireModel: "claude-haiku-4-5",
			models:       func(u string) string { return baseURLModel("anthropic", "claude-haiku-4-5", u) },
			pathContains: "/v1/messages", costID: modelsdev.NewID("anthropic", "claude-haiku-4-5"), structured: "native",
			check: func(t *testing.T, r wireReq) { assert.NotEmpty(t, r.Header.Get("Anthropic-Version")) },
		},
		{
			// No base_url in the configuration, so docker-agent resolves the models.dev alias
			// claude-haiku-4-5 to its pinned version; the SDK reads ANTHROPIC_BASE_URL.
			name: "anthropic/ANTHROPIC_BASE_URL+alias", proto: protoAnthropic, credVar: "ANTHROPIC_API_KEY",
			wireModel: "claude-haiku-4-5-20251001",
			models:    func(string) string { return "models:\n  m:\n    provider: anthropic\n    model: claude-haiku-4-5\n" },
			sdkEnv:    func(_ *testing.T, u string) map[string]string { return map[string]string{"ANTHROPIC_BASE_URL": u} },
			costID:    modelsdev.NewID("anthropic", "claude-haiku-4-5"), structured: "native",
		},
		{
			name: "custom/provider:anthropic", proto: protoAnthropic, credVar: "CORP_KEY", wireModel: "claude-haiku-4-5",
			models: func(u string) string {
				return "providers:\n  corp:\n    provider: anthropic\n    base_url: " + u + "\n    token_key: CORP_KEY\n" +
					"models:\n  m:\n    provider: corp\n    model: claude-haiku-4-5\n"
			},
			costID: modelsdev.NewID("anthropic", "claude-haiku-4-5"), structured: "native",
		},

		// --- Google Gemini and Vertex AI ----------------------------------------------------------
		{
			name: "google/gemini/GOOGLE_API_KEY", proto: protoGemini, credVar: "GOOGLE_API_KEY", wireModel: "gemini-2.5-flash",
			models:       func(u string) string { return baseURLModel("google", "gemini-2.5-flash", u+"/") },
			pathContains: "/v1beta/models/gemini-2.5-flash:streamGenerateContent",
			costID:       modelsdev.NewID("google", "gemini-2.5-flash"), structured: "native",
		},
		{
			name: "google/gemini/GEMINI_API_KEY", proto: protoGemini, credVar: "GEMINI_API_KEY", wireModel: "gemini-2.5-flash",
			models: func(u string) string { return baseURLModel("google", "gemini-2.5-flash", u+"/") },
			costID: modelsdev.NewID("google", "gemini-2.5-flash"), structured: "native",
		},
		{
			name: "custom/provider:google", proto: protoGemini, credVar: "CORP_KEY", wireModel: "gemini-2.5-flash",
			models: func(u string) string {
				return "providers:\n  corp:\n    provider: google\n    base_url: " + u + "/\n    token_key: CORP_KEY\n" +
					"models:\n  m:\n    provider: corp\n    model: gemini-2.5-flash\n"
			},
			costID: modelsdev.NewID("google", "gemini-2.5-flash"), structured: "native",
		},
		{
			// Vertex AI behind a proxy: GOOGLE_GENAI_USE_VERTEXAI and a base_url, no project, no
			// credentials (the genai SDK skips Application Default Credentials then).
			name: "google/vertex/proxy", proto: protoGemini, wireModel: "gemini-2.5-flash",
			extraEnv:     map[string]string{"GOOGLE_GENAI_USE_VERTEXAI": "true"},
			models:       func(u string) string { return baseURLModel("google", "gemini-2.5-flash", u+"/") },
			pathContains: "/v1beta1/", costID: modelsdev.NewID("google", "gemini-2.5-flash"), structured: "native",
		},
		{
			// Vertex AI with a project and Application Default Credentials: a service account whose
			// token_uri is the fake. ADC is found through GOOGLE_APPLICATION_CREDENTIALS of the
			// provider process (see TestProvidersProcessEnvironmentBoundary for `env`).
			name: "google/vertex/project+ADC", proto: protoGemini, wireModel: "gemini-2.5-flash", wantCred: googleToken,
			models: func(u string) string {
				return baseURLModel("google", "gemini-2.5-flash", u+"/", "provider_opts:", "  project: fake-project",
					"  location: us-central1")
			},
			sdkEnv: func(t *testing.T, u string) map[string]string {
				return map[string]string{"GOOGLE_APPLICATION_CREDENTIALS": serviceAccountFile(t, u+"/token")}
			},
			pathContains: "/v1beta1/projects/fake-project/locations/us-central1/publishers/google/models/gemini-2.5-flash",
			costID:       modelsdev.NewID("google", "gemini-2.5-flash"), structured: "native",
		},

		// --- Amazon Bedrock -----------------------------------------------------------------------
		{
			name: "amazon-bedrock/bearer", proto: protoBedrock, credVar: "AWS_BEARER_TOKEN_BEDROCK",
			wireModel: "amazon.nova-micro-v1:0",
			models: func(u string) string {
				return "models:\n  m:\n    provider: amazon-bedrock\n    model: amazon.nova-micro-v1:0\n" +
					"    provider_opts:\n      endpoint_url: " + u + "\n      region: us-east-1\n"
			},
			pathContains: "/converse-stream", costID: modelsdev.NewID("amazon-bedrock", "amazon.nova-micro-v1:0"),
			structured: "tool", tls: true,
		},
		{
			name: "custom/provider:amazon-bedrock", proto: protoBedrock, credVar: "CORP_KEY",
			wireModel: "amazon.nova-micro-v1:0",
			models: func(u string) string {
				return "providers:\n  corp:\n    provider: amazon-bedrock\n    token_key: CORP_KEY\n    provider_opts:\n" +
					"      endpoint_url: " + u + "\n      region: eu-west-1\n" +
					"models:\n  m:\n    provider: corp\n    model: amazon.nova-micro-v1:0\n"
			},
			costID: modelsdev.NewID("amazon-bedrock", "amazon.nova-micro-v1:0"), structured: "tool", tls: true,
		},
	}

	// Every built-in OpenAI-compatible alias, through a model's base_url.
	for _, a := range aliasModels {
		alias := modelprovider.Aliases[a.alias]
		c := providerCase{
			name: "alias/" + a.alias, proto: protoChat, credVar: alias.TokenEnvVar, wireModel: a.model,
			models: func(u string) string {
				return baseURLModel(a.alias, fmt.Sprintf("%q", a.model), v1(u))
			},
			pathContains: "/v1/chat/completions", structured: "native",
		}
		if a.alias != "ollama" {
			c.costID = modelsdev.NewID(a.alias, a.model)
		}
		if a.alias == "github-copilot" {
			c.check = func(t *testing.T, r wireReq) {
				assert.Equal(t, "copilot-developer-cli", r.Header.Get("Copilot-Integration-Id"))
			}
		}
		cases = append(cases, c)
	}
	return cases
}

// serviceAccountFile writes a Google service account key whose token endpoint is tokenURI.
func serviceAccountFile(t *testing.T, tokenURI string) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	der, err := x509.MarshalPKCS8PrivateKey(key)
	require.NoError(t, err)
	doc, err := json.Marshal(map[string]string{
		"type": "service_account", "project_id": "fake-project", "private_key_id": "fake-key-id",
		"private_key":  string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})),
		"client_email": "agentrun@fake-project.iam.gserviceaccount.com", "client_id": "1", "token_uri": tokenURI,
	})
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "sa.json")
	require.NoError(t, os.WriteFile(path, doc, 0o600))
	return path
}

// setupCase starts a fake for c and returns it with the base request: c's `env`, its SDK
// variables, and the isolation settings, without inheritEnvironment.
func setupCase(t *testing.T, c providerCase, agentExtra, prompt string) (*fakeModels, Request) {
	t.Helper()
	isolation := isolateProviderEnv(t)
	f := newFakeModels(t, providerScript)
	if c.tls {
		f = newFakeModelsTLS(t, providerScript)
	}
	env := c.env()
	maps.Copy(env, c.sdkVars(t, f.URL))
	maps.Copy(env, isolation)
	return f, Request{
		Name: "providers", YAML: []byte(c.yaml(f.URL, agentExtra)), Prompt: prompt, Env: env,
		InheritEnvironment: false,
	}
}

// sdkVars returns c's SDK variables for the fake at url.
func (c providerCase) sdkVars(t *testing.T, url string) map[string]string {
	if c.sdkEnv == nil {
		return map[string]string{}
	}
	return c.sdkEnv(t, url)
}

// withoutKeys returns a copy of env without the keys.
func withoutKeys(env map[string]string, keys ...string) map[string]string {
	out := maps.Clone(env)
	for _, k := range keys {
		delete(out, k)
	}
	return out
}

// checkRequest checks what the fake received for c.
func checkRequest(t *testing.T, c providerCase, f *fakeModels, want int, cred string) []wireReq {
	t.Helper()
	reqs := f.requests()
	require.Len(t, reqs, want, "model requests (other requests: %v)", f.other())
	for _, r := range reqs {
		assert.Equal(t, c.proto, r.Proto, "wire protocol")
		assert.Equal(t, c.wireModel, r.Model, "model on the wire")
		assert.True(t, r.Stream, "docker-agent streams")
		assert.Equal(t, cred, r.credential(), "credential presented")
		if r.Proto == protoGemini {
			// docker-agent's Gemini client sends system messages as user contents
			// (pkg/model/provider/gemini/client.go:414 messageRoleToGemini), not systemInstruction.
			require.NotEmpty(t, r.Messages)
			assert.Equal(t, "user", r.Messages[0].Role)
			assert.Contains(t, r.Messages[0].Text, testInstruction, "the agent's instruction reaches the model")
		} else {
			assert.Contains(t, r.System, testInstruction, "the agent's instruction reaches the model")
		}
		if c.pathContains != "" {
			assert.Contains(t, r.Path, c.pathContains)
		}
	}
	if c.check != nil {
		c.check(t, reqs[0])
	}
	return reqs
}

func noClaudeWarnings(t *testing.T, warnings []string) {
	t.Helper()
	for _, w := range warnings {
		assert.NotRegexp(t, regexp.MustCompile(`(?i)claude|harness|anthropic`), w, "a Claude-specific warning for a non-harness agent")
	}
}

// TestProviders runs every provider case through AgentRun's run and preview.
func TestProviders(t *testing.T) {
	for _, c := range providerCases() {
		t.Run(c.name, func(t *testing.T) {
			t.Run("plain", func(t *testing.T) {
				f, req := setupCase(t, c, "", "Say hello")
				var summary string
				req.Progress = func(p Progress) {
					if p.Summary {
						summary = p.Message
					}
				}
				res, err := Run(t.Context(), req)
				require.NoError(t, err)
				assert.Equal(t, "Hello from the fake: Say hello", res.Output)
				assert.Equal(t, "root", res.AgentName)
				if res.Usage.Cost > 0 {
					assert.Contains(t, summary, "estimated cost $", "the summary shows the cost")
				} else {
					assert.Contains(t, summary, "no cost estimate (no known pricing for the models)")
				}
				assert.Contains(t, summary, "12 input and 3 output tokens")
				assert.False(t, res.HasStructuredOutput)
				assert.Equal(t, []string{}, res.ToolCalls)
				assert.Empty(t, res.RejectedToolCalls)
				assert.Equal(t, int64(12), res.Usage.InputTokens, "input tokens")
				assert.Equal(t, int64(3), res.Usage.OutputTokens, "output tokens")
				assert.InDelta(t, expectedCost(t, c, 1), res.Usage.Cost, 1e-12, "cost")
				assert.Empty(t, res.Warnings)
				noClaudeWarnings(t, res.Warnings)
				reqs := checkRequest(t, c, f, 1, c.expectedCred())
				t.Logf("REPORT %s | %s %s | model %s | cost %.10f | other requests %v", c.name, reqs[0].Proto, reqs[0].Path,
					reqs[0].Model, res.Usage.Cost, f.other())
			})

			t.Run("tool", func(t *testing.T) {
				f, req := setupCase(t, c, fsToolset, "Read the marker")
				dir := t.TempDir()
				marker := "marker-" + strings.ReplaceAll(c.name, "/", "-")
				require.NoError(t, os.WriteFile(filepath.Join(dir, "marker.txt"), []byte(marker), 0o600))
				req.WorkingDir = dir
				res, err := Run(t.Context(), req)
				require.NoError(t, err)
				assert.Contains(t, res.Output, "file says: ")
				assert.Contains(t, res.Output, marker, "the tool result went back to the model")
				assert.Equal(t, []string{"read_file"}, res.ToolCalls)
				assert.Empty(t, res.RejectedToolCalls)
				assert.Equal(t, int64(24), res.Usage.InputTokens, "input tokens of both requests")
				assert.Equal(t, int64(6), res.Usage.OutputTokens, "output tokens of both requests")
				assert.InDelta(t, expectedCost(t, c, 2), res.Usage.Cost, 1e-12, "cost")
				reqs := checkRequest(t, c, f, 2, c.expectedCred())
				assert.True(t, reqs[0].hasTool("read_file"), "tools offered: %v", reqs[0].Tools)
				results := reqs[1].toolResults()
				require.Len(t, results, 1, "one tool result in the second request")
				assert.Contains(t, results[0].Text, marker)
			})

			if c.structured != "" {
				t.Run("structured_output/"+c.structured, func(t *testing.T) {
					f, req := setupCase(t, c, structuredBlock(c.structured), "Review this change.")
					res, err := Run(t.Context(), req)
					require.NoError(t, err)
					require.True(t, res.HasStructuredOutput)
					reqs := checkRequest(t, c, f, 1, c.expectedCred())
					if c.structured == "tool" {
						assert.Equal(t, map[string]any{"risk": "high", "summary": "tool mode"}, res.StructuredOutput)
						assert.True(t, reqs[0].hasTool(structuredoutput.ToolName))
						assert.Equal(t, []string{}, res.ToolCalls, "the output tool is not reported")
					} else {
						assert.Equal(t, map[string]any{"risk": "low", "summary": "sample"}, res.StructuredOutput)
						require.NotNil(t, reqs[0].Schema, "the schema is sent with the protocol's structured output")
						assert.Contains(t, reqs[0].Schema["properties"], "risk")
					}
				})
			}

			t.Run("preview", func(t *testing.T) {
				doc := []byte(c.yaml("http://127.0.0.1:1", fsToolset+reviewSchema))
				res := validate.Validate(t.Context(), "providers.yaml", doc)
				require.True(t, res.Valid, "docker-agent validation: %v", res.Errors)
				t.Logf("REPORT-PREVIEW %s | requiredEnvVars %v | warnings %v", c.name, res.RequiredEnvVars, res.Warnings)
				noClaudeWarnings(t, res.Warnings)
				for _, inherit := range []bool{false, true} {
					keys := map[string]bool{}
					for k := range c.env() {
						keys[k] = true
					}
					problems, warnings := CheckConfig(t.Context(), "providers.yaml", doc, CheckOptions{
						InheritEnvironment: inherit, EnvKeys: keys, EnvValues: c.env(), ToolApprovalDeny: true,
					})
					assert.Empty(t, problems)
					assert.Empty(t, warnings)
				}
			})
		})
	}
}

// TestProvidersCoverAllAliases keeps the cases in step with docker-agent's providers.
func TestProvidersCoverAllAliases(t *testing.T) {
	covered := map[string]bool{"chatgpt": true} // chatgpt/base_url
	for _, a := range aliasModels {
		covered[a.alias] = true
	}
	for name := range modelprovider.Aliases {
		assert.True(t, covered[name], "alias %q has no provider case", name)
	}
	names := map[string]bool{}
	for _, c := range providerCases() {
		names[strings.Split(c.name, "/")[0]] = true
	}
	for _, core := range modelprovider.CoreProviders {
		assert.True(t, names[core], "core provider %q has no provider case", core)
	}
}

// TestProvidersCredentials resolves each case's credential from the inherited environment, and
// checks that without inheritEnvironment the provider's environment is not used.
func TestProvidersCredentials(t *testing.T) {
	for _, c := range providerCases() {
		if c.credVar == "" {
			continue
		}
		t.Run(c.name, func(t *testing.T) {
			t.Run("inherited", func(t *testing.T) {
				f, req := setupCase(t, c, "", "Say hello")
				t.Setenv(c.credVar, inheritedKey)
				// The SDK variables come from the provider's environment too.
				for k, v := range c.sdkVars(t, f.URL) {
					t.Setenv(k, v)
				}
				req.Env = c.extraEnv
				req.InheritEnvironment = true
				res, err := Run(t.Context(), req)
				require.NoError(t, err)
				assert.Equal(t, "Hello from the fake: Say hello", res.Output)
				checkRequest(t, c, f, 1, inheritedKey)
			})
			t.Run("not-inherited", func(t *testing.T) {
				f, req := setupCase(t, c, "", "Say hello")
				t.Setenv(c.credVar, processOnlyKey)
				req.Env = withoutKeys(req.Env, c.credVar)
				req.InheritEnvironment = false
				_, err := Run(t.Context(), req)
				for _, r := range f.requests() {
					assert.NotEqual(t, processOnlyKey, r.credential(), "inheritEnvironment: false, but the model request "+
						"carried %s of the provider's own environment (%s %s)", c.credVar, r.Proto, r.Path)
					assert.NotContains(t, fmt.Sprint(r.Header), processOnlyKey)
				}
				if c.credVar == "OPENAI_API_KEY" && len(f.requests()) > 0 {
					// docker-agent requires no credential for an `openai` model with a base_url and
					// no token_key (pkg/config/gather.go:231): the SDK sends what the run child's
					// environment has, which is none of the provider's.
					require.NoError(t, err)
					assert.Empty(t, f.requests()[0].credential(), "the request has no credential")
					return
				}
				require.Error(t, err, "the run succeeded without %s in env and without inheritEnvironment", c.credVar)
				if c.credVar == "AWS_BEARER_TOKEN_BEDROCK" {
					// Without a bearer token the AWS SDK tries its default chain, which finds nothing
					// here. Phase 2 rewrites the error to name the variable.
					assert.Contains(t, err.Error(), "get credentials")
					return
				}
				assert.Contains(t, err.Error(), c.credVar, "the error names the missing credential")
			})
		})
	}
}

// --- the provider process's own environment -----------------------------------------------------

// TestProvidersProcessEnvironmentBoundary checks endpoint and credential variables that
// docker-agent's provider SDKs read from the process environment themselves, against AgentRun's
// contract: they resolve from `env`, and from the provider's environment only with
// inheritEnvironment. The run child's environment (childenv.go) makes them hold.
func TestProvidersProcessEnvironmentBoundary(t *testing.T) {
	// A base URL variable in `env` and another in the provider's environment: the one in `env`
	// applies, and with inheritEnvironment: false the other is not seen.
	baseURLVars := []struct {
		name, variable, model, credVar string
		url                            func(string) string
	}{
		{"OPENAI_BASE_URL", "OPENAI_BASE_URL", "models:\n  m:\n    provider: openai\n    model: gpt-4o-mini\n", "OPENAI_API_KEY",
			func(u string) string { return u + "/v1" }},
		{"ANTHROPIC_BASE_URL", "ANTHROPIC_BASE_URL", "models:\n  m:\n    provider: anthropic\n    model: claude-haiku-4-5\n",
			"ANTHROPIC_API_KEY", func(u string) string { return u }},
		{"GOOGLE_GEMINI_BASE_URL", "GOOGLE_GEMINI_BASE_URL", "models:\n  m:\n    provider: google\n    model: gemini-2.5-flash\n",
			"GOOGLE_API_KEY", func(u string) string { return u + "/" }},
	}
	for _, bv := range baseURLVars {
		t.Run("base-url-variable/"+bv.name, func(t *testing.T) {
			for _, inherit := range []bool{false, true} {
				isolation := isolateProviderEnv(t)
				inEnv := newFakeModels(t, providerScript)
				inProcess := newFakeModels(t, providerScript)
				t.Setenv(bv.variable, bv.url(inProcess.URL))
				c := providerCase{models: func(string) string { return bv.model }}
				env := map[string]string{bv.credVar: providerKey, bv.variable: bv.url(inEnv.URL)}
				maps.Copy(env, isolation)
				res, err := Run(t.Context(), Request{
					Name: "providers", YAML: []byte(c.yaml("", "")), Prompt: "Say hello", InheritEnvironment: inherit, Env: env,
				})
				require.NoError(t, err)
				assert.Equal(t, "Hello from the fake: Say hello", res.Output)
				assert.Len(t, inEnv.requests(), 1, "the requests go to the %s of env (inheritEnvironment %v)", bv.variable, inherit)
				assert.Empty(t, inProcess.requests(), "env wins over the provider's environment (inheritEnvironment %v)", inherit)
			}
		})
	}

	// anthropic-sdk-go adds ANTHROPIC_AUTH_TOKEN of the process environment as Authorization:
	// Bearer when the process environment has no ANTHROPIC_API_KEY (client.go:64-75
	// DefaultClientOptions), also for a custom base_url; docker-agent sets the key it resolved
	// as X-Api-Key besides.
	t.Run("ANTHROPIC_AUTH_TOKEN", func(t *testing.T) {
		const procToken = "fake-anthropic-auth-token-of-the-process"
		for _, c := range []providerCase{
			{credVar: "ANTHROPIC_API_KEY", models: func(u string) string { return baseURLModel("anthropic", "claude-haiku-4-5", u) }},
			{credVar: "CORP_KEY", models: func(u string) string {
				return "providers:\n  corp:\n    provider: anthropic\n    base_url: " + u + "\n    token_key: CORP_KEY\n" +
					"models:\n  m:\n    provider: corp\n    model: claude-haiku-4-5\n"
			}},
		} {
			t.Run(c.credVar, func(t *testing.T) {
				f, req := setupCase(t, c, "", "Say hello")
				t.Setenv("ANTHROPIC_AUTH_TOKEN", procToken)
				_, err := Run(t.Context(), req)
				require.NoError(t, err)
				reqs := f.requests()
				require.Len(t, reqs, 1)
				assert.Equal(t, providerKey, reqs[0].Header.Get("X-Api-Key"))
				assert.NotContains(t, reqs[0].Header.Get("Authorization"), procToken, "inheritEnvironment: false keeps the "+
					"provider's ANTHROPIC_AUTH_TOKEN from the SDK")

				// With inheritEnvironment, as with `docker agent run` in the same environment: the
				// SDK sends the inherited token unless the key comes from ANTHROPIC_API_KEY, which env
				// sets in the run child's environment. Phase 2 warns about it in the preview.
				req.InheritEnvironment = true
				_, err = Run(t.Context(), req)
				require.NoError(t, err)
				require.Len(t, f.requests(), 2)
				auth := f.requests()[1].Header.Get("Authorization")
				if c.credVar == "ANTHROPIC_API_KEY" {
					assert.Empty(t, auth, "the SDK sees ANTHROPIC_API_KEY of env and adds no token")
				} else {
					assert.Equal(t, "Bearer "+procToken, auth, "inherited, as with docker agent run")
				}
			})
		}
	})

	bedrock := func(u string) string {
		return "models:\n  m:\n    provider: amazon-bedrock\n    model: amazon.nova-micro-v1:0\n" +
			"    provider_opts:\n      endpoint_url: " + u + "\n      region: us-east-1\n"
	}
	t.Run("bedrock/sigv4-keys-in-env", func(t *testing.T) {
		isolation := isolateProviderEnv(t)
		f := newFakeModels(t, providerScript)
		c := providerCase{models: bedrock}
		env := map[string]string{"AWS_ACCESS_KEY_ID": "AKIAFAKEENVKEY000001", "AWS_SECRET_ACCESS_KEY": "fake-env-secret-0123456789"}
		maps.Copy(env, isolation)
		res, err := Run(t.Context(), Request{
			Name: "providers", YAML: []byte(c.yaml(f.URL, "")), Prompt: "Say hello", InheritEnvironment: false, Env: env,
		})
		require.NoError(t, err, "AWS access keys in env reach the AWS SDK")
		require.Len(t, f.requests(), 1)
		assert.Equal(t, "sigv4:AKIAFAKEENVKEY000001", f.requests()[0].credential(), "Bedrock signs with the keys of env")
		assert.Equal(t, "Hello from the fake: Say hello", res.Output)
	})
	t.Run("bedrock/sigv4-keys-of-the-process", func(t *testing.T) {
		isolation := isolateProviderEnv(t)
		f := newFakeModels(t, providerScript)
		t.Setenv("AWS_ACCESS_KEY_ID", "AKIAFAKEPROCESSKEY01")
		t.Setenv("AWS_SECRET_ACCESS_KEY", "fake-process-secret-0123456789")
		c := providerCase{models: bedrock}
		_, err := Run(t.Context(), Request{Name: "providers", YAML: []byte(c.yaml(f.URL, "")), Prompt: "Say hello",
			InheritEnvironment: true})
		require.NoError(t, err, "SigV4 with the process's keys and inheritEnvironment")
		require.Len(t, f.requests(), 1)
		assert.Equal(t, "sigv4:AKIAFAKEPROCESSKEY01", f.requests()[0].credential(), "Bedrock over SigV4")

		_, err = Run(t.Context(), Request{Name: "providers", YAML: []byte(c.yaml(f.URL, "")), Prompt: "Say hello",
			InheritEnvironment: false, Env: isolation})
		require.Error(t, err, "without inheritEnvironment the AWS SDK finds no credentials")
		for _, r := range f.requests()[1:] {
			assert.NotEqual(t, "sigv4:AKIAFAKEPROCESSKEY01", r.credential(), "inheritEnvironment: false, but Bedrock "+
				"signed the request with the AWS access key of the provider's own environment")
		}
	})
	t.Run("google/ADC-in-env", func(t *testing.T) {
		isolation := isolateProviderEnv(t)
		f := newFakeModels(t, providerScript)
		c := providerCase{models: func(u string) string {
			return baseURLModel("google", "gemini-2.5-flash", u+"/", "provider_opts:", "  project: fake-project",
				"  location: us-central1")
		}}
		env := map[string]string{"GOOGLE_APPLICATION_CREDENTIALS": serviceAccountFile(t, f.URL+"/token")}
		maps.Copy(env, isolation)
		res, err := Run(t.Context(), Request{
			Name: "providers", YAML: []byte(c.yaml(f.URL, "")), Prompt: "Say hello", InheritEnvironment: false, Env: env,
		})
		require.NoError(t, err, "GOOGLE_APPLICATION_CREDENTIALS in env reaches Vertex AI")
		assert.Equal(t, "Hello from the fake: Say hello", res.Output)
		require.Len(t, f.requests(), 1)
		assert.Equal(t, googleToken, f.requests()[0].credential(), "the token of the service account in env")
	})
	t.Run("MODEL_RUNNER_HOST-in-env", func(t *testing.T) {
		isolation := isolateProviderEnv(t)
		inEnv := newFakeModels(t, providerScript)
		inProcess := newFakeModels(t, providerScript)
		t.Setenv("MODEL_RUNNER_HOST", inProcess.URL)
		c := providerCase{models: func(string) string { return "models:\n  m:\n    provider: dmr\n    model: ai/qwen3\n" }}
		env := map[string]string{"MODEL_RUNNER_HOST": inEnv.URL}
		maps.Copy(env, isolation)
		res, err := Run(t.Context(), Request{Name: "providers", YAML: []byte(c.yaml("", "")), Prompt: "Say hello", Env: env})
		require.NoError(t, err)
		assert.Equal(t, "Hello from the fake: Say hello", res.Output)
		assert.Len(t, inEnv.requests(), 1, "Docker Model Runner at the MODEL_RUNNER_HOST of env")
		assert.Empty(t, inProcess.requests())
	})
}

// TestProvidersInheritedEnvironmentParity: with inheritEnvironment (the default), the variables
// the SDKs read from the provider's environment reach them as they reach `docker agent run` in
// that environment, also those docker-agent does not ask for. Phase 2 warns about them in the
// preview; the run child keeps the parity.
func TestProvidersInheritedEnvironmentParity(t *testing.T) {
	t.Run("OPENAI_API_KEY to a base_url without token_key", func(t *testing.T) {
		f, req := setupCase(t, providerCase{models: func(u string) string { return baseURLModel("openai", "gpt-4o-mini", u+"/v1") }},
			"", "Say hello")
		t.Setenv("OPENAI_API_KEY", inheritedKey)
		req.InheritEnvironment = true
		_, err := Run(t.Context(), req)
		require.NoError(t, err)
		require.Len(t, f.requests(), 1)
		assert.Equal(t, inheritedKey, f.requests()[0].credential(), "openai-go reads OPENAI_API_KEY itself")
	})
	t.Run("OPENAI_ORG_ID to an OpenAI-compatible alias", func(t *testing.T) {
		f, req := setupCase(t, providerCase{credVar: "MISTRAL_API_KEY",
			models: func(u string) string { return baseURLModel("mistral", "devstral-small-2507", u+"/v1") }}, "", "Say hello")
		t.Setenv("OPENAI_ORG_ID", "org-of-the-provider")
		req.InheritEnvironment = true
		_, err := Run(t.Context(), req)
		require.NoError(t, err)
		require.Len(t, f.requests(), 1)
		assert.Equal(t, "org-of-the-provider", f.requests()[0].Header.Get("Openai-Organization"))

		req.InheritEnvironment = false
		_, err = Run(t.Context(), req)
		require.NoError(t, err)
		require.Len(t, f.requests(), 2)
		assert.Empty(t, f.requests()[1].Header.Get("Openai-Organization"), "not without inheritEnvironment")
	})
}

// --- model selection: routing, first_available, fallback, auto -------------------------------

// TestProvidersRouting routes by rule to models of different protocols, and to the router's own
// fallback model.
func TestProvidersRouting(t *testing.T) {
	isolateProviderEnv(t)
	f := newFakeModels(t, providerScript)
	models := fmt.Sprintf(`providers:
  corp:
    api_type: openai_chatcompletions
    base_url: %[1]s/v1
    token_key: FAKE_PROVIDER_KEY
models:
  m:
    provider: corp
    model: router-default
    routing:
      - model: poet
        examples: ["write a poem", "compose verses about nature"]
      - model: calc
        examples: ["calculate the sum", "compute numbers"]
  poet:
    provider: anthropic
    model: claude-haiku-4-5
    base_url: %[1]s
  calc:
    provider: google
    model: gemini-2.5-flash
    base_url: %[1]s/
`, f.URL)
	c := providerCase{models: func(string) string { return models }}
	env := map[string]string{"FAKE_PROVIDER_KEY": providerKey, "ANTHROPIC_API_KEY": providerKey, "GOOGLE_API_KEY": providerKey}
	doc := []byte(c.yaml(f.URL, ""))
	vr := validate.Validate(t.Context(), "routing.yaml", doc)
	require.True(t, vr.Valid, "%v", vr.Errors)
	problems, warnings := CheckConfig(t.Context(), "routing.yaml", doc, CheckOptions{ToolApprovalDeny: true})
	assert.Empty(t, problems)
	assert.Empty(t, warnings)

	for _, tc := range []struct {
		prompt, model string
		proto         wireProto
	}{
		{"please write a poem about the sea", "claude-haiku-4-5", protoAnthropic},
		{"calculate the sum of these numbers", "gemini-2.5-flash", protoGemini},
		{"hello there", "router-default", protoChat},
	} {
		t.Run(string(tc.proto), func(t *testing.T) {
			before := len(f.requests())
			res, err := Run(t.Context(), Request{Name: "routing", YAML: doc, Prompt: tc.prompt, Env: env})
			require.NoError(t, err)
			assert.Equal(t, "Hello from the fake: "+tc.prompt, res.Output)
			reqs := f.requests()[before:]
			require.Len(t, reqs, 1)
			assert.Equal(t, tc.proto, reqs[0].Proto)
			assert.Equal(t, tc.model, reqs[0].Model)
			assert.Equal(t, providerKey, reqs[0].credential())
			assert.Equal(t, int64(12), res.Usage.InputTokens)
			t.Logf("REPORT routing %s -> %s %s, cost %.10f", tc.prompt, reqs[0].Proto, reqs[0].Model, res.Usage.Cost)
		})
	}
}

// TestProvidersFirstAvailable selects the first candidate whose credential `env` has.
func TestProvidersFirstAvailable(t *testing.T) {
	isolateProviderEnv(t)
	f := newFakeModels(t, providerScript)
	models := fmt.Sprintf(`models:
  m:
    first_available: [claude, gem, gpt]
  claude:
    provider: anthropic
    model: claude-haiku-4-5
    base_url: %[1]s
    token_key: ANTHROPIC_API_KEY
  gem:
    provider: google
    model: gemini-2.5-flash
    base_url: %[1]s/
    token_key: GOOGLE_API_KEY
  gpt:
    provider: openai
    model: gpt-5-mini
    base_url: %[1]s/v1
    token_key: OPENAI_API_KEY
`, f.URL)
	c := providerCase{models: func(string) string { return models }}
	doc := []byte(c.yaml(f.URL, ""))
	vr := validate.Validate(t.Context(), "first.yaml", doc)
	require.True(t, vr.Valid, "%v", vr.Errors)

	for _, tc := range []struct {
		credVar, model string
		proto          wireProto
	}{
		{"GOOGLE_API_KEY", "gemini-2.5-flash", protoGemini},
		{"OPENAI_API_KEY", "gpt-5-mini", protoResponses},
		{"ANTHROPIC_API_KEY", "claude-haiku-4-5", protoAnthropic},
	} {
		t.Run(tc.credVar, func(t *testing.T) {
			before := len(f.requests())
			res, err := Run(t.Context(), Request{Name: "first", YAML: doc, Prompt: "Say hello",
				Env: map[string]string{tc.credVar: providerKey}})
			require.NoError(t, err)
			assert.Equal(t, "Hello from the fake: Say hello", res.Output)
			reqs := f.requests()[before:]
			require.Len(t, reqs, 1)
			assert.Equal(t, tc.proto, reqs[0].Proto)
			assert.Equal(t, tc.model, reqs[0].Model)
			assert.Positive(t, res.Usage.Cost, "the selected model is priced")
		})
	}
	t.Run("none", func(t *testing.T) {
		_, err := Run(t.Context(), Request{Name: "first", YAML: doc, Prompt: "Say hello"})
		require.Error(t, err)
		for _, v := range []string{"ANTHROPIC_API_KEY", "GOOGLE_API_KEY", "OPENAI_API_KEY"} {
			assert.Contains(t, err.Error(), v)
		}
	})
}

// TestProvidersFallback falls back from a failing model to models of other protocols.
func TestProvidersFallback(t *testing.T) {
	for _, tc := range []struct {
		name, primary, fallback, fbModel string
		fbProto                          wireProto
	}{
		{"chat->anthropic", "    provider: corp\n    model: failing-model\n", "    provider: anthropic\n    model: claude-haiku-4-5\n    base_url: %[1]s\n",
			"claude-haiku-4-5", protoAnthropic},
		{"anthropic->gemini", "    provider: anthropic\n    model: claude-haiku-4-5\n    base_url: %[1]s\n",
			"    provider: google\n    model: gemini-2.5-flash\n    base_url: %[1]s/\n", "gemini-2.5-flash", protoGemini},
		{"gemini->responses", "    provider: google\n    model: gemini-2.5-flash\n    base_url: %[1]s/\n",
			"    provider: openai\n    model: gpt-5-mini\n    base_url: %[1]s/v1\n", "gpt-5-mini", protoResponses},
	} {
		t.Run(tc.name, func(t *testing.T) {
			isolateProviderEnv(t)
			var fallbackModel string
			f := newFakeModels(t, func(r wireReq) wireReply {
				if r.Model != fallbackModel {
					return wireReply{Status: 400, Err: "the primary model is unavailable"}
				}
				return providerScript(r)
			})
			fallbackModel = tc.fbModel
			models := fmt.Sprintf("providers:\n  corp:\n    api_type: openai_chatcompletions\n    base_url: %[1]s/v1\n"+
				"    token_key: FAKE_PROVIDER_KEY\nmodels:\n  m:\n"+tc.primary+"  fb:\n"+tc.fallback, f.URL)
			c := providerCase{models: func(string) string { return models }}
			doc := []byte(c.yaml(f.URL, "    fallback:\n      models: [fb]\n      retries: -1\n"))
			vr := validate.Validate(t.Context(), "fallback.yaml", doc)
			require.True(t, vr.Valid, "%v", vr.Errors)
			var progress []string
			res, err := Run(t.Context(), Request{Name: "fallback", YAML: doc, Prompt: "Say hello",
				Env:      map[string]string{"FAKE_PROVIDER_KEY": providerKey, "ANTHROPIC_API_KEY": providerKey, "GOOGLE_API_KEY": providerKey, "OPENAI_API_KEY": providerKey},
				Progress: func(p Progress) { progress = append(progress, p.Message) }})
			require.NoError(t, err)
			assert.Equal(t, "Hello from the fake: Say hello", res.Output)
			reqs := f.requests()
			require.GreaterOrEqual(t, len(reqs), 2)
			last := reqs[len(reqs)-1]
			assert.Equal(t, tc.fbProto, last.Proto)
			assert.Equal(t, int64(12), res.Usage.InputTokens, "only the answered request has usage")
			assert.Positive(t, res.Usage.Cost, "the fallback model is priced")
			assert.Contains(t, strings.Join(progress, "\n"), "falling back to", "the fallback is reported")
			t.Logf("REPORT fallback %s: %d requests, progress %q, warnings %q", tc.name, len(reqs), progress, res.Warnings)
		})
	}
}

// TestProvidersAuto runs `model: auto`, which docker-agent resolves to the first provider whose
// credential is available (pkg/config/auto.go AvailableProviders, from `env` here). The selected
// model has no base_url, so the fake is reached through the SDK's own base URL variable in env.
func TestProvidersAuto(t *testing.T) {
	for _, tc := range []struct {
		credVar, baseVar, model string
		proto                   wireProto
		base                    func(string) string
		tls                     bool
	}{
		{"ANTHROPIC_API_KEY", "ANTHROPIC_BASE_URL", "claude-sonnet-5", protoAnthropic, func(u string) string { return u }, false},
		{"OPENAI_API_KEY", "OPENAI_BASE_URL", "gpt-5.6", protoResponses, func(u string) string { return u + "/v1" }, false},
		{"GOOGLE_API_KEY", "GOOGLE_GEMINI_BASE_URL", "gemini-3.8-flash", protoGemini, func(u string) string { return u + "/" }, false},
		{"AWS_BEARER_TOKEN_BEDROCK", "AWS_ENDPOINT_URL_BEDROCK_RUNTIME", "global.anthropic.claude-sonnet-5", protoBedrock,
			func(u string) string { return u }, true},
	} {
		t.Run(tc.credVar, func(t *testing.T) {
			isolation := isolateProviderEnv(t)
			f := newFakeModels(t, providerScript)
			if tc.tls {
				f = newFakeModelsTLS(t, providerScript)
			}
			c := providerCase{ref: "auto"}
			doc := []byte(c.yaml(f.URL, ""))
			vr := validate.Validate(t.Context(), "auto.yaml", doc)
			require.True(t, vr.Valid, "%v", vr.Errors)
			problems, warnings := CheckConfig(t.Context(), "auto.yaml", doc, CheckOptions{ToolApprovalDeny: true,
				EnvKeys: map[string]bool{tc.credVar: true}})
			assert.Empty(t, problems)
			assert.Empty(t, warnings)
			env := map[string]string{tc.credVar: providerKey, "AWS_REGION": "us-east-1", tc.baseVar: tc.base(f.URL)}
			maps.Copy(env, isolation)
			res, err := Run(t.Context(), Request{Name: "auto", YAML: doc, Prompt: "Say hello", Env: env})
			require.NoError(t, err)
			assert.Equal(t, "Hello from the fake: Say hello", res.Output)
			reqs := f.requests()
			require.Len(t, reqs, 1, "other requests: %v", f.other())
			assert.Equal(t, tc.proto, reqs[0].Proto)
			assert.Equal(t, tc.model, reqs[0].Model)
			assert.Equal(t, providerKey, reqs[0].credential())
			t.Logf("REPORT auto %s -> %s %s cost %.10f", tc.credVar, reqs[0].Proto, reqs[0].Model, res.Usage.Cost)
		})
	}
}

// TestProvidersBedrockNativeStructuredOutput: structured_output in its default (native) mode on
// Bedrock.
func TestProvidersBedrockNativeStructuredOutput(t *testing.T) {
	t.Skip("phase 2: docker-agent's Bedrock client never sends the schema (pkg/model/provider/bedrock/client.go does " +
		"not read ModelOptions.StructuredOutput()); AgentRun's preview will warn and suggest structured_output.mode: tool")
	var bedrock providerCase
	for _, c := range providerCases() {
		if c.name == "amazon-bedrock/bearer" {
			bedrock = c
		}
	}
	f, req := setupCase(t, bedrock, structuredBlock("native"), "Review this change.")
	_, runErr := Run(t.Context(), req)
	reqs := f.requests()
	require.NotEmpty(t, reqs)
	_, warnings := CheckConfig(t.Context(), "providers.yaml", req.YAML, CheckOptions{ToolApprovalDeny: true,
		EnvKeys: map[string]bool{bedrock.credVar: true}})
	vr := validate.Validate(t.Context(), "providers.yaml", req.YAML)
	if reqs[0].Schema == nil && len(warnings) == 0 && len(vr.Warnings) == 0 {
		t.Errorf("BUG: structured_output (native mode) on amazon-bedrock: the schema is not sent to Bedrock and the "+
			"preview does not warn, so the run depends on the model answering JSON unprompted (here: %v). Root cause: "+
			"pkg/model/provider/bedrock/client.go never reads ModelOptions.StructuredOutput() (only openai, anthropic, "+
			"gemini and dmr do), and AgentRun's CheckConfig (check.go:57) warns about unenforced structured_output only "+
			"for coding harnesses (W5); `mode: tool` works", runErr)
	}
}

// TestProvidersRoutingCost checks the cost of a routed request: docker-agent's catalog price of
// the model that answered.
func TestProvidersRoutingCost(t *testing.T) {
	t.Skip("docker-agent bug: pkg/runtime/loop.go:625-634 prices a routed turn with the router's own model " +
		"(rulebased/client.go:187), not the model that answered; an upstream report is pending")
	for _, router := range []struct{ name, model string }{
		{"router-on-a-custom-provider", "    provider: corp\n    model: router-default\n"},
		{"router-on-a-catalog-model", "    provider: openai\n    model: gpt-4o-mini\n    base_url: %[1]s/v1\n"},
	} {
		t.Run(router.name, func(t *testing.T) {
			isolateProviderEnv(t)
			f := newFakeModels(t, providerScript)
			models := fmt.Sprintf("providers:\n  corp:\n    api_type: openai_chatcompletions\n    base_url: %[1]s/v1\n"+
				"    token_key: FAKE_PROVIDER_KEY\nmodels:\n  m:\n"+router.model+"    routing:\n      - model: poet\n"+
				"        examples: [\"write a poem\"]\n  poet:\n    provider: anthropic\n    model: claude-haiku-4-5\n"+
				"    base_url: %[1]s\n", f.URL)
			c := providerCase{models: func(string) string { return models }}
			res, err := Run(t.Context(), Request{Name: "routing", YAML: []byte(c.yaml(f.URL, "")), Prompt: "write a poem please",
				Env: map[string]string{"FAKE_PROVIDER_KEY": providerKey, "ANTHROPIC_API_KEY": providerKey, "OPENAI_API_KEY": providerKey}})
			require.NoError(t, err)
			require.Len(t, f.requests(), 1)
			require.Equal(t, protoAnthropic, f.requests()[0].Proto, "routed to the Anthropic model")
			want := expectedCost(t, providerCase{costID: modelsdev.NewID("anthropic", "claude-haiku-4-5")}, 1)
			if diff := res.Usage.Cost - want; diff > 1e-12 || diff < -1e-12 {
				t.Errorf("BUG (upstream docker-agent): the request was routed to anthropic/claude-haiku-4-5 (catalog cost %.7f), "+
					"but AgentRun reports cost %.7f. Root cause: pkg/runtime/loop.go:625-634 prices the turn with the "+
					"models.dev entry of the agent's model ID, which for a rule-based router is the router's own "+
					"provider/model (pkg/model/provider/rulebased/client.go:187 BaseConfig, not LastSelectedModelID), and "+
					"applyModelCost (loop.go:1267-1276) keeps that entry for routers", want, res.Usage.Cost)
			}
		})
	}
}
