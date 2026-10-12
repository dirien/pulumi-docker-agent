// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package examples

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/pulumi/providertest/pulumitest"
	"github.com/pulumi/providertest/pulumitest/opttest"
	"github.com/pulumi/pulumi/sdk/v3/go/auto"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// docsExample is one document of docs/yaml/*.yaml, the source of the Example Usage sections that
// `make docs` (provider/internal/docsgen) converts into every language.
type docsExample struct {
	Description string            `yaml:"description"`
	SkipPreview string            `yaml:"skipPreview"`
	Files       map[string]string `yaml:"files"`
	Notes       map[string]string `yaml:"notes"`
	program     string
}

// TestDocsExamples previews every example of docs/yaml with the provider in bin/, so that the
// examples of the API docs and docs/_index.md stay valid: types, enum values, references and
// validation. Examples that read from a remote registry set `skipPreview`. Nothing is created;
// `~` is a temporary home directory.
func TestDocsExamples(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	files, err := filepath.Glob(filepath.Join(root, "docs", "yaml", "*.yaml"))
	require.NoError(t, err)
	require.NotEmpty(t, files)
	pulumiHome := os.Getenv("PULUMI_HOME")
	if pulumiHome == "" {
		home, err := os.UserHomeDir()
		require.NoError(t, err)
		pulumiHome = filepath.Join(home, ".pulumi")
	}
	for _, file := range files {
		for _, ex := range readDocsExamples(t, file) {
			var program struct {
				Name   string `yaml:"name"`
				Config map[string]struct {
					Secret bool `yaml:"secret"`
				} `yaml:"config"`
			}
			require.NoError(t, yaml.Unmarshal([]byte(ex.program), &program), file)
			t.Run(strings.TrimSuffix(filepath.Base(file), "-examples.yaml")+"/"+program.Name, func(t *testing.T) {
				t.Parallel()
				if ex.SkipPreview != "" {
					t.Skip(ex.SkipPreview)
				}
				dir := t.TempDir()
				require.NoError(t, os.WriteFile(filepath.Join(dir, "Pulumi.yaml"), []byte(ex.program), 0o600))
				for name, content := range ex.Files {
					require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
				}
				pt := pulumitest.NewPulumiTest(t, dir,
					opttest.LocalProviderPath(providerName, filepath.Join(root, "bin")),
					opttest.SkipInstall(),
					opttest.SkipStackCreate(),
					opttest.Env("HOME", t.TempDir()),
					opttest.Env("PULUMI_HOME", pulumiHome),
				)
				stack := pt.NewStack(t, "")
				for key, c := range program.Config {
					require.NoError(t, stack.SetConfig(context.Background(), key,
						auto.ConfigValue{Value: "test-" + key, Secret: c.Secret}))
				}
				pt.Preview(t)
			})
		}
	}
}

var (
	docsSeparatorRE = regexp.MustCompile(`(?m)^---[ \t]*$`)
	docsNameRE      = regexp.MustCompile(`(?m)^name:`)
)

// readDocsExamples splits a docs/yaml file into its examples: the keys before `name:` describe
// the example, the rest is the Pulumi YAML program.
func readDocsExamples(t *testing.T, file string) []docsExample {
	t.Helper()
	b, err := os.ReadFile(file) //nolint:gosec // the files of docs/yaml
	require.NoError(t, err)
	var examples []docsExample
	for _, doc := range docsSeparatorRE.Split(string(b), -1) {
		loc := docsNameRE.FindStringIndex(doc)
		if loc == nil {
			continue // the file's header comment
		}
		var ex docsExample
		require.NoError(t, yaml.Unmarshal([]byte(doc[:loc[0]]), &ex), file)
		require.NotEmpty(t, ex.Description, "%s: an example without a description", file)
		ex.program = doc[loc[0]:]
		examples = append(examples, ex)
	}
	require.NotEmpty(t, examples, file)
	return examples
}

var (
	docsRegionRE = regexp.MustCompile(`(?s)<!-- docsgen begin: .*?<!-- docsgen end -->`)
	docsFenceRE  = regexp.MustCompile("(?ms)^```(\\w+)\n(.*?)^```$")
)

// docsSnippets returns the generated programs in one language: every example of the API docs
// (provider/pkg/provider/embed) and of the generated regions of docs/_index.md, by name.
func docsSnippets(t *testing.T, root, lang string) map[string]string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(root, "provider", "pkg", "provider", "embed", "*-examples.md"))
	require.NoError(t, err)
	require.NotEmpty(t, files)
	files = append(files, filepath.Join(root, "docs", "_index.md"))
	snippets := map[string]string{}
	for _, file := range files {
		b, err := os.ReadFile(file) //nolint:gosec // the generated docs
		require.NoError(t, err)
		text := string(b)
		if filepath.Base(file) == "_index.md" {
			text = strings.Join(docsRegionRE.FindAllString(text, -1), "\n")
		}
		base := strings.TrimSuffix(strings.TrimSuffix(filepath.Base(file), ".md"), "-examples")
		n := 0
		for _, m := range docsFenceRE.FindAllStringSubmatch(text, -1) {
			if m[1] == lang {
				snippets[fmt.Sprintf("%s%d", strings.Trim(base, "_"), n)] = m[2]
				n++
			}
		}
	}
	require.NotEmpty(t, snippets, lang)
	return snippets
}

// TestDocsSnippetsGo builds every generated Go example against the Go SDK in sdk/.
func TestDocsSnippetsGo(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	dir := t.TempDir()
	for name, code := range docsSnippets(t, root, "go") {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, name), 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(dir, name, "main.go"), []byte(code), 0o600))
	}
	gomod := fmt.Sprintf("module docscheck\n\ngo 1.26\n\nrequire %s v0.0.0\n\nreplace %s => %s\n",
		sdkModule, sdkModule, filepath.Join(root, "sdk"))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o600))
	sum, err := os.ReadFile(filepath.Join(root, "sdk", "go.sum"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.sum"), sum, 0o600))
	for _, args := range [][]string{{"mod", "tidy"}, {"vet", "./..."}} {
		cmd := exec.Command("go", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "go %s:\n%s", strings.Join(args, " "), out)
	}
}

// TestDocsSnippetsTypeScript type-checks every generated TypeScript example against the built
// Node.js SDK (sdk/nodejs/bin, from `make build_nodejs`).
func TestDocsSnippetsTypeScript(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	sdk := filepath.Join(root, "sdk", "nodejs")
	requireFile(t, filepath.Join(sdk, "bin", "index.d.ts"), "make build_nodejs")
	tsc := filepath.Join(sdk, "node_modules", ".bin", "tsc")
	requireFile(t, tsc, "make build_nodejs")
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "src"), 0o750))
	for name, code := range docsSnippets(t, root, "typescript") {
		require.NoError(t, os.WriteFile(filepath.Join(dir, "src", name+".ts"), []byte(code), 0o600))
	}
	nodeModules := filepath.Join(sdk, "node_modules")
	tsconfig, err := json.Marshal(map[string]any{
		"compilerOptions": map[string]any{
			"strict": true, "noEmit": true, "target": "es2020", "module": "commonjs", "moduleResolution": "node",
			"skipLibCheck": true, "baseUrl": ".", "typeRoots": []string{filepath.Join(nodeModules, "@types")},
			"types": []string{"node"},
			"paths": map[string][]string{
				"@ediri/pulumi-docker-agent": {filepath.Join(sdk, "bin")},
				"@pulumi/pulumi":             {filepath.Join(nodeModules, "@pulumi", "pulumi")},
				"@pulumi/pulumi/*":           {filepath.Join(nodeModules, "@pulumi", "pulumi", "*")},
			},
		},
		"include": []string{"src/*.ts"},
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tsconfig.json"), tsconfig, 0o600))
	cmd := exec.Command(tsc, "-p", dir) //nolint:gosec // the SDK's TypeScript compiler
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "tsc:\n%s", out)
}
