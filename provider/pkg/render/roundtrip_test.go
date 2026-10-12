// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package render

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/docker/docker-agent/pkg/config"
	"github.com/docker/docker-agent/pkg/config/latest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// loadJSON loads a configuration with the docker-agent library and returns the loaded
// config as JSON, the form the round-trip comparisons use.
func loadJSON(t *testing.T, name string, data []byte) (string, error) {
	t.Helper()
	return loadSourceJSON(t, config.NewBytesSource(name, data))
}

func loadSourceJSON(t *testing.T, src config.Source) (string, error) {
	t.Helper()
	cfg, err := config.Load(t.Context(), src)
	if err != nil {
		return "", err
	}
	return configJSON(t, cfg), nil
}

func configJSON(t *testing.T, cfg *latest.Config) string {
	t.Helper()
	b, err := json.MarshalIndent(cfg, "", "  ")
	require.NoError(t, err)
	return string(b)
}

// dirSource is an in-memory configuration in a directory: like a file source, it resolves
// `instruction_file` references relative to that directory.
type dirSource struct {
	name, dir string
	data      []byte
}

func (s dirSource) Name() string                         { return s.name }
func (s dirSource) ParentDir() string                    { return s.dir }
func (s dirSource) Read(context.Context) ([]byte, error) { return s.data, nil }

// upstreamExamples returns the YAML example configurations of docker-agent in testdata/upstream
// (copied from the pinned module by `make generate_config_types`), subdirectories included.
func upstreamExamples(t *testing.T) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(filepath.Join("testdata", "upstream"), func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && filepath.Ext(path) == ".yaml" {
			files = append(files, path)
		}
		return err
	})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(files), 190, "the upstream examples are missing; run make generate_config_types")
	return files
}

// TestRoundTripUpstreamExamples proves that every example configuration docker-agent ships
// can be expressed with the typed inputs: each one is decoded into a Document without
// anything landing in an `extra` map (except the keys docker-agent never reads, see
// UnreadKey), rendered again, and loaded by the docker-agent library. The loaded
// configuration must equal the one loaded from the original file. Both are loaded from the
// example's directory, so `instruction_file` references resolve.
func TestRoundTripUpstreamExamples(t *testing.T) {
	t.Parallel()
	for _, file := range upstreamExamples(t) {
		rel, err := filepath.Rel(filepath.Join("testdata", "upstream"), file)
		require.NoError(t, err)
		t.Run(strings.TrimSuffix(filepath.ToSlash(rel), ".yaml"), func(t *testing.T) {
			t.Parallel()
			orig, err := os.ReadFile(file)
			require.NoError(t, err)
			dir, name := filepath.Dir(file), filepath.Base(file)
			want, err := loadSourceJSON(t, dirSource{name: name, dir: dir, data: orig})
			require.NoError(t, err, "the upstream example does not load")

			dec, err := DecodeYAML(orig)
			require.NoError(t, err)
			for _, p := range dec.Untyped {
				_, unread := UnreadKey(p)
				assert.True(t, unread, "%s has no typed input", p)
			}

			rendered, err := Render(dec.Document)
			require.NoError(t, err)
			got, err := loadSourceJSON(t, dirSource{name: name, dir: dir, data: []byte(rendered)})
			require.NoError(t, err, "the rendered configuration does not load:\n%s", rendered)
			assert.JSONEq(t, want, got, "rendered:\n%s", rendered)
		})
	}
}

// TestUnreadKeysAreNotRead checks the claim behind UnreadKey against the pinned docker-agent
// sources: no RAG strategy reads the parameters that the examples set without effect.
func TestUnreadKeysAreNotRead(t *testing.T) {
	t.Parallel()
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/docker/docker-agent").Output()
	require.NoError(t, err)
	sources, err := filepath.Glob(filepath.Join(strings.TrimSpace(string(out)), "pkg", "rag", "strategy", "*.go"))
	require.NoError(t, err)
	require.NotEmpty(t, sources)
	getParam := regexp.MustCompile(`GetParam\(\s*cfg\.Params,\s*"([^"]+)"`)
	var read []string
	for _, f := range sources {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		require.NoError(t, err)
		for _, m := range getParam.FindAllStringSubmatch(string(b), -1) {
			read = append(read, m[1])
		}
	}
	require.NotEmpty(t, read, "no GetParam calls found; update this test for the new docker-agent sources")
	for _, key := range UnreadKeyNames() {
		assert.False(t, slices.Contains(read, key), "docker-agent now reads %q: give it a typed input and remove it from unreadKeys", key)
	}
}
