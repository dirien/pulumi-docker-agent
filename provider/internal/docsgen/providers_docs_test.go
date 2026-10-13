// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	modelprovider "github.com/docker/docker-agent/pkg/model/provider"
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

	for _, name := range modelprovider.CoreProviders {
		assert.Contains(t, rows, name, "core provider %q has no row in the credentials table", name)
	}
	names := make([]string, 0, len(modelprovider.Aliases))
	for name := range modelprovider.Aliases {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		alias := modelprovider.Aliases[name]
		row, ok := rows[name]
		if !assert.True(t, ok, "alias %q has no row in the credentials table", name) {
			continue
		}
		if alias.TokenEnvVar != "" {
			assert.Contains(t, row, "`"+alias.TokenEnvVar+"`", "the row of alias %q names its key variable", name)
		}
		if alias.BaseURL != "" {
			assert.Contains(t, row, "`"+alias.BaseURL+"`", "the row of alias %q names its default endpoint", name)
		}
	}
}
