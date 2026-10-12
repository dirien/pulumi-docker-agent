// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/dirien/pulumi-docker-agent/provider/pkg/render"
)

// userSettingsLocation matches the YAML location a user settings validation message starts
// with: `settings.layout.sidebar_position: ...`, `aliases.dev: ...`, `board.columns[1]: ...`
// or `settings.tab_title_max_length must not be negative`.
var userSettingsLocation = regexp.MustCompile(`^([a-z_]+(?:\.[A-Za-z0-9_-]+|\[\d+\])*)(?::|\s)`)

// userSettingsProperty returns the input a user settings validation message is about, for
// example `settings.layout.sidebarPosition` for "settings.layout.sidebar_position: invalid
// value ...", or "" when the message names no input. A location below what the inputs model
// resolves to the closest input.
func userSettingsProperty(msg string) string {
	m := userSettingsLocation.FindStringSubmatch(msg)
	if m == nil {
		return ""
	}
	var path render.Path
	t := reflect.TypeFor[UserSettingsArgs]()
	loc := m[1]
	for loc != "" {
		var seg string
		index := -1
		if strings.HasPrefix(loc, "[") {
			end := strings.IndexByte(loc, ']')
			index, _ = strconv.Atoi(loc[1:end])
			loc = loc[end+1:]
		} else {
			loc = strings.TrimPrefix(loc, ".")
			end := strings.IndexAny(loc, ".[")
			if end < 0 {
				end = len(loc)
			}
			seg, loc = loc[:end], loc[end:]
		}
		for t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		switch {
		case index >= 0 && t.Kind() == reflect.Slice:
			path, t = path.Index(index), t.Elem()
		case index < 0 && t.Kind() == reflect.Map:
			path, t = path.Key(seg), t.Elem()
		case index < 0 && t.Kind() == reflect.Struct:
			f, ok := userSettingsField(t, seg)
			if !ok {
				return string(path)
			}
			name, _, _ := strings.Cut(f.Tag.Get("pulumi"), ",")
			path, t = path.Field(name), f.Type
		default:
			return string(path)
		}
	}
	return string(path)
}

// userSettingsField returns the field of t whose YAML key is key or, for the provider's own
// messages, whose input name is key (`extra`).
func userSettingsField(t reflect.Type, key string) (reflect.StructField, bool) {
	if f, ok := fieldByTag(t, "yaml", key); ok {
		return f, true
	}
	return fieldByTag(t, "pulumi", key)
}

// fieldByTag returns the input field of t whose tag (the part before the first comma) is
// name.
func fieldByTag(t reflect.Type, tag, name string) (reflect.StructField, bool) {
	for i := range t.NumField() {
		f := t.Field(i)
		if n, _, _ := strings.Cut(f.Tag.Get(tag), ","); n == name && f.Tag.Get("pulumi") != "" {
			return f, true
		}
	}
	return reflect.StructField{}, false
}
