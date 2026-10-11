// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Shims for `go` and `make` that let TestUpgradeScriptRecordsEveryFailure run
// scripts/upgrade-docker-agent.sh without network, module downloads or builds. FAIL names the
// step that fails.
const goShim = `#!/usr/bin/env bash
case "$1 $2" in
  "list -m")
    case "$*" in
      *'{{.Version}}'*) echo v1.0.0 ;;
      *'{{.Dir}}'*) echo "$NEW_MODULE_DIR" ;;
    esac ;;
  "get "*) if [ "$FAIL" = go-get ]; then echo "go: github.com/docker/docker-agent@$3: invalid version: unknown revision" >&2; exit 1; fi ;;
  "mod tidy") ;;
  "mod download") echo "go: module not found" >&2; exit 1 ;;
  "run ./internal/schemagen")
    case "$3" in
      diff) echo "# docker-agent v1.0.0 → v2.0.0: configuration schema changes" ;;
      changelog) echo changelog >>"$CALLS" ;;
    esac ;;
  *) echo "unexpected go $*" >&2; exit 2 ;;
esac
`

const makeShim = `#!/usr/bin/env bash
echo "make $1" >>"$CALLS"
case "$1" in
  generate_config_types) [ "$FAIL" != generate ] || { echo "schemagen: ModelConfig.seed: union oneOf[integer|string] needs a rule in unionFields" >&2; exit 1; } ;;
  codegen) [ "$FAIL" != codegen ] || { echo "pkg/agentrun/agentrun.go:42:2: undefined: runtime.WithEnv" >&2; exit 1; } ;;
  test_provider) [ "$FAIL" != tests ] || { echo "--- FAIL: TestRoundTripUpstreamExamples" >&2; exit 1; } ;;
esac
`

// TestUpgradeScriptRecordsEveryFailure is the regression test for upgrades that failed before
// the tests: every step must record a status and a report with the failing step and its
// output, and the script must exit 0 so the workflow opens the pull request.
func TestUpgradeScriptRecordsEveryFailure(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is not available")
	}
	script, err := filepath.Abs(filepath.Join("..", "..", "..", "scripts", "upgrade-docker-agent.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		fail, status, failedStep, output string
		calls                            []string
		noSchema                         bool
	}{
		{fail: "", status: "ok", calls: []string{"make generate_config_types", "make codegen", "make test_provider", "changelog"}},
		{fail: "go-get", status: "go-get-failed", failedStep: "go get github.com/docker/docker-agent@v2.0.0",
			output: "unknown revision"},
		{fail: "generate", status: "generate-failed", failedStep: "make generate_config_types",
			output: "needs a rule in unionFields", calls: []string{"make generate_config_types", "changelog"}},
		{fail: "codegen", status: "codegen-failed", failedStep: "make codegen", output: "undefined: runtime.WithEnv",
			calls: []string{"make generate_config_types", "make codegen", "changelog"}},
		{fail: "tests", status: "tests-failed", failedStep: "make test_provider",
			output: "--- FAIL: TestRoundTripUpstreamExamples",
			calls:  []string{"make generate_config_types", "make codegen", "make test_provider", "changelog"}},
		// The script stops before any step (no vendored schema, and the old module cannot
		// be downloaded): the EXIT trap still writes a status and a report.
		{fail: "", status: "script-failed", noSchema: true, failedStep: "upgrade-docker-agent.sh"},
	} {
		name := tc.status
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			write := func(path, content string, mode os.FileMode) {
				t.Helper()
				full := filepath.Join(root, path)
				if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(full, []byte(content), mode); err != nil {
					t.Fatal(err)
				}
			}
			body, err := os.ReadFile(script)
			if err != nil {
				t.Fatal(err)
			}
			write("repo/scripts/upgrade-docker-agent.sh", string(body), 0o755)
			write("repo/CHANGELOG.md", "# Changelog\n\n## [Unreleased]\n", 0o644)
			write("repo/provider/go.mod", "module x\n", 0o644)
			if !tc.noSchema {
				write("repo/provider/pkg/render/schema/agent-schema.json", "{}", 0o644)
			}
			write("newmod/agent-schema.json", "{}", 0o644)
			write("shims/go", goShim, 0o755)
			write("shims/make", makeShim, 0o755)

			cmd := exec.Command("bash", filepath.Join(root, "repo", "scripts", "upgrade-docker-agent.sh"))
			cmd.Dir = filepath.Join(root, "repo")
			cmd.Env = append(os.Environ(),
				"PATH="+filepath.Join(root, "shims")+string(os.PathListSeparator)+os.Getenv("PATH"),
				"DOCKER_AGENT_VERSION=v2.0.0", "FAIL="+tc.fail, "CALLS="+filepath.Join(root, "calls"),
				"NEW_MODULE_DIR="+filepath.Join(root, "newmod"), "STRICT=", "GIT_DIR="+filepath.Join(root, "no-git"))
			out, err := cmd.CombinedOutput()
			if tc.status == "script-failed" {
				if err == nil {
					t.Fatalf("the script should fail when it cannot start the upgrade:\n%s", out)
				}
			} else if err != nil {
				t.Fatalf("the script must exit 0 once the upgrade was attempted: %v\n%s", err, out)
			}

			read := func(path string) string {
				t.Helper()
				b, err := os.ReadFile(filepath.Join(root, "repo", path))
				if err != nil {
					t.Fatalf("%s: %v\n%s", path, err, out)
				}
				return string(b)
			}
			if got := strings.TrimSpace(read("bin/upgrade-status")); got != tc.status {
				t.Errorf("status = %q, want %q\n%s", got, tc.status, out)
			}
			if got := strings.TrimSpace(read("bin/upgrade-failed-step")); got != tc.failedStep {
				t.Errorf("failed step = %q, want %q", got, tc.failedStep)
			}
			report := read("bin/upgrade-report.md")
			if tc.status != "script-failed" {
				if !strings.Contains(report, "## Upgrade steps") {
					t.Errorf("the report has no step table:\n%s", report)
				}
			}
			if tc.failedStep != "" && tc.status != "script-failed" {
				for _, want := range []string{"## Failure: `" + tc.failedStep + "`", tc.output, "| **failed** |"} {
					if !strings.Contains(report, want) {
						t.Errorf("the report lacks %q:\n%s", want, report)
					}
				}
			}
			calls, _ := os.ReadFile(filepath.Join(root, "calls"))
			if got := strings.Fields(strings.ReplaceAll(strings.TrimSpace(string(calls)), "make ", "make_")); strings.Join(got, ",") !=
				strings.ReplaceAll(strings.Join(tc.calls, ","), "make ", "make_") {
				t.Errorf("steps run = %q, want %q (steps after a failure are skipped)", calls, tc.calls)
			}
		})
	}
}
