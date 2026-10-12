// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package examples

import (
	"path/filepath"
	"testing"

	"github.com/pulumi/providertest/pulumitest"
	"github.com/pulumi/providertest/pulumitest/opttest"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/optpreview"
	"github.com/stretchr/testify/assert"
)

// TestNullMapValuesE2E deploys testdata/null-values-ts, a TypeScript program that passes null as
// map values (an AgentConfig toolset's env, an AgentRun's env); the SDK sends them as null. The
// provider used to panic while it decoded them; now they are absent keys. (Pulumi YAML drops
// `X: null` from a map before it reaches the provider.)
func TestNullMapValuesE2E(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	const program = "testdata/null-values-ts"
	llm := newFakeLLM(t)
	pt := pulumitest.NewPulumiTest(t, filepath.FromSlash(program),
		opttest.LocalProviderPath(providerName, filepath.Join(root, "bin")),
		opttest.SkipInstall(),
		opttest.SkipStackCreate(),
		opttest.RequireYarnLinks(false),
	)
	useLocalSDK(t, program, pt.WorkingDir(), root)
	pt.NewStack(t, "")
	installDependencies(t, pt.WorkingDir())
	pt.SetConfig(t, "baseUrl", llm.URL)

	pt.Preview(t)
	up := pt.Up(t)
	yaml := stringOutput(t, up.Outputs, "yaml")
	assert.Contains(t, yaml, "KEPT: kept")
	assert.NotContains(t, yaml, "DROPPED", "a null map value is an absent key")
	assert.Equal(t, []any{"NULLS_API_KEY"}, outputValue(t, up.Outputs, "envKeys"))
	assert.Len(t, llm.received(), 1, "the run ran")
	// Null and absent are the same: the next preview has nothing to change.
	pt.Preview(t, optpreview.ExpectNoChanges())
	assert.Len(t, llm.received(), 1)
	pt.Destroy(t)
}
