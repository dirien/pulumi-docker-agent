// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package render

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Schema paths: `Config.<prop>` for root properties, `<Definition>.<prop>`, `<path>[]` for
// array items, `<path>{}` for map values and `<path>|<i>` for branch i of a oneOf, anyOf or
// allOf. They match the paths of `schemagen matrix` and of the upgrade schema diff.

// untypedPaths are the values that are free-form by design. They are typed `any` or
// `map[string]any` and rendered verbatim. Everything else must be typed.
var untypedPaths = map[string]string{
	"Config.flavors{}":                     "a flavor is a YAML patch over the whole document (JSON Merge Patch plus `key+`/`key-` operators)",
	"MCPToolset.config":                    "server-specific configuration passed to the MCP server",
	"Toolset.config":                       "server-specific configuration passed to the MCP server",
	"ModelConfig.provider_opts":            "provider-specific client options, different for every provider",
	"ProviderConfig.provider_opts":         "provider-specific client options, different for every provider",
	"AgentConfig.structured_output.schema": "a user-supplied JSON schema",
	"ApiConfig.output_schema":              "a user-supplied JSON schema",
	"ScriptShellToolConfig.args":           "JSON schema properties of the script's arguments",
}

// foldedBranches are union branches without a field of their own: they are expressed
// through another branch with the same meaning, exactly as docker-agent's loader treats them.
var foldedBranches = map[string]string{
	"AgentConfig.instruction|1":      "a list of strings is joined with a blank line, so it is the string form",
	"AgentConfig.instruction_file|0": "a single path is the list form with one entry",
	"AgentConfig.commands|1":         "a list of single-entry maps is merged in order into the map form",
	"Commands|1":                     "a list of single-entry maps is merged in order into the map form",
	"AgentConfig.skills|0":           "`true` is `[\"local\"]` and `false` is an empty list",
	"SkillsConfig|0":                 "`true` is `[\"local\"]` and `false` is an empty list",
	"ModelConfig.task_budget|1":      "`{type: tokens, total: N}` is `N`; `tokens` is the only type",
	"ProviderConfig.task_budget|1":   "`{type: tokens, total: N}` is `N`; `tokens` is the only type",
	"EvaluatorConfig.timeout|1":      "a number of seconds N is the duration `Ns`",
}

// loaderOnlyFields are typed fields that agent-schema.json does not define, but that
// docker-agent's loader accepts (see loaderOnly in provider/internal/schemagen).
var loaderOnlyFields = map[string]string{
	"McpDefinition.toon":              "mcps entries are decoded as toolsets of type mcp",
	"McpDefinition.readonly":          "mcps entries are decoded as toolsets of type mcp",
	"McpDefinition.model":             "mcps entries are decoded as toolsets of type mcp",
	"McpDefinition.timeout":           "mcps entries are decoded as toolsets of type mcp",
	"McpDefinition.allow_private_ips": "mcps entries are decoded as toolsets of type mcp",
	"ApiArg.enum":                     "API arguments are free-form JSON schema properties; the docs use enum",
}

type coverage struct {
	defs     map[string]map[string]any
	errs     []string
	seen     map[string]bool
	matched  map[reflect.Type]map[string]bool // YAML keys of a Go type matched by the schema
	typed    []string
	untyped  []string
	folded   []string
	usedSkip map[string]bool
}

// TestSchemaCoverage walks every definition and property of the pinned agent-schema.json
// and fails when a property has no typed field, when a field is `any` without being listed
// in untypedPaths, when a union branch is neither typed nor listed in foldedBranches, when a
// required property is an optional input, or when a typed field has no schema property.
func TestSchemaCoverage(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("schema/agent-schema.json")
	require.NoError(t, err)
	var schema map[string]any
	require.NoError(t, json.Unmarshal(raw, &schema))

	c := &coverage{defs: map[string]map[string]any{}, seen: map[string]bool{},
		matched: map[reflect.Type]map[string]bool{}, usedSkip: map[string]bool{}}
	for k, v := range schema["definitions"].(map[string]any) {
		c.defs[k] = v.(map[string]any)
	}
	c.object("Config", schema, reflect.TypeFor[Document]())

	// Every definition must be reachable from the root document.
	for def := range c.defs {
		if !slices.ContainsFunc(slices.Collect(mapsKeys(c.seen)), func(k string) bool { return strings.HasPrefix(k, def+"@") }) {
			c.fail("%s: definition is not reachable from the root document", def)
		}
	}
	// Every typed field must come from the schema (or be a known loader-only key).
	for typ, keys := range c.matched {
		for _, f := range fieldsOf(typ) {
			if f.yamlKey == "-" || f.extra || keys[f.yamlKey] {
				continue
			}
			if _, ok := loaderOnlyFields[typ.Name()+"."+f.yamlKey]; ok {
				continue
			}
			c.fail("%s.%s (YAML key %q): typed field without a schema property", typ.Name(), f.pulumi, f.yamlKey)
		}
	}
	for _, list := range []map[string]string{untypedPaths, foldedBranches} {
		for p := range list {
			if !c.usedSkip[p] {
				c.fail("%s: listed as untyped or folded, but the schema has no such path", p)
			}
		}
	}
	sort.Strings(c.errs)
	t.Logf("schema paths: %d typed, %d free-form (untypedPaths), %d folded union branches", len(c.typed),
		len(c.untyped), len(c.folded))
	require.Empty(t, c.errs, "agent-schema.json is not fully covered by the generated types; run make "+
		"generate_config_types, or update provider/internal/schemagen/overrides.go and the lists in this test")
}

func mapsKeys[V any](m map[string]V) func(func(string) bool) {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}

func (c *coverage) fail(format string, args ...any) {
	c.errs = append(c.errs, fmt.Sprintf(format, args...))
}

func deref(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

func refName(n map[string]any) string {
	ref, _ := n["$ref"].(string)
	return strings.TrimPrefix(ref, "#/definitions/")
}

func types(n map[string]any) []string {
	switch t := n["type"].(type) {
	case string:
		return []string{t}
	case []any:
		var out []string
		for _, e := range t {
			out = append(out, e.(string))
		}
		return out
	}
	return nil
}

func branches(n map[string]any) []map[string]any {
	var out []map[string]any
	for _, k := range []string{"oneOf", "anyOf"} {
		if l, ok := n[k].([]any); ok {
			for _, b := range l {
				out = append(out, b.(map[string]any))
			}
		}
	}
	return out
}

func isUnion(n map[string]any) bool {
	return n["type"] == nil && n["properties"] == nil && n["$ref"] == nil && len(branches(n)) > 0
}

func isMapSchema(n map[string]any) bool {
	_, ok := n["additionalProperties"].(map[string]any)
	return ok
}

func isFree(t reflect.Type) bool {
	return t.Kind() == reflect.Interface || (t.Kind() == reflect.Map && t.Elem().Kind() == reflect.Interface)
}

// value checks the schema node n at path against the Go type t.
func (c *coverage) value(path string, n map[string]any, t reflect.Type) {
	t = deref(t)
	if reason, ok := untypedPaths[path]; ok {
		c.usedSkip[path] = true
		if !isFree(t) {
			c.fail("%s: listed in untypedPaths (%s) but typed as %s", path, reason, t)
		}
		c.untyped = append(c.untyped, path)
		return
	}
	if def := refName(n); def != "" {
		key := def + "@" + t.String()
		if c.seen[key] {
			return
		}
		c.seen[key] = true
		c.value(def, c.defs[def], t)
		return
	}
	if all, ok := n["allOf"].([]any); ok && n["properties"] == nil {
		// Aliases (a $ref plus constraints) and merged definitions (RAGToolset).
		for i, b := range all {
			bn := b.(map[string]any)
			if refName(bn) != "" || bn["properties"] != nil {
				c.value(fmt.Sprintf("%s|%d", path, i), bn, t)
			}
		}
		return
	}
	if isFree(t) {
		c.fail("%s: typed as %s, which is free-form; type it or list it in untypedPaths", path, t)
		return
	}
	if isUnion(n) {
		c.union(path, n, []reflect.Type{t})
		return
	}
	ts := types(n)
	switch {
	case slices.Contains(ts, "array") || n["items"] != nil:
		if t.Kind() != reflect.Slice {
			c.fail("%s: array typed as %s", path, t)
			return
		}
		c.value(path+"[]", n["items"].(map[string]any), t.Elem())
	case slices.Contains(ts, "object") || n["properties"] != nil:
		switch {
		case n["properties"] != nil:
			c.object(path, n, t)
		case isMapSchema(n):
			if t.Kind() != reflect.Map {
				c.fail("%s: map typed as %s", path, t)
				return
			}
			c.value(path+"{}", n["additionalProperties"].(map[string]any), t.Elem())
		case n["patternProperties"] != nil:
			if t.Kind() != reflect.Map {
				c.fail("%s: map typed as %s", path, t)
				return
			}
			for _, p := range n["patternProperties"].(map[string]any) {
				c.value(path+"{}", p.(map[string]any), t.Elem())
			}
		default:
			c.fail("%s: free-form object typed as %s", path, t)
		}
	case len(ts) == 1:
		want := map[string]reflect.Kind{"string": reflect.String, "boolean": reflect.Bool, "integer": reflect.Int,
			"number": reflect.Float64}[ts[0]]
		if t.Kind() != want {
			c.fail("%s: %s typed as %s", path, ts[0], t)
			return
		}
		c.typed = append(c.typed, path)
	default:
		c.fail("%s: unsupported schema %v", path, n)
	}
}

// object checks the properties of an object node against the fields of struct type t.
func (c *coverage) object(path string, n map[string]any, t reflect.Type) {
	if t.Kind() != reflect.Struct {
		c.fail("%s: object typed as %s", path, t)
		return
	}
	if c.matched[t] == nil {
		c.matched[t] = map[string]bool{}
	}
	fields := fieldsOf(t)
	props, _ := n["properties"].(map[string]any)
	required := map[string]bool{}
	if r, ok := n["required"].([]any); ok {
		for _, k := range r {
			required[k.(string)] = true
		}
	}
	for _, k := range slices.Sorted(mapsKeys(props)) {
		p := path + "." + k
		var group []reflect.Type
		var infos []fieldInfo
		for _, f := range fields {
			if f.yamlKey == k && !f.extra {
				group = append(group, t.Field(f.index).Type)
				infos = append(infos, f)
			}
		}
		if len(group) == 0 {
			c.fail("%s: no typed field", p)
			continue
		}
		c.matched[t][k] = true
		if required[k] && len(infos) == 1 && !infos[0].required {
			c.fail("%s: required by the schema but an optional input", p)
		}
		prop := props[k].(map[string]any)
		switch {
		case infos[0].keyedBy != "":
			// agents: a map in YAML, a list keyed by name in Pulumi.
			c.value(p+"{}", prop["additionalProperties"].(map[string]any), t.Field(infos[0].index).Type.Elem())
		case len(group) == 1 && !isUnion(prop):
			c.value(p, prop, group[0])
		default:
			c.union(p, prop, group)
		}
	}
}

// union checks every branch of a union node against the fields that represent it. A union
// struct (fields rendered into the value) is expanded into its fields.
func (c *coverage) union(path string, n map[string]any, group []reflect.Type) {
	if len(group) == 1 && deref(group[0]).Kind() == reflect.Struct {
		if fs := fieldsOf(deref(group[0])); len(fs) > 0 && fs[0].value {
			st := deref(group[0])
			group = nil
			for _, f := range fs {
				group = append(group, st.Field(f.index).Type)
			}
		}
	}
	bs := branches(n)
	if len(bs) == 0 {
		if len(group) > 1 {
			c.fail("%s: %d fields share the key, but the schema has no union", path, len(group))
			return
		}
		c.value(path, n, group[0])
		return
	}
	for i, b := range bs {
		bp := fmt.Sprintf("%s|%d", path, i)
		if reason, ok := foldedBranches[bp]; ok {
			c.usedSkip[bp] = true
			c.folded = append(c.folded, bp+" ("+reason+")")
			continue
		}
		matches := c.matching(b, group)
		switch {
		case len(matches) == 0:
			c.fail("%s: union branch %v has no typed field; type it or list it in foldedBranches", bp, b)
		case slices.Contains(types(b), "array") && isUnion(c.resolve(c.resolve(b)["items"])):
			// A list whose items are a union: each item branch has its own field.
			c.union(bp+"[]", c.resolve(b)["items"].(map[string]any), elems(matches))
		case c.isUnionMap(b):
			c.union(bp+"{}", c.resolve(b)["additionalProperties"].(map[string]any), elems(matches))
		default:
			c.value(bp, b, matches[0])
		}
	}
}

func elems(ts []reflect.Type) []reflect.Type {
	out := make([]reflect.Type, len(ts))
	for i, t := range ts {
		out[i] = deref(t).Elem()
	}
	return out
}

func (c *coverage) resolve(n any) map[string]any {
	m, _ := n.(map[string]any)
	if m == nil {
		return map[string]any{}
	}
	if def := refName(m); def != "" {
		return c.defs[def]
	}
	return m
}

func (c *coverage) isUnionMap(b map[string]any) bool {
	ap, ok := c.resolve(b)["additionalProperties"].(map[string]any)
	return ok && isUnion(ap)
}

// matching returns the fields whose Go kind can hold the branch.
func (c *coverage) matching(b map[string]any, group []reflect.Type) []reflect.Type {
	r := c.resolve(b)
	var kind reflect.Kind
	ts := types(r)
	switch {
	case r["properties"] != nil:
		kind = reflect.Struct
	case slices.Contains(ts, "array"):
		kind = reflect.Slice
	case slices.Contains(ts, "object"):
		kind = reflect.Map
	case len(ts) == 1:
		kind = map[string]reflect.Kind{"string": reflect.String, "boolean": reflect.Bool, "integer": reflect.Int,
			"number": reflect.Float64}[ts[0]]
	}
	var out []reflect.Type
	for _, g := range group {
		if deref(g).Kind() == kind {
			out = append(out, g)
		}
	}
	return out
}
