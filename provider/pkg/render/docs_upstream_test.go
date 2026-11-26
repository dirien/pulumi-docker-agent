// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package render

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// docsYAMLFence matches a fenced YAML block of a Markdown page.
var docsYAMLFence = regexp.MustCompile("(?ms)^```(?:yaml|yml)[^\n]*\n(.*?)^```")

// TestUpstreamDocsRoundTrip runs every complete agent configuration (a YAML block with a
// top-level `agents:` key) of the documentation that ships with the pinned docker-agent module
// (its docs/ directory, which docs.docker.com vendors) through decode -> Render -> load and
// compares the loaded configurations, like TestDocsExamplesRoundTrip does for the snapshot in
// testdata/docs. It follows every docker-agent upgrade without a snapshot to refresh. A block
// that docker-agent itself does not load (a fragment with placeholders, a file reference) is
// skipped.
func TestUpstreamDocsRoundTrip(t *testing.T) {
	t.Parallel()
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/docker/docker-agent").Output()
	if err != nil {
		t.Skipf("the docker-agent module is not available: %v", err)
	}
	docs := filepath.Join(strings.TrimSpace(string(out)), "docs")
	var pages []string
	require.NoError(t, filepath.WalkDir(docs, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(path, ".md") {
			pages = append(pages, path)
		}
		return err
	}))
	require.NotEmpty(t, pages, "no Markdown pages in %s", docs)

	total := 0
	for _, page := range pages {
		b, err := os.ReadFile(page) //nolint:gosec // a page of the pinned module
		require.NoError(t, err)
		rel, err := filepath.Rel(docs, page)
		require.NoError(t, err)
		for i, m := range docsYAMLFence.FindAllStringSubmatch(string(b), -1) {
			orig := []byte(m[1])
			if !regexp.MustCompile(`(?m)^agents:`).Match(orig) {
				continue
			}
			total++
			name := fmt.Sprintf("%s#%d", strings.TrimSuffix(filepath.ToSlash(rel), "/index.md"), i+1)
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				want, err := loadJSON(t, "agent.yaml", orig)
				if err != nil {
					t.Skipf("docker-agent does not load the docs example: %v", err)
				}
				dec, err := DecodeYAML(orig)
				require.NoError(t, err)
				assert.Empty(t, dec.Untyped, "keys without a typed input")
				rendered, err := Render(dec.Document)
				require.NoError(t, err)
				got, err := loadJSON(t, "agent.yaml", []byte(rendered))
				require.NoError(t, err, "the rendered configuration does not load:\n%s", rendered)
				assert.JSONEq(t, want, got, "rendered:\n%s", rendered)
			})
		}
	}
	require.Positive(t, total, "no complete agent configuration in %s", docs)
}
