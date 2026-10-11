// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package ociagent

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCompat checks interoperability with the real docker-agent CLI against a registry:2
// container:
//
//   - artifacts pushed by this package are accepted by `docker agent share pull` and
//     `docker agent debug config`, by tag and by digest;
//   - artifacts pushed by `docker agent share push` are read by Pull.
//
// It is skipped in -short mode and when docker-agent or Docker are not available.
func TestCompat(t *testing.T) {
	if testing.Short() {
		t.Skip("compatibility test skipped in -short mode")
	}
	cli, err := exec.LookPath("docker-agent")
	if err != nil {
		t.Skip("docker-agent is not on PATH")
	}
	if err := exec.Command("docker", "info").Run(); err != nil {
		t.Skip("Docker is not available")
	}

	host := startRegistry(t)
	ctx := t.Context()

	t.Run("provider push, CLI pull", func(t *testing.T) {
		ref := host + "/compat/provider:v1"
		a, err := Build([]byte(sampleYAML), Annotations([]byte(sampleYAML), FormatCreated(time.Now()), nil))
		require.NoError(t, err)
		res, err := Push(ctx, ref, a, Options{})
		require.NoError(t, err)

		for _, r := range []string{ref, res.PinnedReference} {
			dir := t.TempDir()
			out := runCLI(t, cli, dir, "share", "pull", r)
			assert.Contains(t, out, "Agent saved to")

			// share pull writes <ref with / replaced by _>.yaml into the working directory.
			file := filepath.Join(dir, strings.ReplaceAll(r, "/", "_")+".yaml")
			got, err := os.ReadFile(file)
			require.NoError(t, err, "share pull output:\n%s", out)
			assert.Equal(t, sampleYAML, string(got))

			out = runCLI(t, cli, t.TempDir(), "debug", "config", r)
			assert.Contains(t, out, "root:")
			assert.Contains(t, out, "openai/gpt-5-mini")
		}
	})

	t.Run("CLI push, provider pull", func(t *testing.T) {
		ref := host + "/compat/cli:v1"
		dir := t.TempDir()
		file := filepath.Join(dir, "agent.yaml")
		require.NoError(t, os.WriteFile(file, []byte(sampleYAML), 0o600))
		runCLI(t, cli, dir, "share", "push", file, ref)

		pulled, err := Pull(ctx, ref, Options{})
		require.NoError(t, err)
		assert.Equal(t, sampleYAML, string(pulled.YAML))
		assert.Equal(t, DockerAgentVersion, pulled.Annotations[AnnotationVersion])
		assert.Equal(t, "pulumi", pulled.Annotations[AnnotationAuthors])

		// The CLI's layer and config are byte-identical to ours; only the timestamp differs.
		a, err := Build([]byte(sampleYAML), Annotations([]byte(sampleYAML), pulled.Annotations[AnnotationCreated], nil))
		require.NoError(t, err)
		assert.Equal(t, string(a.Manifest), string(pulled.Manifest))
		assert.Equal(t, a.Digest.String(), pulled.Digest)

		digest, found, err := Head(ctx, ref, Options{})
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, pulled.Digest, digest)
	})

	t.Run("registry:2 rejects delete with 405", func(t *testing.T) {
		ref := host + "/compat/delete:v1"
		a, err := Build([]byte(sampleYAML), Annotations([]byte(sampleYAML), FormatCreated(time.Now()), nil))
		require.NoError(t, err)
		res, err := Push(ctx, ref, a, Options{})
		require.NoError(t, err)
		out, err := Delete(ctx, ref, res.Digest, Options{})
		require.NoError(t, err)
		assert.False(t, out.Deleted)
		assert.Contains(t, out.Warning, "does not support deleting manifests")
	})
}

// runCLI runs docker-agent with an isolated HOME, telemetry off, in dir.
func runCLI(t *testing.T, cli, dir string, args ...string) string {
	t.Helper()
	home := t.TempDir()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, cli, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"HOME="+home,
		"TELEMETRY_ENABLED=false",
		"DOCKER_AGENT_DATA_DIR="+filepath.Join(home, "data"),
		"DOCKER_CONFIG="+filepath.Join(home, "docker"),
		"NO_COLOR=1",
	)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	require.NoError(t, err, "docker-agent %s failed:\n%s", strings.Join(args, " "), out.String())
	return out.String()
}

// startRegistry runs registry:2 on a free localhost port and returns localhost:<port>.
func startRegistry(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := l.Addr().(*net.TCPAddr).Port
	require.NoError(t, l.Close())

	name := fmt.Sprintf("pulumi-docker-agent-compat-%d", port)
	out, err := exec.Command("docker", "run", "-d", "--rm", "--name", name,
		"-p", fmt.Sprintf("127.0.0.1:%d:5000", port), "registry:2").CombinedOutput()
	require.NoError(t, err, "starting registry:2: %s", out)
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", name).Run() })

	host := fmt.Sprintf("localhost:%d", port)
	deadline := time.Now().Add(30 * time.Second)
	for {
		resp, err := http.Get("http://" + host + "/v2/") //nolint:noctx // test helper
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return host
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("registry:2 on %s did not become ready: %v", host, err)
		}
		time.Sleep(200 * time.Millisecond)
	}
}
