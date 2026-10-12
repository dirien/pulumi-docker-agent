// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package render

import (
	"errors"
	"reflect"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// PathIndex maps the values of a docker-agent YAML document to the inputs they were decoded
// into, so that a problem docker-agent reports for a YAML location (`agents.root.safety`,
// `models.claude.max_tokens`) can be attributed to the input that produced it
// (`agents[0].safety`, `models["claude"].maxTokens`).
type PathIndex struct {
	root  *yaml.Node
	nodes map[*yaml.Node]Path
}

// IndexYAML decodes a docker-agent YAML document, usually the output of Render, and indexes
// the input path of each of its values.
func IndexYAML(data []byte) (*PathIndex, error) {
	dec, err := DecodeYAML(data)
	if err != nil {
		return nil, err
	}
	return &PathIndex{root: dec.root, nodes: dec.nodes}, nil
}

// Lookup returns the input path of the value at yamlPath: mapping keys and, for sequences,
// item indexes in decimal (`agents`, `root`, `toolsets`, `2`, `ref`). A path that continues
// below a value the document does not have resolves to the deepest value on the way, so a
// problem is still attributed to the closest input. ok is false when not even the first key
// is in the document.
func (ix *PathIndex) Lookup(yamlPath []string) (path Path, ok bool) {
	if ix == nil || ix.root == nil || len(ix.root.Content) == 0 {
		return "", false
	}
	n := resolve(ix.root.Content[0])
	for _, seg := range yamlPath {
		var next *yaml.Node
		switch n.Kind {
		case yaml.MappingNode:
			for i := 0; i+1 < len(n.Content); i += 2 {
				if n.Content[i].Value == seg {
					next = n.Content[i+1]
					break
				}
			}
		case yaml.SequenceNode:
			if i, err := strconv.Atoi(seg); err == nil && i >= 0 && i < len(n.Content) {
				next = n.Content[i]
			}
		}
		if next == nil {
			break
		}
		if p, found := ix.nodes[next]; found {
			path, ok = p, true
		}
		n = resolve(next)
	}
	return path, ok
}

// InputPath returns the input path of a rendering error of doc, for example
// `agents[0].toolsets[1]` for an error Render reports at `agents["root"].toolsets[1]`
// (rendering errors name agents by key, input paths by index). ok is false when err carries
// no path.
func InputPath(err error, doc Document) (Path, bool) {
	var pe *pathError
	if !errors.As(err, &pe) {
		return "", false
	}
	segs, valid := splitPath(pe.path)
	if !valid {
		return Path(ErrorProperty(err)), true
	}
	var out Path
	v := reflect.ValueOf(doc)
	keyedBy := ""
	for _, s := range segs {
		for v.IsValid() && (v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface) && !v.IsNil() {
			v = v.Elem()
		}
		switch {
		case s.isKey && keyedBy != "":
			i := keyedIndex(v, keyedBy, s.name)
			if i < 0 {
				return out.Key(s.name), true
			}
			out, v = out.Index(i), v.Index(i)
		case s.isKey:
			out = out.Key(s.name)
			if v.IsValid() && v.Kind() == reflect.Map {
				v = v.MapIndex(reflect.ValueOf(s.name))
			} else {
				v = reflect.Value{}
			}
		case s.isIndex:
			out = out.Index(s.index)
			if v.IsValid() && v.Kind() == reflect.Slice && s.index < v.Len() {
				v = v.Index(s.index)
			} else {
				v = reflect.Value{}
			}
		default:
			out = out.Field(s.name)
			field, f, found := fieldByName(v, s.name)
			if !found {
				v, keyedBy = reflect.Value{}, ""
				continue
			}
			v, keyedBy = field, f.keyedBy
			continue
		}
		keyedBy = ""
	}
	return out, true
}

// fieldByName returns the field of the struct v whose input name is name.
func fieldByName(v reflect.Value, name string) (reflect.Value, fieldInfo, bool) {
	if !v.IsValid() || v.Kind() != reflect.Struct {
		return reflect.Value{}, fieldInfo{}, false
	}
	for _, f := range fieldsOf(v.Type()) {
		if f.pulumi == name {
			return v.Field(f.index), f, true
		}
	}
	return reflect.Value{}, fieldInfo{}, false
}

// keyedIndex returns the index of the element of the keyed list v whose key field is key.
func keyedIndex(v reflect.Value, keyField, key string) int {
	if !v.IsValid() || v.Kind() != reflect.Slice {
		return -1
	}
	for i := range v.Len() {
		if f, _, ok := fieldByName(v.Index(i), keyField); ok && f.Kind() == reflect.String && f.String() == key {
			return i
		}
	}
	return -1
}

// pathSegment is one step of a Path: a property name, an `[index]` or a `["key"]`.
type pathSegment struct {
	name    string
	index   int
	isIndex bool
	isKey   bool
}

// splitPath splits a Path into its segments. valid is false for a malformed path.
func splitPath(p string) (segs []pathSegment, valid bool) {
	for p != "" {
		switch {
		case strings.HasPrefix(p, "[\""):
			end := closingQuote(p[1:])
			if end < 0 || len(p) < end+3 || p[end+2] != ']' {
				return nil, false
			}
			key, err := strconv.Unquote(p[1 : end+2])
			if err != nil {
				return nil, false
			}
			segs = append(segs, pathSegment{name: key, isKey: true})
			p = p[end+3:]
		case strings.HasPrefix(p, "["):
			end := strings.IndexByte(p, ']')
			i, err := strconv.Atoi(p[1:max(end, 1)])
			if end < 0 || err != nil {
				return nil, false
			}
			segs = append(segs, pathSegment{index: i, isIndex: true})
			p = p[end+1:]
		default:
			p = strings.TrimPrefix(p, ".")
			end := strings.IndexAny(p, ".[")
			if end < 0 {
				end = len(p)
			}
			if end == 0 {
				return nil, false
			}
			segs = append(segs, pathSegment{name: p[:end]})
			p = p[end:]
		}
	}
	return segs, true
}

// closingQuote returns the index of the quote that closes the Go string literal s starts
// with, or -1.
func closingQuote(s string) int {
	for i := 1; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '"':
			return i
		}
	}
	return -1
}
