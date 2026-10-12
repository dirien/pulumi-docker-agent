// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package examples

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// The converter tests run `pulumi convert --from docker-agent` with the locally built converter
// and provider (bin/, `make converter provider`), installed into a throwaway PULUMI_HOME the way
// users install the released plugins, on the example configurations of docker-agent
// (provider/pkg/render/testdata/upstream). They need no registry.

const convertPluginVersion = "0.0.1-alpha.0"

// convertHCLVersion is the pulumi-hcl language plugin the HCL smoke test runs: the version that
// `make docs` pins (PULUMI_HCL_VERSION in the Makefile).
const convertHCLVersion = "0.18.4"

// convertUpstreamDir is the directory of the vendored docker-agent example configurations.
func convertUpstreamDir(root string) string {
	return filepath.Join(root, "provider", "pkg", "render", "testdata", "upstream")
}

// convertEnv returns the environment of the Pulumi CLI for the converter tests: a fresh
// PULUMI_HOME with bin/pulumi-converter-docker-agent and bin/pulumi-resource-docker-agent
// installed as plugins, plugins on PATH ignored, and a file backend.
func convertEnv(t *testing.T, root string) []string {
	t.Helper()
	exe := ""
	if runtime.GOOS == "windows" {
		exe = ".exe"
	}
	converter := filepath.Join(root, "bin", "pulumi-converter-"+providerName+exe)
	requireFile(t, converter, "make converter")
	home := t.TempDir()
	env := append(os.Environ(),
		"PULUMI_HOME="+home,
		"PULUMI_IGNORE_AMBIENT_PLUGINS=true",
		"PULUMI_SKIP_UPDATE_CHECK=true",
		"PULUMI_BACKEND_URL=file://"+filepath.ToSlash(t.TempDir()),
		"PULUMI_CONFIG_PASSPHRASE=convert-test",
	)
	convertRun(t, env, root, "pulumi", "plugin", "install", "converter", providerName, convertPluginVersion, "--file", converter)
	convertRun(t, env, root, "pulumi", "plugin", "install", "resource", providerName, convertPluginVersion,
		"--file", filepath.Join(root, "bin", "pulumi-resource-"+providerName+exe))
	return env
}

// convertRun runs a command and returns its standard output; it fails the test on an error.
func convertRun(t *testing.T, env []string, dir string, name string, args ...string) string {
	t.Helper()
	start := time.Now()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = env
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	require.NoError(t, err, "%s %s:\n%s\n%s", name, strings.Join(args, " "), out, stderr.String())
	t.Logf("%s %s: %s", name, strings.Join(args, " "), time.Since(start).Round(time.Millisecond))
	return string(out)
}

// convertRunCombined runs a command and returns its standard output and error.
func convertRunCombined(t *testing.T, env []string, dir string, name string, args ...string) string {
	t.Helper()
	start := time.Now()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s %s:\n%s", name, strings.Join(args, " "), out)
	t.Logf("%s %s: %s", name, strings.Join(args, " "), time.Since(start).Round(time.Millisecond))
	return string(out)
}

// TestConvertDockerAgentExamples converts every example configuration of docker-agent with
// `pulumi convert --from docker-agent --language yaml`, runs `pulumi up` on the program and checks
// that each AgentConfig renders exactly the YAML of docker-agent-examples/testdata/<name>.yaml
// (render.Render of the decoded example, which the TypeScript and Python programs of
// docker-agent-examples also render). A second preview must show no changes.
//
// Two bugs of Pulumi YAML (outside this repository) affect a few examples; the converter warns
// about both ("Pulumi YAML limitation"):
//
//   - Its program generator writes strings such as `~` unquoted, which the program then reads
//     as null. The test applies the fix the warning describes (quote the value in Main.yaml) and
//     runs those examples as Pulumi YAML.
//   - Its type checker rejects a typed map with a key that is also an input name of the map's
//     values (a model named `model`: "Cannot assign '{provider: string, model: string}' to
//     'string'"). There is no way to write such a map in Pulumi YAML, so those examples are
//     converted to Go and run as a Go program instead.
func TestConvertDockerAgentExamples(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	env := convertEnv(t, root)
	golden := dockerAgentExampleGoldens(t)
	upstream := convertUpstreamDir(root)
	files := convertConfigFiles(t, upstream)
	require.ElementsMatch(t, sortedKeys(golden), sortedKeys(files),
		"the upstream example configurations and docker-agent-examples/testdata differ")

	// Every example converts into one Pulumi YAML program, with one AgentConfig per example.
	all := filepath.Join(t.TempDir(), "all")
	output := convertRunCombined(t, env, upstream, "pulumi", "convert", "--from", providerName, "--language", "yaml",
		"--generate-only", "--out", all)
	assert.ElementsMatch(t, sortedKeys(golden), convertYAMLConfigNames(t, all),
		"the AgentConfigs of the converted program and docker-agent-examples/testdata differ")
	// The instruction files of instruction_file.yaml are copied next to the program.
	requireFile(t, filepath.Join(all, "instructions", "writer.md"), "a converter that copies instruction files")

	limitations := convertYAMLLimitations(output)
	var yamlFiles, goFiles, quoted []string
	for _, name := range sortedKeys(files) {
		details := limitations[files[name]]
		switch {
		case slices.ContainsFunc(details, isConvertTypeCheckerLimitation):
			goFiles = append(goFiles, files[name])
		case len(details) > 0:
			quoted = append(quoted, files[name])
			yamlFiles = append(yamlFiles, files[name])
		default:
			yamlFiles = append(yamlFiles, files[name])
		}
	}
	t.Logf("Pulumi YAML limitations: %d examples run as Go (type checker): %v; %d run as Pulumi YAML "+
		"with quoted strings: %v", len(goFiles), goFiles, len(quoted), quoted)
	if len(goFiles) > 0 {
		convertRequireYAMLTypeCheckerBug(t, env, upstream, goFiles[0])
	}

	rendered := map[string]string{}
	via := map[string]string{}
	for _, lang := range []string{"yaml", "go"} {
		subset := map[string][]string{"yaml": yamlFiles, "go": goFiles}[lang]
		if len(subset) == 0 {
			continue
		}
		out := filepath.Join(t.TempDir(), lang)
		output := convertRunCombined(t, env, upstream, "pulumi", append([]string{"convert", "--from", providerName,
			"--language", lang, "--generate-only", "--out", out, "--"}, subset...)...)
		switch lang {
		case "yaml":
			convertQuoteYAML(t, out, output)
		case "go":
			convertGoModule(t, env, out, root)
		}
		convertRun(t, env, out, "pulumi", "stack", "init", lang, "--non-interactive")
		convertRun(t, env, out, "pulumi", "up", "--yes", "--skip-preview", "--non-interactive", "--parallel", "16")
		convertRun(t, env, out, "pulumi", "preview", "--expect-no-changes", "--non-interactive")
		for name, y := range convertRenderedConfigs(t, env, out) {
			require.NotContains(t, rendered, name)
			rendered[name] = y
			via[name] = lang
		}
	}

	assert.ElementsMatch(t, sortedKeys(golden), sortedKeys(rendered), "the converted AgentConfigs and testdata differ")
	matched := map[string]int{}
	for name, want := range golden {
		if got, ok := rendered[name]; ok && assert.Equal(t, want, got, "%s (%s) renders different YAML than "+
			"docker-agent-examples/testdata/%s.yaml", name, via[name], name) {
			matched[via[name]]++
		}
	}
	t.Logf("%d of %d converted examples render their golden YAML (%d as Pulumi YAML, %d as Go)",
		matched["yaml"]+matched["go"], len(golden), matched["yaml"], matched["go"])
}

// convertConfigFiles returns the configuration files of the upstream examples by example name
// (the path without extension, `/` replaced by `-`): the YAML and HCL files, without the
// instruction files and the README. HCL files with a YAML twin are left out, as the converter
// does.
func convertConfigFiles(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		ext := filepath.Ext(p)
		if ext != ".yaml" && ext != ".hcl" {
			return nil
		}
		if _, err := os.Stat(strings.TrimSuffix(p, ext) + ".yaml"); ext == ".hcl" && err == nil {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		out[strings.ReplaceAll(strings.TrimSuffix(rel, ext), "/", "-")] = rel
		return nil
	})
	require.NoError(t, err)
	return out
}

// convertYAMLConfigNames returns the logical names of the AgentConfigs of a converted Pulumi YAML
// program.
func convertYAMLConfigNames(t *testing.T, dir string) []string {
	t.Helper()
	var program struct {
		Resources map[string]struct {
			Type string `yaml:"type"`
			Name string `yaml:"name"`
		} `yaml:"resources"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(readFixture(t, filepath.Join(dir, "Main.yaml"))), &program))
	var names []string
	for id, r := range program.Resources {
		if r.Type != "docker-agent:AgentConfig" && r.Type != "docker-agent:index:AgentConfig" {
			continue
		}
		if r.Name != "" {
			id = r.Name
		}
		names = append(names, id)
	}
	return names
}

var (
	// convertLimitationRE matches the converter's warning about a configuration that a Pulumi
	// YAML program cannot hold as generated.
	convertLimitationRE = regexp.MustCompile(`(?m)^warning: (\S+):1,1-1: Pulumi YAML limitation; (.*)$`)
	// convertUnquotedRE matches the part of the warning about a string that Pulumi YAML's
	// program generator writes unquoted.
	convertUnquotedRE = regexp.MustCompile(`is the string ("(?:[^"\\]|\\.)*"), which Pulumi YAML's program generator writes unquoted`)
)

// convertYAMLLimitations returns the details of the converter's "Pulumi YAML limitation" warnings
// by file.
func convertYAMLLimitations(output string) map[string][]string {
	out := map[string][]string{}
	for _, m := range convertLimitationRE.FindAllStringSubmatch(output, -1) {
		out[m[1]] = append(out[m[1]], m[2])
	}
	return out
}

// isConvertTypeCheckerLimitation reports whether a warning is about a map key that Pulumi
// YAML's type checker rejects.
func isConvertTypeCheckerLimitation(detail string) bool {
	return strings.Contains(detail, "Pulumi YAML's type checker rejects")
}

// convertRequireYAMLTypeCheckerBug checks that Pulumi YAML still rejects the converted program of
// file, which the converter warns about: once Pulumi YAML accepts it, the examples should run as
// Pulumi YAML and the converter's warning should go.
func convertRequireYAMLTypeCheckerBug(t *testing.T, env []string, upstream, file string) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "yaml-type-checker")
	convertRun(t, env, upstream, "pulumi", "convert", "--from", providerName, "--language", "yaml", "--generate-only",
		"--out", out, "--", file)
	convertRun(t, env, out, "pulumi", "stack", "init", "type-checker", "--non-interactive")
	cmd := exec.Command("pulumi", "preview", "--non-interactive")
	cmd.Dir = out
	cmd.Env = env
	output, err := cmd.CombinedOutput()
	require.Error(t, err, "Pulumi YAML now runs the converted %s: run these examples as Pulumi YAML and drop the "+
		"converter's warning about its type checker (pkg/pcl emitter.key)", file)
	require.Contains(t, string(output), "Cannot assign", "pulumi preview of the converted %s", file)
}

// convertQuoteYAML applies the fix of the converter's warnings about strings that Pulumi YAML's
// program generator writes unquoted: each of them is quoted in Main.yaml.
func convertQuoteYAML(t *testing.T, dir, output string) {
	t.Helper()
	path := filepath.Join(dir, "Main.yaml")
	program := readFixture(t, path)
	for _, m := range convertUnquotedRE.FindAllStringSubmatch(output, -1) {
		s, err := strconv.Unquote(m[1])
		require.NoError(t, err)
		plain := regexp.MustCompile(`(?m)^(\s*(?:- |[\w"'-]+: ))` + regexp.QuoteMeta(s) + `$`)
		require.Regexp(t, plain, program, "Main.yaml has no plain scalar %s", m[1])
		program = plain.ReplaceAllString(program, "${1}"+strings.ReplaceAll(strconv.Quote(s), "$", "$$"))
	}
	require.NoError(t, os.WriteFile(path, []byte(program), 0o600))
}

// convertLocalProvider points a converted project at the provider in bin/ through
// `plugins.providers` in Pulumi.yaml, as examples/hcl does: the Go SDK built from a `replace`
// reports version 0.0.0, and the HCL program pins the version the binary reports.
func convertLocalProvider(t *testing.T, dir, root string) {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(dir, "Pulumi.yaml"), os.O_APPEND|os.O_WRONLY, 0)
	require.NoError(t, err)
	_, err = f.WriteString("plugins:\n  providers:\n    - name: " + providerName + "\n      path: " +
		filepath.ToSlash(filepath.Join(root, "bin")) + "\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())
}

// convertFix applies the fixes of provider/internal/docsgen for the bugs of a language's program
// generator to a converted program file (`docsgen -fix <lang> <file>`): for Java the package
// names, enum values, readFile and the backslashes of text blocks, for Go the list values of
// untyped maps (`pulumi.Any{...}`).
func convertFix(t *testing.T, env []string, root, lang, file string) {
	t.Helper()
	convertRun(t, env, filepath.Join(root, "provider"), "go", "run", "./internal/docsgen", "-fix", lang, file)
}

// convertGoModule points a converted Go program at the Go SDK of this repository and at the
// provider in bin/, and applies the fix the converter's "Pulumi Go limitation" warning
// describes: a list in a free-form map is generated as `pulumi.Any{...}`, which does not
// compile, instead of `pulumi.Any([]interface{}{...})`.
func convertGoModule(t *testing.T, env []string, dir, root string) {
	t.Helper()
	convertFix(t, env, root, "go", filepath.Join(dir, "main.go"))
	convertRun(t, env, dir, "go", "mod", "edit", "-require="+sdkModule+"@v0.0.0",
		"-replace="+sdkModule+"="+filepath.Join(root, "sdk"))
	convertRun(t, env, dir, "go", "mod", "tidy")
	convertLocalProvider(t, dir, root)
}

// convertRenderedConfigs returns the `yaml` output of every AgentConfig of a stack, by logical
// name.
func convertRenderedConfigs(t *testing.T, env []string, dir string) map[string]string {
	t.Helper()
	var state struct {
		Deployment struct {
			Resources []struct {
				URN     string         `json:"urn"`
				Type    string         `json:"type"`
				Outputs map[string]any `json:"outputs"`
			} `json:"resources"`
		} `json:"deployment"`
	}
	require.NoError(t, json.Unmarshal([]byte(convertRun(t, env, dir, "pulumi", "stack", "export")), &state))
	out := map[string]string{}
	for _, r := range state.Deployment.Resources {
		if r.Type != "docker-agent:index:AgentConfig" {
			continue
		}
		name := r.URN[strings.LastIndex(r.URN, "::")+2:]
		y, ok := r.Outputs["yaml"].(string)
		require.True(t, ok, "%s has no yaml output", name)
		out[name] = y
	}
	return out
}

// convertSmokeFiles are the examples the other languages are generated from: free-form values
// (flavors, JSON schemas, provider options), anchors, hooks (enums, shell scripts with
// backslashes), a RAG configuration in a subdirectory, instruction files, an HCL file with a
// file() template, and a YAML/HCL twin.
var convertSmokeFiles = []string{
	"budget.yaml", "flavors.yaml", "hooks.yaml", "instruction_file.yaml", "instructions/coordinator.md",
	"instructions/shared-preamble.md", "instructions/writer.md", "instructions_from_file.hcl",
	"instructions_from_file.md", "pirate.hcl", "pirate.yaml", "rag/semantic_embeddings.yaml",
	"structured-output-tool-mode.yaml", "yaml-anchors.yaml",
}

// TestConvertLanguages converts a handful of examples, with an AgentArtifact and an AgentRun
// each, into every other language and checks that the programs compile where the toolchain is
// installed: TypeScript with tsc against sdk/nodejs/bin, Python with py_compile, Go against the
// sdk/ module, C# against sdk/dotnet, Java with Gradle against sdk/java (a composite build, as
// examples/java does), and HCL with `pulumi preview`. The Go and Java programs get docsgen's
// fixes for their program generators first (see convertFix).
func TestConvertLanguages(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	env := convertEnv(t, root)
	src := t.TempDir()
	for _, f := range convertSmokeFiles {
		data, err := os.ReadFile(filepath.Join(convertUpstreamDir(root), filepath.FromSlash(f)))
		require.NoError(t, err)
		require.NoError(t, os.MkdirAll(filepath.Dir(filepath.Join(src, f)), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(src, filepath.FromSlash(f)), data, 0o600))
	}
	// convert converts the examples and checks that the program has every AgentConfig, by
	// logical name. pulumi-java's program generator ignores the logical name of a resource whose
	// name is not an identifier (PCL's __logicalName) and uses the identifier instead.
	convert := func(t *testing.T, lang, program string) string {
		t.Helper()
		out := filepath.Join(t.TempDir(), lang)
		convertRun(t, env, src, "pulumi", "convert", "--from", providerName, "--language", lang, "--generate-only",
			"--out", out, "--", "--push-to", "localhost:5000/converted", "--run", "Say hello in ${style}.")
		code := readFixture(t, filepath.Join(out, filepath.FromSlash(program)))
		names := []string{"pirate", "yaml-anchors", "hooks", "rag-semantic_embeddings", "instructions_from_file"}
		if lang == "java" {
			names = []string{"pirate", "yamlAnchors", "hooks", "ragSemanticEmbeddings", "instructionsFromFile"}
		}
		for _, name := range names {
			assert.Contains(t, code, `"`+name+`"`, "%s: the program has no resource %s", program, name)
		}
		// The instruction files are copied next to the program.
		requireFile(t, filepath.Join(out, "instructions", "writer.md"), "a converter that copies instruction files")
		return out
	}

	for _, lang := range []string{"typescript", "python", "go", "csharp", "java", "hcl"} {
		t.Run(lang, func(t *testing.T) {
			t.Parallel()
			switch lang {
			case "typescript":
				out := convert(t, lang, "index.ts")
				convertRequireTool(t, "npm")
				sdk := filepath.Join(root, "sdk", "nodejs", "bin")
				requireFile(t, filepath.Join(sdk, "package.json"), "make build_nodejs")
				convertReplaceRegexp(t, filepath.Join(out, "package.json"), `"@ediri/pulumi-docker-agent": "[^"]*"`,
					`"@ediri/pulumi-docker-agent": "file:`+filepath.ToSlash(sdk)+`"`)
				convertRun(t, env, out, "npm", "install", "--no-audit", "--no-fund", "--loglevel=error")
				convertRun(t, env, out, "npx", "--no-install", "tsc", "--noEmit", "-p", ".")
			case "python":
				out := convert(t, lang, "__main__.py")
				python := convertRequireTool(t, "python3", "python")
				convertRun(t, env, out, python, "-m", "py_compile", "__main__.py")
			case "go":
				out := convert(t, lang, "main.go")
				convertRequireTool(t, "go")
				convertGoModule(t, env, out, root)
				convertRun(t, env, out, "go", "build", "./...")
			case "csharp":
				out := convert(t, lang, "Program.cs")
				convertRequireTool(t, "dotnet")
				sdk := filepath.Join(root, "sdk", "dotnet")
				requireFile(t, filepath.Join(sdk, "version.txt"), "make build_dotnet")
				projects, err := filepath.Glob(filepath.Join(out, "*.csproj"))
				require.NoError(t, err)
				require.Len(t, projects, 1)
				convertReplaceRegexp(t, projects[0], `<PackageReference Include="ediri.DockerAgent"[^>]*/>`,
					`<ProjectReference Include="`+filepath.Join(sdk, "ediri.DockerAgent.csproj")+`" />`)
				convertRun(t, env, out, "dotnet", "build", "--nologo", "-v", "quiet")
			case "java":
				app := filepath.Join("src", "main", "java", "generated_program", "App.java")
				out := convert(t, lang, app)
				requireJavaToolchain(t)
				convertFix(t, env, root, lang, filepath.Join(out, app))
				convertJavaProject(t, out, root)
				convertRun(t, env, out, "gradle", "--console=plain", "--quiet", "compileJava")
			case "hcl":
				out := convert(t, lang, "main.tf")
				convertLanguagePlugin(t, env, "hcl", convertHCLVersion)
				convertLocalProvider(t, out, root)
				convertRun(t, env, out, "pulumi", "stack", "init", "test", "--non-interactive")
				convertRun(t, env, out, "pulumi", "preview", "--non-interactive")
			}
		})
	}
}

// convertJavaProject replaces the Maven project of a converted Java program with a Gradle build
// against the Java SDK sources in sdk/java, like examples/java. pulumi-java's generated pom.xml
// also targets Java 11, which has no text blocks.
func convertJavaProject(t *testing.T, dir, root string) {
	t.Helper()
	sdk := filepath.Join(root, "sdk", "java")
	requireFile(t, filepath.Join(sdk, "build.gradle"), "make generate_java")
	require.NoError(t, os.Remove(filepath.Join(dir, "pom.xml")))
	settings := fmt.Sprintf(`rootProject.name = "converted"
includeBuild(%q) {
    dependencySubstitution {
        substitute(module("io.github.dirien:docker-agent")).using(project(":"))
    }
}
`, filepath.ToSlash(sdk))
	build := readFixture(t, filepath.Join("java", "build.gradle"))
	require.Contains(t, build, `"example.App"`)
	build = strings.ReplaceAll(build, `"example.App"`, `"generated_program.App"`)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "settings.gradle"), []byte(settings), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "build.gradle"), []byte(build), 0o600))
}

// convertRequireTool returns the first of names on PATH, or skips the test.
func convertRequireTool(t *testing.T, names ...string) string {
	t.Helper()
	for _, n := range names {
		if lookPath(n) {
			return n
		}
	}
	t.Skipf("%s is not installed", names[0])
	return ""
}

// convertLanguagePlugin makes a language plugin that is not bundled with the CLI (hcl) available
// in the test's PULUMI_HOME: from the user's plugin cache when it has that version, otherwise
// with `pulumi plugin install`.
func convertLanguagePlugin(t *testing.T, env []string, lang, version string) {
	t.Helper()
	var home, testHome string
	if h := os.Getenv("PULUMI_HOME"); h != "" {
		home = h
	} else if u, err := os.UserHomeDir(); err == nil {
		home = filepath.Join(u, ".pulumi")
	}
	for _, e := range env {
		if v, ok := strings.CutPrefix(e, "PULUMI_HOME="); ok {
			testHome = v
		}
	}
	dir := "language-" + lang + "-v" + version
	bin := "pulumi-language-" + lang
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	src := filepath.Join(home, "plugins", dir, bin)
	if _, err := os.Stat(src); err != nil {
		convertRun(t, env, t.TempDir(), "pulumi", "plugin", "install", "language", lang, version)
		return
	}
	// The plugin cache does not follow a symlinked plugin directory: link or copy the binary.
	dst := filepath.Join(testHome, "plugins", dir)
	require.NoError(t, os.MkdirAll(dst, 0o755))
	if os.Link(src, filepath.Join(dst, bin)) != nil {
		data, err := os.ReadFile(src)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dst, bin), data, 0o755)) //nolint:gosec // a plugin binary
	}
}

// convertReplaceRegexp replaces the single match of pattern in a file.
func convertReplaceRegexp(t *testing.T, path, pattern, replacement string) {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	re := regexp.MustCompile(pattern)
	require.Len(t, re.FindAllIndex(data, -1), 1, "%s: expected one match of %s in\n%s", path, pattern, data)
	require.NoError(t, os.WriteFile(path, re.ReplaceAllLiteral(data, []byte(replacement)), 0o600))
}
