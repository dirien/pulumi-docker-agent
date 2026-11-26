// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/docker/docker-agent/pkg/model/provider/catalog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDocsCoverAllModelProviders checks that the credentials tables of "Runtime credentials for
// agents" in docs/installation-configuration.md have a row for every core provider and every
// alias of the pinned docker-agent, with the alias's key variable and default endpoint. A
// docker-agent upgrade that adds an alias, or changes an alias's key or endpoint, fails this test
// until the docs say so (like TestProvidersCoverAllAliases for the provider tests).
func TestDocsCoverAllModelProviders(t *testing.T) {
	root, err := providerRoot()
	require.NoError(t, err)
	b, err := os.ReadFile(filepath.Join(root, "..", "docs", "installation-configuration.md"))
	require.NoError(t, err)
	doc := string(b)
	start := strings.Index(doc, "## Runtime credentials for agents\n")
	require.GreaterOrEqual(t, start, 0, "the section Runtime credentials for agents")
	end := strings.Index(doc[start:], "\n### ")
	require.Positive(t, end)
	section := doc[start : start+end]

	rows := map[string]string{}
	for _, line := range strings.Split(section, "\n") {
		if !strings.HasPrefix(line, "| `") {
			continue
		}
		name, _, _ := strings.Cut(strings.TrimPrefix(line, "| `"), "`")
		rows[name] += line + "\n"
	}

	// The providers with their own API client; every other catalog entry is an alias of an
	// OpenAI-compatible endpoint.
	core := []string{"openai", "anthropic", "google", "amazon-bedrock", "dmr"}
	for _, name := range core {
		assert.Contains(t, rows, name, "core provider %q has no row in the credentials table", name)
	}
	for _, alias := range catalog.Ordered() {
		name := alias.Name
		if slices.Contains(core, name) {
			continue
		}
		row, ok := rows[name]
		if !assert.True(t, ok, "alias %q has no row in the credentials table", name) {
			continue
		}
		if len(alias.TokenEnvVars) > 0 {
			assert.Contains(t, row, "`"+alias.TokenEnvVars[0]+"`", "the row of alias %q names its key variable", name)
		}
		if alias.BaseURL != "" {
			assert.Contains(t, row, "`"+alias.BaseURL+"`", "the row of alias %q names its default endpoint", name)
		}
	}
}
