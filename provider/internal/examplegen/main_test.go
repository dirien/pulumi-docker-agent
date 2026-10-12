// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/docker/docker-agent/pkg/config"
	"github.com/docker/docker-agent/pkg/config/hcl"
	hcl2 "github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/dirien/pulumi-docker-agent/provider/pkg/render"
)

var (
	genOnce     sync.Once
	genFiles    map[string][]byte
	genExamples []*example
	genErr      error
)

// generated runs the generator once for all tests.
func generated(t *testing.T) (map[string][]byte, []*example) {
	t.Helper()
	genOnce.Do(func() { genFiles, genExamples, genErr = generate(filepath.Join("..", "..", upstreamDir)) })
	require.NoError(t, genErr)
	return genFiles, genExamples
}

func upstreamFile(ex *example) string {
	return filepath.Join("..", "..", upstreamDir, filepath.FromSlash(ex.source))
}

// TestEveryUpstreamConfigIsConverted checks that every vendored configuration has a program in
// both languages and a golden file, or is the HCL twin of one that has.
func TestEveryUpstreamConfigIsConverted(t *testing.T) {
	t.Parallel()
	files, examples := generated(t)
	covered := map[string]bool{}
	for _, ex := range examples {
		covered[ex.source] = true
		for _, tw := range ex.twins {
			covered[tw] = true
		}
		assert.Contains(t, files, "typescript/examples/"+ex.tsFile)
		assert.Contains(t, files, "python/examples/"+ex.pyModule+".py")
		assert.Contains(t, files, "testdata/"+ex.name+".yaml")
	}
	dir := filepath.Join("..", "..", upstreamDir)
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || (filepath.Ext(p) != ".yaml" && filepath.Ext(p) != ".hcl") {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		require.NoError(t, err)
		assert.True(t, covered[filepath.ToSlash(rel)], "%s is not converted", rel)
		return nil
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(examples), 200)
}

// TestGenerateIsDeterministic generates twice and compares every file.
func TestGenerateIsDeterministic(t *testing.T) {
	t.Parallel()
	first, _ := generated(t)
	second, _, err := generate(filepath.Join("..", "..", upstreamDir))
	require.NoError(t, err)
	require.Equal(t, len(first), len(second))
	for name, data := range first {
		assert.Equal(t, string(data), string(second[name]), name)
	}
}

// TestGeneratedFilesAreCurrent compares the generated files with the ones in
// examples/docker-agent-examples. Run `make generate_config_types` when it fails.
func TestGeneratedFilesAreCurrent(t *testing.T) {
	t.Parallel()
	files, _ := generated(t)
	out := filepath.Join("..", "..", outDir)
	for name, data := range files {
		got, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(name)))
		if assert.NoError(t, err, "run make generate_config_types") {
			assert.Equal(t, string(data), string(got), "%s is stale; run make generate_config_types", name)
		}
	}
	err := filepath.WalkDir(out, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			if d != nil && (d.Name() == "node_modules" || d.Name() == "venv") {
				return filepath.SkipDir
			}
			return err
		}
		rel, err := filepath.Rel(out, p)
		require.NoError(t, err)
		rel = filepath.ToSlash(rel)
		if generatedFile(rel) {
			assert.Contains(t, files, rel, "%s is no longer generated; run make generate_config_types", rel)
		}
		return nil
	})
	require.NoError(t, err)
}

// TestGoldenIsTheRenderedUpstreamConfig re-derives every golden file: the upstream
// configuration decoded with render.DecodeYAML (keys docker-agent never reads removed) and
// rendered with render.Render. It also checks that docker-agent loads the golden file like
// the upstream configuration.
func TestGoldenIsTheRenderedUpstreamConfig(t *testing.T) {
	t.Parallel()
	files, examples := generated(t)
	for _, ex := range examples {
		t.Run(ex.name, func(t *testing.T) {
			t.Parallel()
			file := upstreamFile(ex)
			data, err := os.ReadFile(file)
			require.NoError(t, err)
			src := config.NewFileSource(file)
			if path.Ext(ex.source) == ".hcl" {
				src = hcl.NewSource(src)
				data, err = src.Read(t.Context())
				require.NoError(t, err)
			}
			dec, err := render.DecodeYAML(data)
			require.NoError(t, err)
			for _, p := range dec.Untyped {
				_, unread := render.UnreadKey(p)
				require.True(t, unread, "%s has no typed input", p)
				removeKey(t, &dec.Document, p)
			}
			want, err := render.Render(dec.Document)
			require.NoError(t, err)
			assert.Equal(t, want, string(files["testdata/"+ex.name+".yaml"]))

			// docker-agent loads the golden file like the upstream one, apart from the keys
			// it never reads.
			upstream, err := config.Load(t.Context(), src)
			require.NoError(t, err)
			golden, err := config.Load(t.Context(), dirSource{name: path.Base(ex.source), dir: filepath.Dir(file), data: []byte(want)})
			require.NoError(t, err)
			a, b := loadedJSON(t, upstream), loadedJSON(t, golden)
			if len(ex.dropped) == 0 {
				assert.JSONEq(t, a, b)
			}
		})
	}
}

func loadedJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return string(b)
}

func removeKey(t *testing.T, doc *render.Document, p render.Path) {
	t.Helper()
	parent, key, err := splitLast(p)
	require.NoError(t, err)
	m, err := lookup(reflect.ValueOf(doc).Elem(), parent)
	require.NoError(t, err)
	m.SetMapIndex(reflect.ValueOf(key), reflect.Value{})
}

// TestCommentsArePreserved collects every comment of every upstream configuration,
// independently of the decoder, and checks that each one appears in the TypeScript and the
// Python program, as often as in the upstream file. Directives (`yaml-language-server`
// modelines, a shebang on the first line) are the only comments that are not carried over.
func TestCommentsArePreserved(t *testing.T) {
	t.Parallel()
	files, examples := generated(t)
	total := 0
	for _, ex := range examples {
		comments := upstreamComments(t, ex)
		total += len(comments)
		for lang, file := range map[string]string{"ts": "typescript/examples/" + ex.tsFile, "py": "python/examples/" + ex.pyModule + ".py"} {
			lines := strings.Split(string(files[file]), "\n")
			want := map[string]int{}
			for _, c := range comments {
				want[c]++
			}
			for c, n := range want {
				expected := c
				if lang == "ts" {
					expected = "//" + strings.TrimPrefix(c, "#")
				}
				got := 0
				for _, l := range lines {
					trimmed := strings.TrimSpace(l)
					if trimmed == expected || strings.HasSuffix(l, " "+expected) || strings.HasSuffix(l, "  "+c) {
						got++
					}
				}
				assert.GreaterOrEqual(t, got, n, "%s: comment %q", file, c)
			}
		}
	}
	assert.Greater(t, total, 1500, "the upstream examples have well over a thousand comment lines")
}

var languageServerLine = regexp.MustCompile(`^#\s*yaml-language-server:`)

// upstreamComments returns the comment lines of an upstream configuration, without blank
// lines and directives.
func upstreamComments(t *testing.T, ex *example) []string {
	t.Helper()
	data, err := os.ReadFile(upstreamFile(ex))
	require.NoError(t, err)
	var out []string
	add := func(c string) {
		for _, l := range strings.Split(c, "\n") {
			l = strings.TrimSpace(l)
			if l == "" || languageServerLine.MatchString(l) {
				continue
			}
			if strings.HasPrefix(l, "#!") && strings.HasPrefix(string(data), l+"\n") {
				continue // the shebang on the first line
			}
			out = append(out, l)
		}
	}
	if path.Ext(ex.source) == ".hcl" {
		tokens, diags := hclsyntax.LexConfig(data, ex.source, hcl2.InitialPos)
		require.False(t, diags.HasErrors(), diags.Error())
		for _, tok := range tokens {
			if tok.Type == hclsyntax.TokenComment {
				text := strings.TrimSpace(string(tok.Bytes))
				if strings.HasPrefix(text, "//") {
					text = "#" + strings.TrimPrefix(text, "//")
				}
				add(text)
			}
		}
		return out
	}
	var root yaml.Node
	require.NoError(t, yaml.Unmarshal(data, &root))
	var walk func(n *yaml.Node)
	walk = func(n *yaml.Node) {
		add(n.HeadComment)
		add(n.LineComment)
		add(n.FootComment)
		for _, c := range n.Content {
			walk(c)
		}
	}
	walk(&root)
	return out
}

// TestPythonNamesMatchTheSDK checks the Python input names the generator uses against the
// generated Python SDK (sdk/python/ediri_docker_agent/_inputs.py).
func TestPythonNamesMatchTheSDK(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "sdk", "python", "ediri_docker_agent", "_inputs.py"))
	require.NoError(t, err)
	classes := map[string][]string{}
	var current string
	classRe := regexp.MustCompile(`^class (\w+)ArgsDict\(TypedDict\):`)
	keyRe := regexp.MustCompile(`^    (\w+): (NotRequired\[|pulumi\.Input|Any\b)`)
	for _, l := range strings.Split(string(data), "\n") {
		if m := classRe.FindStringSubmatch(l); m != nil {
			current = m[1]
			continue
		}
		if m := keyRe.FindStringSubmatch(l); m != nil && current != "" {
			classes[current] = append(classes[current], m[1])
		}
	}
	checked := 0
	seen := map[reflect.Type]bool{}
	var visit func(t reflect.Type)
	visit = func(typ reflect.Type) {
		for typ.Kind() == reflect.Pointer || typ.Kind() == reflect.Slice || typ.Kind() == reflect.Map {
			typ = typ.Elem()
		}
		if typ.Kind() != reflect.Struct || seen[typ] {
			return
		}
		seen[typ] = true
		keys, ok := classes[typ.Name()]
		if typ.Name() != "Document" {
			require.True(t, ok, "no %sArgsDict in the Python SDK", typ.Name())
		}
		for _, f := range fieldsOf(typ) {
			if ok {
				assert.Contains(t, keys, pyName(f.pulumi), "%s.%s", typ.Name(), f.pulumi)
				checked++
			}
			visit(typ.Field(f.index).Type)
		}
	}
	visit(reflect.TypeFor[render.Document]())
	assert.Greater(t, checked, 300)
}

// TestPythonSyntax parses every generated Python file with python3, when it is installed.
func TestPythonSyntax(t *testing.T) {
	t.Parallel()
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is not installed")
	}
	files, _ := generated(t)
	dir := t.TempDir()
	var names []string
	for name, data := range files {
		if strings.HasSuffix(name, ".py") {
			f := filepath.Join(dir, strings.ReplaceAll(name, "/", "_"))
			require.NoError(t, os.WriteFile(f, data, 0o600))
			names = append(names, f)
		}
	}
	slices.Sort(names)
	script := "import ast, sys\nfor f in sys.argv[1:]:\n    ast.parse(open(f, encoding='utf-8').read(), f)\n"
	out, err := exec.CommandContext(context.Background(), py, append([]string{"-I", "-c", script}, names...)...).CombinedOutput()
	require.NoError(t, err, "%s", out)
}

// TestEnumLiteralsInConstants checks that a constant (a YAML anchor) writes enum values
// `as const` in TypeScript: without it, TypeScript infers `string` for them, which the enum
// inputs do not accept. Values written in the AgentConfig arguments stay plain literals.
func TestEnumLiteralsInConstants(t *testing.T) {
	t.Parallel()
	dec, err := render.DecodeYAML([]byte(`agents:
  root:
    model: openai/gpt-5
    safety: strict
    hooks: &hooks
      session_start:
        - type: builtin
          command: add_date
  other:
    model: openai/gpt-5
    hooks: *hooks
`))
	require.NoError(t, err)
	prog, err := build(&example{name: "enums", dec: dec, dropped: map[render.Path]droppedKey{}})
	require.NoError(t, err)
	ts := tsProgram(prog)
	assert.Contains(t, ts, `const hooks = { sessionStart: [{ type: "builtin" as const, command: "add_date" }] };`)
	assert.Contains(t, ts, `safety: "strict",`)
	assert.NotContains(t, pyProgram(prog), "as const")
}

func TestStringLiterals(t *testing.T) {
	t.Parallel()
	assert.Equal(t, `"plain"`, quote("plain", true))
	assert.Equal(t, `'say "hi"'`, quote(`say "hi"`, false))
	assert.Equal(t, `"it's \"x\""`, quote(`it's "x"`, true))
	assert.Equal(t, `"a\\b\tc\x01"`, quote("a\\b\tc\x01", false))
	assert.Equal(t, []string{"`\\", "a \\${x} \\`b\\` \\\\s", "`"}, typescript.block("a ${x} `b` \\s\n"))
	assert.Equal(t, []string{`"""\`, `say \""hi"`, `x\"\""y\""""`}, python.block("say \"\"hi\"\nx\"\"\"y\""))
	assert.Equal(t, "1.0", formatFloat(1))
	assert.Equal(t, "0.75", formatFloat(0.75))
	assert.Equal(t, "example42", tsIdentifier("42"))
	assert.Equal(t, "ragBm25", tsIdentifier("rag-bm25"))
	assert.Equal(t, "example_42", pyModule("42"))
	assert.Equal(t, "rag_bm25", pyModule("rag-bm25"))
}
