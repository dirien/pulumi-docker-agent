// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package render

import (
	"cmp"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Path addresses a value of a Document by input names: `agents[0].toolsets[1].type` or
// `models["claude"].maxTokens`. List items, agents included, are addressed by index and map
// entries by key. The empty path is the document itself.
type Path string

// Field returns the path of the input name of the object at p.
func (p Path) Field(name string) Path {
	if p == "" {
		return Path(name)
	}
	return p + "." + Path(name)
}

// Index returns the path of the i-th item of the list at p.
func (p Path) Index(i int) Path { return Path(fmt.Sprintf("%s[%d]", p, i)) }

// Key returns the path of the entry k of the map at p.
func (p Path) Key(k string) Path { return p + Path("["+strconv.Quote(k)+"]") }

// Decoded is a docker-agent YAML document decoded into the typed inputs by DecodeYAML, with
// what a program generator needs to write it out as code that reads like the original: the
// comments, the order of the keys and the YAML anchors.
type Decoded struct {
	Document Document
	// Untyped lists the paths of the keys that no typed input covers, for example
	// `agents[0].extra["foo"]`. They are kept in the `extra` maps.
	Untyped []Path
	// Comments holds the YAML comments by the path of the value they belong to. Comments
	// inside a shorthand that is folded into one value (an `instruction` list, a
	// `task_budget` object) belong to that value.
	Comments map[Path]*Comment
	// Head and Foot are the comments before the first and after the last key of the document.
	Head, Foot []string
	// Order lists the keys of each object and map in document order: input names for typed
	// objects (`extra` stands for the untyped keys), the keys themselves for maps. Keys that
	// come from a `<<` merge of an anchor are not listed.
	Order map[Path][]string
	// Anchors are the YAML anchors that an alias or a merge key refers to, in document order.
	Anchors []Anchor
	// Aliases maps the path of each value written as an alias (`*name`), and of each anchored
	// value (`&name`), to the anchor name.
	Aliases map[Path]string
	// Merges maps the path of each object or map with `<<` merge keys to the anchors it
	// merges, in YAML order (an earlier anchor wins over a later one).
	Merges map[Path][]string

	// root and nodes record where each YAML node was decoded (keys map to the path of their
	// value), for IndexYAML.
	root  *yaml.Node
	nodes map[*yaml.Node]Path
}

// Comment holds the comments of one value, one line each (`# text`). An empty line separates
// two groups of comments.
type Comment struct {
	Head []string // above the key or list item
	Line []string // at the end of its line
	Foot []string // below it
}

// Anchor is a YAML anchor and the path of the value it is set on.
type Anchor struct {
	Name string
	Path Path
}

// DecodeYAML decodes a docker-agent YAML document into the typed inputs: the reverse of
// Render. It proves that every configuration docker-agent ships as an example can be
// expressed with typed inputs, and it is the input of the example program generator.
//
// The YAML-only shorthands are folded into their typed equivalents, as docker-agent's loader
// does: an `instruction` list is joined with blank lines, a single `instruction_file` becomes
// a list of one, `skills: true` becomes `["local"]`, a `commands` list of maps is merged, a
// `task_budget` object becomes its `total`, and a duration given as a number of seconds
// becomes `<n>s`. Aliases and `<<` merge keys are expanded.
func DecodeYAML(data []byte) (*Decoded, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	d := &decoder{
		out: &Decoded{
			Comments: map[Path]*Comment{},
			Order:    map[Path][]string{},
			Aliases:  map[Path]string{},
			Merges:   map[Path][]string{},
			root:     &root,
			nodes:    map[*yaml.Node]Path{},
		},
		seen:     map[*yaml.Node]bool{},
		anchorAt: map[*yaml.Node]Path{},
		aliasAt:  map[Path]*yaml.Node{},
		mergeAt:  map[Path][]*yaml.Node{},
	}
	if len(root.Content) == 0 {
		return d.out, nil
	}
	top := root.Content[0]
	d.out.Head = slices.Concat(commentLines(root.HeadComment), commentLines(top.HeadComment), commentLines(top.LineComment))
	d.out.Foot = slices.Concat(commentLines(top.FootComment), commentLines(root.FootComment))
	d.seen[top] = true
	d.out.nodes[top] = ""
	err := d.decodeStruct(top, reflect.ValueOf(&d.out.Document).Elem(), "")
	d.finish()
	return d.out, err
}

type decoder struct {
	out      *Decoded
	seen     map[*yaml.Node]bool   // nodes whose comments are recorded
	anchorAt map[*yaml.Node]Path   // where each anchored node was decoded first
	aliasAt  map[Path]*yaml.Node   // the anchored node of each alias
	mergeAt  map[Path][]*yaml.Node // the anchored nodes merged into each mapping
}

// durationKeys are the string fields docker-agent parses as durations, which also accept a
// number of seconds.
var durationKeys = []string{"call_timeout", "cooldown", "indexing_timeout", "initial", "max", "max_time",
	"startup_timeout", "timeout"}

func resolve(n *yaml.Node) *yaml.Node {
	for n != nil && n.Kind == yaml.AliasNode {
		n = n.Alias
	}
	return n
}

func isNull(n *yaml.Node) bool {
	n = resolve(n)
	return n == nil || (n.Kind == yaml.ScalarNode && n.Tag == "!!null")
}

// commentLines splits a yaml.v3 comment into lines (`# text`), without indentation and
// trailing blanks. Blank lines between comments are kept as empty lines.
func commentLines(c string) []string {
	if c == "" {
		return nil
	}
	var out []string
	for _, l := range strings.Split(c, "\n") {
		out = append(out, strings.TrimSpace(l))
	}
	return out
}

// comment records the comments of nodes for the value at path. A node's comments are only
// recorded once, so the comments of an anchored value stay with the anchor.
func (d *decoder) comment(path Path, prepend bool, nodes ...*yaml.Node) {
	var c Comment
	for _, n := range nodes {
		if _, ok := d.out.nodes[n]; n != nil && !ok {
			d.out.nodes[n] = path
		}
		if n == nil || d.seen[n] {
			continue
		}
		d.seen[n] = true
		c.Head = append(c.Head, commentLines(n.HeadComment)...)
		c.Line = append(c.Line, commentLines(n.LineComment)...)
		c.Foot = append(c.Foot, commentLines(n.FootComment)...)
	}
	if len(c.Head)+len(c.Line)+len(c.Foot) == 0 {
		return
	}
	cur := d.out.Comments[path]
	switch {
	case cur == nil:
		d.out.Comments[path] = &c
	case prepend:
		cur.Head, cur.Line, cur.Foot = slices.Concat(c.Head, cur.Head), slices.Concat(c.Line, cur.Line), slices.Concat(c.Foot, cur.Foot)
	default:
		cur.Head, cur.Line, cur.Foot = slices.Concat(cur.Head, c.Head), slices.Concat(cur.Line, c.Line), slices.Concat(cur.Foot, c.Foot)
	}
}

// collect records the comments of n and of everything below it for the value at path: the
// node is folded into a single value.
func (d *decoder) collect(n *yaml.Node, path Path) {
	if n == nil {
		return
	}
	d.comment(path, false, n)
	for _, c := range n.Content {
		d.collect(c, path)
	}
}

// node records the comments of n and the anchor or alias it is for the value at path, and
// returns n with aliases resolved.
func (d *decoder) node(n *yaml.Node, path Path) *yaml.Node {
	d.comment(path, false, n)
	if n == nil {
		return nil
	}
	if n.Kind == yaml.AliasNode {
		t := resolve(n)
		if _, ok := d.aliasAt[path]; !ok {
			d.aliasAt[path] = t
		}
		return t
	}
	if n.Anchor != "" {
		if _, ok := d.anchorAt[n]; !ok {
			d.anchorAt[n] = path
		}
	}
	return n
}

// order appends keys to the key order of the object or map at path.
func (d *decoder) order(path Path, keys ...string) {
	for _, k := range keys {
		if !slices.Contains(d.out.Order[path], k) {
			d.out.Order[path] = append(d.out.Order[path], k)
		}
	}
}

// finish names the anchors that are used and records the aliases and merges.
func (d *decoder) finish() {
	used := map[*yaml.Node]bool{}
	for p, t := range d.aliasAt {
		if _, ok := d.anchorAt[t]; ok {
			used[t] = true
		} else {
			delete(d.aliasAt, p)
		}
	}
	for p, ts := range d.mergeAt {
		for _, t := range ts {
			if _, ok := d.anchorAt[t]; !ok {
				delete(d.mergeAt, p)
				break
			}
			used[t] = true
		}
	}
	nodes := make([]*yaml.Node, 0, len(used))
	for n := range used {
		nodes = append(nodes, n)
	}
	slices.SortFunc(nodes, func(a, b *yaml.Node) int { return cmp.Or(cmp.Compare(a.Line, b.Line), cmp.Compare(a.Column, b.Column)) })
	names := map[*yaml.Node]string{}
	taken := map[string]bool{}
	for _, n := range nodes {
		name := n.Anchor
		for i := 2; taken[name]; i++ {
			name = fmt.Sprintf("%s_%d", n.Anchor, i)
		}
		taken[name] = true
		names[n] = name
		d.out.Anchors = append(d.out.Anchors, Anchor{Name: name, Path: d.anchorAt[n]})
		d.out.Aliases[d.anchorAt[n]] = name
	}
	for p, t := range d.aliasAt {
		d.out.Aliases[p] = names[t]
	}
	for p, ts := range d.mergeAt {
		for _, t := range ts {
			d.out.Merges[p] = append(d.out.Merges[p], names[t])
		}
	}
}

// pair is a key/value pair of a mapping. merged pairs come from a `<<` merge of an anchor.
type pair struct {
	k, v   *yaml.Node
	merged bool
}

// pairs returns the key/value pairs of a mapping, with `<<` merge keys expanded (explicit keys
// win over merged ones, an earlier merged mapping over a later one). With spread, anchors
// merged into the mapping are recorded for path and their pairs are marked as merged;
// otherwise merged pairs count as written out.
func (d *decoder) pairs(n *yaml.Node, path Path, spread bool) []pair {
	n = resolve(n)
	var explicit, merged []pair
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if k.Value != "<<" || k.Tag != "!!merge" {
			explicit = append(explicit, pair{k: k, v: v})
			continue
		}
		d.comment(path, false, k, v)
		sources := []*yaml.Node{v}
		if rv := resolve(v); rv.Kind == yaml.SequenceNode {
			d.comment(path, false, rv)
			sources = rv.Content
		}
		for _, src := range sources {
			fromAnchor := spread && src.Kind == yaml.AliasNode
			if fromAnchor {
				d.mergeAt[path] = append(d.mergeAt[path], resolve(src))
			}
			for _, p := range d.pairs(src, path, false) {
				p.merged = fromAnchor
				merged = append(merged, p)
			}
		}
	}
	out := explicit
	for _, m := range merged {
		if !slices.ContainsFunc(out, func(p pair) bool { return p.k.Value == m.k.Value }) {
			out = append(out, m)
		}
	}
	return out
}

// free records the comments, key order and anchors of a free-form value.
func (d *decoder) free(n *yaml.Node, path Path) {
	n = d.node(n, path)
	if n == nil {
		return
	}
	switch n.Kind {
	case yaml.MappingNode:
		for _, kv := range d.pairs(n, path, true) {
			p := path.Key(kv.k.Value)
			if !kv.merged {
				d.order(path, kv.k.Value)
			}
			d.comment(p, false, kv.k)
			d.free(kv.v, p)
		}
	case yaml.SequenceNode:
		for i, item := range n.Content {
			d.free(item, path.Index(i))
		}
	}
}

func (d *decoder) decodeStruct(n *yaml.Node, v reflect.Value, path Path) error {
	n = d.node(n, path)
	if isNull(n) {
		return nil
	}
	fields := fieldsOf(v.Type())
	if fields[0].value {
		return d.decodeCombined(n, v, fields, path)
	}
	if n.Kind != yaml.MappingNode {
		return fmt.Errorf("%s: expected a mapping, got %s", path, n.ShortTag())
	}
	var extra map[string]any
	for _, kv := range d.pairs(n, path, true) {
		key, val := kv.k.Value, kv.v
		var group []fieldInfo
		for _, f := range fields {
			if f.yamlKey == key && !f.extra {
				group = append(group, f)
			}
		}
		var names []string
		switch {
		case len(group) == 0:
			p := path.Field("extra").Key(key)
			d.comment(p, false, kv.k)
			d.collect(val, p)
			var x any
			if err := val.Decode(&x); err != nil {
				return err
			}
			if extra == nil {
				extra = map[string]any{}
			}
			extra[key] = x
			d.out.Untyped = append(d.out.Untyped, p)
			if !kv.merged {
				d.order(path.Field("extra"), key)
			}
			names = []string{"extra"}
		case group[0].keyedBy != "":
			p := path.Field(group[0].pulumi)
			d.comment(p, false, kv.k)
			if err := d.decodeKeyed(val, v.Field(group[0].index), group[0].keyedBy, p); err != nil {
				return err
			}
			names = []string{group[0].pulumi}
		case len(group) == 1:
			p := path.Field(group[0].pulumi)
			d.comment(p, false, kv.k)
			if err := d.decodeValue(val, v.Field(group[0].index), key, p); err != nil {
				return err
			}
			names = []string{group[0].pulumi}
		default:
			var err error
			if group[0].concat {
				err = d.decodeCombined(val, v, group, path)
			} else {
				err = d.decodeVariant(val, v, group, key, path)
			}
			if err != nil {
				return err
			}
			// The key's comments go to the first field that got a value.
			target := group[0]
			for _, f := range group {
				if !v.Field(f.index).IsZero() {
					target = f
					break
				}
			}
			d.comment(path.Field(target.pulumi), true, kv.k)
			for _, f := range group {
				names = append(names, f.pulumi)
			}
		}
		if !kv.merged {
			d.order(path, names...)
		}
	}
	if extra != nil {
		for _, f := range fields {
			if f.extra {
				v.Field(f.index).Set(reflect.ValueOf(extra))
			}
		}
	}
	return nil
}

// decodeKeyed decodes a mapping into a list of structs whose key field is set to the mapping
// key (the `agents` map).
func (d *decoder) decodeKeyed(n *yaml.Node, v reflect.Value, key string, path Path) error {
	n = d.node(n, path)
	if isNull(n) {
		return nil
	}
	for _, kv := range d.pairs(n, path, false) {
		p := path.Index(v.Len())
		d.comment(p, false, kv.k)
		elem := reflect.New(v.Type().Elem()).Elem()
		if err := d.decodeStruct(kv.v, elem, p); err != nil {
			return err
		}
		for _, f := range fieldsOf(elem.Type()) {
			if f.pulumi == key {
				elem.Field(f.index).SetString(kv.k.Value)
			}
		}
		v.Set(reflect.Append(v, elem))
	}
	return nil
}

// decodeCombined distributes the items of a list, or the entries of a map, over the fields
// that are rendered together: scalars go to the field with string elements, mappings to the
// field with object elements. path is the path of the struct the fields belong to.
func (d *decoder) decodeCombined(n *yaml.Node, v reflect.Value, group []fieldInfo, path Path) error {
	n = d.node(n, path.Field(group[0].pulumi))
	pick := func(item *yaml.Node) (fieldInfo, reflect.Value, error) {
		item = resolve(item)
		for _, f := range group {
			fv := v.Field(f.index)
			elem := fv.Type().Elem()
			if (item.Kind == yaml.ScalarNode) == (elem.Kind() == reflect.String) {
				d.order(path, f.pulumi)
				return f, fv, nil
			}
		}
		return fieldInfo{}, reflect.Value{}, fmt.Errorf("%s: no field for a %s item", path, item.ShortTag())
	}
	addEntry := func(k *yaml.Node, val *yaml.Node, comments ...*yaml.Node) error {
		f, fv, err := pick(val)
		if err != nil {
			return err
		}
		if fv.IsNil() {
			fv.Set(reflect.MakeMap(fv.Type()))
		}
		p := path.Field(f.pulumi).Key(k.Value)
		d.comment(p, false, append(comments, k)...)
		d.order(path.Field(f.pulumi), k.Value)
		elem := reflect.New(fv.Type().Elem()).Elem()
		if err := d.decodeValue(val, elem, k.Value, p); err != nil {
			return err
		}
		fv.SetMapIndex(reflect.ValueOf(k.Value), elem)
		return nil
	}
	isMap := v.Field(group[0].index).Kind() == reflect.Map
	switch {
	case isNull(n):
		return nil
	case n.Kind == yaml.ScalarNode && n.Tag == "!!bool":
		// skills: true is the same as [local]; false is the same as no skills.
		if n.Value == "true" {
			for _, f := range group {
				if fv := v.Field(f.index); fv.Type() == reflect.TypeFor[[]string]() {
					fv.Set(reflect.ValueOf([]string{"local"}))
					d.order(path, f.pulumi)
				}
			}
		}
		return nil
	case isMap && n.Kind == yaml.MappingNode:
		for _, kv := range d.pairs(n, path, false) {
			if err := addEntry(kv.k, kv.v); err != nil {
				return err
			}
		}
		return nil
	case isMap && n.Kind == yaml.SequenceNode:
		// The list form of commands: single-entry maps, merged in order. The comments of a
		// list item go to its first entry.
		for _, item := range n.Content {
			comments := []*yaml.Node{item}
			for _, kv := range d.pairs(item, path, false) {
				if err := addEntry(kv.k, kv.v, comments...); err != nil {
					return err
				}
				comments = nil
			}
			d.collect(item, path.Field(group[0].pulumi))
		}
		return nil
	case !isMap && n.Kind == yaml.SequenceNode:
		for _, item := range n.Content {
			f, fv, err := pick(item)
			if err != nil {
				return err
			}
			elem := reflect.New(fv.Type().Elem()).Elem()
			if err := d.decodeValue(item, elem, "", path.Field(f.pulumi).Index(fv.Len())); err != nil {
				return err
			}
			fv.Set(reflect.Append(fv, elem))
		}
		return nil
	}
	return fmt.Errorf("%s: unexpected %s", path, n.ShortTag())
}

// decodeVariant decodes a union value into the variant field whose type matches it. path is
// the path of the struct the fields belong to.
func (d *decoder) decodeVariant(n *yaml.Node, v reflect.Value, group []fieldInfo, key string, path Path) error {
	if isNull(n) {
		d.comment(path.Field(group[0].pulumi), false, n)
		return nil
	}
	r := resolve(n)
	want := map[string]reflect.Kind{"!!bool": reflect.Bool, "!!int": reflect.Int, "!!str": reflect.String}[r.ShortTag()]
	if r.Kind == yaml.SequenceNode {
		want = reflect.Slice
	}
	for _, f := range group {
		fv := v.Field(f.index)
		k := fv.Kind()
		if k == reflect.Pointer {
			k = fv.Type().Elem().Kind()
		}
		if k == want {
			return d.decodeValue(n, fv, key, path.Field(f.pulumi))
		}
	}
	return fmt.Errorf("%s: no variant for a %s value", path.Field(group[0].pulumi), r.ShortTag())
}

func (d *decoder) decodeValue(n *yaml.Node, v reflect.Value, key string, path Path) error {
	n = d.node(n, path)
	switch v.Kind() {
	case reflect.Pointer:
		if isNull(n) {
			return nil
		}
		p := reflect.New(v.Type().Elem())
		if err := d.decodeValue(n, p.Elem(), key, path); err != nil {
			return err
		}
		v.Set(p)
		return nil
	case reflect.Interface:
		d.free(n, path)
		var x any
		if err := n.Decode(&x); err != nil {
			return err
		}
		if x != nil {
			v.Set(reflect.ValueOf(x))
		}
		return nil
	case reflect.Struct:
		return d.decodeStruct(n, v, path)
	case reflect.String:
		switch {
		case n.Kind == yaml.SequenceNode: // instruction: [a, b] is "a\n\nb"
			var parts []string
			if err := n.Decode(&parts); err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			d.collect(n, path)
			v.SetString(strings.Join(parts, "\n\n"))
		case n.ShortTag() == "!!int" && slices.Contains(durationKeys, key):
			v.SetString(n.Value + "s")
		case n.Kind == yaml.ScalarNode:
			v.SetString(n.Value)
		default:
			return fmt.Errorf("%s: expected a string, got %s", path, n.ShortTag())
		}
		return nil
	case reflect.Bool:
		b, err := strconv.ParseBool(n.Value)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		v.SetBool(b)
		return nil
	case reflect.Int:
		if n.Kind == yaml.MappingNode { // task_budget: {type: tokens, total: N} is N
			d.collect(n, path)
			for _, kv := range d.pairs(n, path, false) {
				if kv.k.Value == "total" {
					return d.decodeValue(kv.v, v, key, path)
				}
			}
			return fmt.Errorf("%s: object without total", path)
		}
		i, err := strconv.ParseInt(n.Value, 0, 64)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		v.SetInt(i)
		return nil
	case reflect.Float64:
		f, err := strconv.ParseFloat(n.Value, 64)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		v.SetFloat(f)
		return nil
	case reflect.Slice:
		if isNull(n) {
			return nil
		}
		items := n.Content
		if n.Kind != yaml.SequenceNode {
			// A single item is a list of one: `instruction_file: a.md`, or a hook event
			// written as one mapping instead of a list.
			items = []*yaml.Node{n}
		}
		for i, item := range items {
			elem := reflect.New(v.Type().Elem()).Elem()
			if err := d.decodeValue(item, elem, "", path.Index(i)); err != nil {
				return err
			}
			v.Set(reflect.Append(v, elem))
		}
		return nil
	case reflect.Map:
		if isNull(n) {
			return nil
		}
		if v.IsNil() {
			v.Set(reflect.MakeMap(v.Type()))
		}
		for _, kv := range d.pairs(n, path, true) {
			p := path.Key(kv.k.Value)
			d.comment(p, false, kv.k)
			if !kv.merged {
				d.order(path, kv.k.Value)
			}
			elem := reflect.New(v.Type().Elem()).Elem()
			if err := d.decodeValue(kv.v, elem, kv.k.Value, p); err != nil {
				return err
			}
			v.SetMapIndex(reflect.ValueOf(kv.k.Value), elem)
		}
		return nil
	}
	return fmt.Errorf("%s: unsupported type %s", path, v.Type())
}
