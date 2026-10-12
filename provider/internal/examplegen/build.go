// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/dirien/pulumi-docker-agent/provider/pkg/render"
)

// The program of an example is built as a small expression tree, which the TypeScript and
// Python printers write out.
type expr interface{ isExpr() }

// objectExpr is an object literal: typed inputs or a map.
type objectExpr struct {
	entries []*entry
	// multiline objects are never written on one line (agents).
	multiline bool
}

// listExpr is a list literal. Its entries have no key.
type listExpr struct{ items []*entry }

// scalarExpr is a string, bool, int64, float64 or nil.
type scalarExpr struct {
	value any
	float bool // an integral float64 is written with a decimal point
	// enum marks a value of a Pulumi enum inside a constant. TypeScript infers `string` for a
	// string in an object literal that has no declared type, which an enum input does not
	// accept, so it is written `"value" as const`.
	enum bool
}

// varExpr refers to a variable of the program.
type varExpr struct{ ts, py string }

func (*objectExpr) isExpr() {}
func (*listExpr) isExpr()   {}
func (*scalarExpr) isExpr() {}
func (*varExpr) isExpr()    {}

// entry is an object property, a list item, a spread of a variable into an object, or only
// comments.
type entry struct {
	key     *propKey
	value   expr
	spread  *varExpr
	comment render.Comment
}

// propKey is a property name in each language: an input name (camelCase, snake_case) or a
// map key (the same in both).
type propKey struct{ ts, py string }

// constDef is a variable the program defines before the AgentConfig: the value of a YAML
// anchor.
type constDef struct {
	name  varExpr
	path  render.Path
	value reflect.Value // the anchored value, without the key of a keyed object (Agent.name)
	expr  expr          // built on first use
}

// program is what the printers write for one example.
type program struct {
	ex     *example
	consts []*constDef
	args   *objectExpr // the AgentConfig arguments
	foot   []string    // comments after the last argument
}

type builder struct {
	ex      *example
	dec     *render.Decoded
	root    reflect.Value
	used    map[render.Path]bool
	consts  map[string]*constDef
	defined []*constDef // in the order they are completed (dependencies first)
	reads   map[render.Path]varExpr
	inConst int // > 0 while a constant is built
}

// build turns an example into a program and checks that every comment is placed.
func build(ex *example) (*program, error) {
	b := &builder{
		ex:     ex,
		dec:    ex.dec,
		root:   reflect.ValueOf(&ex.dec.Document).Elem(),
		used:   map[render.Path]bool{},
		consts: map[string]*constDef{},
		reads:  map[render.Path]varExpr{},
	}
	// The names a program defines besides the constants.
	taken := map[string]bool{"config": true, "yaml": true, "dockeragent": true, "fs": true, "path": true,
		"pulumi": true, "pathlib": true, "re": true}
	for _, r := range ex.reads {
		v := readVar(r)
		b.reads[r.path] = v
		for _, n := range []string{v.ts, v.ts + "Vars", v.py, v.py + "_vars", v.py + "_file"} {
			taken[n] = true
		}
	}
	for _, a := range ex.dec.Anchors {
		v, err := lookup(b.root, a.Path)
		if err != nil {
			return nil, fmt.Errorf("anchor %s: %w", a.Name, err)
		}
		name := varExpr{ts: tsIdentifier(a.Name), py: pyName(tsIdentifier(a.Name))}
		if taken[name.ts] || taken[name.py] {
			return nil, fmt.Errorf("the variable of anchor %s clashes with another name of the program", a.Name)
		}
		taken[name.ts], taken[name.py] = true, true
		b.consts[a.Name] = &constDef{name: name, path: a.Path, value: withoutKey(v)}
	}
	args := b.object(b.root, "", false)
	args.multiline = true
	if r := ex.skipValidation; r != nil {
		args.entries = append(args.entries, &entry{
			key:   &propKey{ts: "skipValidation", py: "skip_validation"},
			value: &scalarExpr{value: true},
			// The printers pick the comment of their language.
			comment: render.Comment{Head: []string{skipMarker}},
		})
	}
	var unplaced []string
	for p, c := range ex.dec.Comments {
		if !b.used[p] && len(c.Head)+len(c.Line)+len(c.Foot) > 0 {
			unplaced = append(unplaced, string(p))
		}
	}
	if len(unplaced) > 0 {
		slices.Sort(unplaced)
		return nil, fmt.Errorf("comments without a place in the program: %s", strings.Join(unplaced, ", "))
	}
	return &program{ex: ex, consts: b.defined, args: args, foot: ex.dec.Foot}, nil
}

// skipMarker stands for the skipValidation comment, which differs per language.
const skipMarker = "\x00skipValidation"

// comment returns the comments at p and marks them placed.
func (b *builder) comment(p render.Path) render.Comment {
	c := b.dec.Comments[p]
	if c == nil {
		return render.Comment{}
	}
	b.used[p] = true
	return *c
}

// constFor returns the constant of an anchor, building its expression on first use.
func (b *builder) constFor(name string) *constDef {
	c := b.consts[name]
	if c == nil {
		return nil
	}
	if c.expr == nil {
		b.inConst++
		v, _ := lookup(b.root, c.path)
		if keyField(derefType(v.Type())) != nil {
			c.expr = b.object(reflect.Indirect(v), c.path, true)
		} else {
			c.expr = b.valueNoAlias(v, c.path)
		}
		b.inConst--
		b.defined = append(b.defined, c)
	}
	return c
}

// withoutKey returns v, or for a keyed object (an agent) a copy without its key.
func withoutKey(v reflect.Value) reflect.Value {
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		v = v.Elem()
	}
	if !v.IsValid() {
		return v
	}
	f := keyField(v.Type())
	if f == nil {
		return v
	}
	c := reflect.New(v.Type()).Elem()
	c.Set(v)
	c.Field(f.index).SetZero()
	return c
}

// alias returns a reference to the constant of the anchor at p, or nil when the value at p
// is written out (no anchor, or a value that differs from the anchored one).
func (b *builder) alias(v reflect.Value, p render.Path) expr {
	name, ok := b.dec.Aliases[p]
	if !ok {
		return nil
	}
	c := b.consts[name]
	wv := withoutKey(v)
	if c == nil || !wv.IsValid() || !c.value.IsValid() || !reflect.DeepEqual(wv.Interface(), c.value.Interface()) {
		return nil
	}
	c = b.constFor(name)
	if f := keyField(derefType(v.Type())); f != nil {
		return &objectExpr{multiline: true, entries: []*entry{
			{key: &propKey{ts: f.pulumi, py: pyName(f.pulumi)}, value: &scalarExpr{value: reflect.Indirect(v).Field(f.index).String()}},
			{spread: &c.name},
		}}
	}
	return &c.name
}

func derefType(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

// value builds the expression of a typed value.
func (b *builder) value(v reflect.Value, p render.Path) expr {
	if r, ok := b.reads[p]; ok {
		return &r
	}
	if e := b.alias(v, p); e != nil {
		return e
	}
	return b.valueNoAlias(v, p)
}

func (b *builder) valueNoAlias(v reflect.Value, p render.Path) expr {
	switch v.Kind() {
	case reflect.Pointer:
		return b.valueNoAlias(v.Elem(), p)
	case reflect.Interface:
		if v.IsNil() {
			return &scalarExpr{}
		}
		return b.freeNoAlias(v.Elem().Interface(), p)
	case reflect.Struct:
		return b.object(v, p, false)
	case reflect.Slice:
		l := &listExpr{}
		for i := range v.Len() {
			ip := p.Index(i)
			l.items = append(l.items, &entry{value: b.value(v.Index(i), ip), comment: b.comment(ip)})
		}
		return l
	case reflect.Map:
		return b.mapObject(v, p)
	case reflect.String:
		_, enum := v.Type().MethodByName("Values") // a Pulumi enum (infer.Enum)
		return &scalarExpr{value: v.String(), enum: enum && b.inConst > 0}
	case reflect.Bool:
		return &scalarExpr{value: v.Bool()}
	case reflect.Int, reflect.Int64, reflect.Int32:
		return &scalarExpr{value: v.Int()}
	case reflect.Float64, reflect.Float32:
		return &scalarExpr{value: v.Float(), float: true}
	}
	panic(fmt.Sprintf("examplegen: unsupported %s at %s", v.Type(), p))
}

// spreads returns the spread entries of the anchors merged into the object at p, last anchor
// first (in YAML an earlier merged anchor wins, in a spread a later one), or nil when they
// cannot be spread into a value of type t.
func (b *builder) spreads(p render.Path, t reflect.Type) []*entry {
	merges := b.dec.Merges[p]
	if len(merges) == 0 {
		return nil
	}
	var out []*entry
	for i := len(merges) - 1; i >= 0; i-- {
		c := b.consts[merges[i]]
		if c == nil || c.value.Type() != t {
			return nil
		}
		c = b.constFor(merges[i])
		out = append(out, &entry{spread: &c.name})
	}
	return out
}

// object builds a typed object: its key (Agent.name) first, then the merged anchors, then the
// inputs in YAML order, then any other set input.
func (b *builder) object(v reflect.Value, p render.Path, skipKey bool) *objectExpr {
	t := v.Type()
	obj := &objectExpr{}
	done := map[string]bool{}
	if f := keyField(t); f != nil {
		obj.multiline = true
		done[f.pulumi] = true
		if !skipKey {
			obj.entries = append(obj.entries, &entry{key: &propKey{ts: f.pulumi, py: pyName(f.pulumi)},
				value: &scalarExpr{value: v.Field(f.index).String()}})
		}
	}
	spreads := b.spreads(p, t)
	obj.entries = append(obj.entries, spreads...)
	for _, name := range b.dec.Order[p] {
		f, ok := fieldByName(t, name)
		if !ok || done[name] {
			continue
		}
		done[name] = true
		b.field(obj, v, f, p, true)
	}
	if spreads == nil {
		for _, f := range fieldsOf(t) {
			if !done[f.pulumi] {
				b.field(obj, v, f, p, false)
			}
		}
	}
	return obj
}

// field adds the entry of input f of the object v at p. inYAML is true when the YAML sets
// the key.
func (b *builder) field(obj *objectExpr, v reflect.Value, f fieldInfo, p render.Path, inYAML bool) {
	fp := p.Field(f.pulumi)
	if f.extra {
		b.extra(obj, fp)
		return
	}
	fv := v.Field(f.index)
	var set bool
	switch fv.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Slice, reflect.Map:
		set = !fv.IsNil()
	case reflect.Struct:
		set = true
	default:
		set = inYAML || f.required || !fv.IsZero()
	}
	c := b.comment(fp)
	if !set {
		if len(c.Head)+len(c.Line)+len(c.Foot) > 0 {
			obj.entries = append(obj.entries, &entry{comment: c})
		}
		return
	}
	obj.entries = append(obj.entries, &entry{key: &propKey{ts: f.pulumi, py: pyName(f.pulumi)}, value: b.value(fv, fp), comment: c})
}

// extra places the keys of the `extra` map at p: only keys docker-agent never reads end up
// there, and they are left out with a comment.
func (b *builder) extra(obj *objectExpr, p render.Path) {
	for _, key := range b.dec.Order[p] {
		kp := p.Key(key)
		d, ok := b.ex.dropped[kp]
		if !ok {
			panic(fmt.Sprintf("examplegen: untyped key %s", kp))
		}
		c := b.comment(kp)
		text, err := yaml.Marshal(map[string]any{d.key: d.value})
		if err != nil {
			panic(err)
		}
		var lines []string
		lines = append(lines, c.Head...)
		lines = append(lines, noteMarker+"Left out: "+d.reason+". The upstream line:")
		for i, l := range strings.Split(strings.TrimSuffix(string(text), "\n"), "\n") {
			if i == 0 && len(c.Line) > 0 {
				l += "  " + strings.Join(c.Line, " ")
			}
			lines = append(lines, "# "+l)
		}
		lines = append(lines, c.Foot...)
		obj.entries = append(obj.entries, &entry{comment: render.Comment{Head: lines}})
	}
}

// noteMarker starts a comment line the generator writes (a note on a left-out key or on a
// file the program reads); the printers word-wrap it.
const noteMarker = "\x00note "

// mapObject builds a map: the merged anchors, then the keys in YAML order, then the others
// sorted.
func (b *builder) mapObject(v reflect.Value, p render.Path) *objectExpr {
	obj := &objectExpr{}
	spreads := b.spreads(p, v.Type())
	obj.entries = append(obj.entries, spreads...)
	keys := make([]string, 0, v.Len())
	for _, k := range v.MapKeys() {
		keys = append(keys, k.String())
	}
	sort.Strings(keys)
	order := slices.Clone(b.dec.Order[p])
	if spreads == nil {
		for _, k := range keys {
			if !slices.Contains(order, k) {
				order = append(order, k)
			}
		}
	}
	for _, k := range order {
		ev := v.MapIndex(reflect.ValueOf(k))
		if !ev.IsValid() {
			continue
		}
		kp := p.Key(k)
		obj.entries = append(obj.entries, &entry{key: &propKey{ts: k, py: k}, value: b.value(ev, kp), comment: b.comment(kp)})
	}
	return obj
}

// freeNoAlias builds a free-form value (a JSON schema, provider options, a flavor).
func (b *builder) freeNoAlias(x any, p render.Path) expr {
	switch x := x.(type) {
	case nil:
		return &scalarExpr{}
	case map[string]any:
		return b.mapObject(reflect.ValueOf(x), p)
	case []any:
		l := &listExpr{}
		for i, item := range x {
			ip := p.Index(i)
			l.items = append(l.items, &entry{value: b.value(reflect.ValueOf(&item).Elem(), ip), comment: b.comment(ip)})
		}
		return l
	case string, bool:
		return &scalarExpr{value: x}
	case int:
		return &scalarExpr{value: int64(x)}
	case int64:
		return &scalarExpr{value: x}
	case uint64:
		return &scalarExpr{value: int64(x)} //nolint:gosec // example values are small
	case float64:
		return &scalarExpr{value: x, float: true}
	}
	panic(fmt.Sprintf("examplegen: unsupported free-form %T at %s", x, p))
}

// fieldInfo is the metadata of a generated input field, read from its tags.
type fieldInfo struct {
	index    int
	pulumi   string
	yamlKey  string
	required bool
	key      bool // the key of a keyed list (Agent.name)
	extra    bool
}

func fieldsOf(t reflect.Type) []fieldInfo {
	var out []fieldInfo
	for i := range t.NumField() {
		sf := t.Field(i)
		tag, ok := sf.Tag.Lookup("pulumi")
		if !ok || !sf.IsExported() {
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		render := strings.Split(sf.Tag.Get("render"), ",")
		out = append(out, fieldInfo{
			index:    i,
			pulumi:   name,
			yamlKey:  sf.Tag.Get("yaml"),
			required: !strings.Contains(opts, "optional"),
			key:      slices.Contains(render, "key"),
			extra:    slices.Contains(render, "extra"),
		})
	}
	return out
}

func fieldByName(t reflect.Type, name string) (fieldInfo, bool) {
	for _, f := range fieldsOf(t) {
		if f.pulumi == name {
			return f, true
		}
	}
	return fieldInfo{}, false
}

func fieldByYAMLKey(t reflect.Type, key string) (fieldInfo, bool) {
	for _, f := range fieldsOf(t) {
		if f.yamlKey == key {
			return f, true
		}
	}
	return fieldInfo{}, false
}

func keyField(t reflect.Type) *fieldInfo {
	if t.Kind() != reflect.Struct {
		return nil
	}
	for _, f := range fieldsOf(t) {
		if f.key {
			return &f
		}
	}
	return nil
}
