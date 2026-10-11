// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

// Package render turns the typed Docker Agent configuration inputs into a deterministic
// docker-agent YAML document.
//
// The input types (config_types.gen.go) are generated from docker-agent's agent-schema.json
// by provider/internal/schemagen. They are also the Pulumi object types of the provider
// schema (they carry `pulumi` struct tags and descriptions), so the renderer and the schema
// can never drift apart. Each field also carries its YAML key (`yaml` tag) and, where
// needed, rendering options (`render` tag):
//
//	render:"extra"        the map is merged into the enclosing mapping (escape hatch)
//	render:"key"          the field is the key of the enclosing keyed list (Agent.Name)
//	render:"keyed=name"   a list rendered as a mapping keyed by the element's `name`
//	render:"concat"       several fields share one YAML key and are combined: lists are
//	                      concatenated, maps merged (a key must not appear twice)
//	render:"value,concat" the same, but the fields form the value of the struct itself
//
// Fields that share a YAML key without `concat` are mutually exclusive variants of a union
// (for example `thinkingBudget` and `thinkingBudgetTokens`).
package render

//go:generate go run ../../internal/schemagen generate

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Render returns the docker-agent YAML for doc.
//
// The output is deterministic: agents keep their list order, typed fields are written in
// a fixed order (the generated field order), and the keys of every map (models, providers,
// env, headers, extra, and any free-form value) are sorted. Empty and nil values are
// omitted. Keys are snake_case, as docker-agent expects.
func Render(doc Document) (string, error) {
	if len(doc.Agents) == 0 {
		return "", &pathError{path: "agents", err: errors.New("at least one agent is required")}
	}
	seen := map[string]bool{}
	for i, a := range doc.Agents {
		if a.Name == "" {
			return "", &pathError{path: fmt.Sprintf("agents[%d]", i), err: errors.New("name must not be empty")}
		}
		if seen[a.Name] {
			return "", &pathError{path: fmt.Sprintf("agents[%d]", i), err: fmt.Errorf("duplicate agent name %q", a.Name)}
		}
		seen[a.Name] = true
	}
	if err := checkTopLevelExtra(doc.Extra); err != nil {
		return "", err
	}
	if doc.Version == nil || *doc.Version == "" {
		v := DefaultConfigVersion
		doc.Version = &v
	}

	root, err := renderStruct(reflect.ValueOf(doc))
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{root}}); err != nil {
		return "", fmt.Errorf("encoding YAML: %w", err)
	}
	if err := enc.Close(); err != nil {
		return "", fmt.Errorf("encoding YAML: %w", err)
	}
	return buf.String(), nil
}

// SHA256 returns the hex-encoded sha256 of s.
func SHA256(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// DefaultAgent returns the agent docker-agent starts by default: `root` when an agent has
// that name, otherwise the first agent.
func DefaultAgent(names []string) string {
	if slices.Contains(names, "root") {
		return "root"
	}
	if len(names) > 0 {
		return names[0]
	}
	return ""
}

// fieldInfo is the rendering metadata of a struct field, read from its tags.
type fieldInfo struct {
	index    int
	pulumi   string
	yamlKey  string
	required bool
	extra    bool
	concat   bool
	value    bool   // the field renders into the value of the struct itself
	keyedBy  string // pulumi name of the key field of a keyed list
}

func fieldsOf(t reflect.Type) []fieldInfo {
	out := make([]fieldInfo, 0, t.NumField())
	for i := range t.NumField() {
		sf := t.Field(i)
		ptag, ok := sf.Tag.Lookup("pulumi")
		if !ok || !sf.IsExported() {
			continue
		}
		name, opts, _ := strings.Cut(ptag, ",")
		f := fieldInfo{index: i, pulumi: name, yamlKey: sf.Tag.Get("yaml"), required: !strings.Contains(opts, "optional")}
		for _, o := range strings.Split(sf.Tag.Get("render"), ",") {
			switch {
			case o == "extra":
				f.extra = true
			case o == "concat":
				f.concat = true
			case o == "value":
				f.value = true
			case strings.HasPrefix(o, "keyed="):
				f.keyedBy = strings.TrimPrefix(o, "keyed=")
			}
		}
		out = append(out, f)
	}
	return out
}

// renderStruct renders a generated config type: a mapping with one key per field, or, for
// union types, the combined value of its fields.
func renderStruct(v reflect.Value) (*yaml.Node, error) {
	fields := fieldsOf(v.Type())
	if len(fields) > 0 && fields[0].value {
		return renderUnion(v, fields)
	}

	m := newMap()
	var extra map[string]any
	done := map[string]bool{}
	for _, f := range fields {
		if f.extra {
			extra, _ = v.Field(f.index).Interface().(map[string]any)
			continue
		}
		if f.yamlKey == "-" || done[f.yamlKey] {
			continue
		}
		done[f.yamlKey] = true
		var group []fieldInfo
		for _, g := range fields {
			if g.yamlKey == f.yamlKey && !g.extra {
				group = append(group, g)
			}
		}
		n, err := renderGroup(v, group)
		if err != nil {
			return nil, err
		}
		if n != nil {
			m.set(f.yamlKey, n)
		}
	}
	if err := m.merge(extra); err != nil {
		return nil, at("extra", err)
	}
	return m.node, nil
}

// pathError is a rendering error with the path of the input it is about, for example
// `agents["root"].toolsets[0].type`.
type pathError struct {
	path string
	err  error
}

func (e *pathError) Error() string { return e.path + ": " + e.err.Error() }
func (e *pathError) Unwrap() error { return e.err }

// at prefixes err's path with seg: a property name, or an `[index]`/`["key"]` selector.
func at(seg string, err error) error {
	if err == nil {
		return nil
	}
	var pe *pathError
	if errors.As(err, &pe) && pe == err {
		sep := "."
		if strings.HasPrefix(pe.path, "[") {
			sep = ""
		}
		return &pathError{path: seg + sep + pe.path, err: pe.err}
	}
	return &pathError{path: seg, err: err}
}

// ErrorProperty returns the top-level input a rendering error is about (for example `models`
// for `models["m"].thinkingBudget: ...`), or "" when the error has no path.
func ErrorProperty(err error) string {
	var pe *pathError
	if !errors.As(err, &pe) {
		return ""
	}
	if i := strings.IndexAny(pe.path, ".["); i >= 0 {
		return pe.path[:i]
	}
	return pe.path
}

// checkTopLevelExtra rejects top-level `extra` keys that are typed inputs. Such a key would
// replace the typed section in the YAML (for example `extra.agents` would replace the typed
// agents, while `agentNames` and `defaultAgent` still report the typed ones).
func checkTopLevelExtra(extra map[string]any) error {
	typed := map[string]string{}
	for _, f := range fieldsOf(reflect.TypeFor[Document]()) {
		if !f.extra && f.yamlKey != "-" {
			typed[f.yamlKey] = f.pulumi
		}
	}
	for _, k := range sortedKeys(extra) {
		if name, ok := typed[k]; ok && extra[k] != nil {
			return &pathError{path: "extra", err: fmt.Errorf("%q is a typed input; set `%s` instead of extra.%s", k, name, k)}
		}
	}
	return nil
}

func atIndex(i int, err error) error  { return at(fmt.Sprintf("[%d]", i), err) }
func atKey(k string, err error) error { return at(fmt.Sprintf("[%q]", k), err) }

// renderGroup renders the fields that share one YAML key. It returns nil when the key is
// to be omitted.
func renderGroup(v reflect.Value, group []fieldInfo) (*yaml.Node, error) {
	if len(group) == 1 {
		f := group[0]
		n, err := renderField(v.Field(f.index), f)
		return n, at(f.pulumi, err)
	}
	var nodes []*yaml.Node
	var set []string
	for _, f := range group {
		n, err := renderField(v.Field(f.index), f)
		if err != nil {
			return nil, at(f.pulumi, err)
		}
		if n != nil {
			nodes = append(nodes, n)
			set = append(set, "`"+f.pulumi+"`")
		}
	}
	switch {
	case len(nodes) == 0:
		return nil, nil
	case len(nodes) == 1:
		return nodes[0], nil
	case group[0].concat:
		return combine(nodes, set)
	default:
		return nil, fmt.Errorf("%s are mutually exclusive (they set the same YAML key %q)",
			strings.Join(set, " and "), group[0].yamlKey)
	}
}

// renderUnion renders a union type: its fields are combined into one list or mapping.
func renderUnion(v reflect.Value, fields []fieldInfo) (*yaml.Node, error) {
	var nodes []*yaml.Node
	var set []string
	for _, f := range fields {
		n, err := renderField(v.Field(f.index), f)
		if err != nil {
			return nil, at(f.pulumi, err)
		}
		if n != nil {
			nodes = append(nodes, n)
			set = append(set, "`"+f.pulumi+"`")
		}
	}
	if len(nodes) == 0 {
		// An empty value of the same kind as the first field.
		if v.Field(fields[0].index).Kind() == reflect.Map {
			return newMap().node, nil
		}
		return &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}, nil
	}
	return combine(nodes, set)
}

// combine concatenates sequences or merges mappings. A mapping key set by two fields is an
// error.
func combine(nodes []*yaml.Node, fields []string) (*yaml.Node, error) {
	switch nodes[0].Kind {
	case yaml.SequenceNode:
		seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for _, n := range nodes {
			seq.Content = append(seq.Content, n.Content...)
		}
		return seq, nil
	case yaml.MappingNode:
		type entry struct{ k, v *yaml.Node }
		var entries []entry
		seen := map[string]bool{}
		for _, n := range nodes {
			for i := 0; i+1 < len(n.Content); i += 2 {
				k := n.Content[i].Value
				if seen[k] {
					return nil, fmt.Errorf("%q is set in more than one of %s", k, strings.Join(fields, ", "))
				}
				seen[k] = true
				entries = append(entries, entry{n.Content[i], n.Content[i+1]})
			}
		}
		sort.SliceStable(entries, func(i, j int) bool { return entries[i].k.Value < entries[j].k.Value })
		m := newMap()
		for _, e := range entries {
			m.node.Content = append(m.node.Content, e.k, e.v)
		}
		return m.node, nil
	}
	return nil, fmt.Errorf("cannot combine %s", strings.Join(fields, " and "))
}

// renderField renders one field value. It returns nil for values that are omitted: nil
// pointers, nil interfaces and empty optional collections.
func renderField(v reflect.Value, f fieldInfo) (*yaml.Node, error) {
	if f.keyedBy != "" {
		return renderKeyed(v, f.keyedBy)
	}
	switch v.Kind() {
	case reflect.String:
		if f.required && v.String() == "" {
			return nil, fmt.Errorf("must not be empty")
		}
	case reflect.Slice, reflect.Map:
		if v.Len() == 0 && !f.required {
			return nil, nil
		}
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return nil, nil
		}
	}
	// An object that is set but empty renders as `{}`: for docker-agent an explicit empty
	// object can differ from an absent one (for example `cost: {}` means "free").
	return renderValue(v)
}

// renderKeyed renders a list of structs as a mapping keyed by the field named key. The
// list order is kept.
func renderKeyed(v reflect.Value, key string) (*yaml.Node, error) {
	m := newMap()
	for i := range v.Len() {
		e := v.Index(i)
		var name string
		for _, f := range fieldsOf(e.Type()) {
			if f.pulumi == key {
				name = e.Field(f.index).String()
			}
		}
		n, err := renderStruct(e)
		if err != nil {
			return nil, atKey(name, err)
		}
		m.set(name, n)
	}
	return m.node, nil
}

// renderValue renders a value of a generated field type.
func renderValue(v reflect.Value) (*yaml.Node, error) {
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return anyNode(nil)
		}
		return renderValue(v.Elem())
	case reflect.Interface:
		if v.IsNil() {
			return anyNode(nil)
		}
		return anyNode(v.Interface())
	case reflect.Struct:
		return renderStruct(v)
	case reflect.String:
		return str(v.String()), nil
	case reflect.Bool:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(v.Bool())}, nil
	case reflect.Int, reflect.Int64, reflect.Int32:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.FormatInt(v.Int(), 10)}, nil
	case reflect.Float64, reflect.Float32:
		return floatNode(v.Float()), nil
	case reflect.Slice:
		seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for i := range v.Len() {
			n, err := renderValue(v.Index(i))
			if err != nil {
				return nil, atIndex(i, err)
			}
			seq.Content = append(seq.Content, n)
		}
		return seq, nil
	case reflect.Map:
		keys := make([]string, 0, v.Len())
		for _, k := range v.MapKeys() {
			keys = append(keys, k.String())
		}
		sort.Strings(keys)
		m := newMap()
		for _, k := range keys {
			n, err := renderValue(v.MapIndex(reflect.ValueOf(k)))
			if err != nil {
				return nil, atKey(k, err)
			}
			m.set(k, n)
		}
		return m.node, nil
	}
	return nil, fmt.Errorf("unsupported value of type %s", v.Type())
}

// orderedMap is a YAML mapping node that keeps insertion order.
type orderedMap struct {
	node *yaml.Node
}

func newMap() *orderedMap {
	return &orderedMap{node: &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}}
}

// set adds key or, if it is already present, replaces its value in place.
func (m *orderedMap) set(key string, value *yaml.Node) {
	for i := 0; i+1 < len(m.node.Content); i += 2 {
		if m.node.Content[i].Value == key {
			m.node.Content[i+1] = value
			return
		}
	}
	m.node.Content = append(m.node.Content, str(key), value)
}

// merge applies extra on top of m: existing keys are replaced in place, new keys are
// appended in sorted order. A nil value removes nothing and is skipped.
func (m *orderedMap) merge(extra map[string]any) error {
	for _, k := range sortedKeys(extra) {
		v := extra[k]
		if k == "" {
			return fmt.Errorf("keys must not be empty")
		}
		if v == nil {
			continue
		}
		n, err := anyNode(v)
		if err != nil {
			return fmt.Errorf("%s: %w", k, err)
		}
		m.set(k, n)
	}
	return nil
}

func str(s string) *yaml.Node {
	n := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s}
	switch {
	case !strings.Contains(s, "\n"):
	case strings.TrimSpace(s) == "":
		// A literal block with no content ("|2+" and an empty body), which is what yaml.v3
		// emits for "\n", is rejected by docker-agent's YAML parser; quote it instead.
		n.Style = yaml.DoubleQuotedStyle
	default:
		n.Style = yaml.LiteralStyle
	}
	return n
}

func floatNode(f float64) *yaml.Node {
	var s string
	switch {
	case math.IsInf(f, 1):
		s = ".inf"
	case math.IsInf(f, -1):
		s = "-.inf"
	case math.IsNaN(f):
		s = ".nan"
	default:
		s = strconv.FormatFloat(f, 'f', -1, 64)
		if !strings.Contains(s, ".") {
			// Keep the value a float when it is read back (1 -> 1.0).
			s += ".0"
		}
	}
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: s}
}

// anyNode converts a free-form value (as decoded from Pulumi: maps, slices, strings,
// float64 numbers and bools) into a YAML node with sorted map keys. Integral numbers are
// written as integers because Pulumi represents every number as a float64.
func anyNode(v any) (*yaml.Node, error) {
	switch x := v.(type) {
	case nil:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}, nil
	case string:
		return str(x), nil
	case bool:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(x)}, nil
	case float64:
		if x == math.Trunc(x) && math.Abs(x) < 1<<53 {
			return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.FormatInt(int64(x), 10)}, nil
		}
		return floatNode(x), nil
	case float32:
		return anyNode(float64(x))
	case int:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.Itoa(x)}, nil
	case int64:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.FormatInt(x, 10)}, nil
	case int32:
		return anyNode(int64(x))
	case map[string]any:
		m := newMap()
		for _, k := range sortedKeys(x) {
			n, err := anyNode(x[k])
			if err != nil {
				return nil, fmt.Errorf("%s: %w", k, err)
			}
			m.set(k, n)
		}
		return m.node, nil
	case map[string]string:
		m := newMap()
		for _, k := range sortedKeys(x) {
			m.set(k, str(x[k]))
		}
		return m.node, nil
	case []any:
		seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for i, e := range x {
			n, err := anyNode(e)
			if err != nil {
				return nil, fmt.Errorf("[%d]: %w", i, err)
			}
			seq.Content = append(seq.Content, n)
		}
		return seq, nil
	case []string:
		seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for _, e := range x {
			seq.Content = append(seq.Content, str(e))
		}
		return seq, nil
	}

	// Fall back to reflection for other map and slice shapes.
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Map:
		if rv.Type().Key().Kind() != reflect.String {
			return nil, fmt.Errorf("unsupported map key type %s", rv.Type().Key())
		}
		keys := make([]string, 0, rv.Len())
		for _, k := range rv.MapKeys() {
			keys = append(keys, k.String())
		}
		sort.Strings(keys)
		m := newMap()
		for _, k := range keys {
			n, err := anyNode(rv.MapIndex(reflect.ValueOf(k).Convert(rv.Type().Key())).Interface())
			if err != nil {
				return nil, fmt.Errorf("%s: %w", k, err)
			}
			m.set(k, n)
		}
		return m.node, nil
	case reflect.Slice, reflect.Array:
		seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for i := range rv.Len() {
			n, err := anyNode(rv.Index(i).Interface())
			if err != nil {
				return nil, fmt.Errorf("[%d]: %w", i, err)
			}
			seq.Content = append(seq.Content, n)
		}
		return seq, nil
	case reflect.Pointer:
		if rv.IsNil() {
			return anyNode(nil)
		}
		return anyNode(rv.Elem().Interface())
	}
	return nil, fmt.Errorf("unsupported value of type %T", v)
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
