// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package examples

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The import tests run `pulumi import` with code generation against the locally built
// provider and SDKs, the way a user adopts existing docker-agent files: the generated code
// becomes the program, and the next preview must show no changes. They need no registry,
// except for the AgentArtifact part of TestImportYAML, which is skipped without one.

func TestImportTypeScript(t *testing.T) { runImport(t, "typescript") }
func TestImportPython(t *testing.T)     { runImport(t, "python") }
func TestImportYAML(t *testing.T)       { runImport(t, "yaml") }

// importAgentYAML is an existing agent file: comments, a named model, the list form of
// `instruction` and a key that has no typed input in an `extra` map are all part of it.
const importAgentYAML = `# A research team, written by hand.
version: "16"
metadata:
  author: platform-team
  tags: [research]
models:
  fast:
    provider: openai
    model: gpt-5-mini
    temperature: 0.2
agents:
  root:
    model: fast
    description: Coordinates the team
    instruction:
      - Answer briefly.
      - Delegate research questions to the researcher.
    sub_agents: [researcher]
    toolsets:
      - type: think
  researcher:
    model: anthropic/claude-sonnet-4-5
    instruction: Research the question and cite your sources.
    toolsets:
      - type: mcp
        ref: docker:duckduckgo
      - type: fetch
      - type: rag
        ref: knowledge
rag:
  knowledge:
    docs: [./docs]
    strategies:
      - type: chunked-embeddings
        embedding_model: openai/text-embedding-3-small
        database: ./rag.db
        batch_size: 50
`

// importProject is a Pulumi project in one of the import test languages.
type importProject struct {
	lang, dir, program string
	env                []string
}

// runImport imports an AgentConfig from a local file (`pulumi import <type> <name> <id>`), and
// then a UserSettings and an AgentsIgnore that another stack wrote (`pulumi import -f`),
// each time writing the generated code into the program and checking that nothing changes.
func runImport(t *testing.T, lang string) {
	t.Parallel()
	root := repoRoot(t)
	start := time.Now()
	pr := newImportProject(t, root, lang)
	t.Logf("[import %s] setup: %s", lang, time.Since(start).Round(time.Millisecond))

	// AgentConfig from ./agent.yaml.
	step := time.Now()
	require.NoError(t, os.WriteFile(filepath.Join(pr.dir, "agent.yaml"), []byte(importAgentYAML), 0o600))
	pr.pulumi(t, "stack", "init", "agentconfig")
	program := pr.importResources(t, "docker-agent:index:AgentConfig", "assistant", "./agent.yaml")
	for _, typed := range []string{"researcher", "gpt-5-mini", "docker:duckduckgo", "platform-team", "batch_size"} {
		assert.Contains(t, program, typed, "the generated program misses %q", typed)
	}
	pr.pulumi(t, "preview", "--expect-no-changes")
	t.Logf("[import %s] AgentConfig: %s", lang, time.Since(step).Round(time.Millisecond))

	// UserSettings and AgentsIgnore files that another Pulumi stack wrote and forgot.
	step = time.Now()
	settingsFile := filepath.Join(t.TempDir(), "cagent", "config.yaml")
	workspace := filepath.Join(pr.dir, "workspace")
	ignoreFile := filepath.Join(workspace, ".agentsignore")
	seedLocalFiles(t, root, settingsFile, workspace)
	settings, ignore := readBytes(t, settingsFile), readBytes(t, ignoreFile)

	pr.pulumi(t, "stack", "init", "localfiles")
	importFile := filepath.Join(t.TempDir(), "resources.json")
	writeImportFile(t, importFile, []map[string]string{
		{"type": "docker-agent:index:UserSettings", "name": "settings", "id": settingsFile},
		{"type": "docker-agent:index:AgentsIgnore", "name": "ignore", "id": "./workspace"},
	})
	program = pr.importResources(t, "-f", importFile)
	for _, typed := range []string{"reviewer", "dracula", "redact_secrets", "*.pem", "./workspace"} {
		assert.Contains(t, program, typed, "the generated program misses %q", typed)
	}
	pr.pulumi(t, "up", "--yes", "--expect-no-changes")
	// The hook's env is secret in the schema: the imported inputs and state, and the rendered
	// file in `yaml`, keep it encrypted.
	assert.Contains(t, string(settings), importedHookToken)
	state, _ := pr.pulumi(t, "stack", "export")
	assert.NotContains(t, state, importedHookToken, "the imported state has the hook's env in plain text")
	assert.Equal(t, settings, readBytes(t, settingsFile), "up changed the adopted user settings")
	assert.Equal(t, ignore, readBytes(t, ignoreFile), "up changed the adopted .agentsignore")
	pr.pulumi(t, "preview", "--expect-no-changes")
	pr.pulumi(t, "refresh", "--yes", "--expect-no-changes")

	// Adopted files are left in place on destroy (after unprotecting: `pulumi import`
	// protects what it imports).
	pr.pulumi(t, "state", "unprotect", "--all", "--yes")
	pr.pulumi(t, "destroy", "--yes")
	assert.Equal(t, settings, readBytes(t, settingsFile), "destroy removed or changed the adopted user settings")
	assert.Equal(t, ignore, readBytes(t, ignoreFile), "destroy removed or changed the adopted .agentsignore")
	for _, f := range []string{settingsFile, ignoreFile} {
		_, err := os.Stat(f + ".pulumi-backup")
		assert.True(t, os.IsNotExist(err), "an adopted file got a backup: %s", f)
	}
	t.Logf("[import %s] UserSettings and AgentsIgnore: %s", lang, time.Since(step).Round(time.Millisecond))

	if lang == "yaml" {
		importArtifacts(t, root, pr)
	}
	t.Logf("[import %s] total: %s", lang, time.Since(start).Round(time.Millisecond))
}

// importArtifacts pushes agent.yaml with a separate stack, then imports the artifact as an
// AgentArtifact (ID: the tag) and its configuration as an AgentConfig (ID: oci://<tag>), with
// registry credentials in the provider configuration.
func importArtifacts(t *testing.T, root string, pr *importProject) {
	t.Helper()
	registry := os.Getenv(registryEnv)
	if registry == "" {
		t.Logf("[import yaml] AgentArtifact skipped: %s is not set", registryEnv)
		return
	}
	registry = strings.TrimSuffix(strings.TrimPrefix(registry, "http://"), "/")
	step := time.Now()
	ref := registry + "/examples/import-yaml:v1"
	seed := newSeedProject(t, root, fmt.Sprintf(`
  agent:
    type: docker-agent:index:AgentArtifact
    properties:
      reference: %q
      yaml:
        fn::readFile: %q
`, ref, filepath.Join(pr.dir, "agent.yaml")))
	seed.pulumi(t, "up", "--yes")

	pr.pulumi(t, "stack", "init", "artifact")
	// Structured and secret provider configuration reaches the provider `pulumi import`
	// creates as raw JSON strings.
	host := strings.SplitN(ref, "/", 2)[0]
	pr.pulumi(t, "config", "set", "docker-agent:forceHttps", "true")
	pr.pulumi(t, "config", "set", "--path", "docker-agent:registryAuth[0].address", host)
	pr.pulumi(t, "config", "set", "--path", "docker-agent:registryAuth[0].username", "importer")
	pr.pulumi(t, "config", "set", "--secret", "--path", "docker-agent:registryAuth[0].password", "not-checked-by-registry-2")
	importFile := filepath.Join(t.TempDir(), "resources.json")
	writeImportFile(t, importFile, []map[string]string{
		{"type": "docker-agent:index:AgentArtifact", "name": "published", "id": ref},
		{"type": "docker-agent:index:AgentConfig", "name": "pulled", "id": "oci://" + ref},
	})
	program := pr.importResources(t, "-f", importFile)
	assert.Contains(t, program, ref)
	assert.Contains(t, program, "researcher")
	pr.pulumi(t, "preview", "--expect-no-changes")
	pr.pulumi(t, "refresh", "--yes", "--expect-no-changes")
	t.Logf("[import yaml] AgentArtifact and AgentConfig from OCI: %s", time.Since(step).Round(time.Millisecond))
}

// newImportProject creates an empty program in lang that uses the local provider and SDK,
// with a file backend, and installs its dependencies.
func newImportProject(t *testing.T, root, lang string) *importProject {
	t.Helper()
	pr := &importProject{lang: lang, dir: t.TempDir(), env: backendEnv(t)}
	plugins := fmt.Sprintf("plugins:\n  providers:\n    - name: %s\n      path: %q\n", providerName, filepath.Join(root, "bin"))
	files := map[string]string{}
	switch lang {
	case "typescript":
		sdk := filepath.Join(root, "sdk", "nodejs", "bin")
		requireFile(t, filepath.Join(sdk, "package.json"), "make build_nodejs")
		pr.program = "index.ts"
		files["Pulumi.yaml"] = "name: import-ts\nruntime:\n  name: nodejs\n  options:\n    packagemanager: npm\n" + plugins
		files["package.json"] = fmt.Sprintf(`{
    "name": "import-ts",
    "main": "index.ts",
    "private": true,
    "devDependencies": {"@types/node": "^22.20.5", "typescript": "^5.9.3"},
    "dependencies": {"@ediri/pulumi-docker-agent": %q, "@pulumi/pulumi": "^3.268.0"}
}
`, "file:"+filepath.ToSlash(sdk))
		files["tsconfig.json"] = `{"compilerOptions": {"strict": true, "outDir": "bin", "target": "es2020", "module": "commonjs",
    "moduleResolution": "node"}, "files": ["index.ts"]}
`
		files["index.ts"] = "export {};\n"
	case "python":
		sdk := filepath.Join(root, "sdk", "python", "bin")
		requireFile(t, filepath.Join(sdk, "pyproject.toml"), "make build_python")
		pr.program = "__main__.py"
		files["Pulumi.yaml"] = "name: import-python\nruntime:\n  name: python\n  options:\n    toolchain: pip\n" +
			"    virtualenv: venv\n" + plugins
		files["requirements.txt"] = "pulumi>=3.268.0,<4.0.0\n" + sdk + "\n"
		files["__main__.py"] = "import pulumi\n"
	case "yaml":
		// Pulumi YAML reads the program from Main.yaml when Pulumi.yaml has none.
		pr.program = "Main.yaml"
		files["Pulumi.yaml"] = "name: import-yaml\nruntime: yaml\n" + plugins
	default:
		t.Fatalf("unknown language %s", lang)
	}
	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(pr.dir, name), []byte(content), 0o600))
	}
	if lang != "yaml" {
		installDependencies(t, pr.dir)
	}
	return pr
}

// importResources runs `pulumi import` with the given arguments, writes the generated code
// into the program and returns it. The import must not report invalid inputs.
func (pr *importProject) importResources(t *testing.T, args ...string) string {
	t.Helper()
	// Generate into a file outside the project: `--out` truncates its file first, and Pulumi
	// YAML reads the program while the import runs.
	out := filepath.Join(t.TempDir(), "generated")
	stdout, stderr := pr.pulumi(t, append([]string{"import", "--yes", "--out", out}, args...)...)
	for _, bad := range []string{"almost certainly a bug", "failed to validate"} {
		assert.NotContains(t, stdout+stderr, bad, "pulumi import reported a problem")
	}
	program := string(readBytes(t, out))
	t.Logf("[import %s] generated program:\n%s", pr.lang, program)
	require.NoError(t, os.WriteFile(filepath.Join(pr.dir, pr.program), []byte(program), 0o600))
	return program
}

func (pr *importProject) pulumi(t *testing.T, args ...string) (string, string) {
	t.Helper()
	return runIn(t, pr.dir, pr.env, "pulumi", args...)
}

// importedHookToken is a credential in the `env` of a hook of the imported user settings.
const importedHookToken = "xoxb-import-0123456789abcdef"

// seedLocalFiles writes a user configuration and an `.agentsignore` with a separate stack,
// which is then removed without destroying its resources: the files stay behind, exactly as
// the provider writes them, ready to be imported.
func seedLocalFiles(t *testing.T, root, settingsFile, workspace string) {
	t.Helper()
	seed := newSeedProject(t, root, fmt.Sprintf(`
  settings:
    type: docker-agent:index:UserSettings
    properties:
      path: %q
      settings:
        theme: auto
        themeDark: dracula
        safety: balanced
        layout: {sidebarPosition: left, hideUsage: true}
        permissions: {deny: ["shell:cmd=sudo*"], allow: ["read_*"]}
        keybindings: [{action: commands, keys: [f2, ctrl+k]}]
        hooks:
          preToolUse: [{matcher: shell, hooks: [{type: builtin, command: redact_secrets}]}]
          sessionStart: [{type: command, command: notify, env: {SLACK_TOKEN: %q}}]
      aliases:
        reviewer: {path: docker.io/myorg/reviewer:latest, safety: restricted}
      providers:
        gateway: {baseUrl: "https://llm.example.com/v1", tokenKey: GATEWAY_API_KEY, thinkingBudget: {effort: high}}
      defaultModel: {provider: gateway, model: qwen3-coder}
      sandboxAllowlist: ["registry.npmjs.org:443"]
      extra:
        future_setting: {enabled: true}
  ignore:
    type: docker-agent:index:AgentsIgnore
    properties:
      directory: %q
      comment: Keep secrets away from docker-agent.
      patterns: [.env, .env.*, "*.pem", "!public.pem", build/]
`, settingsFile, importedHookToken, workspace))
	seed.pulumi(t, "up", "--yes")
	seed.pulumi(t, "stack", "rm", "--force", "--yes")
}

// newSeedProject creates a YAML project with the given resources (indented YAML) and a stack.
func newSeedProject(t *testing.T, root, resources string) *importProject {
	t.Helper()
	pr := &importProject{lang: "yaml", dir: t.TempDir(), env: backendEnv(t), program: "Pulumi.yaml"}
	project := fmt.Sprintf("name: import-seed\nruntime: yaml\nplugins:\n  providers:\n    - name: %s\n      path: %q\n"+
		"resources:%s", providerName, filepath.Join(root, "bin"), resources)
	require.NoError(t, os.WriteFile(filepath.Join(pr.dir, "Pulumi.yaml"), []byte(project), 0o600))
	pr.pulumi(t, "stack", "init", "seed")
	return pr
}

// backendEnv is a file backend in a temp dir.
func backendEnv(t *testing.T) []string {
	t.Helper()
	return []string{"PULUMI_BACKEND_URL=file://" + filepath.ToSlash(t.TempDir()), "PULUMI_CONFIG_PASSPHRASE=import-test"}
}

func writeImportFile(t *testing.T, path string, resources []map[string]string) {
	t.Helper()
	data, err := json.MarshalIndent(map[string]any{"resources": resources}, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0o600))
}

func readBytes(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return data
}
