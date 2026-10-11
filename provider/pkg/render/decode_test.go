// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package render

import (
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// decodeYAML decodes a docker-agent YAML document into the typed inputs: the reverse of
// Render, used to prove that every configuration docker-agent ships as an example can be
// expressed with typed inputs. It returns the paths of the keys that no typed field
// covers; they are kept in the `extra` maps.
//
// The YAML-only shorthands are folded into their typed equivalents, as docker-agent's
// loader does: an `instruction` list is joined with blank lines, a single
// `instruction_file` becomes a list of one, `skills: true` becomes `["local"]`, a
// `commands` list of maps is merged, a `task_budget` object becomes its `total`, and a
// duration given as a number of seconds becomes `<n>s`.
func decodeYAML(data []byte) (Document, []string, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return Document{}, nil, err
	}
	var doc Document
	if len(root.Content) == 0 {
		return doc, nil, nil
	}
	d := &decoder{}
	err := d.decodeStruct(root.Content[0], reflect.ValueOf(&doc).Elem(), "")
	return doc, d.untyped, err
}

type decoder struct {
	untyped []string
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

// pairs returns the key/value pairs of a mapping, with `<<` merge keys expanded (explicit
// keys win over merged ones).
func pairs(n *yaml.Node) [][2]*yaml.Node {
	n = resolve(n)
	var explicit, merged [][2]*yaml.Node
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if k.Value == "<<" && k.Tag == "!!merge" {
			v = resolve(v)
			sources := []*yaml.Node{v}
			if v.Kind == yaml.SequenceNode {
				sources = v.Content
			}
			for _, src := range sources {
				merged = append(merged, pairs(src)...)
			}
			continue
		}
		explicit = append(explicit, [2]*yaml.Node{k, v})
	}
	out := explicit
	for _, m := range merged {
		if !slices.ContainsFunc(out, func(p [2]*yaml.Node) bool { return p[0].Value == m[0].Value }) {
			out = append(out, m)
		}
	}
	return out
}

func (d *decoder) decodeStruct(n *yaml.Node, v reflect.Value, path string) error {
	n = resolve(n)
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
	for _, kv := range pairs(n) {
		key, val := kv[0].Value, kv[1]
		var group []fieldInfo
		for _, f := range fields {
			if f.yamlKey == key && !f.extra {
				group = append(group, f)
			}
		}
		p := path + "." + key
		switch {
		case len(group) == 0:
			var x any
			if err := val.Decode(&x); err != nil {
				return err
			}
			if extra == nil {
				extra = map[string]any{}
			}
			extra[key] = x
			d.untyped = append(d.untyped, strings.TrimPrefix(p, "."))
		case group[0].keyedBy != "":
			if err := d.decodeKeyed(val, v.Field(group[0].index), group[0].keyedBy, p); err != nil {
				return err
			}
		case len(group) == 1:
			if err := d.decodeValue(val, v.Field(group[0].index), key, p); err != nil {
				return err
			}
		case group[0].concat:
			if err := d.decodeCombined(val, v, group, p); err != nil {
				return err
			}
		default:
			if err := d.decodeVariant(val, v, group, key, p); err != nil {
				return err
			}
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

// decodeKeyed decodes a mapping into a list of structs whose key field is set to the
// mapping key (the `agents` map).
func (d *decoder) decodeKeyed(n *yaml.Node, v reflect.Value, key, path string) error {
	for _, kv := range pairs(n) {
		elem := reflect.New(v.Type().Elem()).Elem()
		if err := d.decodeStruct(kv[1], elem, path+"."+kv[0].Value); err != nil {
			return err
		}
		for _, f := range fieldsOf(elem.Type()) {
			if f.pulumi == key {
				elem.Field(f.index).SetString(kv[0].Value)
			}
		}
		v.Set(reflect.Append(v, elem))
	}
	return nil
}

// decodeCombined distributes the items of a list, or the entries of a map, over the fields
// that are rendered together: scalars go to the field with string elements, mappings to
// the field with object elements.
func (d *decoder) decodeCombined(n *yaml.Node, v reflect.Value, group []fieldInfo, path string) error {
	n = resolve(n)
	pick := func(item *yaml.Node) (reflect.Value, error) {
		item = resolve(item)
		for _, f := range group {
			fv := v.Field(f.index)
			elem := fv.Type().Elem()
			if (item.Kind == yaml.ScalarNode) == (elem.Kind() == reflect.String) {
				return fv, nil
			}
		}
		return reflect.Value{}, fmt.Errorf("%s: no field for a %s item", path, item.ShortTag())
	}
	addEntry := func(k string, val *yaml.Node) error {
		fv, err := pick(val)
		if err != nil {
			return err
		}
		if fv.IsNil() {
			fv.Set(reflect.MakeMap(fv.Type()))
		}
		elem := reflect.New(fv.Type().Elem()).Elem()
		if err := d.decodeValue(val, elem, k, path+"."+k); err != nil {
			return err
		}
		fv.SetMapIndex(reflect.ValueOf(k), elem)
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
				}
			}
		}
		return nil
	case isMap && n.Kind == yaml.MappingNode:
		for _, kv := range pairs(n) {
			if err := addEntry(kv[0].Value, kv[1]); err != nil {
				return err
			}
		}
		return nil
	case isMap && n.Kind == yaml.SequenceNode:
		// The list form of commands: single-entry maps, merged in order.
		for _, item := range n.Content {
			for _, kv := range pairs(item) {
				if err := addEntry(kv[0].Value, kv[1]); err != nil {
					return err
				}
			}
		}
		return nil
	case !isMap && n.Kind == yaml.SequenceNode:
		for i, item := range n.Content {
			fv, err := pick(item)
			if err != nil {
				return err
			}
			elem := reflect.New(fv.Type().Elem()).Elem()
			if err := d.decodeValue(item, elem, "", fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
			fv.Set(reflect.Append(fv, elem))
		}
		return nil
	}
	return fmt.Errorf("%s: unexpected %s", path, n.ShortTag())
}

// decodeVariant decodes a union value into the variant field whose type matches it.
func (d *decoder) decodeVariant(n *yaml.Node, v reflect.Value, group []fieldInfo, key, path string) error {
	n = resolve(n)
	if isNull(n) {
		return nil
	}
	want := map[string]reflect.Kind{"!!bool": reflect.Bool, "!!int": reflect.Int, "!!str": reflect.String}[n.ShortTag()]
	if n.Kind == yaml.SequenceNode {
		want = reflect.Slice
	}
	for _, f := range group {
		fv := v.Field(f.index)
		k := fv.Kind()
		if k == reflect.Pointer {
			k = fv.Type().Elem().Kind()
		}
		if k == want {
			return d.decodeValue(n, fv, key, path)
		}
	}
	return fmt.Errorf("%s: no variant for a %s value", path, n.ShortTag())
}

func (d *decoder) decodeValue(n *yaml.Node, v reflect.Value, key, path string) error {
	n = resolve(n)
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
			for _, kv := range pairs(n) {
				if kv[0].Value == "total" {
					return d.decodeValue(kv[1], v, key, path)
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
			if err := d.decodeValue(item, elem, "", fmt.Sprintf("%s[%d]", path, i)); err != nil {
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
		for _, kv := range pairs(n) {
			elem := reflect.New(v.Type().Elem()).Elem()
			if err := d.decodeValue(kv[1], elem, kv[0].Value, path+"."+kv[0].Value); err != nil {
				return err
			}
			v.SetMapIndex(reflect.ValueOf(kv[0].Value), elem)
		}
		return nil
	}
	return fmt.Errorf("%s: unsupported type %s", path, v.Type())
}
