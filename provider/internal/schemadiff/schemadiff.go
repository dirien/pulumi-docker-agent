// Package schemadiff compares two versions of docker-agent's agent-schema.json (a JSON Schema
// draft-07 document with root `properties` and `definitions`) and renders the result as a
// Markdown report and a CHANGELOG entry. It powers `make upgrade_docker_agent`.
//
// # Path notation
//
// Every schema node that the walker visits has a path:
//
//   - root properties: Config.<prop>            (e.g. Config.agents)
//   - definition properties: <Definition>.<prop>  (e.g. ModelConfig.provider)
//   - nested inline object properties: <parent path>.<prop>
//   - array items: <path>[]                     (e.g. RAGConfig.strategies[].chunking.size)
//   - additionalProperties / patternProperties values: <path>{}  (e.g. ApiConfig.args{}.type)
//   - union branch i of oneOf/anyOf/allOf: <path>|<i>  (e.g. ModelConfig.task_budget|1.total)
//
// Union branches are numbered consecutively across oneOf, anyOf and allOf (in that order), so the
// index is unique even if a node combines several keywords. When a node has a schema-valued
// additionalProperties and patternProperties, or several patternProperties, the extra map values
// are addressed as <path>{<pattern>}; with a single map-value schema (the only case in
// agent-schema.json) it is always <path>{}.
//
// The walker does not follow $ref (a reference is recorded as the type `ref:<Definition>`) and
// does not descend into if/then/else or not, which only carry validation constraints.
package schemadiff

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// RootName is the path prefix of the schema's root properties (Config.<prop>).
const RootName = "Config"

// Diff is the structural difference between two agent-schema.json documents. All slices are
// sorted by path (digit runs compare numerically, so Toolset|2 sorts before Toolset|10).
type Diff struct {
	Version VersionChange

	AddedDefinitions   []Definition
	RemovedDefinitions []Definition

	// AddedProperties and RemovedProperties hold real configuration keys at every nesting level,
	// including the properties of added and removed definitions. A $ref property without a
	// description carries the description of the referenced definition.
	AddedProperties   []Property
	RemovedProperties []Property

	// AddedConstraints and RemovedConstraints hold properties that a oneOf/anyOf/allOf branch
	// re-declares to constrain a key the owning object already declares, such as the
	// `Toolset|3|0.type` (const "api") branches that tie toolset types to required fields.
	AddedConstraints   []Property
	RemovedConstraints []Property

	TypeChanges     []TypeChange
	EnumChanges     []EnumChange
	RequiredChanges []RequiredChange

	// DescriptionChanges lists the root, definition and property paths whose description changed.
	DescriptionChanges []string
}

// VersionChange describes the root `version` enum (the docker-agent configuration versions).
type VersionChange struct {
	Old, New             []string // enum values in schema order
	Added, Removed       []string
	OldLatest, NewLatest string // highest numeric value ("" when absent)
}

// Changed reports whether the version enum gained or lost values.
func (v VersionChange) Changed() bool { return len(v.Added) > 0 || len(v.Removed) > 0 }

// LatestChanged reports whether the latest configuration version changed.
func (v VersionChange) LatestChanged() bool { return v.OldLatest != v.NewLatest }

// Definition is an added or removed entry of `definitions`.
type Definition struct {
	Name        string
	Type        string
	Description string
}

// Property is an added or removed property.
type Property struct {
	Path        string
	Type        string
	Description string
	// Constraint marks a re-declaration inside a union branch (see Diff.AddedConstraints).
	Constraint bool
}

// TypeChange is a property or definition whose type changed. Old and New are rendered type
// strings; inline objects whose only change is their property set are not type changes (those
// show up as added/removed properties).
type TypeChange struct {
	Path     string
	Old, New string
}

// EnumChange lists values added to or removed from an enum (or a const, treated as a
// single-value enum) at a path.
type EnumChange struct {
	Path           string
	Added, Removed []string
}

// RequiredChange lists keys added to or removed from the `required` list of the object at Path.
type RequiredChange struct {
	Path           string
	Added, Removed []string
}

// Compare parses two agent-schema.json documents and returns their structural difference.
func Compare(oldRaw, newRaw []byte) (*Diff, error) {
	oldIdx, err := index(oldRaw)
	if err != nil {
		return nil, fmt.Errorf("old schema: %w", err)
	}
	newIdx, err := index(newRaw)
	if err != nil {
		return nil, fmt.Errorf("new schema: %w", err)
	}

	d := &Diff{Version: compareVersions(oldIdx.version, newIdx.version)}

	for _, name := range sortedKeys(newIdx.defs) {
		if _, ok := oldIdx.defs[name]; !ok {
			d.AddedDefinitions = append(d.AddedDefinitions, newDefinition(name, newIdx.defs[name]))
		}
	}
	for _, name := range sortedKeys(oldIdx.defs) {
		if _, ok := newIdx.defs[name]; !ok {
			d.RemovedDefinitions = append(d.RemovedDefinitions, newDefinition(name, oldIdx.defs[name]))
		}
	}

	paths := make(map[string]struct{}, len(newIdx.entries))
	for p := range oldIdx.entries {
		paths[p] = struct{}{}
	}
	for p := range newIdx.entries {
		paths[p] = struct{}{}
	}
	for _, p := range sortedPaths(paths) {
		o, n := oldIdx.entries[p], newIdx.entries[p]
		switch {
		case o == nil:
			if n.kind == kindProperty {
				prop := newIdx.property(p, n)
				if n.constraint {
					d.AddedConstraints = append(d.AddedConstraints, prop)
				} else {
					d.AddedProperties = append(d.AddedProperties, prop)
				}
			}
		case n == nil:
			if o.kind == kindProperty {
				prop := oldIdx.property(p, o)
				if o.constraint {
					d.RemovedConstraints = append(d.RemovedConstraints, prop)
				} else {
					d.RemovedProperties = append(d.RemovedProperties, prop)
				}
			}
		default:
			d.compareEntry(p, o, n)
		}
	}
	return d, nil
}

func (d *Diff) compareEntry(path string, o, n *entry) {
	named := n.kind == kindProperty || n.kind == kindDefinition || n.kind == kindRoot
	if named && renderType(o.schema, true) != renderType(n.schema, true) {
		d.TypeChanges = append(d.TypeChanges, TypeChange{
			Path: path, Old: renderType(o.schema, false), New: renderType(n.schema, false),
		})
	}
	oe, oHas := enumValues(o.schema)
	ne, nHas := enumValues(n.schema)
	if oHas || nHas {
		if added, removed := setDiff(oe, ne); len(added)+len(removed) > 0 {
			d.EnumChanges = append(d.EnumChanges, EnumChange{Path: path, Added: added, Removed: removed})
		}
	}
	or, nr := sortedStrings(stringList(o.schema["required"])), sortedStrings(stringList(n.schema["required"]))
	if added, removed := setDiff(or, nr); len(added)+len(removed) > 0 {
		d.RequiredChanges = append(d.RequiredChanges, RequiredChange{Path: path, Added: added, Removed: removed})
	}
	if named && description(o.schema) != description(n.schema) {
		d.DescriptionChanges = append(d.DescriptionChanges, path)
	}
}

// Empty reports whether the two schemas are structurally identical (descriptions included).
func (d *Diff) Empty() bool {
	return !d.Version.Changed() &&
		len(d.AddedDefinitions)+len(d.RemovedDefinitions)+
			len(d.AddedProperties)+len(d.RemovedProperties)+
			len(d.AddedConstraints)+len(d.RemovedConstraints)+
			len(d.TypeChanges)+len(d.EnumChanges)+len(d.RequiredChanges)+
			len(d.DescriptionChanges) == 0
}

// Summary returns a one-line summary such as
// "config version 16 → 17, 3 definitions added, 12 properties added, 1 enum changed".
func (d *Diff) Summary() string {
	var parts []string
	switch {
	case d.Version.LatestChanged():
		parts = append(parts, fmt.Sprintf("config version %s → %s", orNone(d.Version.OldLatest), orNone(d.Version.NewLatest)))
	case d.Version.Changed():
		parts = append(parts, "config version list changed")
	}
	add := func(n int, singular, plural, verb string) {
		if n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s %s", n, pluralize(n, singular, plural), verb))
		}
	}
	add(len(d.AddedDefinitions), "definition", "definitions", "added")
	add(len(d.RemovedDefinitions), "definition", "definitions", "removed")
	add(len(d.AddedProperties), "property", "properties", "added")
	add(len(d.RemovedProperties), "property", "properties", "removed")
	add(len(d.TypeChanges), "type", "types", "changed")
	add(len(d.EnumChanges), "enum", "enums", "changed")
	add(len(d.RequiredChanges), "required list", "required lists", "changed")
	add(len(d.AddedConstraints)+len(d.RemovedConstraints), "union constraint", "union constraints", "changed")
	add(len(d.DescriptionChanges), "description", "descriptions", "changed")
	if len(parts) == 0 {
		return "no configuration schema changes"
	}
	return strings.Join(parts, ", ")
}

// PropertyPaths returns the sorted paths of every property (constraint re-declarations excluded)
// in an agent-schema.json document, in the notation described in the package documentation.
func PropertyPaths(raw []byte) ([]string, error) {
	idx, err := index(raw)
	if err != nil {
		return nil, err
	}
	set := map[string]struct{}{}
	for p, e := range idx.entries {
		if e.kind == kindProperty && !e.constraint {
			set[p] = struct{}{}
		}
	}
	return sortedPaths(set), nil
}

// ---------------------------------------------------------------- schema walk

type entryKind int

const (
	kindRoot entryKind = iota
	kindDefinition
	kindProperty
	kindItems
	kindMapValue
	kindBranch
)

type entry struct {
	kind       entryKind
	schema     map[string]any
	constraint bool
}

type schemaIndex struct {
	entries map[string]*entry
	defs    map[string]map[string]any
	version []string
}

func index(raw []byte) (*schemaIndex, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var root map[string]any
	if err := dec.Decode(&root); err != nil {
		return nil, fmt.Errorf("parse schema: %w", err)
	}
	if root == nil {
		return nil, errors.New("parse schema: not a JSON object")
	}
	idx := &schemaIndex{entries: map[string]*entry{}, defs: map[string]map[string]any{}}

	defs := objectMap(root["definitions"])
	for name, v := range defs {
		if m, ok := v.(map[string]any); ok {
			idx.defs[name] = m
		}
	}
	rootSchema := make(map[string]any, len(root))
	for k, v := range root {
		if k != "definitions" && k != "$defs" {
			rootSchema[k] = v
		}
	}
	idx.walk(RootName, kindRoot, rootSchema, nil, false)
	for _, name := range sortedKeys(idx.defs) {
		idx.walk(name, kindDefinition, idx.defs[name], nil, false)
	}

	if v, ok := objectMap(root["properties"])["version"].(map[string]any); ok {
		idx.version, _ = enumValues(v)
	}
	return idx, nil
}

// walk records s at path and descends into its sub-schemas. inherited holds the property names
// declared by the object that owns the current union branch chain; a branch property with one of
// those names is a constraint re-declaration.
func (idx *schemaIndex) walk(path string, kind entryKind, s map[string]any, inherited map[string]bool, constraint bool) {
	if _, dup := idx.entries[path]; dup {
		return
	}
	idx.entries[path] = &entry{kind: kind, schema: s, constraint: constraint}
	if _, ok := s["$ref"]; ok {
		return
	}

	props := objectMap(s["properties"])
	for _, name := range sortedKeys(props) {
		if child, ok := props[name].(map[string]any); ok {
			idx.walk(path+"."+name, kindProperty, child, nil, constraint || inherited[name])
		}
	}

	switch items := s["items"].(type) {
	case map[string]any:
		idx.walk(path+"[]", kindItems, items, nil, constraint)
	case []any: // draft-07 tuple validation (unused by agent-schema.json)
		for i, it := range items {
			if m, ok := it.(map[string]any); ok {
				idx.walk(path+"[]|"+strconv.Itoa(i), kindItems, m, nil, constraint)
			}
		}
	}

	for _, mv := range mapValueSchemas(s) {
		idx.walk(path+mv.suffix, kindMapValue, mv.schema, nil, constraint)
	}

	if branches := unionBranches(s); len(branches) > 0 {
		owner := make(map[string]bool, len(inherited)+len(props))
		for k := range inherited {
			owner[k] = true
		}
		for k := range props {
			owner[k] = true
		}
		for i, b := range branches {
			if m, ok := b.(map[string]any); ok {
				idx.walk(path+"|"+strconv.Itoa(i), kindBranch, m, owner, constraint)
			}
		}
	}
}

type mapValue struct {
	suffix string
	schema map[string]any
}

// mapValueSchemas returns the schema-valued additionalProperties and patternProperties of s.
func mapValueSchemas(s map[string]any) []mapValue {
	var out []mapValue
	if ap, ok := s["additionalProperties"].(map[string]any); ok {
		out = append(out, mapValue{"{}", ap})
	}
	pp := objectMap(s["patternProperties"])
	for _, pattern := range sortedKeys(pp) {
		if m, ok := pp[pattern].(map[string]any); ok {
			out = append(out, mapValue{"{" + pattern + "}", m})
		}
	}
	if len(out) == 1 {
		out[0].suffix = "{}"
	}
	return out
}

// unionBranches returns the oneOf, anyOf and allOf branches of s, in that order.
func unionBranches(s map[string]any) []any {
	var out []any
	for _, kw := range []string{"oneOf", "anyOf", "allOf"} {
		if list, ok := s[kw].([]any); ok {
			out = append(out, list...)
		}
	}
	return out
}

// ---------------------------------------------------------------- type strings

const maxListedProps = 8

// TypeString renders the compact type of a schema node, e.g. `string`, `array<string>`,
// `map<ref:Toolset>`, `oneOf[boolean|array<string>]`, `ref:HooksConfig` or `object{a,b}`.
// It is exported for reuse by tools that print schema paths.
func TypeString(schema map[string]any) string { return renderType(schema, false) }

// renderType renders s. With elide, inline object property lists collapse to `object{…}` and
// constants to `const`, so that comparing two renderings ignores changes reported elsewhere
// (added/removed properties, enum changes).
func renderType(v any, elide bool) string {
	s, ok := v.(map[string]any)
	if !ok {
		if b, isBool := v.(bool); isBool && !b {
			return "never"
		}
		return "any"
	}
	if ref, ok := s["$ref"].(string); ok {
		return "ref:" + refName(ref)
	}
	own := ownType(s, elide)
	comb := unionType(s, elide, own != "")
	switch {
	case own == "" && comb == "":
		return valueType(s, elide)
	case own == "":
		return comb
	case comb == "":
		return own
	case own == "object":
		return comb // a free-form object refined by its branches (e.g. allOf[ref:RAGConfig|object{…}])
	default:
		return own + "&" + comb
	}
}

func ownType(s map[string]any, elide bool) string {
	switch t := s["type"].(type) {
	case string:
		return primitiveType(t, s, elide)
	case []any:
		parts := make([]string, 0, len(t))
		for _, x := range t {
			if name, ok := x.(string); ok {
				parts = append(parts, primitiveType(name, s, elide))
			}
		}
		return strings.Join(parts, "|")
	}
	switch {
	case s["properties"] != nil || s["patternProperties"] != nil || isObject(s["additionalProperties"]):
		return primitiveType("object", s, elide)
	case s["items"] != nil:
		return primitiveType("array", s, elide)
	}
	return ""
}

func primitiveType(t string, s map[string]any, elide bool) string {
	switch t {
	case "array":
		switch items := s["items"].(type) {
		case map[string]any:
			return "array<" + renderType(items, elide) + ">"
		case []any:
			return "array<tuple>"
		default:
			return "array<any>"
		}
	case "object":
		return objectType(s, elide)
	default:
		return t
	}
}

func objectType(s map[string]any, elide bool) string {
	var values []string
	seen := map[string]bool{}
	for _, mv := range mapValueSchemas(s) {
		if t := renderType(mv.schema, elide); !seen[t] {
			seen[t] = true
			values = append(values, t)
		}
	}
	mapType := ""
	if len(values) > 0 {
		mapType = "map<" + strings.Join(values, "|") + ">"
	}
	names := sortedKeys(objectMap(s["properties"]))
	if len(names) == 0 {
		if mapType != "" {
			return mapType
		}
		return "object"
	}
	var obj string
	switch {
	case elide:
		obj = "object{…}"
	case len(names) > maxListedProps:
		obj = fmt.Sprintf("object{%s,…+%d}", strings.Join(names[:maxListedProps], ","), len(names)-maxListedProps)
	default:
		obj = "object{" + strings.Join(names, ",") + "}"
	}
	if mapType != "" {
		obj += "+" + mapType
	}
	return obj
}

// unionType renders the oneOf/anyOf/allOf keywords of s. Branches that carry no type information
// (pure validation constraints such as `required`, `not` or `const`) are left out of allOf, and a
// keyword made only of such branches is left out entirely when s has its own type.
func unionType(s map[string]any, elide, hasOwn bool) string {
	var parts []string
	for _, kw := range []string{"oneOf", "anyOf", "allOf"} {
		list, ok := s[kw].([]any)
		if !ok || len(list) == 0 {
			continue
		}
		typed := anyTyped(list)
		if hasOwn && !typed {
			continue
		}
		var rendered []string
		for _, b := range list {
			if kw == "allOf" && typed && !isTyped(b) {
				continue
			}
			rendered = append(rendered, renderType(b, elide))
		}
		parts = append(parts, kw+"["+strings.Join(rendered, "|")+"]")
	}
	return strings.Join(parts, "&")
}

func anyTyped(list []any) bool {
	for _, b := range list {
		if isTyped(b) {
			return true
		}
	}
	return false
}

// isTyped reports whether a union branch declares a type ($ref, type, or typed nested unions).
func isTyped(b any) bool {
	m, ok := b.(map[string]any)
	if !ok {
		return false
	}
	if m["$ref"] != nil || m["type"] != nil {
		return true
	}
	for _, kw := range []string{"oneOf", "anyOf", "allOf"} {
		if sub, ok := m[kw].([]any); ok && anyTyped(sub) {
			return true
		}
	}
	return false
}

// valueType renders a node without type keywords: a const, an untyped enum or `any`.
func valueType(s map[string]any, elide bool) string {
	if c, ok := s["const"]; ok {
		if elide {
			return "const"
		}
		return "const:" + valueString(c)
	}
	if values, ok := s["enum"].([]any); ok && len(values) > 0 {
		kind := jsonKind(values[0])
		for _, v := range values[1:] {
			if jsonKind(v) != kind {
				return "enum"
			}
		}
		return kind
	}
	return "any"
}

func jsonKind(v any) string {
	switch x := v.(type) {
	case string:
		return "string"
	case bool:
		return "boolean"
	case json.Number:
		if _, err := x.Int64(); err == nil {
			return "integer"
		}
		return "number"
	case nil:
		return "null"
	default:
		return "enum"
	}
}

// ---------------------------------------------------------------- helpers

func newDefinition(name string, s map[string]any) Definition {
	return Definition{Name: name, Type: renderType(s, false), Description: description(s)}
}

// property describes the entry at path; a reference without its own description borrows the
// description of the referenced definition.
func (idx *schemaIndex) property(path string, e *entry) Property {
	desc := description(e.schema)
	if ref, ok := e.schema["$ref"].(string); ok && desc == "" {
		desc = description(idx.defs[refName(ref)])
	}
	return Property{Path: path, Type: renderType(e.schema, false), Description: desc, Constraint: e.constraint}
}

func compareVersions(oldValues, newValues []string) VersionChange {
	added, removed := setDiff(oldValues, newValues)
	return VersionChange{
		Old: oldValues, New: newValues, Added: added, Removed: removed,
		OldLatest: latest(oldValues), NewLatest: latest(newValues),
	}
}

// latest returns the numerically highest value, or the last value when none is numeric.
func latest(values []string) string {
	best, bestN := "", math.Inf(-1)
	for _, v := range values {
		if n, err := strconv.ParseFloat(v, 64); err == nil && n > bestN {
			best, bestN = v, n
		}
	}
	if best == "" && len(values) > 0 {
		return values[len(values)-1]
	}
	return best
}

// enumValues returns the enum values (plus const) of s as strings, and whether s has either.
func enumValues(s map[string]any) ([]string, bool) {
	var out []string
	has := false
	if list, ok := s["enum"].([]any); ok {
		has = true
		for _, v := range list {
			out = append(out, valueString(v))
		}
	}
	if c, ok := s["const"]; ok {
		has = true
		out = append(out, valueString(c))
	}
	return out, has
}

func valueString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

// setDiff returns the values of b missing from a (in b's order) and of a missing from b (in a's
// order).
func setDiff(a, b []string) (added, removed []string) {
	inA := make(map[string]bool, len(a))
	for _, v := range a {
		inA[v] = true
	}
	inB := make(map[string]bool, len(b))
	for _, v := range b {
		inB[v] = true
	}
	for _, v := range b {
		if !inA[v] {
			added = append(added, v)
		}
	}
	for _, v := range a {
		if !inB[v] {
			removed = append(removed, v)
		}
	}
	return added, removed
}

func description(s map[string]any) string {
	d, _ := s["description"].(string)
	return d
}

func refName(ref string) string {
	for _, prefix := range []string{"#/definitions/", "#/$defs/"} {
		if strings.HasPrefix(ref, prefix) {
			return strings.TrimPrefix(ref, prefix)
		}
	}
	return ref
}

func objectMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func isObject(v any) bool {
	_, ok := v.(map[string]any)
	return ok
}

func stringList(v any) []string {
	list, _ := v.([]any)
	out := make([]string, 0, len(list))
	for _, x := range list {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func sortedStrings(s []string) []string {
	sort.Strings(s)
	return s
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedPaths(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return lessPath(out[i], out[j]) })
	return out
}

// lessPath orders paths lexically, except that runs of digits compare numerically.
func lessPath(a, b string) bool {
	for a != "" && b != "" {
		da, db := digitPrefix(a), digitPrefix(b)
		if da > 0 && db > 0 {
			na, _ := strconv.ParseUint(a[:da], 10, 64)
			nb, _ := strconv.ParseUint(b[:db], 10, 64)
			if na != nb {
				return na < nb
			}
			if a[:da] != b[:db] {
				return a[:da] < b[:db]
			}
			a, b = a[da:], b[db:]
			continue
		}
		if a[0] != b[0] {
			return a[0] < b[0]
		}
		a, b = a[1:], b[1:]
	}
	return len(a) < len(b)
}

func digitPrefix(s string) int {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' && i < 18 {
		i++
	}
	return i
}

func pluralize(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}
