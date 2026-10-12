// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeTree writes files (slash-separated paths relative to dir).
func writeTree(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		file := filepath.Join(dir, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(file), 0o755))
		require.NoError(t, os.WriteFile(file, []byte(content), 0o644))
	}
}

// syntheticUpstream writes an upstream examples directory with an HCL example that reads a
// Markdown file and a text file in subdirectories.
func syntheticUpstream(t *testing.T, hclBody string) string {
	t.Helper()
	dir := t.TempDir()
	writeTree(t, dir, map[string]string{
		"basic.yaml":        "agents:\n  root:\n    model: openai/gpt-5-mini\n    instruction: Be brief.\n",
		"prompted.hcl":      hclBody,
		"prompts/system.md": "You answer questions about Pulumi.\n",
		"notes/welcome.txt": "Hi! Ask me about Pulumi.",
		otherFilesList:      "# The example files that are not copied here.\n",
	})
	return dir
}

const promptedHCL = `agent "root" {
  model           = "openai/gpt-5-mini"
  instruction     = file("prompts/system.md")
  welcome_message = file("notes/welcome.txt")
}
`

// TestHCLReadsInSubdirectories checks that the .md and .txt files an HCL example reads from
// subdirectories are copied next to both programs, that write removes stale copies (and the
// directories they leave empty) but no project file, and that the programs read the copies.
func TestHCLReadsInSubdirectories(t *testing.T) {
	t.Parallel()
	files, _, err := generate(syntheticUpstream(t, promptedHCL))
	require.NoError(t, err)
	for _, lang := range []string{"typescript", "python"} {
		assert.Equal(t, "You answer questions about Pulumi.\n", string(files[lang+"/examples/prompts/system.md"]))
		assert.Equal(t, "Hi! Ask me about Pulumi.", string(files[lang+"/examples/notes/welcome.txt"]))
	}
	assert.Contains(t, string(files["typescript/examples/prompted.ts"]), `"prompts/system.md"`)
	assert.Contains(t, string(files["python/examples/prompted.py"]), `"notes/welcome.txt"`)
	for rel := range files {
		assert.True(t, generatedFile(rel), "%s is generated but generatedFile rejects it", rel)
	}

	out := t.TempDir()
	writeTree(t, out, map[string]string{
		"typescript/package.json":             "{}",
		"python/requirements.txt":             "pulumi\n",
		"typescript/examples/old/stale.md":    "stale",
		"python/examples/old/deeper/x.txt":    "stale",
		"python/examples/notes/old.txt":       "stale",
		"typescript/examples/notes/keep.sh":   "not generated",
		"typescript/node_modules/x/README.md": "local state",
	})
	require.NoError(t, write(out, files))
	for rel := range files {
		assert.FileExists(t, filepath.Join(out, filepath.FromSlash(rel)))
	}
	for _, kept := range []string{"typescript/package.json", "python/requirements.txt", "typescript/examples/notes/keep.sh",
		"typescript/node_modules/x/README.md"} {
		assert.FileExists(t, filepath.Join(out, filepath.FromSlash(kept)))
	}
	for _, gone := range []string{"typescript/examples/old", "python/examples/old", "python/examples/notes/old.txt"} {
		assert.NoFileExists(t, filepath.Join(out, filepath.FromSlash(gone)))
		assert.NoDirExists(t, filepath.Join(out, filepath.FromSlash(gone)))
	}
}

// TestHCLReadsThatCannotBeCopied checks that file() paths that would leave the example
// directory, are hidden, or are not text files fail the generation with a clear message.
func TestHCLReadsThatCannotBeCopied(t *testing.T) {
	t.Parallel()
	// docker-agent itself refuses to read outside the configuration's directory.
	for _, read := range []string{"../outside.md", "/etc/passwd"} {
		_, _, err := generate(syntheticUpstream(t, strings.Replace(promptedHCL, "prompts/system.md", read, 1)))
		require.Error(t, err, read)
		assert.Contains(t, err.Error(), "path must be a local relative path", read)
	}
	// Files docker-agent reads, but that the programs cannot read a copy of.
	for _, read := range []string{"prompts/run.sh", ".hidden/system.md", "prompts/sys tem.md", "prompts/data.json"} {
		dir := syntheticUpstream(t, strings.Replace(promptedHCL, "prompts/system.md", read, 1))
		writeTree(t, dir, map[string]string{read: "You answer questions about Pulumi.\n"})
		_, _, err := generate(dir)
		require.Error(t, err, read)
		assert.Contains(t, err.Error(), "cannot be copied next to the programs", read)
	}
}

// TestUpgradeWorkflowAcceptsGeneratedFiles checks that the publish job of
// .github/workflows/upgrade-docker-agent.yml accepts exactly the paths generatedFile accepts
// under examples/docker-agent-examples, so that a regenerated example never gets an upgrade
// rejected and nothing else is let through.
func TestUpgradeWorkflowAcceptsGeneratedFiles(t *testing.T) {
	t.Parallel()
	workflow, err := os.ReadFile(filepath.Join("..", "..", "..", ".github", "workflows", "upgrade-docker-agent.yml"))
	require.NoError(t, err)
	m := regexp.MustCompile(`(?m)^\s*allowed='([^']+)'$`).FindSubmatch(workflow)
	require.NotNil(t, m, "the allowed paths of the upgrade workflow")
	allowed := regexp.MustCompile(string(m[1])) // grep -E syntax that RE2 understands

	files, _ := generated(t)
	synthetic, _, err := generate(syntheticUpstream(t, promptedHCL))
	require.NoError(t, err)
	var paths []string
	for rel := range files {
		paths = append(paths, rel)
	}
	for rel := range synthetic {
		paths = append(paths, rel)
	}
	paths = append(paths,
		"typescript/examples/a/b/c.txt", "python/examples/a-b/c_d.e.md",
		"typescript/examples/../x.md", "typescript/examples/.hidden/x.md", "typescript/examples/a//x.md",
		"typescript/examples/x.sh", "typescript/examples/sub/x.ts", "python/examples/sub/x.py",
		"typescript/package.json", "python/requirements.txt", "python/Pulumi.yaml", "typescript/.npmrc",
		"typescript/examples/x y.md", "testdata/sub/x.yaml", "README.txt",
	)
	slices.Sort(paths)
	for _, rel := range slices.Compact(paths) {
		assert.Equal(t, generatedFile(rel), allowed.MatchString("examples/docker-agent-examples/"+rel),
			"generatedFile and the upgrade workflow disagree about %s", rel)
	}
}
