// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package agentsignore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docker/docker-agent/pkg/fsx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRender(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "", Render("", nil))
	assert.Equal(t, "a\n!b\n", Render("", []string{"a", "!b"}))
	assert.Equal(t, "# Managed by Pulumi.\n#\n# Secrets below.\n\n.env\n",
		Render("Managed by Pulumi.\n\nSecrets below.\n", []string{".env"}))
	assert.Equal(t, "# only a note\n", Render("only a note", nil))
	assert.Equal(t, Render("x\r\ny", []string{"a"}), Render("x\ny", []string{"a"}))
	assert.Len(t, SHA256(""), 64)
}

// writeProject writes the rendered file plus some project files into a temp dir.
func writeProject(t *testing.T, content string, files ...string) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, FileName), []byte(content), 0o644))
	for _, f := range files {
		p := filepath.Join(dir, filepath.FromSlash(f))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte("x"), 0o644))
	}
	return dir
}

// TestDockerAgentReadsRenderedFile checks the rendered file with docker-agent's own
// reader and matcher (pkg/fsx): the patterns come back unchanged and in order, and they
// hide exactly the documented paths.
func TestDockerAgentReadsRenderedFile(t *testing.T) {
	t.Parallel()
	patterns := []string{"secrets.env", "/root-only.txt", "*.key", "!public.key", ".env.*", "build/", "docs/**/*.draft.md"}
	errs, warnings := Validate(patterns)
	require.Empty(t, errs)
	require.Empty(t, warnings)

	files := []string{
		"secrets.env", "config/secrets.env", "root-only.txt", "sub/root-only.txt", "id.key", "public.key",
		".env.local", "build/out.bin", "docs/a/b/notes.draft.md", "docs/readme.md", "main.go",
	}
	dir := writeProject(t, Render("Managed by Pulumi", patterns), files...)

	globs, err := fsx.ReadAgentsIgnoreGlobs(filepath.Join(dir, FileName))
	require.NoError(t, err)
	assert.Equal(t, patterns, globs)

	m, err := fsx.NewAgentsIgnoreMatcher(dir)
	require.NoError(t, err)
	require.NotNil(t, m)
	ignored := map[string]bool{}
	for _, f := range files {
		ignored[f] = m.Match(filepath.Join(dir, filepath.FromSlash(f)))
	}
	assert.Equal(t, map[string]bool{
		"secrets.env": true, "config/secrets.env": true, "root-only.txt": true, "sub/root-only.txt": false,
		"id.key": true, "public.key": false, ".env.local": true, "build/out.bin": true,
		"docs/a/b/notes.draft.md": true, "docs/readme.md": false, "main.go": false,
	}, ignored)
	assert.True(t, m.Match(filepath.Join(dir, FileName)), "the file itself is always hidden")

	// Our matcher agrees with docker-agent's.
	gm := Matcher(patterns)
	for f, want := range ignored {
		assert.Equal(t, want, gm.Match(strings.Split(f, "/"), false), f)
	}
}

func TestValidateErrors(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"":                 "empty pattern",
		"   ":              "empty pattern",
		" secrets.env":     "leading or trailing whitespace",
		"secrets.env ":     "leading or trailing whitespace",
		"a\nb":             "contains a line break",
		"#secret":          "would be read as a comment",
		"!":                "matches nothing",
		"/":                "matches nothing",
		"!/":               "matches nothing",
		"[abc":             "invalid glob",
		"docs/[a-/x":       "invalid glob",
		"docs/**.md":       "mixes `**` with other characters",
		"a/b**/c":          "mixes `**` with other characters",
		"src/**foo/bar.go": "mixes `**` with other characters",
	}
	for p, want := range cases {
		errs, _ := Validate([]string{"ok.txt", p})
		require.Len(t, errs, 1, "%q", p)
		assert.Equal(t, 1, errs[0].Index)
		assert.Contains(t, errs[0].String(), want, "%q", p)
		assert.True(t, strings.HasPrefix(errs[0].String(), "patterns[1]: "))
	}
}

// TestRejectedPatternsMisbehaveInDockerAgent proves the errors are real: docker-agent
// reads these patterns differently from what was written, or never matches them.
func TestRejectedPatternsMisbehaveInDockerAgent(t *testing.T) {
	t.Parallel()
	dir := writeProject(t, " secrets.env\n#secret\nsecrets.env \n")
	globs, err := fsx.ReadAgentsIgnoreGlobs(filepath.Join(dir, FileName))
	require.NoError(t, err)
	assert.Equal(t, []string{"secrets.env", "secrets.env"}, globs, "whitespace is trimmed and # lines are dropped")

	never := map[string][]string{
		"docs/**.md": {"docs/a.md", "docs/x/a.md"},
		"[abc":       {"[abc", "a"},
		"docs/[a-/x": {"docs/a-/x", "docs/[a-/x"},
	}
	for pattern, paths := range never {
		m := Matcher([]string{pattern})
		for _, p := range paths {
			assert.False(t, m.Match(strings.Split(p, "/"), false), "%q unexpectedly matches %q", pattern, p)
		}
	}
	// `**` inside a simple name (no slash) still works like `*`, so it is accepted.
	errs, _ := Validate([]string{"**.md"})
	assert.Empty(t, errs)
	assert.True(t, Matcher([]string{"**.md"}).Match([]string{"docs", "a.md"}, false))
	// An escaped leading # matches a file whose name starts with #.
	errs, _ = Validate([]string{`\#notes`})
	assert.Empty(t, errs)
	assert.True(t, Matcher([]string{`\#notes`}).Match([]string{"#notes"}, false))
}

func TestValidateWarnings(t *testing.T) {
	t.Parallel()
	errs, warnings := Validate([]string{"*.key", "!public.key", "!other.txt", "*.key", "secret.txt # keep out", "!build/", "build/"})
	require.Empty(t, errs)
	var texts []string
	for _, w := range warnings {
		texts = append(texts, w.String())
	}
	all := strings.Join(texts, "\n")
	assert.Contains(t, all, `patterns[3]: "*.key" repeats patterns[0]`)
	assert.Contains(t, all, `patterns[4]: "secret.txt # keep out": gitignore syntax has no trailing comments`)
	assert.Contains(t, all, `patterns[2]: "!other.txt" re-includes "other.txt", but no earlier pattern excludes it`)
	assert.Contains(t, all, `patterns[5]: "!build/" re-includes "build"`)
	assert.NotContains(t, all, "!public.key", "public.key is excluded by *.key first")

	// A negation that targets a file below an excluded directory is effective in
	// docker-agent (go-git matches the full path).
	_, warnings = Validate([]string{"secrets/", "!secrets/public.pem"})
	assert.Empty(t, warnings)
	assert.Equal(t, 0, len(warnings))
}
