// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package agentsignore

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/docker/docker-agent/pkg/fsx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseInvertsRender checks that Parse returns the inputs a rendered file was written
// from.
func TestParseInvertsRender(t *testing.T) {
	t.Parallel()
	cases := []struct {
		comment  string
		patterns []string
	}{
		{"", []string{}},
		{"", []string{"a", "!b", `\#literal`}},
		{"only a note", []string{}},
		{"Managed by Pulumi.", []string{".env"}},
		{"Managed by Pulumi.\n\nSecrets below.", []string{".env", "*.pem", "build/"}},
		{"  indented\n#hash", []string{"docs/**/*.md"}},
	}
	for _, c := range cases {
		content := Render(c.comment, c.patterns)
		got := Parse(content)
		assert.Equal(t, Parsed{Comment: c.comment, Patterns: c.patterns}, got, "%q", content)
		assert.Equal(t, content, Render(got.Comment, got.Patterns))
	}
}

// TestParseReadsLikeDockerAgent checks hand-written files: the patterns are exactly the
// globs docker-agent reads from the file.
func TestParseReadsLikeDockerAgent(t *testing.T) {
	t.Parallel()
	for name, content := range map[string]string{
		"blank lines and indentation": "\n\n# Secrets\n\n  .env  \n\t*.pem\n\n",
		"comments between patterns":   "# header\n.env\n# certificates\n*.pem\n   # indented comment\nbuild/\n",
		"windows line endings":        "# header\r\n\r\n.env\r\n*.pem\r\n",
		"no trailing newline":         ".env\n*.pem",
		"only comments":               "# nothing\n#\n",
	} {
		file := filepath.Join(t.TempDir(), FileName)
		require.NoError(t, os.WriteFile(file, []byte(content), 0o644))
		want, err := fsx.ReadAgentsIgnoreGlobs(file)
		require.NoError(t, err)
		got := Parse(content)
		if len(want) == 0 {
			assert.Empty(t, got.Patterns, name)
		} else {
			assert.Equal(t, want, got.Patterns, name)
		}
	}

	got := Parse("\n# Secrets\n#   indented note\n\n.env\n# certificates\n*.pem\n")
	assert.Equal(t, Parsed{Comment: "Secrets\n  indented note", Patterns: []string{".env", "*.pem"}, DroppedComments: 1}, got)
}
