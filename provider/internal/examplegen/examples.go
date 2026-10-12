// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/docker/docker-agent/pkg/config"
	"github.com/docker/docker-agent/pkg/config/hcl"
	pygen "github.com/pulumi/pulumi/pkg/v3/codegen/python"

	"github.com/dirien/pulumi-docker-agent/provider/pkg/render"
	"github.com/dirien/pulumi-docker-agent/provider/pkg/validate"
)

// example is one upstream example configuration and what is generated from it.
type example struct {
	name   string   // the example name: the upstream path without extension, `/` -> `-`
	source string   // the upstream path relative to examples/, slash-separated
	twins  []string // HCL files of the same configuration, covered by this example
	dec    *render.Decoded
	head   []string // the comment block at the top of the upstream file
	golden string   // the YAML the programs render

	// skipValidation explains why the programs set skipValidation, or is empty.
	skipValidation *skipReason
	// dropped are the keys docker-agent never reads (render.UnreadKey), left out of the
	// programs, by path.
	dropped map[render.Path]droppedKey
	// reads are the files an HCL configuration reads with docker-agent's file() helper.
	reads []fileRead

	tsIdent, tsFile, pyModule string
}

type droppedKey struct {
	key, reason string
	value       any
}

// skipReason is a validation error that only happens because the provider validates in
// memory, with the explanation the programs carry.
type skipReason struct {
	match string // a substring of the docker-agent error
	what  string // short form for the README
	ts    string // the comment of the TypeScript program
	py    string // the comment of the Python program
}

// skipReasons are the validation errors the programs may avoid with skipValidation. Any other
// validation error fails the generation.
var skipReasons = []*skipReason{{
	match: "'instruction_file' is only supported for local file-based configs",
	what:  "`instruction_file` reads files relative to the agent file",
	ts: "`instructionFile` is read relative to the agent file when docker-agent loads it. The provider " +
		"validates the rendered YAML in memory, where there are no files, so validation is skipped.",
	py: "`instruction_file` is read relative to the agent file when docker-agent loads it. The provider " +
		"validates the rendered YAML in memory, where there are no files, so validation is skipped.",
}}

// loadExamples reads every example configuration under dir (the vendored upstream examples).
func loadExamples(ctx context.Context, dir string) ([]*example, error) {
	var yamls, hcls []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		switch filepath.Ext(p) {
		case ".yaml":
			yamls = append(yamls, filepath.ToSlash(rel))
		case ".hcl":
			hcls = append(hcls, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(yamls) == 0 {
		return nil, fmt.Errorf("no example configurations in %s; run `go run ./internal/schemagen generate` first", dir)
	}
	slices.Sort(yamls)
	slices.Sort(hcls)

	bySource := map[string]*example{}
	var out []*example
	for _, rel := range yamls {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			return nil, err
		}
		dec, err := render.DecodeYAML(data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", rel, err)
		}
		ex := &example{source: rel, dec: dec}
		bySource[rel] = ex
		out = append(out, ex)
	}
	for _, rel := range hcls {
		file := filepath.Join(dir, filepath.FromSlash(rel))
		twin := strings.TrimSuffix(rel, ".hcl") + ".yaml"
		if ex, ok := bySource[twin]; ok {
			if err := sameConfig(ctx, file, filepath.Join(dir, filepath.FromSlash(twin))); err != nil {
				return nil, fmt.Errorf("%s: %w", rel, err)
			}
			ex.twins = append(ex.twins, rel)
			continue
		}
		ex, err := loadHCL(ctx, file, rel)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", rel, err)
		}
		out = append(out, ex)
	}

	for _, ex := range out {
		if err := ex.finish(ctx, dir); err != nil {
			return nil, fmt.Errorf("%s: %w", ex.source, err)
		}
	}
	slices.SortFunc(out, func(a, b *example) int { return strings.Compare(a.name, b.name) })
	if err := checkNames(out); err != nil {
		return nil, err
	}
	return out, nil
}

// sameConfig checks that docker-agent loads an HCL example and its YAML twin into the same
// configuration, so the YAML example also stands for the HCL one.
func sameConfig(ctx context.Context, hclFile, yamlFile string) error {
	fromHCL, err := config.Load(ctx, hcl.NewSource(config.NewFileSource(hclFile)))
	if err != nil {
		return err
	}
	fromYAML, err := config.Load(ctx, config.NewFileSource(yamlFile))
	if err != nil {
		return err
	}
	a, err := json.Marshal(fromHCL)
	if err != nil {
		return err
	}
	b, err := json.Marshal(fromYAML)
	if err != nil {
		return err
	}
	if string(a) != string(b) {
		return fmt.Errorf("docker-agent loads it differently from %s; it needs its own program", filepath.Base(yamlFile))
	}
	return nil
}

// finish names the example, leaves out the keys docker-agent never reads, renders the golden
// YAML and validates it.
func (ex *example) finish(ctx context.Context, dir string) error {
	ext := path.Ext(ex.source)
	ex.name = strings.ReplaceAll(strings.TrimSuffix(ex.source, ext), "/", "-")
	ex.tsFile = ex.name + ".ts"
	ex.tsIdent = tsIdentifier(ex.name)
	ex.pyModule = pyModule(ex.name)
	ex.head = leadingComments(ex.dec)

	ex.dropped = map[render.Path]droppedKey{}
	for _, p := range ex.dec.Untyped {
		reason, ok := render.UnreadKey(p)
		if !ok {
			return fmt.Errorf("%s has no typed input: extend the generated types (provider/internal/schemagen), "+
				"or, if docker-agent never reads it, add it to the unread keys of provider/pkg/render", p)
		}
		parent, key, err := splitLast(p)
		if err != nil {
			return err
		}
		m, err := lookup(reflect.ValueOf(&ex.dec.Document).Elem(), parent)
		if err != nil {
			return fmt.Errorf("%s: %w", p, err)
		}
		ex.dropped[p] = droppedKey{key: key, reason: reason, value: m.MapIndex(reflect.ValueOf(key)).Interface()}
		m.SetMapIndex(reflect.ValueOf(key), reflect.Value{})
	}

	golden, err := render.Render(ex.dec.Document)
	if err != nil {
		return err
	}
	ex.golden = golden

	res := validate.Validate(ctx, ex.name+".yaml", []byte(golden))
	if res.Valid {
		return nil
	}
	for _, e := range res.Errors {
		i := slices.IndexFunc(skipReasons, func(r *skipReason) bool { return strings.Contains(e, r.match) })
		if i < 0 {
			return fmt.Errorf("the rendered configuration does not validate: %s", e)
		}
		ex.skipValidation = skipReasons[i]
	}
	// It must load where the files it refers to are.
	src := dirSource{name: path.Base(ex.source), dir: filepath.Join(dir, filepath.FromSlash(path.Dir(ex.source))), data: []byte(golden)}
	if _, err := config.Load(ctx, src); err != nil {
		return fmt.Errorf("the rendered configuration does not load next to the upstream file either: %w", err)
	}
	return nil
}

// dirSource is an in-memory configuration in a directory: like a file source, it resolves
// `instruction_file` references relative to that directory.
type dirSource struct {
	name, dir string
	data      []byte
}

func (s dirSource) Name() string                         { return s.name }
func (s dirSource) ParentDir() string                    { return s.dir }
func (s dirSource) Read(context.Context) ([]byte, error) { return s.data, nil }

// checkNames fails when two examples would get the same file, module or identifier.
func checkNames(examples []*example) error {
	seen := map[string]string{}
	for _, ex := range examples {
		for _, n := range []string{"ts:" + strings.ToLower(ex.tsFile), "id:" + ex.tsIdent, "py:" + ex.pyModule} {
			if other, ok := seen[n]; ok {
				return fmt.Errorf("%s and %s map to the same name %q", other, ex.source, n[3:])
			}
			seen[n] = ex.source
		}
	}
	return nil
}

// tsReserved are the JavaScript reserved words and the names the generated index.ts uses.
var tsReserved = []string{
	"break", "case", "catch", "class", "const", "continue", "debugger", "default", "delete", "do", "else",
	"enum", "export", "extends", "false", "finally", "for", "function", "if", "implements", "import", "in",
	"instanceof", "interface", "let", "new", "null", "package", "private", "protected", "public", "return",
	"static", "super", "switch", "this", "throw", "true", "try", "typeof", "var", "void", "while", "with",
	"yield", "await", "configs",
}

// tsIdentifier returns the camelCase identifier an example is imported as in index.ts.
func tsIdentifier(name string) string {
	var b strings.Builder
	upper := false
	for _, r := range name {
		switch {
		case r == '-' || r == '_' || r == '.':
			upper = b.Len() > 0
		case upper:
			b.WriteRune(unicode.ToUpper(r))
			upper = false
		default:
			b.WriteRune(r)
		}
	}
	id := b.String()
	if id == "" || unicode.IsDigit(rune(id[0])) {
		id = "example" + strings.ToUpper(id[:1]) + id[1:]
	}
	if slices.Contains(tsReserved, id) {
		id += "Example"
	}
	return id
}

// pyModule returns the Python module name of an example.
func pyModule(name string) string {
	m := strings.NewReplacer("-", "_", ".", "_").Replace(name)
	if m == "" || unicode.IsDigit(rune(m[0])) {
		m = "example_" + m
	}
	return pygen.EnsureKeywordSafe(m)
}

// pyName is the Python SDK's name of an input (snake_case).
func pyName(name string) string { return pygen.PyName(name) }

// splitLast splits a path into the path of its parent and its last map key.
func splitLast(p render.Path) (render.Path, string, error) {
	s := string(p)
	if !strings.HasSuffix(s, `"]`) {
		return "", "", fmt.Errorf("%s does not end with a map key", p)
	}
	i := strings.LastIndex(s, `["`)
	key, err := strconv.Unquote(s[i+1 : len(s)-1])
	if err != nil {
		return "", "", err
	}
	return render.Path(s[:i]), key, nil
}

// segments splits a path into input names, list indexes (int) and map keys (quoted strings).
func segments(p render.Path) ([]any, error) {
	s := string(p)
	var out []any
	for s != "" {
		switch {
		case strings.HasPrefix(s, "."):
			s = s[1:]
		case strings.HasPrefix(s, `["`):
			end := 2
			for end < len(s) && s[end] != '"' {
				if s[end] == '\\' {
					end++
				}
				end++
			}
			if end+1 >= len(s) || s[end+1] != ']' {
				return nil, fmt.Errorf("bad path %q", p)
			}
			k, err := strconv.Unquote(s[1 : end+1])
			if err != nil {
				return nil, fmt.Errorf("bad path %q: %w", p, err)
			}
			out = append(out, strconv.Quote(k))
			s = s[end+2:]
		case strings.HasPrefix(s, "["):
			end := strings.IndexByte(s, ']')
			i, err := strconv.Atoi(s[1:end])
			if err != nil {
				return nil, fmt.Errorf("bad path %q: %w", p, err)
			}
			out = append(out, i)
			s = s[end+1:]
		default:
			end := strings.IndexAny(s, ".[")
			if end < 0 {
				end = len(s)
			}
			out = append(out, s[:end])
			s = s[end:]
		}
	}
	return out, nil
}

// lookup returns the value at p below v (a Document).
func lookup(v reflect.Value, p render.Path) (reflect.Value, error) {
	segs, err := segments(p)
	if err != nil {
		return reflect.Value{}, err
	}
	for _, seg := range segs {
		for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
			v = v.Elem()
		}
		switch s := seg.(type) {
		case int:
			if v.Kind() != reflect.Slice || s >= v.Len() {
				return reflect.Value{}, fmt.Errorf("no item %d at %s", s, p)
			}
			v = v.Index(s)
		case string:
			if strings.HasPrefix(s, `"`) {
				k, _ := strconv.Unquote(s)
				if v.Kind() != reflect.Map {
					return reflect.Value{}, fmt.Errorf("no map for key %q at %s", k, p)
				}
				v = v.MapIndex(reflect.ValueOf(k))
				if !v.IsValid() {
					return reflect.Value{}, fmt.Errorf("no key %q at %s", k, p)
				}
				continue
			}
			f, ok := fieldByName(v.Type(), s)
			if !ok {
				return reflect.Value{}, fmt.Errorf("no input %q at %s", s, p)
			}
			v = v.Field(f.index)
		}
	}
	return v, nil
}
