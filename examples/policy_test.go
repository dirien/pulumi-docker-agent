// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package examples

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/pulumi/providertest/pulumitest"
	"github.com/pulumi/providertest/pulumitest/opttest"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/optpreview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The policies of the pack in policy/, by default enforcement level.
var (
	mandatoryPolicies = []string{
		"agentrun-tool-approval-deny",
		"agentconfig-no-autonomous-safety",
		"agentconfig-no-literal-credentials",
		"permissions-no-wildcard-allow",
		"usersettings-no-autonomous-safety",
	}
	advisoryPolicies = []string{
		"agentrun-reference-pinned",
		"agentconfig-sandbox-for-local-tools",
	}
	allowlistPolicies = []string{
		"agentconfig-allowed-model-providers",
		"agentconfig-allowed-mcp-refs",
		"agentartifact-allowed-registries",
	}
)

// TestPolicyPack previews programs with the policy pack in policy/ (`pulumi preview
// --policy-pack <repo>/policy`): the fixture testdata/policy-violations must fail and name every
// mandatory policy, the allowlist policies must report it once they are enabled, the remediation
// must fix toolApproval, and examples/yaml must pass. Previews need no registry and no model API
// key. The test installs the pack's dependencies (policy/node_modules) with npm.
func TestPolicyPack(t *testing.T) {
	root := repoRoot(t)
	pack := policyPackDir(t, root)

	t.Run("violations", func(t *testing.T) {
		t.Parallel()
		out, err := previewWithPolicies(t, root, "testdata/policy-violations", pack, "")
		require.Error(t, err, "the preview must fail on mandatory policy violations:\n%s", out)
		want := map[string]string{}
		for _, name := range mandatoryPolicies {
			want[name] = "mandatory"
		}
		for _, name := range advisoryPolicies {
			want[name] = "advisory"
		}
		// The allowlist policies are disabled by default.
		assert.Equal(t, want, reportedPolicies(out), "reported policies:\n%s", out)
		assert.Contains(t, out, `AgentRun "unattended-run" sets toolApproval "allow"`)
		assert.Contains(t, out, `AgentRun "hooked-run" sets allowHookApprovals`)
		assert.Contains(t, out, `uses the "autonomous" safety mode (agent "root")`)
		assert.Contains(t, out, "agents[0].toolsets[1].env.GITHUB_PERSONAL_ACCESS_TOKEN")
		assert.NotContains(t, out, "literal-token-value-0123456789", "the output repeats the credential")
		assert.Contains(t, out, `AgentRun "tagged-run" runs an agent from registry "ghcr.io" by tag`)
		assert.Contains(t, out, `permissions.allow[0] ("*" matches every tool)`)
		assert.Contains(t, out, `settings.permissions.allow[0] ("shell" approves every call of shell)`)
		assert.Contains(t, out, `UserSettings "user-settings" makes docker-agent approve every tool call without asking `+
			`(settings.safety, alias "pirate")`)
	})

	t.Run("allowlists", func(t *testing.T) {
		t.Parallel()
		config := writePolicyConfig(t, map[string]any{
			"agentconfig-allowed-model-providers": map[string]any{
				"enforcementLevel": "mandatory", "allowedProviders": []string{"anthropic"},
			},
			"agentconfig-allowed-mcp-refs": map[string]any{
				"enforcementLevel": "mandatory", "allowedRefs": []string{"docker:duckduckgo"},
			},
			"agentartifact-allowed-registries": map[string]any{
				"enforcementLevel": "mandatory", "allowedRegistries": []string{"ghcr.io/example"},
			},
			// Fix toolApproval and allowHookApprovals instead of failing on them.
			"agentrun-tool-approval-deny": "remediate",
		})
		out, err := previewWithPolicies(t, root, "testdata/policy-violations", pack, config)
		require.Error(t, err, "the preview must fail on the allowlist violations:\n%s", out)
		want := map[string]string{
			// The remediation set toolApproval to "deny" and allowHookApprovals to false, so the
			// validation passes.
			"agentrun-tool-approval-deny": "remediate",
		}
		for _, name := range append([]string{"agentconfig-no-autonomous-safety", "agentconfig-no-literal-credentials",
			"permissions-no-wildcard-allow", "usersettings-no-autonomous-safety"}, allowlistPolicies...) {
			want[name] = "mandatory"
		}
		for _, name := range advisoryPolicies {
			want[name] = "advisory"
		}
		assert.Equal(t, want, reportedPolicies(out), "reported policies:\n%s", out)
		assert.Contains(t, out, `openai by model "gpt"`)
		assert.Contains(t, out, `ref "docker:github-official"`)
		assert.Contains(t, out, `AgentArtifact "artifact" uses a reference in registry "docker.io" that is not allowed`)
		assert.NotContains(t, out, "unsafe-agent:v1", "the message names the registry only")
		// The tagged run pulls from ghcr.io/example, which is allowed.
		assert.NotContains(t, out, `AgentRun "tagged-run" uses`)
		// The remediation set toolApproval to "deny" and allowHookApprovals to false.
		assert.NotContains(t, out, `sets toolApproval "allow"`)
		assert.NotContains(t, out, `sets allowHookApprovals`)
	})

	t.Run("yaml example passes", func(t *testing.T) {
		t.Parallel()
		out, err := previewWithPolicies(t, root, "yaml", pack, "")
		require.NoError(t, err, "examples/yaml must pass the policy pack:\n%s", out)
		assert.Empty(t, reportedPolicies(out), "examples/yaml violates policies:\n%s", out)
		assert.Contains(t, out, "docker-agent-policies@v0.1.0", "the policy pack was not loaded:\n%s", out)
	})
}

// previewWithPolicies runs `pulumi preview --policy-pack pack` (and `--policy-pack-config config`
// when set) on a copy of the program in dir and returns the combined output.
func previewWithPolicies(t *testing.T, root, dir, pack, config string) (string, error) {
	t.Helper()
	start := time.Now()
	pt := pulumitest.NewPulumiTest(t, dir,
		opttest.LocalProviderPath(providerName, filepath.Join(root, "bin")),
		opttest.SkipInstall(),
		opttest.SkipStackCreate(),
	)
	pt.NewStack(t, "")
	var buf bytes.Buffer
	opts := []optpreview.Option{
		optpreview.PolicyPacks(pack),
		optpreview.ProgressStreams(&buf),
		optpreview.ErrorProgressStreams(&buf),
		optpreview.Color("never"),
	}
	if config != "" {
		opts = append(opts, optpreview.PolicyPackConfigs(config))
	}
	_, err := pt.CurrentStack().Preview(pt.Context(), opts...)
	out := buf.String()
	if err != nil {
		// The automation API puts stdout and stderr of a failed command into the error.
		out += "\n" + err.Error()
	}
	t.Logf("[%s] preview with policies: %s", dir, time.Since(start).Round(time.Millisecond))
	return out, err
}

// policyPackDir returns the policy pack directory after installing its dependencies, which
// include the nodejs SDK built by `make build_nodejs`.
func policyPackDir(t *testing.T, root string) string {
	t.Helper()
	requireFile(t, filepath.Join(root, "sdk", "nodejs", "bin", "package.json"), "make build_nodejs")
	dir := filepath.Join(root, "policy")
	start := time.Now()
	cmd := exec.Command("npm", "install", "--no-audit", "--no-fund")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "npm install in %s:\n%s", dir, out)
	t.Logf("[policy] npm install: %s", time.Since(start).Round(time.Millisecond))
	return dir
}

// writePolicyConfig writes a policy pack configuration file (`--policy-pack-config`).
func writePolicyConfig(t *testing.T, config map[string]any) string {
	t.Helper()
	data, err := json.MarshalIndent(config, "", "  ")
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "policy-config.json")
	require.NoError(t, os.WriteFile(path, data, 0o600))
	return path
}

// policyLine matches a policy result in the summary of a preview, for example
// "- [mandatory]  agentrun-tool-approval-deny  (docker-agent:index:AgentRun: run)".
var policyLine = regexp.MustCompile(`(?m)^\s*- \[(mandatory|advisory|remediate)\]\s+(\S+)\s+\(`)

// reportedPolicies returns the policies a preview reported, with their enforcement level.
func reportedPolicies(out string) map[string]string {
	got := map[string]string{}
	for _, m := range policyLine.FindAllStringSubmatch(out, -1) {
		got[m[2]] = m[1]
	}
	return got
}
