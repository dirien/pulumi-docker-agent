// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package pcl

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/function"

	"github.com/dirien/pulumi-docker-agent/provider/pkg/render"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata/")

var (
	upstreamDir = filepath.Join("..", "render", "testdata", "upstream")
	// goldenDir holds what the example programs of examples/docker-agent-examples render:
	// render.Render of each decoded upstream example.
	goldenDir = filepath.Join("..", "..", "..", "examples", "docker-agent-examples", "testdata")
)

var (
	upstreamOnce    sync.Once
	upstreamConfigs []*Config
	upstreamDiags   hcl.Diagnostics
	upstreamProgram []byte
	upstreamErr     error
)

// upstream converts every upstream example once for all tests.
func upstream(t *testing.T) ([]*Config, []byte) {
	t.Helper()
	upstreamOnce.Do(func() {
		upstreamConfigs, upstreamDiags = Load(upstreamDir, LoadOptions{})
		if !upstreamDiags.HasErrors() {
			upstreamProgram, _, upstreamErr = Program(upstreamConfigs, Options{})
		}
	})
	require.False(t, upstreamDiags.HasErrors(), "%v", upstreamDiags)
	require.NoError(t, upstreamErr)
	return upstreamConfigs, upstreamProgram
}

// TestUpstreamExamplesRoundTrip converts every example configuration of docker-agent, evaluates
// the AgentConfig inputs of the program the way Pulumi hands them to the provider (objects by
// input name, every number a float64), renders them and compares the result with the YAML the
// generated example programs render (examples/docker-agent-examples/testdata).
func TestUpstreamExamplesRoundTrip(t *testing.T) {
	t.Parallel()
	configs, program := upstream(t)
	goldens, err := filepath.Glob(filepath.Join(goldenDir, "*.yaml"))
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(goldens), 200)

	// The HCL twins of YAML examples are recognised and skipped with a warning.
	var skipped []string
	for _, d := range upstreamDiags {
		if d.Severity == hcl.DiagWarning && d.Summary == "Skipped" {
			skipped = append(skipped, d.Subject.Filename)
		}
	}
	assert.ElementsMatch(t, []string{"gopher.hcl", "pirate.hcl"}, skipped)
	assert.Len(t, configs, len(goldens), "one AgentConfig per example")

	inputs := evalConfigs(t, program, upstreamDir)
	require.Len(t, inputs, len(goldens))
	for _, g := range goldens {
		name := strings.TrimSuffix(filepath.Base(g), ".yaml")
		args, ok := inputs[name]
		if !assert.True(t, ok, "no AgentConfig named %s", name) {
			continue
		}
		var doc render.Document
		require.NoError(t, assign(reflect.ValueOf(&doc).Elem(), args, ""), name)
		got, err := render.Render(doc)
		require.NoError(t, err, name)
		want, err := os.ReadFile(g)
		require.NoError(t, err)
		assert.Equal(t, string(want), got, "%s renders different YAML than %s", name, g)
	}
}

// TestUpstreamCommentsArePlaced checks that every comment of the upstream examples is placed
// at a YAML key (none is left over), and that every comment line of a YAML example is in the
// program.
func TestUpstreamCommentsArePlaced(t *testing.T) {
	t.Parallel()
	configs, program := upstream(t)
	text := string(program)
	for _, c := range configs {
		e := &emitter{c: c, dec: c.Decoded, used: map[render.Path]bool{}, usedNotes: map[render.Path]bool{}}
		e.object(reflect.ValueOf(&c.Decoded.Document).Elem(), "", "")
		require.NoError(t, e.err)
		n := len(e.notes)
		e.leftovers()
		assert.Len(t, e.notes, n, "%s: comments without a YAML key: %v", c.Source, e.notes[n:])

		if filepath.Ext(c.Source) != ".yaml" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(upstreamDir, filepath.FromSlash(c.Source)))
		require.NoError(t, err)
		for i, l := range yamlComments(data) {
			if languageServer.MatchString(l) || (i == 0 && strings.HasPrefix(l, "#!")) {
				continue // directives are left out
			}
			assert.Contains(t, text, strings.TrimSpace(commentText(l)), "%s: comment %q is missing", c.Source, l)
		}
	}
}

// yamlComments returns the lines of a YAML file that are only a comment (outside block
// scalars, roughly: lines whose first non-blank character is `#` and that are not more
// indented than the block scalar they would be in).
func yamlComments(data []byte) []string {
	var out []string
	s := bufio.NewScanner(bytes.NewReader(data))
	block := -1 // indentation of the key of the current block scalar
	for s.Scan() {
		line := s.Text()
		trimmed := strings.TrimSpace(line)
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if block >= 0 && (trimmed == "" || indent > block) {
			continue
		}
		block = -1
		if strings.HasPrefix(trimmed, "#") {
			out = append(out, trimmed)
			continue
		}
		if strings.HasSuffix(trimmed, "|") || strings.HasSuffix(trimmed, ">") || strings.HasSuffix(trimmed, "|-") ||
			strings.HasSuffix(trimmed, ">-") || strings.HasSuffix(trimmed, "|+") {
			block = indent
		}
	}
	return out
}

// TestProgramIsDeterministic converts the upstream examples twice.
func TestProgramIsDeterministic(t *testing.T) {
	t.Parallel()
	_, first := upstream(t)
	configs, diags := Load(upstreamDir, LoadOptions{})
	require.False(t, diags.HasErrors())
	second, _, err := Program(configs, Options{})
	require.NoError(t, err)
	assert.Equal(t, string(first), string(second))
}

// TestGoldenPrograms compares the programs of a few examples with testdata/<name>.pp. Run
// `go test ./pkg/pcl -update` to rewrite them.
func TestGoldenPrograms(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		paths []string
		opts  Options
	}{
		"budget":                 {paths: []string{"budget.yaml"}},
		"yaml-anchors":           {paths: []string{"yaml-anchors.yaml"}},
		"instruction_file":       {paths: []string{"instruction_file.yaml"}},
		"instructions_from_file": {paths: []string{"instructions_from_file.hcl"}},
		"pirate-push-run": {
			paths: []string{"pirate.yaml", "pirate.hcl"},
			opts:  Options{PushTo: "ghcr.io/acme/agents", Run: "Tell me about ${treasure}.", ProviderVersion: "0.1.0"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			configs, diags := Load(upstreamDir, LoadOptions{Paths: tc.paths})
			require.False(t, diags.HasErrors(), "%v", diags)
			got, _, err := Program(configs, tc.opts)
			require.NoError(t, err)
			path := filepath.Join("testdata", name+".pp")
			if *update {
				require.NoError(t, os.MkdirAll("testdata", 0o755))
				require.NoError(t, os.WriteFile(path, got, 0o600))
			}
			want, err := os.ReadFile(path)
			require.NoError(t, err, "golden file missing; run go test ./pkg/pcl -update")
			assert.Equal(t, string(want), string(got))
		})
	}
}

// TestPushAndRunSkipInstructionFile checks that a configuration with `instruction_file` gets
// no AgentArtifact or AgentRun: docker-agent cannot read its instruction files from an OCI
// artifact or a configuration in memory.
func TestPushAndRunSkipInstructionFile(t *testing.T) {
	t.Parallel()
	configs, diags := Load(upstreamDir, LoadOptions{Paths: []string{"instruction_file.yaml", "pirate.yaml"}})
	require.False(t, diags.HasErrors(), "%v", diags)
	program, warnings, err := Program(configs, Options{PushTo: "localhost:5000/agents", Run: "Hi"})
	require.NoError(t, err)
	require.Len(t, warnings, 1)
	assert.Equal(t, "instruction_file.yaml", warnings[0].Subject.Filename)
	assert.Equal(t, "No AgentArtifact or AgentRun", warnings[0].Summary)
	text := string(program)
	assert.Contains(t, text, `resource pirateArtifact "docker-agent:index:AgentArtifact"`)
	assert.Contains(t, text, `resource pirateRun "docker-agent:index:AgentRun"`)
	assert.Contains(t, text, `reference     = "localhost:5000/agents/pirate:latest"`)
	assert.NotContains(t, text, "instructionFileArtifact")
	assert.NotContains(t, text, "instructionFileRun")
	assert.Contains(t, text, "skipValidation = true")
}

// TestLanguageLimitations checks the warnings about values that the YAML and Go program
// generators write incorrectly, on the upstream examples that have them.
func TestLanguageLimitations(t *testing.T) {
	t.Parallel()
	configs, _ := upstream(t)
	_, diags, err := Program(configs, Options{})
	require.NoError(t, err)
	got := map[string][]string{}
	for _, d := range diags {
		got[d.Summary] = append(got[d.Summary], d.Subject.Filename)
	}
	assert.Equal(t, []string{"dev-team.yaml", "filesystem_allow_deny.yaml", "mcp-definitions.yaml",
		"shared-commands-skills.yaml", "shared-toolsets.yaml", "sub-agents-from-registry.yaml", "typo.yaml"},
		got[YAMLLimitation])
	assert.Equal(t, []string{"anthropic-fallbacks.yaml", "api-tool.yaml", "structured-output.yaml",
		"structured-output-tool-mode.yaml"}, got[GoLimitation])
	for _, d := range diags {
		switch d.Subject.Filename {
		case "filesystem_allow_deny.yaml":
			assert.Contains(t, d.Detail, "`agents.root.toolsets[0].allow_list[1]` is the string \"~\"")
			assert.Contains(t, d.Detail, "reads it as null")
		case "typo.yaml":
			assert.Contains(t, d.Detail, "rejects the key \"model\" of `models`")
		case "structured-output-tool-mode.yaml":
			assert.Contains(t, d.Detail, "`agents.root.structured_output.schema.required` is a list")
		}
	}
	for s, want := range map[string]string{"~": "null", "Null": "null", "True": "a bool", "0x1F": "a number",
		".inf": "a number", "16": "", "true": "", "yes": "", "2026-01-01": "", "~/.ssh": "", "": ""} {
		assert.Equal(t, want, misreadAs(s), s)
	}
}

func TestQuote(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"plain":            `"plain"`,
		`say "hi"\now`:     `"say \"hi\"\\now"`,
		"a\nb\tc\rd":       `"a\nb\tc\rd"`,
		"${env.X} %{ if }": `"$${env.X} %%{ if }"`,
		"$${x} $ { %":      `"$$${x} $ { %"`,
		"\x01\u2028":       `"\u0001\u2028"`,
		"\u00fc\u00f1\u00ed \U0001F3F4\u200D\u2620\uFE0F": "\"\u00fc\u00f1\u00ed \U0001F3F4\u200D\u2620\uFE0F\"", // written as is
	} {
		assert.Equal(t, want, quote(in), in)
		v := evalString(t, want)
		if !strings.ContainsRune(in, 0x2028) {
			assert.Equal(t, in, v, "the literal evaluates to the input")
		}
	}
}

func TestHeredoc(t *testing.T) {
	t.Parallel()
	assert.Empty(t, heredocMarker("one line"))
	assert.Empty(t, heredocMarker("no newline\nat the end"))
	assert.Empty(t, heredocMarker("\n"))
	assert.Empty(t, heredocMarker("carriage\r\nreturn\n"))
	assert.Equal(t, "EOT", heredocMarker("one line\n"))
	assert.Equal(t, "EOT2", heredocMarker("text\nEOT\n"))
	assert.Equal(t, "EOT3", heredocMarker("text\n  EOT\nEOT2\n"))

	// Template sequences are written in quoted strings, where pulumi-hcl keeps their escapes.
	assert.Empty(t, heredocMarker("a ${b}\n"))
	assert.Empty(t, heredocMarker("a %{ if }\n"))

	for _, s := range []string{"line\n", "a $b {c} % $ {d}\n  indented\n\ttab\n", "trailing  \n\nblank lines\n\n"} {
		src := "x = <<" + heredocMarker(s) + "\n" + s + heredocMarker(s) + "\n"
		f, diags := hclsyntax.ParseConfig([]byte(src), "t.pp", hcl.InitialPos)
		require.False(t, diags.HasErrors(), "%v", diags)
		v, diags := f.Body.(*hclsyntax.Body).Attributes["x"].Expr.Value(nil)
		require.False(t, diags.HasErrors(), "%v", diags)
		assert.Equal(t, s, v.AsString())
	}
}

func TestIdentifier(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"pirate":                 "pirate",
		"basic_agent":            "basicAgent",
		"dhi-dhi_agent":          "dhiDhiAgent",
		"rag-semantic.embedding": "ragSemanticEmbedding",
		"42":                     "agent42",
		"Code Review":            "codeReview",
		"---":                    "agent",
	} {
		assert.Equal(t, want, Identifier(in), in)
	}
	assert.Equal(t, "claude", objectKey("claude"))
	assert.Equal(t, `"gpt-4.1"`, objectKey("gpt-4.1"))
	assert.Equal(t, `"true"`, objectKey("true"))
	assert.Equal(t, `"$${x}"`, objectKey("${x}"))
}

func TestArtifactReferences(t *testing.T) {
	t.Parallel()
	one := []*Config{{Name: "basic_agent"}}
	two := []*Config{{Name: "basic_agent"}, {Name: "Basic-Agent"}}

	refs, err := artifactReferences(two, "ghcr.io/acme/agents/")
	require.NoError(t, err)
	assert.Equal(t, "ghcr.io/acme/agents/basic-agent:latest", refs[two[0]])
	assert.Equal(t, "ghcr.io/acme/agents/basic-agent-2:latest", refs[two[1]])

	refs, err = artifactReferences(one, "localhost:5000/team/assistant:v1")
	require.NoError(t, err)
	assert.Equal(t, "localhost:5000/team/assistant:v1", refs[one[0]])

	refs, err = artifactReferences(one, "localhost:5000/team")
	require.NoError(t, err)
	assert.Equal(t, "localhost:5000/team/basic-agent:latest", refs[one[0]])

	_, err = artifactReferences(two, "ghcr.io/acme/agent:v1")
	assert.ErrorContains(t, err, "single configuration")
	_, err = artifactReferences(one, "ghcr.io/acme/agent@sha256:abc")
	assert.Error(t, err)
	_, err = artifactReferences(one, "oci://ghcr.io/acme")
	assert.Error(t, err)
}

// TestLoadDirectory covers which files are converted, untyped keys, and names.
func TestLoadDirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(dir, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
	}
	agent := "agents:\n  root:\n    model: openai/gpt-5-mini\n"
	write("agent.yaml", "# My agent\n\n"+agent+"    future_key: 42 # not typed yet\n")
	write("team/agent.yml", agent)
	write("team-agent.yaml", agent)
	write("Pulumi.yaml", "name: x\nruntime: yaml\n")
	write("Pulumi.dev.yaml", agent)
	write("compose.yaml", "services:\n  web:\n    image: nginx\n")
	write("broken.yaml", "not: [valid\n")
	write(".github/agent.yaml", agent)
	write("node_modules/x/agent.yaml", agent)
	write("out/agent.yaml", agent)
	write("main.tf", "resource \"x\" \"y\" {}\n")
	write("packer.hcl", "source \"amazon-ebs\" \"x\" {\n  region = \"eu\"\n}\n")

	configs, diags := Load(dir, LoadOptions{Exclude: []string{filepath.Join(dir, "out")}})
	require.False(t, diags.HasErrors(), "%v", diags)
	var names, sources []string
	for _, c := range configs {
		names = append(names, c.Name)
		sources = append(sources, c.Source)
	}
	assert.Equal(t, []string{"agent", "team-agent", "team-agent-yml"}, names)
	assert.Equal(t, []string{"agent.yaml", "team-agent.yaml", "team/agent.yml"}, sources)
	require.Len(t, diags, 1)
	assert.Equal(t, hcl.DiagWarning, diags[0].Severity)
	assert.Contains(t, diags[0].Summary, `agents[0].extra["future_key"]`)

	program, _, err := Program(configs, Options{})
	require.NoError(t, err)
	text := string(program)
	assert.Contains(t, text, "      extra = {\n        future_key = 42\n      }")
	assert.Contains(t, text, "# My agent\n#\n# agents.root.future_key:\n#   not typed yet\n")
	assert.Contains(t, text, `__logicalName = "team-agent-yml"`)

	// A single file, and a file that is not an agent configuration.
	configs, diags = Load(dir, LoadOptions{Paths: []string{"team/agent.yml", "compose.yaml"}})
	require.False(t, diags.HasErrors(), "%v", diags)
	require.Len(t, configs, 1)
	assert.Equal(t, "team-agent", configs[0].Name)

	_, diags = Load(dir, LoadOptions{Paths: []string{"missing.yaml"}})
	assert.True(t, diags.HasErrors())
	_, diags = Load(dir, LoadOptions{Paths: []string{"../outside.yaml"}})
	assert.True(t, diags.HasErrors())

	write("bad-agent.yaml", "agents:\n  root: [not, a, mapping]\n")
	_, diags = Load(dir, LoadOptions{Paths: []string{"bad-agent.yaml"}})
	assert.True(t, diags.HasErrors())
}

// TestLoadHCL covers comments, the order of attributes and file() reads of an HCL file.
func TestLoadHCL(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "agents", "prompts"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "agents", "prompts", "root.md"), []byte("Be helpful.\nBe brief.\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "agents", "team.hcl"), []byte(`// The team.

model "fast" {
  provider = "openai"   # the default provider
  model    = "gpt-5-mini"
}

/* The root agent
   delegates to the helper. */
agent "root" {
  model       = "fast"
  instruction = file("prompts/root.md")
  description = "Root"

  toolset "shell" {}
  # Reads files.
  toolset "filesystem" {}
}

agent "helper" {
  model       = "fast"
  instruction = file("prompts/root.md", { unused = "x" })
}
# The end.
`), 0o600))
	configs, diags := Load(dir, LoadOptions{})
	require.False(t, diags.HasErrors(), "%v", diags)
	require.Len(t, configs, 1)
	c := configs[0]
	assert.Equal(t, "agents-team", c.Name)
	assert.Equal(t, []string{"agents/prompts/root.md"}, c.Files)
	assert.Equal(t, map[render.Path]string{"agents[0].instruction": "agents/prompts/root.md"}, c.ReadFiles)
	assert.Equal(t, []string{"models", "agents"}, c.Decoded.Order[""])
	assert.Equal(t, []string{"model", "instruction", "description", "toolsets"}, c.Decoded.Order["agents[0]"])

	program, _, err := Program(configs, Options{})
	require.NoError(t, err)
	text := string(program)
	assert.Contains(t, text, `# Converted from agents/team.hcl.
#
# The team.
#
# models.fast.provider:
#   the default provider
#
# agents.root:
#   The root agent
#   delegates to the helper.
#
# agents.root.toolsets[1]:
#   Reads files.
#
# agents.helper.instruction:
#   The HCL file renders prompts/root.md as a template with file() and the variables { unused =
#   "x" }.`)
	assert.Contains(t, text, "# end of agents/team.hcl:\n#   The end.\n")
	assert.Contains(t, text, `instruction = readFile("agents/prompts/root.md")`)

	inputs := evalConfigs(t, program, dir)
	var doc render.Document
	require.NoError(t, assign(reflect.ValueOf(&doc).Elem(), inputs["agents-team"], ""))
	got, err := render.Render(doc)
	require.NoError(t, err)
	want, err := render.Render(c.Decoded.Document)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

// evalConfigs evaluates the inputs of the AgentConfig resources of a program, by logical
// name, as Pulumi passes them to the provider. readFile reads relative to dir.
func evalConfigs(t *testing.T, program []byte, dir string) map[string]map[string]any {
	t.Helper()
	f, diags := hclsyntax.ParseConfig(program, "main.pp", hcl.InitialPos)
	require.False(t, diags.HasErrors(), "%v", diags)
	ctx := &hcl.EvalContext{Functions: map[string]function.Function{
		"readFile": function.New(&function.Spec{
			Params: []function.Parameter{{Name: "path", Type: cty.String}},
			Type:   function.StaticReturnType(cty.String),
			Impl: func(args []cty.Value, _ cty.Type) (cty.Value, error) {
				data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(args[0].AsString())))
				return cty.StringVal(string(data)), err
			},
		}),
	}}
	out := map[string]map[string]any{}
	for _, b := range f.Body.(*hclsyntax.Body).Blocks {
		if b.Type != "resource" || b.Labels[1] != ConfigType {
			continue
		}
		name := b.Labels[0]
		args := map[string]any{}
		for n, a := range b.Body.Attributes {
			v, diags := a.Expr.Value(ctx)
			require.False(t, diags.HasErrors(), "%s.%s: %v", name, n, diags)
			args[n] = goValue(v)
		}
		if ln, ok := args["__logicalName"].(string); ok {
			name = ln
		}
		delete(args, "__logicalName")
		delete(args, "skipValidation")
		require.NotContains(t, out, name)
		out[name] = args
	}
	return out
}

// goValue converts a PCL value the way Pulumi represents it: every number is a float64.
func goValue(v cty.Value) any {
	if v.IsNull() {
		return nil
	}
	t := v.Type()
	switch {
	case t == cty.String:
		return v.AsString()
	case t == cty.Bool:
		return v.True()
	case t == cty.Number:
		f, _ := v.AsBigFloat().Float64()
		return f
	case t.IsObjectType() || t.IsMapType():
		out := map[string]any{}
		for it := v.ElementIterator(); it.Next(); {
			k, e := it.Element()
			out[k.AsString()] = goValue(e)
		}
		return out
	case t.IsTupleType() || t.IsListType():
		out := []any{}
		for it := v.ElementIterator(); it.Next(); {
			_, e := it.Element()
			out = append(out, goValue(e))
		}
		return out
	}
	panic(fmt.Sprintf("unsupported %s", t.FriendlyName()))
}

// assign decodes a Pulumi value into a typed input, by input name.
func assign(dst reflect.Value, v any, p render.Path) error {
	if v == nil {
		return nil
	}
	wrong := func() error { return fmt.Errorf("%s: %T for %s", p, v, dst.Type()) }
	switch dst.Kind() {
	case reflect.Pointer:
		ptr := reflect.New(dst.Type().Elem())
		if err := assign(ptr.Elem(), v, p); err != nil {
			return err
		}
		dst.Set(ptr)
	case reflect.Struct:
		m, ok := v.(map[string]any)
		if !ok {
			return wrong()
		}
		for k, val := range m {
			f, ok := fieldByName(dst.Type(), k)
			if !ok {
				return fmt.Errorf("%s: %s has no input %q", p, dst.Type().Name(), k)
			}
			if err := assign(dst.Field(f.index), val, p.Field(k)); err != nil {
				return err
			}
		}
	case reflect.Slice:
		l, ok := v.([]any)
		if !ok {
			return wrong()
		}
		s := reflect.MakeSlice(dst.Type(), len(l), len(l))
		for i, item := range l {
			if err := assign(s.Index(i), item, p.Index(i)); err != nil {
				return err
			}
		}
		dst.Set(s)
	case reflect.Map:
		m, ok := v.(map[string]any)
		if !ok {
			return wrong()
		}
		out := reflect.MakeMap(dst.Type())
		for k, val := range m {
			elem := reflect.New(dst.Type().Elem()).Elem()
			if err := assign(elem, val, p.Key(k)); err != nil {
				return err
			}
			out.SetMapIndex(reflect.ValueOf(k).Convert(dst.Type().Key()), elem)
		}
		dst.Set(out)
	case reflect.Interface:
		dst.Set(reflect.ValueOf(v))
	case reflect.String:
		s, ok := v.(string)
		if !ok {
			return wrong()
		}
		dst.SetString(s)
	case reflect.Bool:
		b, ok := v.(bool)
		if !ok {
			return wrong()
		}
		dst.SetBool(b)
	case reflect.Int, reflect.Int64:
		f, ok := v.(float64)
		if !ok || f != math.Trunc(f) {
			return wrong()
		}
		dst.SetInt(int64(f))
	case reflect.Float64:
		f, ok := v.(float64)
		if !ok {
			return wrong()
		}
		dst.SetFloat(f)
	default:
		return fmt.Errorf("%s: unsupported %s", p, dst.Kind())
	}
	return nil
}

func evalString(t *testing.T, literal string) string {
	t.Helper()
	f, diags := hclsyntax.ParseConfig([]byte("x = "+literal+"\n"), "t.pp", hcl.InitialPos)
	require.False(t, diags.HasErrors(), "%v", diags)
	v, diags := f.Body.(*hclsyntax.Body).Attributes["x"].Expr.Value(nil)
	require.False(t, diags.HasErrors(), "%v", diags)
	return v.AsString()
}
