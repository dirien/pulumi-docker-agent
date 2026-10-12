// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package examples

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/pulumi/providertest/pulumitest"
	"github.com/pulumi/providertest/pulumitest/assertpreview"
	"github.com/pulumi/providertest/pulumitest/assertrefresh"
	"github.com/pulumi/providertest/pulumitest/opttest"
	"github.com/pulumi/pulumi/sdk/v3/go/auto"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/optpreview"
	"github.com/pulumi/pulumi/sdk/v3/go/common/workspace"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// upgradeBaseline is the released provider whose state the upgrade test starts from.
const upgradeBaseline = "v0.1.1"

// The upgrade tests change the provider version of a program's default provider, as an SDK
// program does when it upgrades the SDK: the generated SDKs pin the version of every resource,
// so the default provider is default_<version>, and the engine asks the new provider's
// DiffConfig whether the old configuration needs a replacement, which would replace every
// resource of the provider. The programs pin the version with options.version, and the
// provider versions are plugins installed in a PULUMI_HOME of the test's own, so the engine
// resolves them by version as it does for SDK programs. Swapping the binary behind an
// unversioned provider (plugins.providers) would change no provider and test nothing.

// TestAgentRunUpgradeFromV011E2E deploys testdata/agent-run-upgrade (examples/agent-run-yaml as
// of v0.1.1) with the released v0.1.1 provider, then upgrades the program's provider version to
// the provider in bin/: preview --expect-no-changes, refresh and up must show no changes and
// must not run the agent again, the AgentRun keeps its ID, and destroy runs nothing. The baseline
// plugin is downloaded from the GitHub release; set DOCKER_AGENT_TEST_SKIP_UPGRADE=1 to skip the
// test offline.
func TestAgentRunUpgradeFromV011E2E(t *testing.T) {
	t.Parallel()
	if os.Getenv("DOCKER_AGENT_TEST_SKIP_UPGRADE") == "1" {
		t.Skip("DOCKER_AGENT_TEST_SKIP_UPGRADE=1")
	}
	root := repoRoot(t)
	const program = "agent-run-upgrade"
	const apiKey = "sk-fake-upgrade-key-0123456789"
	baseline := installReleasedProvider(t, upgradeBaseline)
	current := providerVersion(t, filepath.Join(root, "bin"))
	require.NotEqual(t, strings.TrimPrefix(upgradeBaseline, "v"), current, "bin/ must be another version than the baseline")
	home := pluginHome(t, map[string]string{strings.TrimPrefix(upgradeBaseline, "v"): baseline, current: filepath.Join(root, "bin")})
	llm := newFakeLLM(t)

	start := time.Now()
	pt := pulumitest.NewPulumiTest(t, filepath.Join("testdata", program),
		opttest.Env("PULUMI_HOME", home),
		opttest.SkipInstall(),
		opttest.SkipStackCreate(),
	)
	pt.NewStack(t, "")
	pt.SetConfig(t, "baseUrl", llm.URL)
	pt.SetConfig(t, "model", "fake-model")
	require.NoError(t, pt.CurrentStack().SetConfig(pt.Context(), "apiKey", auto.ConfigValue{Value: apiKey, Secret: true}))

	step := time.Now()
	up := pt.Up(t)
	t.Logf("[%s] up with %s: %s", program, upgradeBaseline, time.Since(step).Round(time.Millisecond))
	assert.Equal(t, "low", stringOutput(t, up.Outputs, "risk"))
	require.Len(t, llm.received(), 1, "up runs the agent once")
	ids := stateIDs(t, pt)
	require.Contains(t, ids, "review")
	assert.Equal(t, "review", ids["review"], "%s uses the resource name as the ID", upgradeBaseline)
	assertDefaultProvider(t, pt, "0.1.1")

	// The same stack and program, now with the provider version of bin/.
	setProviderVersion(t, pt, `"0.1.1"`, `"`+current+`"`)
	step = time.Now()
	preview := pt.Preview(t, optpreview.ExpectNoChanges(), optpreview.Diff())
	assertpreview.HasNoChanges(t, preview)
	assertrefresh.HasNoChanges(t, pt.Refresh(t))
	up = pt.Up(t)
	assertNoChanges(t, up)
	assert.Equal(t, "low", stringOutput(t, up.Outputs, "risk"))
	assert.Len(t, llm.received(), 1, "the upgrade must not run the agent again")
	assert.Equal(t, "review", stateIDs(t, pt)["review"], "the AgentRun keeps its ID")
	assertDefaultProvider(t, pt, current)
	t.Logf("[%s] preview, refresh, up with %s: %s", program, current, time.Since(step).Round(time.Millisecond))

	pt.Destroy(t)
	assert.Len(t, llm.received(), 1, "destroy runs nothing: the state has no deletePrompt")
	t.Logf("[%s] total: %s", program, time.Since(start).Round(time.Millisecond))
}

// TestAgentRunProviderUpgradeE2E upgrades the provider version of an AgentRun with a deletePrompt
// in a workingDir, from the provider in bin/ to the same provider under the next version: the
// upgrade must neither run the agent again nor run the old resource's deletePrompt after it,
// which a replacement of the provider would do (create the new run, then delete the old one with
// the old provider process). Destroy runs the deletePrompt once.
func TestAgentRunProviderUpgradeE2E(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	const program = "agent-run-provider-upgrade"
	const from, to = "0.0.0-e2e.1", "0.0.0-e2e.2"
	bin := filepath.Join(root, "bin")
	home := pluginHome(t, map[string]string{from: bin, to: bin})
	llm := newFakeLLM(t)
	work := t.TempDir()

	pt := pulumitest.NewPulumiTest(t, filepath.Join("testdata", program),
		opttest.Env("PULUMI_HOME", home),
		opttest.SkipInstall(),
		opttest.SkipStackCreate(),
	)
	setProviderVersion(t, pt, "PROVIDER_VERSION", from)
	pt.NewStack(t, "")
	pt.SetConfig(t, "baseUrl", llm.URL)
	pt.SetConfig(t, "workDir", work)
	require.NoError(t, pt.CurrentStack().SetConfig(pt.Context(), "apiKey",
		auto.ConfigValue{Value: "sk-fake-provider-upgrade-0123456789", Secret: true}))

	pt.Up(t)
	require.Equal(t, []string{"Write the release notes into the working directory."}, userMessages(llm))

	setProviderVersion(t, pt, from, to)
	pt.Preview(t, optpreview.ExpectNoChanges(), optpreview.Diff())
	assertNoChanges(t, pt.Up(t))
	assert.Equal(t, []string{"Write the release notes into the working directory."}, userMessages(llm),
		"the upgrade runs neither the prompt nor the deletePrompt")
	assertDefaultProvider(t, pt, to)

	pt.Destroy(t)
	assert.Equal(t, []string{"Write the release notes into the working directory.",
		"Delete the release notes from the working directory."}, userMessages(llm), "destroy runs the deletePrompt once")
}

// installReleasedProvider installs a released provider plugin from its GitHub release and returns
// its directory. `pulumi plugin install` skips a plugin that is installed.
func installReleasedProvider(t *testing.T, version string) string {
	t.Helper()
	cmd := exec.Command("pulumi", "plugin", "install", "resource", providerName, version,
		"--server", "github://api.github.com/dirien/pulumi-docker-agent")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "installing the released %s provider failed; set DOCKER_AGENT_TEST_SKIP_UPGRADE=1 to "+
		"skip this test offline:\n%s", version, out)
	pluginDir, err := workspace.GetPluginDir()
	require.NoError(t, err)
	dir := filepath.Join(pluginDir, "resource-"+providerName+"-"+version)
	require.FileExists(t, filepath.Join(dir, providerBinary()))
	return dir
}

func providerBinary() string {
	if runtime.GOOS == "windows" {
		return "pulumi-resource-" + providerName + ".exe"
	}
	return "pulumi-resource-" + providerName
}

// providerVersion returns the version a provider binary reports.
func providerVersion(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("pulumi", "package", "get-schema", filepath.Join(dir, providerBinary())).Output()
	require.NoError(t, err)
	var schema struct {
		Version string `json:"version"`
	}
	require.NoError(t, json.Unmarshal(out, &schema))
	require.NotEmpty(t, schema.Version)
	return schema.Version
}

// pluginHome returns a PULUMI_HOME of the test's own with the provider binaries in the given
// directories installed as plugins of the given versions.
func pluginHome(t *testing.T, versions map[string]string) string {
	t.Helper()
	home := t.TempDir()
	for version, dir := range versions {
		cmd := exec.Command("pulumi", "plugin", "install", "resource", providerName, version, "--file",
			filepath.Join(dir, providerBinary()))
		cmd.Env = append(os.Environ(), "PULUMI_HOME="+home, "PULUMI_SKIP_UPDATE_CHECK=true")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "installing %s as version %s:\n%s", dir, version, out)
	}
	return home
}

// setProviderVersion replaces the provider version that the program's resources pin.
func setProviderVersion(t *testing.T, pt *pulumitest.PulumiTest, old, version string) {
	t.Helper()
	replaceInFile(t, filepath.Join(pt.WorkingDir(), "Pulumi.yaml"), "version: "+old, "version: "+version)
}

// assertNoChanges fails when an update changed resources.
func assertNoChanges(t *testing.T, up auto.UpResult) {
	t.Helper()
	for op, n := range *up.Summary.ResourceChanges {
		assert.True(t, op == "same" || n == 0, "the update changed resources (%s: %d):\n%s", op, n, up.StdOut)
	}
}

// assertDefaultProvider checks that the stack's only docker-agent provider is the default
// provider of a version (default_0_1_1 for 0.1.1, possibly followed by the plugin download URL).
func assertDefaultProvider(t *testing.T, pt *pulumitest.PulumiTest, version string) {
	t.Helper()
	var state struct {
		Resources []stateResource `json:"resources"`
	}
	require.NoError(t, json.Unmarshal(pt.ExportStack(t).Deployment, &state))
	var names []string
	for _, r := range state.Resources {
		if r.Type == "pulumi:providers:"+providerName && !r.Delete {
			names = append(names, r.URN[strings.LastIndex(r.URN, "::")+2:])
		}
	}
	want := "default_" + strings.NewReplacer(".", "_", "-", "_").Replace(version)
	require.Len(t, names, 1, "docker-agent providers in the state: %v", names)
	assert.True(t, names[0] == want || strings.HasPrefix(names[0], want+"_"), "provider %s, want %s", names[0], want)
}

// userMessages returns the user message of every request the fake model got.
func userMessages(llm *fakeLLM) []string {
	var out []string
	for _, r := range llm.received() {
		out = append(out, strings.TrimSpace(r.User))
	}
	return out
}

// stateIDs returns the IDs of the AgentRuns in the stack's state, by resource name.
func stateIDs(t *testing.T, pt *pulumitest.PulumiTest) map[string]string {
	t.Helper()
	ids := map[string]string{}
	for name, r := range agentRunsInState(t, pt.ExportStack(t).Deployment) {
		ids[name] = r.ID
	}
	return ids
}
