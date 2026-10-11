// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/docker/docker-agent/pkg/userconfig"
	"github.com/stretchr/testify/assert"
)

// TestUserSettingsCoversDockerAgentUserConfig is the drift guard for UserSettings. It walks
// docker-agent's userconfig.Config (and every type it embeds from pkg/config/latest)
// alongside UserSettingsArgs and fails when docker-agent has a YAML key that the typed
// inputs do not represent, when an input names a key docker-agent does not know, or when
// the value kinds disagree. The docker-agent upgrade workflow runs it, so a new user
// setting fails the upgrade PR until it is modelled (or allowlisted below).
func TestUserSettingsCoversDockerAgentUserConfig(t *testing.T) {
	t.Parallel()
	w := &coverageWalker{
		distinct: map[string]bool{},
		types:    map[string]bool{},
		// Keys deliberately not exposed as inputs, keyed by their path.
		allow: map[string]string{
			"version": "always written as userconfig.CurrentVersion, like docker-agent's own save",
		},
	}
	w.walk(reflect.TypeFor[userconfig.Config](), reflect.TypeFor[UserSettingsArgs](), "")
	for _, p := range w.problems {
		t.Error(p)
	}
	t.Logf("docker-agent user configuration: %d distinct keys in %d types (%d key paths); typed: %d paths, "+
		"allowlisted: %d, inline `extra` escape hatches: %d",
		len(w.distinct), len(w.types), w.upstreamKeys, w.covered, len(w.allow), w.inline)
	assert.Equal(t, w.upstreamKeys, w.covered+len(w.allow), "every docker-agent key is either typed or allowlisted")
}

type coverageWalker struct {
	allow        map[string]string
	distinct     map[string]bool
	types        map[string]bool
	problems     []string
	upstreamKeys int
	covered      int
	inline       int
}

type keyedField struct {
	key    string
	typ    reflect.Type
	inline bool
}

// upstreamFields returns the YAML keys goccy/go-yaml uses for t: the `yaml` tag, else the
// `json` tag, else the lower-cased field name. Untagged embedded structs are flattened.
func covUpstreamFields(t reflect.Type) []keyedField {
	var out []keyedField
	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		tag := f.Tag.Get("yaml")
		if tag == "" {
			tag = f.Tag.Get("json")
		}
		name, opts, _ := strings.Cut(tag, ",")
		if name == "-" {
			continue
		}
		if f.Anonymous && tag == "" && covDeref(f.Type).Kind() == reflect.Struct {
			out = append(out, covUpstreamFields(covDeref(f.Type))...)
			continue
		}
		if name == "" && strings.Contains(opts, "inline") {
			out = append(out, keyedField{typ: f.Type, inline: true})
			continue
		}
		if name == "" {
			name = strings.ToLower(f.Name)
		}
		out = append(out, keyedField{key: name, typ: f.Type})
	}
	return out
}

// inputFields returns the YAML keys of a provider input type (its `yaml` tags).
func covInputFields(t reflect.Type) []keyedField {
	var out []keyedField
	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() || f.Tag.Get("pulumi") == "" {
			continue
		}
		name, opts, _ := strings.Cut(f.Tag.Get("yaml"), ",")
		switch {
		case name == "-":
			continue
		case name == "" && opts == "inline":
			out = append(out, keyedField{typ: f.Type, inline: true})
		default:
			out = append(out, keyedField{key: name, typ: f.Type})
		}
	}
	return out
}

func covDeref(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

func (w *coverageWalker) fail(path, format string, args ...any) {
	w.problems = append(w.problems, fmt.Sprintf("%s: %s", covDisplayPath(path), fmt.Sprintf(format, args...)))
}

func covDisplayPath(path string) string {
	if path == "" {
		return "<root>"
	}
	return path
}

func covJoin(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

func (w *coverageWalker) walk(up, ours reflect.Type, path string) {
	up, ours = covDeref(up), covDeref(ours)
	switch up.Kind() {
	case reflect.Struct:
		if ours.Kind() != reflect.Struct {
			w.fail(path, "docker-agent has an object (%s), the input is %s", up, ours)
			return
		}
		w.walkStruct(up, ours, path)
	case reflect.Slice, reflect.Array:
		if ours.Kind() != reflect.Slice {
			w.fail(path, "docker-agent has a list (%s), the input is %s", up, ours)
			return
		}
		w.walk(up.Elem(), ours.Elem(), path+"[]")
	case reflect.Map:
		if ours.Kind() != reflect.Map {
			w.fail(path, "docker-agent has a map (%s), the input is %s", up, ours)
			return
		}
		w.walk(up.Elem(), ours.Elem(), path+".*")
	default:
		if !covCompatibleKinds(up.Kind(), ours.Kind()) {
			w.fail(path, "docker-agent has a %s, the input is a %s", up.Kind(), ours.Kind())
		}
	}
}

func covCompatibleKinds(up, ours reflect.Kind) bool {
	group := func(k reflect.Kind) string {
		switch k {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			return "int"
		case reflect.Float32, reflect.Float64:
			return "float"
		default:
			return k.String()
		}
	}
	return group(up) == group(ours)
}

func (w *coverageWalker) walkStruct(up, ours reflect.Type, path string) {
	upFields, ourFields := covUpstreamFields(up), covInputFields(ours)
	byKey := map[string]keyedField{}
	ourInline := false
	for _, f := range ourFields {
		if f.inline {
			ourInline = true
			continue
		}
		byKey[f.key] = f
	}
	upKeys := map[string]bool{}
	upInline := false
	for _, f := range upFields {
		if f.inline {
			upInline = true
			continue
		}
		upKeys[f.key] = true
		w.upstreamKeys++
		if w.distinct != nil {
			w.distinct[up.PkgPath()+"."+up.Name()+"."+f.key] = true
			w.types[up.PkgPath()+"."+up.Name()] = true
		}
		p := covJoin(path, f.key)
		if _, ok := w.allow[p]; ok {
			if _, typed := byKey[f.key]; typed {
				w.fail(p, "is allowlisted but also typed; remove it from the allowlist")
			}
			continue
		}
		mine, ok := byKey[f.key]
		if !ok {
			w.fail(p, "docker-agent key %q (%s.%s) is not represented in %s", f.key, up.PkgPath(), up.Name(), ours.Name())
			continue
		}
		w.covered++
		w.walk(f.typ, mine.typ, p)
	}
	for _, k := range slices.Sorted(func(yield func(string) bool) {
		for k := range byKey {
			if !yield(k) {
				return
			}
		}
	}) {
		if !upKeys[k] {
			w.fail(covJoin(path, k), "%s has a key docker-agent's %s does not know", ours.Name(), up.Name())
		}
	}
	switch {
	case upInline && !ourInline:
		w.fail(path, "docker-agent keeps unknown keys here (inline map); add an `extra` input")
	case ourInline && !upInline:
		w.fail(path, "%s has an `extra` input but docker-agent's %s drops unknown keys", ours.Name(), up.Name())
	case upInline:
		w.inline++
	}
}

// TestUserSettingsCoverageWalkerFindsGaps makes sure the drift guard actually fails.
func TestUserSettingsCoverageWalkerFindsGaps(t *testing.T) {
	t.Parallel()
	type upstreamNested struct {
		A string `yaml:"a"`
		B []int  `json:"b,omitempty"`
	}
	type upstream struct {
		Known  upstreamNested    `yaml:"known"`
		New    bool              `yaml:"brand_new,omitempty"`
		Kinds  int               `yaml:"kinds"`
		Extras map[string]any    `yaml:",inline"`
		Map    map[string]string `yaml:"map"`
	}
	type oursNested struct {
		A *string `pulumi:"a,optional" yaml:"a"`
		C *string `pulumi:"c,optional" yaml:"c"`
	}
	type ours struct {
		Known *oursNested       `pulumi:"known,optional" yaml:"known"`
		Kinds *string           `pulumi:"kinds,optional" yaml:"kinds"`
		Map   map[string]string `pulumi:"map,optional"   yaml:"map"`
	}
	w := &coverageWalker{allow: map[string]string{}}
	w.walk(reflect.TypeFor[upstream](), reflect.TypeFor[ours](), "")
	all := strings.Join(w.problems, "\n")
	assert.Contains(t, all, `known.b: docker-agent key "b"`)
	assert.Contains(t, all, `brand_new: docker-agent key "brand_new"`)
	assert.Contains(t, all, "known.c: oursNested has a key docker-agent's upstreamNested does not know")
	assert.Contains(t, all, "kinds: docker-agent has a int, the input is a string")
	assert.Contains(t, all, "add an `extra` input")
	assert.Len(t, w.problems, 5, all)
}
