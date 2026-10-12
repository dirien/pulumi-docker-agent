// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package pcl

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"

	dahcl "github.com/docker/docker-agent/pkg/config/hcl"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
	"gopkg.in/yaml.v3"

	"github.com/dirien/pulumi-docker-agent/provider/pkg/render"
)

// loadHCL loads a docker-agent HCL file. docker-agent's own converter turns it into YAML,
// which is decoded like a YAML file; the comments, the order of the agent attributes and the
// file() calls come from the HCL source.
func loadHCL(dir, rel string, data []byte) (*Config, hcl.Diagnostics) {
	file := filepath.Join(dir, filepath.FromSlash(rel))
	converted, err := dahcl.ToYAML(data, file)
	if err != nil {
		if dahcl.LooksLikeHCL(data) {
			return nil, hcl.Diagnostics{diagnostic(hcl.DiagError, rel, "Cannot convert the configuration", err.Error())}
		}
		return nil, nil // not a docker-agent file
	}
	var probe yaml.Node
	if err := yaml.Unmarshal(converted, &probe); err != nil || !hasAgents(&probe) {
		return nil, nil
	}
	dec, err := render.DecodeYAML(converted)
	if err != nil {
		return nil, hcl.Diagnostics{diagnostic(hcl.DiagError, rel, "Cannot convert the configuration", err.Error())}
	}
	parsed, diags := hclsyntax.ParseConfig(data, rel, hcl.InitialPos)
	if diags.HasErrors() {
		return nil, diags
	}
	c := &Config{Source: rel, Decoded: dec}
	h := &hclFile{c: c, dir: dir, base: path.Dir(rel), src: data, counts: map[string]int{}, reads: map[string]bool{}}
	h.body(parsed.Body.(*hclsyntax.Body), "", true)
	if err := h.comments(data); err != nil {
		return nil, hcl.Diagnostics{diagnostic(hcl.DiagError, rel, "Cannot convert the configuration", err.Error())}
	}
	return c, c.finish(dir)
}

// hclFile collects what the converter takes from the HCL source: where each attribute and
// block is (to place the comments), the order of the agent attributes and the file() calls.
type hclFile struct {
	c      *Config
	dir    string
	base   string // the directory of the file, relative to dir
	src    []byte
	items  []hclItem
	counts map[string]int  // list items per YAML key path
	reads  map[string]bool // the attributes (YAML key paths) the program reads with readFile
}

type hclItem struct {
	line, col int
	label     string
}

// listBlocks are the block types that docker-agent's converter collects into a list, with
// the YAML key of the list. Other blocks with a label become a map keyed by the label, and
// blocks without a label a single object.
var listBlocks = map[string]string{
	"toolset": "toolsets", "post_edit": "post_edit", "strategy": "strategies", "routing": "routing",
	"hook": "hooks", "pre_tool_use": "pre_tool_use", "post_tool_use": "post_tool_use",
	"session_start": "session_start", "session_end": "session_end", "permission_request": "permission_request",
	"tool_response_transform": "tool_response_transform", "tool_input_transform": "tool_input_transform",
	"tool_guard": "tool_guard", "before_agent_run": "before_agent_run", "after_agent_complete": "after_agent_complete",
}

// mapBlocks are the labelled block types whose YAML key differs from the block type.
var mapBlocks = map[string]string{
	"agent": "agents", "model": "models", "evaluator": "evaluators", "provider": "providers", "mcp": "mcps",
	"command": "commands", "skill": "skills",
}

// body records the attributes and blocks of an HCL body whose YAML key path is y.
func (h *hclFile) body(b *hclsyntax.Body, y string, top bool) {
	type positioned struct {
		start hcl.Pos
		attr  *hclsyntax.Attribute
		block *hclsyntax.Block
	}
	var all []positioned
	for _, a := range b.Attributes {
		all = append(all, positioned{start: a.SrcRange.Start, attr: a})
	}
	for _, bl := range b.Blocks {
		all = append(all, positioned{start: bl.TypeRange.Start, block: bl})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].start.Byte < all[j].start.Byte })

	var rootOrder []string
	for _, it := range all {
		if a := it.attr; a != nil {
			label := fieldLabel(y, a.Name)
			h.items = append(h.items, hclItem{a.SrcRange.Start.Line, a.SrcRange.Start.Column, label})
			h.fileCalls(a.Expr, label)
			if top {
				rootOrder = append(rootOrder, a.Name)
			}
			continue
		}
		bl := it.block
		var label, key string
		switch {
		case listBlocks[bl.Type] != "":
			key = listBlocks[bl.Type]
			list := fieldLabel(y, key)
			label = indexLabel(list, h.counts[list])
			h.counts[list]++
		case len(bl.Labels) == 1:
			key = bl.Type
			if k, ok := mapBlocks[bl.Type]; ok {
				key = k
			}
			label = keyLabel(fieldLabel(y, key), bl.Labels[0])
		default:
			key = bl.Type
			label = fieldLabel(y, key)
		}
		if top {
			rootOrder = append(rootOrder, key)
		}
		h.items = append(h.items, hclItem{bl.TypeRange.Start.Line, bl.TypeRange.Start.Column, label})
		if top && bl.Type == "agent" && len(bl.Labels) == 1 {
			h.agent(bl, label)
		}
		h.body(bl.Body, label, false)
	}
	if top {
		h.rootOrder(rootOrder)
	}
}

// rootOrder sets the order of the top-level inputs to the order of the HCL file.
func (h *hclFile) rootOrder(keys []string) {
	var order []string
	for _, k := range keys {
		if f, ok := fieldByYAMLKey(reflect.TypeFor[render.Document](), k); ok && !slices.Contains(order, f.pulumi) {
			order = append(order, f.pulumi)
		}
	}
	h.c.Decoded.Order[""] = order
}

// agent sets the order of an agent's attributes and reads its file() calls.
func (h *hclFile) agent(bl *hclsyntax.Block, label string) {
	dec := h.c.Decoded
	i := slices.IndexFunc(dec.Document.Agents, func(a render.Agent) bool { return a.Name == bl.Labels[0] })
	if i < 0 {
		return
	}
	agentPath := render.Path("agents").Index(i)
	attrs := make([]*hclsyntax.Attribute, 0, len(bl.Body.Attributes))
	for _, a := range bl.Body.Attributes {
		attrs = append(attrs, a)
	}
	sort.Slice(attrs, func(i, j int) bool { return attrs[i].SrcRange.Start.Byte < attrs[j].SrcRange.Start.Byte })
	var order []string
	for _, a := range attrs {
		f, ok := fieldByYAMLKey(reflect.TypeFor[render.Agent](), a.Name)
		if !ok {
			continue
		}
		order = append(order, f.pulumi)
		call, ok := a.Expr.(*hclsyntax.FunctionCallExpr)
		if !ok || call.Name != "file" || len(call.Args) != 1 {
			continue
		}
		// file("x.md") reads a file as is: the program reads it too.
		p := agentPath.Field(f.pulumi)
		if rel, ok := h.plainRead(call, dec, p); ok {
			h.reads[fieldLabel(label, a.Name)] = true
			if h.c.ReadFiles == nil {
				h.c.ReadFiles = map[render.Path]string{}
			}
			h.c.ReadFiles[p] = rel
			h.c.addFile(rel)
		}
	}
	for _, f := range dec.Order[agentPath] {
		if !slices.Contains(order, f) {
			order = append(order, f)
		}
	}
	dec.Order[agentPath] = order
}

// plainRead returns the file a one-argument file() call reads, relative to the source
// directory, when its content is exactly the value docker-agent loaded.
func (h *hclFile) plainRead(call *hclsyntax.FunctionCallExpr, dec *render.Decoded, p render.Path) (string, bool) {
	name, diags := call.Args[0].Value(nil)
	if diags.HasErrors() || name.Type() != cty.String || !name.IsKnown() || name.IsNull() {
		return "", false
	}
	rel, ok := inside(h.dir, h.base, name.AsString())
	if !ok {
		return "", false
	}
	content, err := os.ReadFile(filepath.Join(h.dir, filepath.FromSlash(rel)))
	if err != nil {
		return "", false
	}
	v, err := lookup(reflect.ValueOf(&dec.Document).Elem(), p)
	if err != nil {
		return "", false
	}
	for v.Kind() == reflect.Pointer && !v.IsNil() {
		v = v.Elem()
	}
	if v.Kind() != reflect.String || v.String() != string(content) {
		return "", false
	}
	return rel, true
}

// fileCalls adds a note for each file() call in an expression whose value the program holds
// as text: a template, or a file the program does not read itself.
func (h *hclFile) fileCalls(expr hclsyntax.Expression, label string) {
	_ = hclsyntax.VisitAll(expr, func(n hclsyntax.Node) hcl.Diagnostics {
		call, ok := n.(*hclsyntax.FunctionCallExpr)
		if !ok || call.Name != "file" || len(call.Args) == 0 || (n == expr && h.reads[label]) {
			return nil
		}
		file := "a file"
		if v, diags := call.Args[0].Value(nil); !diags.HasErrors() && v.Type() == cty.String && v.IsKnown() && !v.IsNull() {
			file = v.AsString()
		}
		text := fmt.Sprintf("The HCL file reads this value from %s with file(). ", file)
		if len(call.Args) > 1 {
			text = fmt.Sprintf("The HCL file renders %s as a template with file() and the variables %s. ",
				file, h.text(call.Args[1]))
		}
		text += "The program holds the value docker-agent loaded when the file was converted; " +
			"read the file in the program instead to keep them in sync."
		h.c.Comments = append(h.c.Comments, Note{Label: label, Lines: note("", text).Lines})
		return nil
	})
}

// text returns the source text of an expression on one line.
func (h *hclFile) text(expr hclsyntax.Expression) string {
	rng := expr.Range()
	if rng.End.Byte > len(h.src) || rng.Start.Byte > rng.End.Byte {
		return "given"
	}
	return strings.Join(strings.Fields(string(h.src[rng.Start.Byte:rng.End.Byte])), " ")
}

// comments places the comments of the HCL source: a comment on the line of an attribute or
// block is its line comment, the others belong to the next attribute or block. Comments
// before the first item are the head of the file, comments after the last its foot.
func (h *hclFile) comments(src []byte) error {
	tokens, diags := hclsyntax.LexConfig(src, h.c.Source, hcl.InitialPos)
	if diags.HasErrors() {
		return diags
	}
	sort.SliceStable(h.items, func(i, j int) bool {
		if h.items[i].line != h.items[j].line {
			return h.items[i].line < h.items[j].line
		}
		return h.items[i].col < h.items[j].col
	})
	type placed struct {
		item  int // index in h.items, -1 for the head, len(h.items) for the foot
		lines []string
	}
	var all []placed
	for _, tok := range tokens {
		if tok.Type != hclsyntax.TokenComment {
			continue
		}
		lines := hclComment(string(tok.Bytes))
		line := tok.Range.Start.Line
		if i := slices.IndexFunc(h.items, func(it hclItem) bool { return it.line == line && it.col < tok.Range.Start.Column }); i >= 0 {
			all = append(all, placed{i, lines})
			continue
		}
		next := slices.IndexFunc(h.items, func(it hclItem) bool { return it.line > line })
		switch {
		case next < 0:
			all = append(all, placed{len(h.items), lines})
		case next == 0:
			all = append(all, placed{-1, lines})
		default:
			all = append(all, placed{next, lines})
		}
	}
	// The notes on file() calls come first in Comments; comments go in source order after
	// them, so that the notes stay with the file's comments of the same item.
	notes := h.c.Comments
	h.c.Comments = nil
	dec := h.c.Decoded
	for _, p := range all {
		switch p.item {
		case -1:
			dec.Head = append(dec.Head, p.lines...)
		case len(h.items):
			dec.Foot = append(dec.Foot, p.lines...)
		default:
			label := h.items[p.item].label
			if n := len(h.c.Comments); n > 0 && h.c.Comments[n-1].Label == label {
				h.c.Comments[n-1].Lines = append(h.c.Comments[n-1].Lines, p.lines...)
				continue
			}
			h.c.Comments = append(h.c.Comments, Note{Label: label, Lines: p.lines})
		}
	}
	for _, n := range notes {
		i := slices.IndexFunc(h.c.Comments, func(c Note) bool { return c.Label == n.Label })
		if i < 0 {
			h.c.Comments = append(h.c.Comments, n)
			continue
		}
		h.c.Comments[i].Lines = append(append(h.c.Comments[i].Lines, ""), n.Lines...)
	}
	return nil
}

// hclComment turns an HCL comment token (`# x`, `// x`, `/* x */`) into comment lines.
func hclComment(tok string) []string {
	tok = strings.TrimRight(tok, "\r\n")
	if strings.HasPrefix(tok, "/*") {
		tok = strings.TrimSuffix(strings.TrimPrefix(tok, "/*"), "*/")
		var out []string
		for _, l := range strings.Split(tok, "\n") {
			l = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(l), "*"))
			if l == "" {
				out = append(out, "#")
				continue
			}
			out = append(out, "# "+l)
		}
		return trimBlank(out)
	}
	if strings.HasPrefix(tok, "//") {
		tok = "#" + strings.TrimPrefix(tok, "//")
	}
	return []string{tok}
}
