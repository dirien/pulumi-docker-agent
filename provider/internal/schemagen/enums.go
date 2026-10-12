// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// goEnum is a generated Pulumi enum: a named string type with a constant per value and a
// Values method (infer.Enum). Its Kind stays string, so rendering and decoding treat it like
// any other string.
type goEnum struct {
	name   string
	paths  []string // the schema paths that use it, in generation order
	values []enumValue
}

type enumValue struct {
	value string
	name  string // PascalCase, unique within the enum
	doc   string
}

// enumType returns the name of the Pulumi enum generated for the string value at path, or ""
// when the value is a plain string (see enumValuesOf). owner is the Go type the value belongs
// to; it names enums that have no entry in enumTypes.
func (g *generator) enumType(owner, path string, n *jnode) string {
	if name, ok := g.enumAt[path]; ok {
		return name // a path is generated twice when a definition is also merged (RAGToolset)
	}
	values := enumValuesOf(path, n)
	if values == nil {
		return ""
	}
	name, named := enumTypes[path]
	if named {
		if objectTypeName(name) {
			g.fail("%s: enum name %s in enumTypes is also an object type name", path, name)
			return ""
		}
	} else {
		name = g.defaultEnumName(owner, path)
	}
	e := g.enums[name]
	if e == nil {
		e = &goEnum{name: name}
		g.enums[name] = e
	} else if !slices.Equal(e.valueList(), values) {
		g.notes = append(g.notes, fmt.Sprintf("enum %s: %s has the values %s, %s has %s; %s has all of them "+
			"(see enumTypes in overrides.go)", name, e.paths[0], backtickList(e.valueList()), path,
			backtickList(values), name))
	}
	e.paths = append(e.paths, path)
	docs := valueDocs(n.str("description"), values)
	for _, v := range values {
		i := slices.IndexFunc(e.values, func(ev enumValue) bool { return ev.value == v })
		if i < 0 {
			e.values = append(e.values, enumValue{value: v, name: e.uniqueValueName(enumValueName(v))})
			i = len(e.values) - 1
		}
		if e.values[i].doc == "" {
			e.values[i].doc = docs[v]
		}
		if doc, ok := enumValueDocs[name][v]; ok {
			e.values[i].doc = doc
		}
	}
	g.enumAt[path] = name
	return name
}

// enumValuesOf returns the values of the enum at path when it becomes a Pulumi enum: a
// string enum with at least two values, none of them empty or numeric, at a path that is not
// in volatileEnums. Single values (`type: tokens`), numeric sets (the config `version`) and
// volatile sets stay plain strings whose description lists the allowed values.
func enumValuesOf(path string, n *jnode) []string {
	if _, volatile := volatileEnums[path]; volatile || !slices.Equal(n.types(), []string{"string"}) {
		return nil
	}
	var values []string
	for _, v := range n.list("enum") {
		if v.kind != jStr || v.s == "" {
			return nil
		}
		if slices.Contains(values, v.s) {
			continue
		}
		if _, err := strconv.ParseFloat(v.s, 64); err == nil {
			return nil
		}
		values = append(values, v.s)
	}
	if len(values) < 2 {
		return nil
	}
	return values
}

// defaultEnumName names an enum without an entry in enumTypes: the owner type and the
// property name (`HookDefinition.on_error` would be HookDefinitionOnError). A name that is
// taken gets a number.
func (g *generator) defaultEnumName(owner, path string) string {
	key := strings.TrimRight(path, "[]{}")
	key = key[strings.LastIndexAny(key, ".|")+1:]
	base := owner + pascal(key)
	name := base
	for i := 2; objectTypeName(name) || g.enums[name] != nil; i++ {
		name = base + strconv.Itoa(i)
	}
	return name
}

// isEnum reports whether the value at path, its items or its map values are a Pulumi enum.
func (g *generator) isEnum(path string) bool {
	for _, p := range []string{path, path + "[]", path + "{}"} {
		if _, ok := g.enumAt[p]; ok {
			return true
		}
	}
	return false
}

func objectTypeName(name string) bool {
	if name == "Document" {
		return true
	}
	for _, n := range typeNames {
		if n == name {
			return true
		}
	}
	return false
}

func (e *goEnum) valueList() []string {
	out := make([]string, len(e.values))
	for i, v := range e.values {
		out[i] = v.value
	}
	return out
}

func (e *goEnum) uniqueValueName(name string) string {
	taken := func(n string) bool {
		return slices.ContainsFunc(e.values, func(v enumValue) bool { return v.name == n })
	}
	out := name
	for i := 2; taken(out); i++ {
		out = name + strconv.Itoa(i)
	}
	return out
}

// enumValueName returns the PascalCase name of an enum value: every word capitalized, words
// in capitals lower-cased first (`claude-code` is ClaudeCode, `on_failure` OnFailure, `GET`
// Get). enumValueNames overrides it.
func enumValueName(v string) string {
	if n, ok := enumValueNames[v]; ok {
		return n
	}
	var b strings.Builder
	for _, w := range strings.FieldsFunc(v, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if strings.ToUpper(w) == w {
			w = strings.ToLower(w)
		}
		r := []rune(w)
		r[0] = unicode.ToUpper(r[0])
		b.WriteString(string(r))
	}
	name := b.String()
	if name == "" || unicode.IsDigit([]rune(name)[0]) {
		name = "Value" + name
	}
	return name
}

// valueDocs extracts per-value descriptions from a property description that explains the
// values in the form `'value' does something` (or "value"): the text after the quoted value,
// up to the next quoted value or the end of its sentence.
func valueDocs(desc string, values []string) map[string]string {
	type hit struct {
		value      string
		start, end int // of the quoted value
	}
	var hits []hit
	for _, v := range values {
		for _, q := range []string{"'", `"`} {
			if i := strings.Index(desc, q+v+q); i >= 0 {
				hits = append(hits, hit{value: v, start: i, end: i + len(v) + 2})
				break
			}
		}
	}
	slices.SortFunc(hits, func(a, b hit) int { return a.start - b.start })
	out := map[string]string{}
	for i, h := range hits {
		text := desc[h.end:]
		if i+1 < len(hits) {
			text = desc[h.end:hits[i+1].start]
		}
		text = text[:sentenceEnd(text)]
		if doc := cleanValueDoc(text); doc != "" {
			out[h.value] = doc
		}
	}
	return out
}

// sentenceEnd returns the length of the first sentence of text: up to a period that is
// followed by white space and does not end an abbreviation such as "e.g.", or all of it.
func sentenceEnd(text string) int {
	for i := 0; i+1 < len(text); i++ {
		if text[i] != '.' || !unicode.IsSpace(rune(text[i+1])) {
			continue
		}
		word := strings.ToLower(text[strings.LastIndexAny(text[:i], " (")+1 : i])
		if !slices.Contains([]string{"e.g", "i.e", "etc", "vs"}, word) {
			return i + 1
		}
	}
	return len(text)
}

// cleanValueDoc turns the text after a quoted value into a sentence: "(default): auto-restart
// on failure." becomes "Auto-restart on failure (the default).", "is the fail-closed profile"
// "The fail-closed profile.", "continues silently, and" "Continues silently.".
func cleanValueDoc(text string) string {
	text = strings.TrimSpace(text)
	isDefault := strings.HasPrefix(text, "(default)")
	text = strings.TrimSpace(strings.TrimPrefix(text, "(default)"))
	text = strings.TrimSpace(strings.TrimLeft(text, ":=—-"))
	text = strings.TrimPrefix(text, "is ")
	for {
		trimmed := strings.TrimSpace(strings.TrimRight(strings.TrimSpace(text), ",;"))
		for _, conj := range []string{" and", " or"} {
			trimmed = strings.TrimSuffix(trimmed, conj)
		}
		if trimmed == text {
			break
		}
		text = trimmed
	}
	if text == "" || !unicode.IsLetter([]rune(text)[0]) {
		return ""
	}
	r := []rune(text)
	r[0] = unicode.ToUpper(r[0])
	text = strings.TrimSuffix(string(r), ".")
	if isDefault {
		text += " (the default)"
	}
	return text + "."
}

// sortedEnums returns the generated enums by name.
func (g *generator) sortedEnums() []*goEnum {
	out := make([]*goEnum, 0, len(g.enums))
	for _, e := range g.enums {
		out = append(out, e)
	}
	slices.SortFunc(out, func(a, b *goEnum) int { return strings.Compare(a.name, b.name) })
	return out
}
