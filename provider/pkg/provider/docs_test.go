// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// exampleLanguages are the languages of the Registry's language chooser, in its order. Every
// example has exactly one code block per language; a missing one shows "Example coming soon!".
var exampleLanguages = []string{"typescript", "python", "go", "csharp", "java", "yaml", "hcl"}

var (
	exampleBlockRE = regexp.MustCompile(`(?s)\{\{% example %\}\}(.*?)\{\{% /example %\}\}`)
	fenceRE        = regexp.MustCompile("(?m)^```(\\w*)$")
)

// fencedLanguages returns the language of every code block of an example, in order.
func fencedLanguages(t *testing.T, example string) []string {
	t.Helper()
	fences := fenceRE.FindAllStringSubmatch(example, -1)
	require.Equal(t, 0, len(fences)%2, "unbalanced code fences:\n%s", example)
	var langs []string
	for i := 0; i < len(fences); i += 2 {
		require.NotEmpty(t, fences[i][1], "a code block without a language:\n%s", example)
		require.Empty(t, fences[i+1][1], "a code block that does not end with ```:\n%s", example)
		langs = append(langs, fences[i][1])
	}
	return langs
}

// checkExamples asserts that a description has an Example Usage section with at least one
// example, each with a title and one code block per language.
func checkExamples(t *testing.T, token, description string) {
	t.Helper()
	require.Equal(t, 1, strings.Count(description, "{{% examples %}}"), "%s: one {{%% examples %%}} section", token)
	require.Contains(t, description, "{{% examples %}}\n## Example Usage\n", token)
	examples := exampleBlockRE.FindAllStringSubmatch(description, -1)
	require.NotEmpty(t, examples, "%s has no {{%% example %%}}", token)
	require.Equal(t, len(examples), strings.Count(description, "{{% example %}}"), "%s: unterminated example", token)
	for _, m := range examples {
		body := m[1]
		require.Regexp(t, `^\n### \S.*\n\n`+"```", body, "%s: an example starts with its ### title", token)
		assert.Equal(t, exampleLanguages, fencedLanguages(t, body), "%s: the languages of %q", token,
			strings.SplitN(strings.TrimPrefix(body, "\n### "), "\n", 2)[0])
		assert.NotContains(t, body, "Example coming soon", token)
	}
}

// TestSchemaExamples checks the Example Usage section of every resource and function in the
// schema the provider serves: the generated Markdown of provider/internal/docsgen is embedded
// and complete.
func TestSchemaExamples(t *testing.T) {
	t.Parallel()
	resp, err := server(t).GetSchema(p.GetSchemaRequest{})
	require.NoError(t, err)
	var schema struct {
		Resources map[string]struct{ Description string } `json:"resources"`
		Functions map[string]struct{ Description string } `json:"functions"`
	}
	require.NoError(t, json.Unmarshal([]byte(resp.Schema), &schema))
	require.Len(t, schema.Resources, 5)
	require.Len(t, schema.Functions, 2)
	for token, r := range schema.Resources {
		t.Run(token, func(t *testing.T) {
			t.Parallel()
			checkExamples(t, token, r.Description)
			// The Registry shows everything after "## Import" in the resource's Import section.
			imp := strings.SplitN(r.Description, "\n\n## Import\n\n", 2)
			require.Len(t, imp, 2, "%s has no import section", token)
			assert.NotContains(t, imp[1], "{{% example", "%s: the import section comes after the examples", token)
			if token != agentRunToken {
				assert.Contains(t, imp[1], "```sh\n$ pulumi import "+token+" ", token)
			}
		})
	}
	for token, f := range schema.Functions {
		t.Run(token, func(t *testing.T) {
			t.Parallel()
			checkExamples(t, token, f.Description)
			assert.NotContains(t, f.Description, "## Import", token)
		})
	}
}

// TestCommittedSchemaExamples checks the committed schema.json the same way, so that the
// Registry, which reads it, gets every language.
func TestCommittedSchemaExamples(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join("..", "..", "cmd", "pulumi-resource-docker-agent", "schema.json"))
	require.NoError(t, err)
	var schema struct {
		Resources map[string]struct{ Description string } `json:"resources"`
		Functions map[string]struct{ Description string } `json:"functions"`
	}
	require.NoError(t, json.Unmarshal(b, &schema))
	for token, r := range schema.Resources {
		checkExamples(t, token, r.Description)
	}
	for token, f := range schema.Functions {
		checkExamples(t, token, f.Description)
	}
}

// TestOverviewExamples checks the generated language choosers of docs/_index.md.
func TestOverviewExamples(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "_index.md"))
	require.NoError(t, err)
	regions := regexp.MustCompile(`(?s)<!-- docsgen begin: ([\w-]+) [^\n]*-->\n(.*?)<!-- docsgen end -->`).
		FindAllStringSubmatch(string(b), -1)
	require.NotEmpty(t, regions)
	chooser := `{{< chooser language "` + strings.Join(exampleLanguages, ",") + `" >}}`
	for _, m := range regions {
		name, body := m[1], m[2]
		assert.True(t, strings.HasPrefix(body, chooser+"\n"), "%s: the chooser lists every language", name)
		var choosables []string
		for _, c := range regexp.MustCompile(`\{\{% choosable language (\w+) %\}\}`).FindAllStringSubmatch(body, -1) {
			choosables = append(choosables, c[1])
		}
		assert.Equal(t, exampleLanguages, choosables, name)
		assert.Equal(t, exampleLanguages, fencedLanguages(t, body), name)
	}
	// Every language chooser of the page shows every language.
	for _, c := range regexp.MustCompile(`\{\{< chooser language "([^"]+)" >\}\}`).FindAllStringSubmatch(string(b), -1) {
		assert.True(t, slices.Equal(strings.Split(c[1], ","), exampleLanguages), "chooser %q", c[1])
	}
}
