// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package pcl

import (
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/dirien/pulumi-docker-agent/provider/pkg/render"
)

// fieldInfo is the metadata of a generated input field of package render, read from its tags.
type fieldInfo struct {
	index    int
	pulumi   string // the input name (camelCase), which is also the PCL attribute name
	yamlKey  string
	required bool
	key      bool // the key of a keyed list (Agent.name)
	extra    bool // the map of untyped keys
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
		opt := strings.Split(sf.Tag.Get("render"), ",")
		out = append(out, fieldInfo{
			index:    i,
			pulumi:   name,
			yamlKey:  sf.Tag.Get("yaml"),
			required: !strings.Contains(opts, "optional"),
			key:      slices.Contains(opt, "key"),
			extra:    slices.Contains(opt, "extra"),
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
		if f.yamlKey == key && !f.extra {
			return f, true
		}
	}
	return fieldInfo{}, false
}

// keyField returns the key field of a keyed list's element type (Agent.name), or nil.
func keyField(t reflect.Type) *fieldInfo {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
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
			if end < 0 {
				return nil, fmt.Errorf("bad path %q", p)
			}
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
			if v.Kind() != reflect.Struct {
				return reflect.Value{}, fmt.Errorf("no object for input %q at %s", s, p)
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
