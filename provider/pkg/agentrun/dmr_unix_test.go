// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

//go:build linux || darwin

package agentrun

import (
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeDocker puts a `docker` on PATH that answers `docker model status --json` with the fake's
// endpoint, has no model (inspect fails), and pulls it in pullSeconds, logging its arguments. The
// pull replaces the shell (exec), so that the timeout's kill ends it: a process that kept the
// output pipe open would hold up docker-agent's pull until it ends.
func fakeDocker(t *testing.T, endpoint string, pullSeconds int) (argsLog string) {
	t.Helper()
	dir := t.TempDir()
	argsLog = filepath.Join(dir, "args.log")
	script := `#!/bin/sh
echo "$*" >> '` + argsLog + `'
for a in "$@"; do
  case "$a" in
    status) echo '{"running":true,"endpoint":"` + endpoint + `"}'; exit 0 ;;
    inspect) exit 1 ;;
    pull) echo "downloading 50%"; exec sleep ` + strconv.Itoa(pullSeconds) + ` ;;
  esac
done
exit 0
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0o755)) //nolint:gosec // a test script
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return argsLog
}

// TestModelRunner: a Docker Model Runner model without a base_url goes through the Docker CLI's
// connection (the context of DOCKER_CONFIG), its pull is reported and logged instead of written to
// the provider's stdout, and it counts against timeoutSeconds (D7).
func TestModelRunner(t *testing.T) {
	isolation := isolateProviderEnv(t)
	f := newFakeModels(t, providerScript)
	cfgDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cfgDir, "config.json"), []byte(`{"currentContext":"fakectx"}`), 0o600))
	for _, k := range []string{"DOCKER_HOST", "DOCKER_CONTEXT", "DOCKER_TLS", "DOCKER_TLS_VERIFY", "DOCKER_CERT_PATH"} {
		t.Setenv(k, "")
		require.NoError(t, os.Unsetenv(k))
	}
	t.Setenv("DOCKER_CONFIG", cfgDir)
	doc := []byte(agentWith("dmr/ai/smollm2"))

	t.Run("pull", func(t *testing.T) {
		argsLog := fakeDocker(t, f.URL+"/engines/v1/", 0)
		var mu sync.Mutex
		var debug, progress []string
		res, err := Run(t.Context(), Request{Name: "dmr", YAML: doc, Prompt: "Say hello", Env: maps.Clone(isolation),
			Progress: func(p Progress) { progress = append(progress, p.Message) },
			Log: func(_ slog.Level, msg string) {
				mu.Lock()
				defer mu.Unlock()
				debug = append(debug, msg)
			}})
		require.NoError(t, err)
		assert.Equal(t, "Hello from the fake: Say hello", res.Output)
		logged, err := os.ReadFile(argsLog)
		require.NoError(t, err)
		assert.Contains(t, string(logged), "--config="+cfgDir+" --context=fakectx model pull ai/smollm2",
			"docker model runs with docker agent run's connection")
		assert.Contains(t, strings.Join(progress, "\n"), "pulling the Docker Model Runner model ai/smollm2 (within timeoutSeconds)")
		mu.Lock()
		assert.Contains(t, strings.Join(debug, "\n"), "docker model pull: downloading 50%")
		mu.Unlock()
	})

	t.Run("a pull that outlasts timeoutSeconds", func(t *testing.T) {
		fakeDocker(t, f.URL+"/engines/v1/", 9)
		start := time.Now()
		_, err := Run(t.Context(), Request{Name: "dmr", YAML: doc, Prompt: "Say hello", Env: maps.Clone(isolation),
			Timeout: time.Second})
		require.Error(t, err)
		assert.Less(t, time.Since(start), 8*time.Second)
		assert.Equal(t, "the agent did not finish within 1s (timeoutSeconds): it timed out while pulling the Docker Model "+
			"Runner model ai/smollm2; pull it first (docker model pull ai/smollm2) or raise timeoutSeconds", err.Error())
	})
}
