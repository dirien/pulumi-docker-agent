// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

// Package examples runs the example programs end to end with pulumitest: the locally built
// provider binary (bin/), the local SDK builds (sdk/), a file:// backend in a temp dir and a
// real OCI registry.
//
// Run them with `make test_examples`, which builds everything and starts a throwaway
// registry:2 container. To reuse a running registry, set DOCKER_AGENT_TEST_REGISTRY to its
// address (for example localhost:5000). The registry must be reachable over plain HTTP on
// localhost and allow deletes (REGISTRY_STORAGE_DELETE_ENABLED=true), because the examples use
// `deleteBehavior: delete`.
package examples

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/pulumi/providertest/pulumitest"
	"github.com/pulumi/providertest/pulumitest/assertpreview"
	"github.com/pulumi/providertest/pulumitest/assertrefresh"
	"github.com/pulumi/providertest/pulumitest/opttest"
	"github.com/pulumi/pulumi/sdk/v3/go/auto"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/events"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/optrefresh"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/optup"
	"github.com/pulumi/pulumi/sdk/v3/go/common/apitype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	providerName = "docker-agent"
	registryEnv  = "DOCKER_AGENT_TEST_REGISTRY"

	agentArtifactType   = "application/vnd.docker.agent.config.v1+json"
	ociManifestType     = "application/vnd.oci.image.manifest.v1+json"
	exampleAuthor       = "Pulumi Docker Agent examples"
	exampleTokenKey     = "AGENT_ANTHROPIC_API_KEY"
	sdkModule           = "github.com/dirien/pulumi-docker-agent/sdk"
	expectedYAMLFixture = "testdata/research-assistant.yaml"
)

func TestExampleYAML(t *testing.T)   { runExample(t, "yaml") }
func TestExampleNodejs(t *testing.T) { runExample(t, "nodejs") }
func TestExamplePython(t *testing.T) { runExample(t, "python") }
func TestExampleGo(t *testing.T)     { runExample(t, "go") }
func TestExampleDotnet(t *testing.T) { runExample(t, "dotnet") }
func TestExampleHCL(t *testing.T)    { runExample(t, "hcl") }

// runExample runs up, preview (no changes), refresh (no changes) and destroy for one example
// program and checks its outputs and the registry contents.
func runExample(t *testing.T, lang string) {
	t.Parallel()
	registry := testRegistry(t)
	root := repoRoot(t)

	opts := []opttest.Option{
		// Use bin/pulumi-resource-docker-agent through `plugins.providers` in Pulumi.yaml.
		opttest.LocalProviderPath(providerName, filepath.Join(root, "bin")),
		// The local SDK paths are patched below, then dependencies are installed.
		opttest.SkipInstall(),
		opttest.SkipStackCreate(),
		opttest.RequireYarnLinks(false),
	}
	if lang == "go" {
		opts = append(opts, opttest.GoModReplacement(sdkModule, root, "sdk"))
	}

	start := time.Now()
	pt := pulumitest.NewPulumiTest(t, lang, opts...)
	useLocalSDK(t, lang, pt.WorkingDir(), root)
	// NewStack replaces the relative provider path in Pulumi.yaml with the absolute one.
	pt.NewStack(t, "")
	if lang != "yaml" {
		installDependencies(t, pt.WorkingDir())
	}
	pt.SetConfig(t, "registry", registry)
	t.Logf("[%s] setup (copy, stack, install): %s", lang, time.Since(start).Round(time.Millisecond))

	step := time.Now()
	up := pt.Up(t)
	t.Logf("[%s] up: %s", lang, time.Since(step).Round(time.Millisecond))

	repo := "examples/" + lang + "-agent"
	digest := checkOutputs(t, lang, registry, repo, up.Outputs)
	checkRegistry(t, registry, repo, digest)
	checkDockerAgentCLI(t, stringOutput(t, up.Outputs, "pinnedReference"), stringOutput(t, up.Outputs, "agentYaml"))

	step = time.Now()
	preview := pt.Preview(t)
	assertpreview.HasNoChanges(t, preview)
	t.Logf("[%s] preview: %s", lang, time.Since(step).Round(time.Millisecond))

	step = time.Now()
	refresh := pt.Refresh(t)
	assertrefresh.HasNoChanges(t, refresh)
	t.Logf("[%s] refresh: %s", lang, time.Since(step).Round(time.Millisecond))

	// The artifact still has the digest from `up`: neither preview nor refresh pushed again.
	status, gotDigest, _ := getManifest(t, registry, repo, "v1")
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, digest, gotDigest, "the tag moved after preview/refresh")

	step = time.Now()
	pt.Destroy(t)
	t.Logf("[%s] destroy: %s", lang, time.Since(step).Round(time.Millisecond))

	// deleteBehavior: delete removes the manifest from the registry.
	status, _, _ = getManifest(t, registry, repo, digest)
	assert.Equal(t, http.StatusNotFound, status,
		"%s@%s still exists after destroy; the registry must allow deletes (REGISTRY_STORAGE_DELETE_ENABLED=true)",
		repo, digest)
	t.Logf("[%s] total: %s", lang, time.Since(start).Round(time.Millisecond))
}

// TestExampleDockerServe runs examples/docker-serve-ts, which composes the provider with the
// Pulumi Docker provider: it starts its own registry:2 on a user-defined network, pushes the
// agent, and serves it with `docker agent serve api` in a docker/docker-agent container. It
// needs Docker but not DOCKER_AGENT_TEST_REGISTRY.
func TestExampleDockerServe(t *testing.T) {
	t.Parallel()
	if err := exec.Command("docker", "info").Run(); err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("Docker is not available: %v", err)
		}
		t.Skipf("Docker is not available: %v", err)
	}
	root := repoRoot(t)
	const lang = "docker-serve-ts"

	start := time.Now()
	pt := pulumitest.NewPulumiTest(t, lang,
		opttest.LocalProviderPath(providerName, filepath.Join(root, "bin")),
		opttest.SkipInstall(),
		opttest.SkipStackCreate(),
		opttest.RequireYarnLinks(false),
	)
	useLocalSDK(t, lang, pt.WorkingDir(), root)
	pt.NewStack(t, "")
	// `pulumi up` installs the docker plugin; the docker-agent plugin comes from bin/.
	installDependencies(t, pt.WorkingDir())
	t.Logf("[%s] setup (copy, stack, install): %s", lang, time.Since(start).Round(time.Millisecond))

	step := time.Now()
	up := pt.Up(t)
	t.Logf("[%s] up: %s", lang, time.Since(step).Round(time.Millisecond))

	pinned := stringOutput(t, up.Outputs, "pinnedReference")
	assert.Regexp(t, `^localhost:\d+/examples/serve-agent@sha256:[0-9a-f]{64}$`, pinned)
	digest := pinned[strings.Index(pinned, "@")+1:]
	assert.Regexp(t, `^\d+\.\d+\.\d+\.\d+:5000/examples/serve-agent@`+digest+`$`,
		stringOutput(t, up.Outputs, "serverAgentReference"))

	// The container only reports healthy once /health answers (wait: true), so these
	// requests go to a running server.
	body := httpGet(t, stringOutput(t, up.Outputs, "healthUrl"))
	assert.Contains(t, body, `"status":"ok"`)
	var agents []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Multi       bool   `json:"multi"`
	}
	require.NoError(t, json.Unmarshal([]byte(httpGet(t, stringOutput(t, up.Outputs, "agentsUrl"))), &agents))
	require.Len(t, agents, 1)
	assert.Equal(t, "Answers questions and delegates web research", agents[0].Description)
	assert.True(t, agents[0].Multi, "the served configuration should have several agents")

	step = time.Now()
	assertpreview.HasNoChanges(t, pt.Preview(t))
	t.Logf("[%s] preview: %s", lang, time.Since(step).Round(time.Millisecond))

	step = time.Now()
	assertrefresh.HasNoChanges(t, pt.Refresh(t))
	t.Logf("[%s] refresh: %s", lang, time.Since(step).Round(time.Millisecond))

	step = time.Now()
	pt.Destroy(t)
	t.Logf("[%s] destroy: %s", lang, time.Since(step).Round(time.Millisecond))

	container := stringOutput(t, up.Outputs, "serverContainer")
	err := exec.Command("docker", "inspect", container).Run()
	assert.Error(t, err, "container %s still exists after destroy", container)
	t.Logf("[%s] total: %s", lang, time.Since(start).Round(time.Millisecond))
}

// TestExampleUserSettings runs examples/user-settings-yaml, which writes docker-agent's user
// configuration and a .agentsignore file. The files go to a temp dir (the outputDir stack
// setting); the test checks their content and modes, that the docker-agent CLI reads the
// configuration, drift detection on refresh, and that destroy removes the files. It needs
// no registry.
func TestExampleUserSettings(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	const lang = "user-settings-yaml"

	start := time.Now()
	pt := pulumitest.NewPulumiTest(t, lang,
		opttest.LocalProviderPath(providerName, filepath.Join(root, "bin")),
		opttest.SkipInstall(),
		opttest.SkipStackCreate(),
	)
	pt.NewStack(t, "")
	outDir := t.TempDir()
	pt.SetConfig(t, "outputDir", outDir)

	step := time.Now()
	up := pt.Up(t)
	t.Logf("[%s] up: %s", lang, time.Since(step).Round(time.Millisecond))

	configPath := filepath.Join(outDir, "cagent", "config.yaml")
	ignorePath := filepath.Join(outDir, "project", ".agentsignore")
	assert.Equal(t, configPath, stringOutput(t, up.Outputs, "settingsPath"))
	assert.Equal(t, ignorePath, stringOutput(t, up.Outputs, "agentsIgnorePath"))
	wantConfig := readFixture(t, "testdata/user-settings.yaml")
	wantIgnore := readFixture(t, "testdata/agentsignore")
	assert.Equal(t, wantConfig, stringOutput(t, up.Outputs, "settingsYaml"))
	assert.Equal(t, wantConfig, readFixture(t, configPath))
	assert.Equal(t, wantIgnore, stringOutput(t, up.Outputs, "agentsIgnore"))
	assert.Equal(t, wantIgnore, readFixture(t, ignorePath))
	assert.Equal(t, "version: v1\nsettings:\n  safety: restricted\n  lean: true\n",
		stringOutput(t, up.Outputs, "imageSettingsYaml"))
	if runtime.GOOS != "windows" {
		assertFileMode(t, configPath, 0o600)
		assertFileMode(t, filepath.Dir(configPath), 0o700)
		assertFileMode(t, ignorePath, 0o644)
	}
	checkDockerAgentUserConfig(t, filepath.Dir(configPath))

	step = time.Now()
	assertpreview.HasNoChanges(t, pt.Preview(t))
	assertrefresh.HasNoChanges(t, pt.Refresh(t))
	t.Logf("[%s] preview, refresh: %s", lang, time.Since(step).Round(time.Millisecond))

	// Drift: an edit outside of Pulumi shows up on refresh and the next up reverts it.
	require.NoError(t, os.WriteFile(configPath, []byte("version: v1\nsettings:\n  theme: edited\n"), 0o600))
	refresh := pt.Refresh(t)
	assert.Equal(t, 1, (*refresh.Summary.ResourceChanges)["update"], "refresh should report the edited file:\n%s",
		refresh.StdOut)
	pt.Up(t)
	assert.Equal(t, wantConfig, readFixture(t, configPath))

	step = time.Now()
	pt.Destroy(t)
	t.Logf("[%s] destroy: %s", lang, time.Since(step).Round(time.Millisecond))
	for _, f := range []string{configPath, ignorePath} {
		_, err := os.Stat(f)
		assert.True(t, os.IsNotExist(err), "%s still exists after destroy", f)
	}
	t.Logf("[%s] total: %s", lang, time.Since(start).Round(time.Millisecond))
}

// TestExampleAgentRun runs examples/agent-run-yaml, which runs an agent with AgentRun during
// `pulumi up`. The agent's custom provider points at a fake OpenAI-compatible server started
// by the test, so no API key is needed. The test checks the structured answer, that the key is
// sent to the model API but stored encrypted, that preview and refresh do not run the agent
// again, that a new trigger does, that `rerun: always` runs it on every `pulumi up` but never
// shows as drift on refresh (also not in the refresh of `pulumi up --refresh`), and that destroy
// runs nothing. It needs no registry.
func TestExampleAgentRun(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	const lang = "agent-run-yaml"
	const apiKey = "sk-fake-example-key-0123456789"
	llm := newFakeLLM(t)

	start := time.Now()
	pt := pulumitest.NewPulumiTest(t, lang,
		opttest.LocalProviderPath(providerName, filepath.Join(root, "bin")),
		opttest.SkipInstall(),
		opttest.SkipStackCreate(),
	)
	pt.NewStack(t, "")
	pt.SetConfig(t, "baseUrl", llm.URL)
	pt.SetConfig(t, "model", "fake-model")
	require.NoError(t, pt.CurrentStack().SetConfig(pt.Context(), "apiKey", auto.ConfigValue{Value: apiKey, Secret: true}))

	step := time.Now()
	up := pt.Up(t)
	t.Logf("[%s] up: %s", lang, time.Since(step).Round(time.Millisecond))

	assert.Equal(t, "low", stringOutput(t, up.Outputs, "risk"))
	assert.Equal(t, "sample", stringOutput(t, up.Outputs, "summary"))
	assert.JSONEq(t, `{"risk":"low","summary":"sample"}`, stringOutput(t, up.Outputs, "answer"))
	assert.Equal(t, "root", stringOutput(t, up.Outputs, "agentName"))
	assert.Equal(t, map[string]any{"inputTokens": 10.0, "outputTokens": 5.0, "cost": 0.0}, outputValue(t, up.Outputs, "usage"))
	assert.Empty(t, outputValue(t, up.Outputs, "toolCalls"))
	// `logging: status` (the default) ends the run with a summary, which never quotes the key.
	assert.Contains(t, up.StdOut, "agent run finished in ")
	assert.Contains(t, up.StdOut, "root answered after 0 tool calls; 10 input and 5 output tokens")
	assert.NotContains(t, up.StdOut+up.StdErr, apiKey)

	reqs := llm.received()
	require.Len(t, reqs, 1, "up runs the agent once")
	assert.Equal(t, "Bearer "+apiKey, reqs[0].Authorization)
	assert.Equal(t, "fake-model", reqs[0].Model)
	assert.Contains(t, reqs[0].System, "You review infrastructure change requests.")
	assert.Contains(t, reqs[0].User, "PostgreSQL cluster from version 15 to 17")
	require.NotNil(t, reqs[0].Schema, "structuredOutput is sent as the response schema")
	assert.Contains(t, reqs[0].Schema["properties"], "risk")

	checkAgentRunSecrets(t, pt.ExportStack(t).Deployment)

	step = time.Now()
	assertpreview.HasNoChanges(t, pt.Preview(t))
	assertrefresh.HasNoChanges(t, pt.Refresh(t))
	t.Logf("[%s] preview, refresh: %s", lang, time.Since(step).Round(time.Millisecond))
	assert.Len(t, llm.received(), 1, "preview and refresh never run the agent")

	// A new revision changes `triggers`, so the next up runs the agent again.
	pt.SetConfig(t, "revision", "2")
	up = pt.Up(t)
	assert.Equal(t, 1, (*up.Summary.ResourceChanges)["update"], "only the AgentRun is updated:\n%s", up.StdOut)
	assert.Len(t, llm.received(), 2)
	assert.Equal(t, "low", stringOutput(t, up.Outputs, "risk"))

	// rerun: always runs the agent on every `pulumi up` that includes it.
	step = time.Now()
	pt.SetConfig(t, "rerun", "always")
	preview := pt.Preview(t)
	assert.Equal(t, 1, preview.ChangeSummary[apitype.OpUpdate], "the AgentRun previews as an update:\n%s", preview.StdOut)
	assert.Contains(t, preview.StdOut, "rerun")
	assert.Len(t, llm.received(), 2, "a preview never runs the agent")
	up = pt.Up(t)
	assert.Equal(t, 1, (*up.Summary.ResourceChanges)["update"], "only the AgentRun is updated:\n%s", up.StdOut)
	assert.Len(t, llm.received(), 3)

	// A refresh reports no drift for it, and runs nothing.
	assertrefresh.HasNoChanges(t, pt.Refresh(t))
	pt.Refresh(t, optrefresh.ExpectNoChanges())
	assert.Len(t, llm.received(), 3, "refresh never runs the agent")

	// `pulumi up --refresh` refreshes without drift and then runs the agent once.
	engineEvents := make(chan events.EngineEvent)
	var refreshDiffs []string
	eventsDone := make(chan struct{})
	go func() {
		defer close(eventsDone)
		for e := range engineEvents {
			if e.ResourcePreEvent == nil {
				continue
			}
			if m := e.ResourcePreEvent.Metadata; m.Op == apitype.OpRefresh && m.Type == "docker-agent:index:AgentRun" &&
				(len(m.Diffs) > 0 || len(m.DetailedDiff) > 0) {
				refreshDiffs = append(refreshDiffs, fmt.Sprintf("%s: %v %v", m.URN, m.Diffs, m.DetailedDiff))
			}
		}
	}()
	up = pt.Up(t, optup.Refresh(), optup.EventStreams(engineEvents))
	<-eventsDone
	assert.Empty(t, refreshDiffs, "the refresh of `pulumi up --refresh` reports drift for the AgentRun")
	assert.Equal(t, 1, (*up.Summary.ResourceChanges)["update"], "only the AgentRun is updated:\n%s", up.StdOut)
	assert.Len(t, llm.received(), 4, "up --refresh runs the agent exactly once")

	up = pt.Up(t)
	assert.Len(t, llm.received(), 5, "every up runs the agent again")
	assert.Equal(t, "low", stringOutput(t, up.Outputs, "risk"))
	t.Logf("[%s] rerun always: %s", lang, time.Since(step).Round(time.Millisecond))

	step = time.Now()
	pt.Destroy(t)
	t.Logf("[%s] destroy: %s", lang, time.Since(step).Round(time.Millisecond))
	assert.Len(t, llm.received(), 5, "destroy runs nothing")
	t.Logf("[%s] total: %s", lang, time.Since(start).Round(time.Millisecond))
}

// TestDockerAgentExamplesTypeScript and TestDockerAgentExamplesPython run the generated
// programs of examples/docker-agent-examples, which build every example configuration of
// docker-agent as an AgentConfig with typed inputs (`make generate_config_types` writes them).
// They need no registry.
func TestDockerAgentExamplesTypeScript(t *testing.T) { runDockerAgentExamples(t, "typescript") }
func TestDockerAgentExamplesPython(t *testing.T)     { runDockerAgentExamples(t, "python") }

// runDockerAgentExamples type-checks the TypeScript project (strict, so a wrong input name
// fails), runs preview and up, and checks that the stack output `configs` has exactly the
// examples of docker-agent-examples/testdata and that each rendered YAML is byte-identical to
// its golden file. A second preview must show no changes; then the stack is destroyed.
func runDockerAgentExamples(t *testing.T, lang string) {
	t.Parallel()
	root := repoRoot(t)
	program := "docker-agent-examples/" + lang
	golden := dockerAgentExampleGoldens(t)

	start := time.Now()
	pt := pulumitest.NewPulumiTest(t, program,
		opttest.LocalProviderPath(providerName, filepath.Join(root, "bin")),
		opttest.SkipInstall(),
		opttest.SkipStackCreate(),
		opttest.RequireYarnLinks(false),
	)
	useLocalSDK(t, program, pt.WorkingDir(), root)
	pt.NewStack(t, "")
	installDependencies(t, pt.WorkingDir())
	t.Logf("[%s] setup (copy, stack, install): %s", program, time.Since(start).Round(time.Millisecond))

	if lang == "typescript" {
		step := time.Now()
		cmd := exec.Command("npx", "--no-install", "tsc", "--noEmit", "-p", ".")
		cmd.Dir = pt.WorkingDir()
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "tsc --noEmit:\n%s", out)
		t.Logf("[%s] tsc --noEmit: %s", program, time.Since(step).Round(time.Millisecond))
	}

	step := time.Now()
	pt.Preview(t)
	t.Logf("[%s] preview: %s", program, time.Since(step).Round(time.Millisecond))

	step = time.Now()
	up := pt.Up(t)
	t.Logf("[%s] up: %s", program, time.Since(step).Round(time.Millisecond))
	// One AgentConfig per example, plus the stack.
	assert.Equal(t, len(golden)+1, (*up.Summary.ResourceChanges)["create"])

	configs, ok := outputValue(t, up.Outputs, "configs").(map[string]any)
	require.True(t, ok, "configs is not an object: %#v", up.Outputs["configs"].Value)
	assert.ElementsMatch(t, sortedKeys(golden), sortedKeys(configs), "the examples in configs and testdata differ")
	for name, want := range golden {
		got, ok := configs[name].(string)
		if assert.True(t, ok, "configs[%q] is missing or not a string", name) {
			assert.Equal(t, want, got, "%s renders different YAML than docker-agent-examples/testdata/%s.yaml", name, name)
		}
	}

	step = time.Now()
	assertpreview.HasNoChanges(t, pt.Preview(t))
	t.Logf("[%s] preview: %s", program, time.Since(step).Round(time.Millisecond))

	step = time.Now()
	pt.Destroy(t)
	t.Logf("[%s] destroy: %s", program, time.Since(step).Round(time.Millisecond))
	t.Logf("[%s] total: %s", program, time.Since(start).Round(time.Millisecond))
}

// dockerAgentExampleGoldens reads docker-agent-examples/testdata: example name -> YAML.
func dockerAgentExampleGoldens(t *testing.T) map[string]string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("docker-agent-examples", "testdata", "*.yaml"))
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(files), 200, "run make generate_config_types")
	out := map[string]string{}
	for _, f := range files {
		out[strings.TrimSuffix(filepath.Base(f), ".yaml")] = readFixture(t, f)
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// secretSig marks a secret value in an exported Pulumi state.
const secretSig = "4dabf18193072939515e22adb298388d"

// checkAgentRunSecrets checks the AgentRun's state: `env` (the API key) is a secret, and the
// answer is not made secret by it.
func checkAgentRunSecrets(t *testing.T, deployment json.RawMessage) {
	t.Helper()
	var state struct {
		Resources []struct {
			Type    string                     `json:"type"`
			Inputs  map[string]json.RawMessage `json:"inputs"`
			Outputs map[string]json.RawMessage `json:"outputs"`
		} `json:"resources"`
	}
	require.NoError(t, json.Unmarshal(deployment, &state))
	isSecret := func(raw json.RawMessage) bool {
		var v map[string]any
		return json.Unmarshal(raw, &v) == nil && v[secretSig] != nil
	}
	for _, r := range state.Resources {
		if r.Type != "docker-agent:index:AgentRun" {
			continue
		}
		assert.True(t, isSecret(r.Inputs["env"]), "env input is not a secret")
		assert.True(t, isSecret(r.Outputs["env"]), "env output is not a secret")
		assert.False(t, isSecret(r.Outputs["output"]), "a secret env must not make the answer secret")
		return
	}
	t.Fatal("no AgentRun in the state")
}

// stateResource is a resource of an exported Pulumi state.
type stateResource struct {
	Type    string                     `json:"type"`
	URN     string                     `json:"urn"`
	ID      string                     `json:"id"`
	Delete  bool                       `json:"delete"`
	Inputs  map[string]json.RawMessage `json:"inputs"`
	Outputs map[string]json.RawMessage `json:"outputs"`
}

// agentRunsInState returns the AgentRuns of an exported state that are not pending deletion, by
// resource name.
func agentRunsInState(t *testing.T, deployment json.RawMessage) map[string]stateResource {
	t.Helper()
	var state struct {
		Resources []stateResource `json:"resources"`
	}
	require.NoError(t, json.Unmarshal(deployment, &state))
	runs := map[string]stateResource{}
	for _, r := range state.Resources {
		if r.Type == "docker-agent:index:AgentRun" && !r.Delete {
			runs[r.URN[strings.LastIndex(r.URN, "::")+2:]] = r
		}
	}
	return runs
}

// isSecretJSON reports whether a value of an exported state is a secret.
func isSecretJSON(raw json.RawMessage) bool {
	var v map[string]any
	return json.Unmarshal(raw, &v) == nil && v[secretSig] != nil
}

// stateWithoutSecrets returns an exported state with every secret value replaced by "[secret]":
// what the state stores in plaintext. pulumitest exports with --show-secrets, so the export itself
// contains the decrypted secrets.
func stateWithoutSecrets(t *testing.T, deployment json.RawMessage) string {
	t.Helper()
	var v any
	require.NoError(t, json.Unmarshal(deployment, &v))
	var strip func(any) any
	strip = func(v any) any {
		switch v := v.(type) {
		case map[string]any:
			if v[secretSig] != nil {
				return "[secret]"
			}
			for k, e := range v {
				v[k] = strip(e)
			}
		case []any:
			for i, e := range v {
				v[i] = strip(e)
			}
		}
		return v
	}
	out, err := json.Marshal(strip(v))
	require.NoError(t, err)
	return string(out)
}

// checkDockerAgentUserConfig lists the aliases with the real docker-agent CLI when it is on
// PATH: the CLI must load the written configuration.
func checkDockerAgentUserConfig(t *testing.T, configDir string) {
	t.Helper()
	if !lookPath("docker-agent") {
		t.Log("docker-agent CLI not found on PATH; skipping the CLI compatibility check")
		return
	}
	cmd := exec.Command("docker-agent", "alias", "list", "--config-dir", configDir)
	cmd.Env = append(os.Environ(), "HOME="+t.TempDir(), "TELEMETRY_ENABLED=false", "DOCKER_AGENT_HIDE_TELEMETRY_BANNER=1")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "docker-agent alias list:\n%s", out)
	assert.Regexp(t, `reviewer\s+→ docker\.io/myorg/reviewer:latest \[safety=restricted\]`, string(out))
	assert.Regexp(t, `local\s+→ \./agent\.yaml \[model=anthropic/claude-sonnet-4-5\]`, string(out))
}

func readFixture(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(data)
}

func assertFileMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, want, info.Mode().Perm(), path)
}

func httpGet(t *testing.T, url string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err, "GET %s", url)
	defer resp.Body.Close() //nolint:errcheck
	data, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, "GET %s: %s", url, data)
	return string(data)
}

// useLocalSDK points the copied program at the SDK built in this repository. The programs
// reference it with paths relative to the repository, which break once pulumitest copies them
// to a temp dir.
func useLocalSDK(t *testing.T, lang, dir, root string) {
	t.Helper()
	switch lang {
	case "nodejs", "docker-serve-ts", "agent-run-claude-code-ts":
		sdk := filepath.Join(root, "sdk", "nodejs", "bin")
		requireFile(t, filepath.Join(sdk, "package.json"), "make build_nodejs")
		replaceInFile(t, filepath.Join(dir, "package.json"), "file:../../sdk/nodejs/bin", "file:"+filepath.ToSlash(sdk))
	case "python":
		sdk := filepath.Join(root, "sdk", "python", "bin")
		requireFile(t, filepath.Join(sdk, "pyproject.toml"), "make build_python")
		replaceInFile(t, filepath.Join(dir, "requirements.txt"), "../../sdk/python/bin", sdk)
	case "docker-agent-examples/typescript":
		sdk := filepath.Join(root, "sdk", "nodejs", "bin")
		requireFile(t, filepath.Join(sdk, "package.json"), "make build_nodejs")
		replaceInFile(t, filepath.Join(dir, "package.json"), "file:../../../sdk/nodejs/bin", "file:"+filepath.ToSlash(sdk))
	case "docker-agent-examples/python":
		sdk := filepath.Join(root, "sdk", "python", "bin")
		requireFile(t, filepath.Join(sdk, "pyproject.toml"), "make build_python")
		replaceInFile(t, filepath.Join(dir, "requirements.txt"), "../../../sdk/python/bin", sdk)
	case "dotnet":
		sdk := filepath.Join(root, "sdk", "dotnet")
		requireFile(t, filepath.Join(sdk, "version.txt"), "make build_dotnet")
		replaceInFile(t, filepath.Join(dir, "DockerAgentExample.csproj"),
			"../../sdk/dotnet/ediri.DockerAgent.csproj", filepath.Join(sdk, "ediri.DockerAgent.csproj"))
	}
}

// installDependencies installs the program's dependencies but no plugins. `pulumi install`
// would try to download the plugin version the local SDK build declares (a dev version that
// was never released); `pulumi up` uses the binary from `plugins.providers` instead.
func installDependencies(t *testing.T, dir string) {
	t.Helper()
	cmd := exec.Command("pulumi", "install", "--no-plugins")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "pulumi install --no-plugins failed:\n%s", out)
}

// checkOutputs asserts the stack outputs, which have the same names in every language, and
// returns the pushed digest.
func checkOutputs(t *testing.T, lang, registry, repo string, outputs auto.OutputMap) string {
	t.Helper()
	expectedYAML, err := os.ReadFile(expectedYAMLFixture)
	require.NoError(t, err)

	digest := stringOutput(t, outputs, "digest")
	assert.Regexp(t, `^sha256:[0-9a-f]{64}$`, digest)
	pinned := stringOutput(t, outputs, "pinnedReference")
	assert.Equal(t, registry+"/"+repo+"@"+digest, pinned)
	assert.Equal(t, "docker agent run "+pinned, stringOutput(t, outputs, "runCommand"))

	agentYAML := stringOutput(t, outputs, "agentYaml")
	assert.Equal(t, string(expectedYAML), agentYAML,
		"the %s program rendered different YAML than %s; every example must describe the same agent",
		lang, expectedYAMLFixture)
	assert.Equal(t, agentYAML, stringOutput(t, outputs, "pulledYaml"), "getAgent returned different YAML")
	assert.Equal(t, []any{"root", "researcher"}, outputValue(t, outputs, "pulledAgents"))
	assert.Equal(t, []any{exampleTokenKey}, outputValue(t, outputs, "requiredEnvVars"))

	metadata, ok := outputValue(t, outputs, "pulledMetadata").(map[string]any)
	require.True(t, ok, "pulledMetadata is not an object: %#v", outputs["pulledMetadata"].Value)
	assert.Equal(t, exampleAuthor, metadata["author"])
	assert.Equal(t, "1.0.0", metadata["version"])
	assert.Equal(t, []any{"research", "example"}, metadata["tags"])

	assert.Equal(t, true, outputValue(t, outputs, "rawConfigValid"), "agent.yaml failed validation: %v",
		outputs["rawConfigErrors"].Value)
	assert.Equal(t, []any{"root"}, outputValue(t, outputs, "rawConfigAgents"))
	assert.Empty(t, outputValue(t, outputs, "rawConfigErrors"))
	return digest
}

// checkRegistry reads the manifest straight from the registry, without the provider, and checks
// that it is a docker-agent artifact.
func checkRegistry(t *testing.T, registry, repo, digest string) {
	t.Helper()
	status, gotDigest, m := getManifest(t, registry, repo, "v1")
	require.Equal(t, http.StatusOK, status, "GET %s/%s:v1", registry, repo)
	assert.Equal(t, digest, gotDigest)
	assert.Equal(t, ociManifestType, m.MediaType)
	assert.Equal(t, agentArtifactType, m.ArtifactType)
	require.Len(t, m.Layers, 1)
	assert.Equal(t, "application/vnd.docker.image.rootfs.diff.tar.gzip", m.Layers[0].MediaType)
	assert.NotEmpty(t, m.Annotations["io.docker.agent.version"])
	assert.Equal(t, m.Annotations["io.docker.agent.version"], m.Annotations["io.docker.cagent.version"])
	assert.Equal(t, exampleAuthor, m.Annotations["org.opencontainers.image.authors"])
	assert.Equal(t, "Apache-2.0", m.Annotations["org.opencontainers.image.licenses"])
	assert.Equal(t, "research,example", m.Annotations["io.docker.agent.tags"])
}

// checkDockerAgentCLI pulls the artifact with the real docker-agent CLI when it is on PATH
// (CI installs it): `share pull` must write the pushed YAML unchanged, and `debug config` must
// load it.
func checkDockerAgentCLI(t *testing.T, pinned, agentYAML string) {
	t.Helper()
	if !lookPath("docker-agent") {
		t.Log("docker-agent CLI not found on PATH; skipping the CLI compatibility check")
		return
	}
	dir, home := t.TempDir(), t.TempDir()
	run := func(args ...string) string {
		cmd := exec.Command("docker-agent", args...)
		cmd.Dir = dir
		// A temporary HOME keeps the CLI's local agent store out of the user's home directory.
		cmd.Env = append(os.Environ(), "HOME="+home, "TELEMETRY_ENABLED=false", "DOCKER_AGENT_HIDE_TELEMETRY_BANNER=1")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "docker-agent %s:\n%s", strings.Join(args, " "), out)
		return string(out)
	}

	run("share", "pull", pinned)
	files, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	require.NoError(t, err)
	require.Len(t, files, 1, "docker-agent share pull should write one file")
	pulled, err := os.ReadFile(files[0])
	require.NoError(t, err)
	assert.Equal(t, agentYAML, string(pulled), "docker-agent share pull returned different YAML")

	assert.Contains(t, run("debug", "config", pinned), "ref: docker:duckduckgo")
}

type manifest struct {
	MediaType    string            `json:"mediaType"`
	ArtifactType string            `json:"artifactType"`
	Annotations  map[string]string `json:"annotations"`
	Layers       []struct {
		MediaType string `json:"mediaType"`
		Digest    string `json:"digest"`
	} `json:"layers"`
}

// getManifest fetches a manifest by tag or digest with the OCI distribution API.
func getManifest(t *testing.T, registry, repo, ref string) (int, string, manifest) {
	t.Helper()
	scheme := "https"
	if host := strings.Split(registry, ":")[0]; host == "localhost" || host == "127.0.0.1" {
		scheme = "http"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	url := fmt.Sprintf("%s://%s/v2/%s/manifests/%s", scheme, registry, repo, ref)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	require.NoError(t, err)
	req.Header.Set("Accept", ociManifestType)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err, "GET %s", url)
	defer resp.Body.Close() //nolint:errcheck
	var m manifest
	if resp.StatusCode == http.StatusOK {
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&m))
	}
	return resp.StatusCode, resp.Header.Get("Docker-Content-Digest"), m
}

func testRegistry(t *testing.T) string {
	t.Helper()
	registry := os.Getenv(registryEnv)
	if registry == "" {
		msg := registryEnv + " is not set. Run `make test_examples`, which starts a registry:2 container, " +
			"or set it to the address of a running registry (for example localhost:5000)."
		if os.Getenv("CI") != "" {
			t.Fatal(msg)
		}
		t.Skip(msg)
	}
	return strings.TrimSuffix(strings.TrimPrefix(registry, "http://"), "/")
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)
	root := filepath.Dir(filepath.Dir(file))
	binary := "pulumi-resource-" + providerName
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	requireFile(t, filepath.Join(root, "bin", binary), "make provider")
	return root
}

func requireFile(t *testing.T, path, fix string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("%s is missing; run `%s` first (or use `make test_examples`)", path, fix)
	}
}

func replaceInFile(t *testing.T, path, old, replacement string) {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(data), old, "%s no longer references %s", path, old)
	err = os.WriteFile(path, []byte(strings.ReplaceAll(string(data), old, replacement)), 0o600)
	require.NoError(t, err)
}

func outputValue(t *testing.T, outputs auto.OutputMap, key string) any {
	t.Helper()
	out, ok := outputs[key]
	require.True(t, ok, "missing stack output %q", key)
	return out.Value
}

func stringOutput(t *testing.T, outputs auto.OutputMap, key string) string {
	t.Helper()
	s, ok := outputValue(t, outputs, key).(string)
	require.True(t, ok, "stack output %q is not a string: %#v", key, outputs[key].Value)
	return s
}

// lookPath reports whether a CLI is available, for optional checks.
func lookPath(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
