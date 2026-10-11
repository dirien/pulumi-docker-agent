// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package usersettings

import (
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
)

// YAMLValuer is implemented by input types whose YAML form is not a mapping, for example
// a thinking budget that is written as `high` or `8192`.
type YAMLValuer interface {
	YAMLValue() (any, error)
}

var yamlValuerType = reflect.TypeFor[YAMLValuer]()

// Encode converts a typed input struct into the generic YAML value docker-agent reads.
//
// Every exported field with a `pulumi` tag must also carry a `yaml` tag naming the
// docker-agent key. A `yaml:",inline"` field must be a map[string]any; its entries are
// merged into the surrounding mapping (the `extra` escape hatches) and may not repeat a
// typed key. Fields tagged `yaml:"-"` (for example a resource's `path`) are skipped. Nil
// pointers, nil maps and nil slices are omitted; zero values that were set explicitly
// are kept, so `false` and `0` reach docker-agent, which applies its own defaults.
func Encode(v any) (map[string]any, error) {
	out, err := encodeValue(reflect.ValueOf(v), "")
	if err != nil {
		return nil, err
	}
	m, ok := out.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("internal error: %T does not encode to a mapping", v)
	}
	return m, nil
}

func encodeValue(v reflect.Value, path string) (any, error) {
	if !v.IsValid() {
		return nil, nil
	}
	if v.Type().Implements(yamlValuerType) {
		if v.Kind() == reflect.Pointer && v.IsNil() {
			return nil, nil
		}
		val, err := v.Interface().(YAMLValuer).YAMLValue()
		if err != nil {
			return nil, fmt.Errorf("%s: %w", strings.TrimPrefix(path, "."), err)
		}
		return val, nil
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return nil, nil
		}
		return encodeValue(v.Elem(), path)
	case reflect.Struct:
		return encodeStruct(v, path)
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.IsNil() {
			return nil, nil
		}
		out := make([]any, 0, v.Len())
		for i := range v.Len() {
			e, err := encodeValue(v.Index(i), fmt.Sprintf("%s[%d]", path, i))
			if err != nil {
				return nil, err
			}
			out = append(out, e)
		}
		return out, nil
	case reflect.Map:
		if v.IsNil() {
			return nil, nil
		}
		if v.Type().Key().Kind() != reflect.String {
			return nil, fmt.Errorf("internal error: %s: map keys must be strings", path)
		}
		out := make(map[string]any, v.Len())
		iter := v.MapRange()
		for iter.Next() {
			k := iter.Key().String()
			e, err := encodeValue(iter.Value(), path+"."+k)
			if err != nil {
				return nil, err
			}
			out[k] = e
		}
		return out, nil
	case reflect.Float32, reflect.Float64:
		return v.Float(), nil
	case reflect.Bool:
		return v.Bool(), nil
	case reflect.String:
		return v.String(), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int(), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return v.Uint(), nil
	default:
		return nil, fmt.Errorf("internal error: %s: unsupported kind %s", path, v.Kind())
	}
}

func encodeStruct(v reflect.Value, path string) (map[string]any, error) {
	t := v.Type()
	out := map[string]any{}
	var inline map[string]any
	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() || f.Tag.Get("pulumi") == "" {
			continue
		}
		key, opts, _ := strings.Cut(f.Tag.Get("yaml"), ",")
		switch {
		case key == "-":
			continue
		case key == "" && opts == "inline":
			fv := v.Field(i)
			if fv.Kind() != reflect.Map || fv.Type().Key().Kind() != reflect.String {
				return nil, fmt.Errorf("internal error: %s.%s: inline fields must be string-keyed maps", t.Name(), f.Name)
			}
			if fv.IsNil() {
				continue
			}
			inline = map[string]any{}
			iter := fv.MapRange()
			for iter.Next() {
				inline[iter.Key().String()] = normalize(iter.Value().Interface())
			}
			continue
		case key == "":
			return nil, fmt.Errorf("internal error: %s.%s has no yaml tag", t.Name(), f.Name)
		}
		e, err := encodeValue(v.Field(i), path+"."+key)
		if err != nil {
			return nil, err
		}
		if e == nil {
			continue
		}
		if f.Type.Kind() == reflect.Interface || isAnyMap(f.Type) {
			e = normalize(e)
		}
		out[key] = e
	}
	if len(inline) > 0 {
		keys := make([]string, 0, len(inline))
		for k := range inline {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if _, typed := out[k]; typed || knownKey(t, k) {
				return nil, fmt.Errorf("%s: extra key %q is a typed input; set it through the typed input instead",
					extraPath(path), k)
			}
			out[k] = inline[k]
		}
	}
	return out, nil
}

func extraPath(path string) string {
	if p := strings.TrimPrefix(path, "."); p != "" {
		return p + ".extra"
	}
	return "extra"
}

// knownKey reports whether k is the YAML key of a typed field of t.
func knownKey(t reflect.Type, k string) bool {
	for i := range t.NumField() {
		f := t.Field(i)
		if f.Tag.Get("pulumi") == "" {
			continue
		}
		if key, _, _ := strings.Cut(f.Tag.Get("yaml"), ","); key == k {
			return true
		}
	}
	return false
}

func isAnyMap(t reflect.Type) bool {
	return t.Kind() == reflect.Map && t.Elem().Kind() == reflect.Interface
}

// normalize converts the float64 values Pulumi uses for every number in untyped maps back
// to integers when they have no fractional part, so `context_size: 32768` is not written
// as `32768.0`.
func normalize(v any) any {
	switch x := v.(type) {
	case float64:
		if x == math.Trunc(x) && math.Abs(x) < 1<<53 {
			return int64(x)
		}
		return x
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = normalize(e)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = normalize(e)
		}
		return out
	default:
		return v
	}
}
