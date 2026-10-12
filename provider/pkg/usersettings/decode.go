// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package usersettings

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"slices"
	"strings"

	"github.com/docker/docker-agent/pkg/userconfig"
	"github.com/goccy/go-yaml"
)

// Canonical parses an existing user configuration file with docker-agent's own types and
// decoder, the way docker-agent loads `~/.config/cagent/config.yaml`, and returns the
// document docker-agent writes for it (without comments, `version` set to FormatVersion),
// both as YAML and as the generic value Decode turns into typed inputs. Shorthands are
// expanded the way docker-agent reads them (a hook event written as one mapping, a
// `provider/model` default model), and keys docker-agent does not know are kept only where
// docker-agent keeps them: at the top level and in `settings`.
func Canonical(data []byte) (doc map[string]any, canonical string, err error) {
	cfg := &userconfig.Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, "", errors.New(CleanError(err.Error()))
	}
	cfg.Version = FormatVersion
	out, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, "", fmt.Errorf("rendering user settings: %s", CleanError(err.Error()))
	}
	if err := yaml.Unmarshal(out, &doc); err != nil {
		return nil, "", errors.New(CleanError(err.Error()))
	}
	delete(doc, "version")
	return doc, string(out), nil
}

// YAMLValueSetter is implemented by input types whose YAML form is not (only) a mapping: the
// inverse of YAMLValuer. SetYAMLValue reports false when v is a mapping that is decoded field
// by field.
type YAMLValueSetter interface {
	SetYAMLValue(v any) (bool, error)
}

var yamlValueSetterType = reflect.TypeFor[YAMLValueSetter]()

// Decode fills the typed input struct out from a document returned by Canonical: the
// inverse of Encode. Each field is read from the key in its `yaml` tag; keys without a typed
// field go into the struct's `yaml:",inline"` map (`extra`), and a key that has neither is an
// error. Numbers are converted to the field's type; free-form values (`any`) are kept as
// they are.
func Decode(doc map[string]any, out any) error {
	v := reflect.ValueOf(out)
	if v.Kind() != reflect.Pointer || v.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("internal error: Decode needs a pointer to a struct, got %T", out)
	}
	return decodeStruct(doc, v.Elem(), "")
}

func decodeStruct(m map[string]any, dst reflect.Value, path string) error {
	t := dst.Type()
	used := map[string]bool{}
	var inline reflect.Value
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
			inline = dst.Field(i)
			continue
		case key == "":
			return fmt.Errorf("internal error: %s.%s has no yaml tag", t.Name(), f.Name)
		}
		val, ok := m[key]
		if !ok {
			continue
		}
		used[key] = true
		if err := decodeValue(val, dst.Field(i), join(path, key)); err != nil {
			return err
		}
	}
	var unknown []string
	for k := range m {
		if !used[k] {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	slices.Sort(unknown)
	if !inline.IsValid() {
		return fmt.Errorf("%s has no typed input", join(path, unknown[0]))
	}
	extra := make(map[string]any, len(unknown))
	for _, k := range unknown {
		extra[k] = m[k]
	}
	inline.Set(reflect.ValueOf(extra))
	return nil
}

func decodeValue(src any, dst reflect.Value, path string) error {
	if src == nil {
		return nil
	}
	if dst.Kind() != reflect.Pointer && dst.CanAddr() && dst.Addr().Type().Implements(yamlValueSetterType) {
		handled, err := dst.Addr().Interface().(YAMLValueSetter).SetYAMLValue(src)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if handled {
			return nil
		}
	}
	switch dst.Kind() {
	case reflect.Pointer:
		e := reflect.New(dst.Type().Elem())
		if err := decodeValue(src, e.Elem(), path); err != nil {
			return err
		}
		dst.Set(e)
	case reflect.Struct:
		m, ok := src.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: expected a mapping, got %T", path, src)
		}
		return decodeStruct(m, dst, path)
	case reflect.Slice:
		items, ok := src.([]any)
		if !ok {
			return fmt.Errorf("%s: expected a list, got %T", path, src)
		}
		s := reflect.MakeSlice(dst.Type(), len(items), len(items))
		for i, item := range items {
			if err := decodeValue(item, s.Index(i), fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
		dst.Set(s)
	case reflect.Map:
		m, ok := src.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: expected a mapping, got %T", path, src)
		}
		out := reflect.MakeMapWithSize(dst.Type(), len(m))
		for k, item := range m {
			e := reflect.New(dst.Type().Elem()).Elem()
			if err := decodeValue(item, e, path+"."+k); err != nil {
				return err
			}
			out.SetMapIndex(reflect.ValueOf(k).Convert(dst.Type().Key()), e)
		}
		dst.Set(out)
	case reflect.Interface:
		dst.Set(reflect.ValueOf(src))
	case reflect.String:
		s, ok := src.(string)
		if !ok {
			return fmt.Errorf("%s: expected a string, got %T", path, src)
		}
		dst.SetString(s)
	case reflect.Bool:
		b, ok := src.(bool)
		if !ok {
			return fmt.Errorf("%s: expected a boolean, got %T", path, src)
		}
		dst.SetBool(b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, ok := IntValue(src)
		if !ok {
			return fmt.Errorf("%s: expected an integer, got %v", path, src)
		}
		dst.SetInt(n)
	case reflect.Float32, reflect.Float64:
		f, ok := floatValue(src)
		if !ok {
			return fmt.Errorf("%s: expected a number, got %v", path, src)
		}
		dst.SetFloat(f)
	default:
		return fmt.Errorf("internal error: %s: unsupported kind %s", path, dst.Kind())
	}
	return nil
}

// IntValue returns the integer a YAML number decodes to, and false for anything else.
func IntValue(v any) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int64:
		return n, true
	case uint64:
		if n > math.MaxInt64 {
			return 0, false
		}
		return int64(n), true
	case float64:
		if n == math.Trunc(n) && math.Abs(n) < 1<<53 {
			return int64(n), true
		}
	}
	return 0, false
}

func floatValue(v any) (float64, bool) {
	if f, ok := v.(float64); ok {
		return f, true
	}
	if n, ok := IntValue(v); ok {
		return float64(n), true
	}
	return 0, false
}

func join(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}
