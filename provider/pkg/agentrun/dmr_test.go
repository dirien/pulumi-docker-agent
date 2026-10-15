// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package agentrun

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDockerConnectionArgs: the Docker CLI's connection, as docker agent run selects it without
// flags (cmd/root/dmr.go dmrDockerArgs).
func TestDockerConnectionArgs(t *testing.T) {
	cfgDir := t.TempDir()
	for _, k := range []string{"DOCKER_HOST", "DOCKER_CONTEXT", "DOCKER_TLS", "DOCKER_TLS_VERIFY", "DOCKER_CERT_PATH"} {
		t.Setenv(k, "")
		require.NoError(t, os.Unsetenv(k))
	}
	t.Setenv("DOCKER_CONFIG", cfgDir)
	args := func() []string {
		t.Helper()
		a, err := dockerConnectionArgs()
		require.NoError(t, err)
		return a
	}
	assert.Equal(t, []string{"--config=" + cfgDir, "--context=default"}, args())

	require.NoError(t, os.WriteFile(filepath.Join(cfgDir, "config.json"), []byte(`{"currentContext":"colima"}`), 0o600))
	assert.Equal(t, []string{"--config=" + cfgDir, "--context=colima"}, args(), "the config's current context")

	t.Setenv("DOCKER_CONTEXT", "orbstack")
	assert.Equal(t, []string{"--config=" + cfgDir, "--context=orbstack"}, args())

	t.Setenv("DOCKER_HOST", "unix:///var/run/other.sock")
	assert.Equal(t, []string{"--config=" + cfgDir, "--host=unix:///var/run/other.sock"}, args(), "DOCKER_HOST wins")

	certs := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(certs, "ca.pem"), nil, 0o600))
	t.Setenv("DOCKER_TLS_VERIFY", "1")
	t.Setenv("DOCKER_CERT_PATH", certs)
	assert.Equal(t, []string{"--config=" + cfgDir, "--host=unix:///var/run/other.sock",
		"--tlscacert=" + filepath.Join(certs, "ca.pem"), "--tlscert=", "--tlskey="}, args(),
		"missing client certificates are left empty, as docker does")
}

func TestPullOutput(t *testing.T) {
	var debug, started []string
	w := &pullOutput{debug: func(s string) { debug = append(debug, s) }, started: func(m string) { started = append(started, m) }}
	_, _ = w.Write([]byte("Pulling model ai/smollm2...\n\x1b[2Kdownloading 10%\rdownloading 50%"))
	_, _ = w.Write([]byte("\r\nModel ai/smollm2 pulled successfully.\n"))
	assert.Equal(t, []string{"ai/smollm2"}, started)
	assert.Empty(t, w.pulling(), "the pull finished: a later timeout is not the pull's")
	_, _ = w.Write([]byte("Pulling model ai/other...\n"))
	assert.Equal(t, "ai/other", w.pulling(), "a pull that started and did not finish")
	assert.Equal(t, "docker model pull: Pulling model ai/smollm2...\ndocker model pull: downloading 10%\n"+
		"docker model pull: downloading 50%\ndocker model pull: Model ai/smollm2 pulled successfully.\n"+
		"docker model pull: Pulling model ai/other...", strings.Join(debug, "\n"))
}
