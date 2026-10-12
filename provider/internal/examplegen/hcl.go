// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"github.com/docker/docker-agent/pkg/config"
	"github.com/docker/docker-agent/pkg/config/hcl"
	hcl2 "github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"

	"github.com/dirien/pulumi-docker-agent/provider/pkg/render"
)

// fileRead is a value an HCL configuration reads with docker-agent's file() helper. The
// programs read the file themselves; it is copied next to them.
type fileRead struct {
	path    render.Path       // the input the file's content goes to
	file    string            // relative to the configuration, slash-separated
	vars    map[string]string // template variables (`${name}` in the file), nil for a plain read
	content []byte
}

// copiedRead matches the paths of files that HCL examples read with file() and that are copied
// next to the programs (typescript/examples/<path> and python/examples/<path>): text files
// (.md, .txt) in the example's directory or below, and no segment that is empty, `..` or hidden.
// generatedFile and the allowed paths of .github/workflows/upgrade-docker-agent.yml accept the
// same paths.
var copiedRead = regexp.MustCompile(`^(?:[A-Za-z0-9_-][A-Za-z0-9._-]*/)*[A-Za-z0-9_-][A-Za-z0-9._-]*\.(?:md|txt)$`)

// placeholder is the `${name}` interpolation of an HCL template.
var placeholder = regexp.MustCompile(`\$\{(\w+)\}`)

// loadHCL loads an HCL example that has no YAML twin: docker-agent converts it to YAML, which
// is decoded like the YAML examples. The comments, the order of the attributes and the
// file() calls come from the HCL source. Only attributes of `agent` blocks are supported;
// anything else fails, so a new kind of HCL example gets a look before it is converted.
func loadHCL(ctx context.Context, file, rel string) (*example, error) {
	converted, err := hcl.NewSource(config.NewFileSource(file)).Read(ctx)
	if err != nil {
		return nil, err
	}
	dec, err := render.DecodeYAML(converted)
	if err != nil {
		return nil, err
	}
	src, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	parsed, diags := hclsyntax.ParseConfig(src, filepath.Base(file), hcl2.InitialPos)
	if diags.HasErrors() {
		return nil, diags
	}

	ex := &example{source: rel, dec: dec}
	type item struct {
		line, col int
		path      render.Path
	}
	var items []item
	for _, block := range parsed.Body.(*hclsyntax.Body).Blocks {
		if block.Type != "agent" || len(block.Labels) != 1 {
			return nil, fmt.Errorf("%s block: only agent blocks are supported; extend provider/internal/examplegen/hcl.go", block.Type)
		}
		i := slices.IndexFunc(dec.Document.Agents, func(a render.Agent) bool { return a.Name == block.Labels[0] })
		if i < 0 {
			return nil, fmt.Errorf("agent %q is missing from the converted configuration", block.Labels[0])
		}
		agentPath := render.Path("agents").Index(i)
		items = append(items, item{block.TypeRange.Start.Line, block.TypeRange.Start.Column, agentPath})
		if len(block.Body.Blocks) > 0 {
			return nil, fmt.Errorf("agent %q: nested blocks are not supported; extend provider/internal/examplegen/hcl.go", block.Labels[0])
		}
		attrs := make([]*hclsyntax.Attribute, 0, len(block.Body.Attributes))
		for _, a := range block.Body.Attributes {
			attrs = append(attrs, a)
		}
		slices.SortFunc(attrs, func(a, b *hclsyntax.Attribute) int { return a.SrcRange.Start.Byte - b.SrcRange.Start.Byte })
		var order []string
		for _, a := range attrs {
			f, ok := fieldByYAMLKey(reflect.TypeFor[render.Agent](), a.Name)
			if !ok {
				return nil, fmt.Errorf("agent %q: no input for attribute %q", block.Labels[0], a.Name)
			}
			p := agentPath.Field(f.pulumi)
			order = append(order, f.pulumi)
			items = append(items, item{a.SrcRange.Start.Line, a.SrcRange.Start.Column, p})
			if call, ok := a.Expr.(*hclsyntax.FunctionCallExpr); ok && call.Name == "file" {
				read, err := fileCall(call, filepath.Dir(file), dec, p)
				if err != nil {
					return nil, fmt.Errorf("agent %q, %s: %w", block.Labels[0], a.Name, err)
				}
				ex.reads = append(ex.reads, read)
			}
		}
		dec.Order[agentPath] = order
	}

	// Comments: a comment on the line of an attribute is its line comment, the others belong
	// to the next attribute or block.
	tokens, diags := hclsyntax.LexConfig(src, filepath.Base(file), hcl2.InitialPos)
	if diags.HasErrors() {
		return nil, diags
	}
	for _, tok := range tokens {
		if tok.Type != hclsyntax.TokenComment {
			continue
		}
		text := strings.TrimRight(string(tok.Bytes), "\r\n")
		if strings.HasPrefix(text, "//") {
			text = "#" + strings.TrimPrefix(text, "//")
		}
		line := tok.Range.Start.Line
		c := func(p render.Path) *render.Comment {
			if dec.Comments[p] == nil {
				dec.Comments[p] = &render.Comment{}
			}
			return dec.Comments[p]
		}
		if i := slices.IndexFunc(items, func(it item) bool { return it.line == line && it.col < tok.Range.Start.Column }); i >= 0 {
			c(items[i].path).Line = append(c(items[i].path).Line, text)
			continue
		}
		next := -1
		for i, it := range items {
			if it.line > line && (next < 0 || it.line < items[next].line) {
				next = i
			}
		}
		if next < 0 {
			dec.Foot = append(dec.Foot, text)
			continue
		}
		c(items[next].path).Head = append(c(items[next].path).Head, text)
	}
	return ex, nil
}

// fileCall reads the arguments of a `file("path", { name = "value" })` call and checks that
// reading the file and replacing each `${name}` gives what docker-agent loaded.
func fileCall(call *hclsyntax.FunctionCallExpr, dir string, dec *render.Decoded, p render.Path) (fileRead, error) {
	if len(call.Args) < 1 || len(call.Args) > 2 {
		return fileRead{}, fmt.Errorf("file() takes one or two arguments")
	}
	name, diags := call.Args[0].Value(nil)
	if diags.HasErrors() || name.Type() != cty.String {
		return fileRead{}, fmt.Errorf("the file() path must be a literal string")
	}
	read := fileRead{path: p, file: path.Clean(name.AsString())}
	if strings.HasPrefix(read.file, "../") || path.IsAbs(read.file) {
		return fileRead{}, fmt.Errorf("file() path %q is outside the example directory", read.file)
	}
	if !copiedRead.MatchString(read.file) {
		return fileRead{}, fmt.Errorf("file() path %q cannot be copied next to the programs: only .md and .txt files "+
			"in the example's directory or below it, with letters, digits, '.', '_' and '-' in their path (no "+
			"segment starting with '.'), are; extend copiedRead, generatedFile and the allowed paths of "+
			".github/workflows/upgrade-docker-agent.yml together", read.file)
	}
	content, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(read.file)))
	if err != nil {
		return fileRead{}, err
	}
	read.content = content
	want := string(content)
	if len(call.Args) == 2 {
		vars, diags := call.Args[1].Value(nil)
		if diags.HasErrors() || !vars.Type().IsObjectType() {
			return fileRead{}, fmt.Errorf("the file() template variables must be a literal object")
		}
		read.vars = map[string]string{}
		for k, v := range vars.AsValueMap() {
			if v.Type() != cty.String {
				return fileRead{}, fmt.Errorf("template variable %q is not a string", k)
			}
			read.vars[k] = v.AsString()
		}
		if strings.Contains(want, "%{") || strings.Contains(want, "$${") {
			return fileRead{}, fmt.Errorf("%s uses HCL template directives or escapes; only ${name} is supported", read.file)
		}
		want = placeholder.ReplaceAllStringFunc(want, func(m string) string {
			return read.vars[placeholder.FindStringSubmatch(m)[1]]
		})
	}
	got, err := lookup(reflect.ValueOf(&dec.Document).Elem(), p)
	if err != nil {
		return fileRead{}, err
	}
	for got.Kind() == reflect.Pointer {
		got = got.Elem()
	}
	if got.Kind() != reflect.String || got.String() != want {
		return fileRead{}, fmt.Errorf("reading %s and replacing the ${name} placeholders does not give what docker-agent loads", read.file)
	}
	return read, nil
}
