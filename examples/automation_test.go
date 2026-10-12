// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package examples

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAutomationAPI runs examples/automation-ts, a TypeScript Automation API script with an inline
// program, against the fake OpenAI-compatible server (fakellm_test.go). The script deploys an
// AgentConfig with structured output and an AgentRun to a file backend, reads the answer with
// stack.outputs() and destroys the stack. The test checks the printed answer, the request the
// model received, and that nothing is left in the backend. It needs no registry and no API key.
func TestAutomationAPI(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	sdk := filepath.Join(root, "sdk", "nodejs", "bin")
	requireFile(t, filepath.Join(sdk, "package.json"), "make build_nodejs")
	const apiKey = "sk-fake-automation-key-0123456789"
	const change = "Rotate the TLS certificate of the internal API gateway; the old certificate stays valid for a week."
	llm := newFakeLLM(t)

	start := time.Now()
	dir := t.TempDir()
	copyExample(t, "automation-ts", dir)
	replaceInFile(t, filepath.Join(dir, "package.json"), "file:../../sdk/nodejs/bin", "file:"+filepath.ToSlash(sdk))
	runIn(t, dir, nil, "npm", "install", "--no-audit", "--no-fund")
	runIn(t, dir, nil, "npx", "--no-install", "tsc", "-p", ".")
	t.Logf("[automation-ts] setup (copy, npm install, tsc): %s", time.Since(start).Round(time.Millisecond))

	changeFile := filepath.Join(dir, "change.txt")
	require.NoError(t, os.WriteFile(changeFile, []byte(change), 0o600))
	stateDir := filepath.Join(dir, ".state")

	step := time.Now()
	stdout, stderr := runIn(t, dir, []string{
		"LLM_BASE_URL=" + llm.URL,
		"LLM_MODEL=fake-model",
		"LLM_API_KEY=" + apiKey,
		"DOCKER_AGENT_PROVIDER_PATH=" + filepath.Join(root, "bin"),
		"STATE_DIR=" + stateDir,
		"PULUMI_CONFIG_PASSPHRASE=automation-test",
	}, "node", "bin/index.js", changeFile)
	t.Logf("[automation-ts] run (up, outputs, destroy): %s", time.Since(step).Round(time.Millisecond))

	// The fake model answers with the first enum value and "sample" for strings.
	var result struct {
		Verdict string         `json:"verdict"`
		Risk    string         `json:"risk"`
		Summary string         `json:"summary"`
		Usage   map[string]any `json:"usage"`
	}
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	require.NoError(t, json.Unmarshal([]byte(lines[len(lines)-1]), &result), "stdout:\n%s\nstderr:\n%s", stdout, stderr)
	assert.Equal(t, "approve", result.Verdict)
	assert.Equal(t, "low", result.Risk)
	assert.Equal(t, "sample", result.Summary)
	assert.Equal(t, map[string]any{"inputTokens": 10.0, "outputTokens": 5.0, "cost": 0.0}, result.Usage)
	assert.NotContains(t, stdout+stderr, apiKey, "the API key leaked into the output")

	reqs := llm.received()
	require.Len(t, reqs, 1, "the agent runs once")
	assert.Equal(t, "Bearer "+apiKey, reqs[0].Authorization)
	assert.Equal(t, "fake-model", reqs[0].Model)
	assert.Contains(t, reqs[0].System, "You review infrastructure change requests.")
	assert.Equal(t, change, reqs[0].User)
	require.NotNil(t, reqs[0].Schema, "structuredOutput is sent as the response schema")
	assert.Contains(t, reqs[0].Schema["properties"], "verdict")

	// The script used the file backend, then destroyed and removed the stack.
	assert.DirExists(t, filepath.Join(stateDir, ".pulumi", "stacks"))
	stacks, _ := filepath.Glob(filepath.Join(stateDir, ".pulumi", "stacks", "*", "*.json"))
	assert.Empty(t, stacks, "the stack is still in the backend")
	t.Logf("[automation-ts] total: %s", time.Since(start).Round(time.Millisecond))
}

// copyExample copies an example directory without its build outputs and dependencies.
func copyExample(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "node_modules", "bin", ".state":
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), data, 0o600)
	})
	require.NoError(t, err)
}

// runIn runs a command in dir with extra environment variables and returns its stdout and stderr.
// PULUMI_BACKEND_URL is removed from the inherited environment, so a backend of the developer's
// shell is never used.
func runIn(t *testing.T, dir string, env []string, name string, args ...string) (string, string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "PULUMI_BACKEND_URL=") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	cmd.Env = append(cmd.Env, "PULUMI_SKIP_UPDATE_CHECK=true")
	cmd.Env = append(cmd.Env, env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	require.NoError(t, err, "%s %s:\nstdout:\n%s\nstderr:\n%s", name, strings.Join(args, " "), stdout.String(), stderr.String())
	return stdout.String(), stderr.String()
}
