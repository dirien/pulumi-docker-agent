// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"strings"
)

// matrix renders the coverage matrix: every generated Pulumi input with the schema path
// and schema type it comes from.
func matrix(g *generator) string {
	var b strings.Builder
	types, fields, untyped := 0, 0, 0
	for _, t := range g.sortedTypes() {
		types++
		for _, f := range t.fields {
			if f.render == "extra" || f.render == "key" {
				continue
			}
			fields++
			if isUntyped(f.goType) {
				untyped++
			}
		}
	}
	fmt.Fprintf(&b, "%d Pulumi object types, %d typed properties (%d of them free-form `any`), plus an `extra` map on every mapping type.\n\n",
		types, fields, untyped)
	for _, t := range g.sortedTypes() {
		kind := "mapping"
		if t.union {
			kind = "union (fields render into one value)"
		}
		fmt.Fprintf(&b, "### `%s` (from `%s`, %s)\n\n", t.name, t.path, kind)
		b.WriteString("| Schema path | Schema type | Pulumi property | Go type | YAML key |\n|---|---|---|---|---|\n")
		for _, f := range t.fields {
			key := "`" + f.yamlKey + "`"
			switch f.render {
			case "extra":
				key = "(merged)"
			case "key":
				key = "(map key)"
			case "value,concat":
				key = "(the value)"
			}
			req := ""
			if f.required {
				req = " (required)"
			}
			fmt.Fprintf(&b, "| `%s` | `%s` | `%s`%s | `%s` | %s |\n", f.path, escapePipes(f.schema), f.pulumi, req,
				f.goType, key)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func isUntyped(goType string) bool {
	return goType == goAny || strings.HasSuffix(goType, "]any")
}

// untypedPath reports whether path is, or lies below, a free-form value. It is the
// classifier of the schema diff report.
func untypedPath(path string) (bool, string) {
	for p, goType := range freeForm {
		if path == p || strings.HasPrefix(path, p+".") || strings.HasPrefix(path, p+"{") ||
			strings.HasPrefix(path, p+"[") || strings.HasPrefix(path, p+"|") {
			return true, "free-form value `" + p + "` (" + goType + ")"
		}
	}
	return false, ""
}

func escapePipes(s string) string { return strings.ReplaceAll(s, "|", `\|`) }
