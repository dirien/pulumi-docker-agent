// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package render

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docker/docker-agent/pkg/config"
	"github.com/docker/docker-agent/pkg/config/latest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// skippedExamples are upstream examples the in-memory round trip cannot load, with the
// reason.
var skippedExamples = map[string]string{
	// instruction_file is resolved relative to the config file, which a bytes source does
	// not have. The field itself is typed (Agent.instructionFile).
	"instruction_file.yaml": "uses instruction_file, which needs a file-based config source",
}

// loadJSON loads a configuration with the docker-agent library and returns the loaded
// config as JSON, the form the round-trip comparisons use.
func loadJSON(t *testing.T, name string, data []byte) (string, error) {
	t.Helper()
	cfg, err := config.Load(t.Context(), config.NewBytesSource(name, data))
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

// TestRoundTripUpstreamExamples proves that every example configuration docker-agent ships
// (testdata/upstream, copied from the pinned module by `make generate_config_types`) can be
// expressed with the typed inputs: each one is decoded into a Document without anything
// landing in an `extra` map, rendered again, and loaded by the docker-agent library. The
// loaded configuration must equal the one loaded from the original file.
func TestRoundTripUpstreamExamples(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob(filepath.Join("testdata", "upstream", "*.yaml"))
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(files), 150, "the upstream examples are missing; run make generate_config_types")
	for _, file := range files {
		name := filepath.Base(file)
		t.Run(strings.TrimSuffix(name, ".yaml"), func(t *testing.T) {
			t.Parallel()
			if reason, ok := skippedExamples[name]; ok {
				t.Skip(reason)
			}
			orig, err := os.ReadFile(file)
			require.NoError(t, err)
			want, err := loadJSON(t, name, orig)
			require.NoError(t, err, "the upstream example does not load")

			doc, untyped, err := decodeYAML(orig)
			require.NoError(t, err)
			assert.Empty(t, untyped, "keys without a typed input")

			rendered, err := Render(doc)
			require.NoError(t, err)
			got, err := loadJSON(t, name, []byte(rendered))
			require.NoError(t, err, "the rendered configuration does not load:\n%s", rendered)
			assert.JSONEq(t, want, got, "rendered:\n%s", rendered)
		})
	}
}
