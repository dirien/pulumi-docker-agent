// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// goType is a generated Go struct, which is also a Pulumi object type.
type goType struct {
	name   string
	path   string // schema path it was generated from
	desc   string
	fields []*goField
	// union types render their fields into the value itself (a list or a map) instead of
	// into a mapping with one key per field.
	union bool
}

type goField struct {
	goName   string
	pulumi   string
	yamlKey  string // "-" for fields that are not YAML keys
	render   string // render tag options
	goType   string
	required bool
	desc     string
	path     string // schema path
	schema   string // compact schema type, for the coverage matrix
	setDef   string // Go expression passed to Annotator.SetDefault
}

type generator struct {
	root  *jnode
	defs  *jnode
	types map[string]*goType
	// defTypes caches the Go type name of each definition.
	defTypes map[string]string
	// enums are the generated Pulumi enum types by name, enumAt their names by schema path.
	enums  map[string]*goEnum
	enumAt map[string]string
	// notes are remarks printed by `schemagen generate` that need no action to continue.
	notes []string
	errs  []string
}

func newGenerator(schema *jnode) *generator {
	return &generator{
		root:     schema,
		defs:     schema.get("definitions"),
		types:    map[string]*goType{},
		defTypes: map[string]string{},
		enums:    map[string]*goEnum{},
		enumAt:   map[string]string{},
	}
}

func (g *generator) fail(format string, args ...any) {
	g.errs = append(g.errs, fmt.Sprintf(format, args...))
}

// run builds the Go model for the whole schema, starting at the root document.
func (g *generator) run() error {
	doc := &goType{name: "Document", path: "Config", desc: "The complete, typed docker-agent configuration. " +
		"It is rendered into the YAML document by Render."}
	g.types[doc.name] = doc
	g.buildFields(doc, "Config", g.root)
	// Generate every definition, also those only reachable through aliases, so a new
	// definition never goes unnoticed.
	for _, def := range g.defs.keys {
		g.defType(def)
	}
	g.addExtra()
	if len(g.errs) > 0 {
		slices.Sort(g.errs)
		return fmt.Errorf("the schema has constructs without a generator rule (see overrides.go):\n  %s",
			strings.Join(slices.Compact(g.errs), "\n  "))
	}
	return nil
}

func refName(n *jnode) string {
	ref := n.str("$ref")
	return strings.TrimPrefix(ref, "#/definitions/")
}

// defType returns the Go type name of definition def, generating it on first use.
func (g *generator) defType(def string) string {
	if target, ok := aliases[def]; ok {
		return g.defType(target)
	}
	if name, ok := g.defTypes[def]; ok {
		return name
	}
	node := g.defs.get(def)
	if node == nil {
		g.fail("%s: unknown definition", def)
		return goAny
	}
	name, ok := typeNames[def]
	if !ok {
		g.fail("%s: definition has no Pulumi type name in typeNames", def)
		return goAny
	}
	g.defTypes[def] = name
	t := &goType{name: name, path: def, desc: node.str("description")}
	g.types[name] = t

	switch {
	case unionTypes[def] != nil:
		t.union = true
		for _, spec := range unionTypes[def] {
			t.fields = append(t.fields, &goField{
				goName: pascal(spec.pulumi), pulumi: spec.pulumi, yamlKey: "-", render: "value,concat",
				goType: spec.goType, desc: spec.desc, path: def, schema: schemaType(node),
			})
		}
	case mergedAllOf[def]:
		for i, branch := range node.list("allOf") {
			if r := refName(branch); r != "" {
				g.buildFields(t, r, g.defs.get(r))
				continue
			}
			g.buildFields(t, fmt.Sprintf("%s|%d", def, i), branch)
		}
	case node.get("properties") != nil:
		g.buildFields(t, def, node)
	default:
		g.fail("%s: definition is neither an object, a union nor an alias", def)
	}
	return name
}

// buildFields appends the fields for the properties of object node (at schema path) to t.
func (g *generator) buildFields(t *goType, path string, node *jnode) {
	props := node.get("properties")
	if props == nil {
		g.fail("%s: object without properties", path)
		return
	}
	required := map[string]bool{}
	for _, r := range node.strings("required") {
		required[r] = true
	}
	keys := slices.Clone(props.keys)
	if order, ok := fieldOrder[path]; ok {
		var head []string
		for _, k := range order {
			if props.get(k) == nil {
				continue // removed upstream; the schema diff reports it
			}
			head = append(head, k)
		}
		keys = append(head, slices.DeleteFunc(keys, func(k string) bool { return slices.Contains(head, k) })...)
	}
	if t.name == "Agent" {
		t.fields = append(t.fields, &goField{goName: "Name", pulumi: "name", yamlKey: "-", render: "key",
			goType: "string", required: true, desc: agentNameDoc, path: "AgentConfig.name", schema: "map key"})
	}
	for _, lo := range loaderOnly[path] {
		keys = append(keys, lo.key)
	}
	for _, k := range keys {
		ppath := path + "." + k
		prop := props.get(k)
		if prop == nil {
			prop = g.loaderOnlyProp(path, k)
		}
		req := required[k] || requiredOverrides[ppath]
		if path == "Config" && k == "agents" {
			t.fields = append(t.fields, &goField{goName: "Agents", pulumi: "agents", yamlKey: "agents",
				render: "keyed=name", goType: "[]" + g.defType(refName(prop.get("additionalProperties"))),
				required: true, desc: g.describe(ppath, prop), path: ppath, schema: schemaType(prop)})
			continue
		}
		if specs, ok := unionFields[ppath]; ok {
			for _, spec := range specs {
				f := &goField{goName: pascal(spec.pulumi), pulumi: spec.pulumi, yamlKey: k, goType: spec.goType,
					desc: spec.desc, path: ppath, schema: schemaType(prop)}
				if spec.concat {
					f.render = "concat"
				}
				if f.desc == "" {
					f.desc = g.describe(ppath, prop)
				}
				t.fields = append(t.fields, f)
			}
			continue
		}
		goType := g.typeExpr(t.name, ppath, prop, req) // before describe, which depends on the enums
		f := &goField{goName: pascal(k), pulumi: camel(k), yamlKey: k, required: req,
			goType: goType, desc: g.describe(ppath, prop), path: ppath, schema: schemaType(prop)}
		if path == "Config" && k == "version" {
			f.setDef = "DefaultConfigVersion"
		}
		t.fields = append(t.fields, f)
	}
}

// addExtra appends the `extra` escape hatch to every type that renders as a mapping.
func (g *generator) addExtra() {
	for _, t := range g.types {
		if !t.union {
			t.fields = append(t.fields, &goField{goName: "Extra", pulumi: "extra", yamlKey: "-", render: "extra",
				goType: "map[string]any", desc: extraDoc, path: t.path, schema: "escape hatch"})
		}
	}
}

// typeExpr returns the Go type of the value at path. owner is the Go type the value belongs
// to; it names enums that have no name in enumTypes.
func (g *generator) typeExpr(owner, path string, n *jnode, required bool) string {
	ptr := func(s string) string {
		if required {
			return s
		}
		return "*" + s
	}
	if ff, ok := freeForm[path]; ok {
		return ff
	}
	if r := refName(n); r != "" {
		name := g.defType(r)
		if _, union := unionTypes[resolveAlias(r)]; union {
			return name // union types are only used as map values
		}
		return ptr(name)
	}
	if branches := append(n.list("oneOf"), n.list("anyOf")...); len(branches) > 0 && n.get("type") == nil {
		// A union of branches that all have the same scalar type is that type.
		var kinds []string
		for _, b := range branches {
			if b.get("$ref") != nil || len(b.types()) != 1 {
				kinds = nil
				break
			}
			kinds = append(kinds, b.types()[0])
		}
		if len(kinds) > 0 && len(slices.Compact(slices.Clone(kinds))) == 1 && kinds[0] != "object" && kinds[0] != "array" {
			return ptr(scalarGo(kinds[0]))
		}
		g.fail("%s: union %s needs a rule in unionFields", path, schemaType(n))
		return goAny
	}
	types := n.types()
	switch {
	case slices.Contains(types, "array"):
		items := n.get("items")
		if items == nil {
			g.fail("%s: array without items", path)
			return "[]any"
		}
		return "[]" + g.typeExpr(owner, path+"[]", items, true)
	case slices.Contains(types, "object"):
		if n.get("properties") != nil {
			name, ok := typeNames[path]
			if !ok {
				g.fail("%s: inline object needs a Pulumi type name in typeNames", path)
				return goAny
			}
			if _, done := g.types[name]; !done {
				t := &goType{name: name, path: path, desc: n.str("description")}
				g.types[name] = t
				g.buildFields(t, path, n)
			}
			return ptr(name)
		}
		if ap := n.get("additionalProperties"); ap != nil && ap.kind == jObj {
			return "map[string]" + g.typeExpr(owner, path+"{}", ap, true)
		}
		if pp := n.get("patternProperties"); pp != nil && len(pp.keys) == 1 {
			return "map[string]" + g.typeExpr(owner, path+"{}", pp.get(pp.keys[0]), true)
		}
		g.fail("%s: free-form object needs an entry in freeForm", path)
		return goAnyMap
	case len(types) == 1:
		if e := g.enumType(owner, path, n); e != "" {
			return ptr(e)
		}
		if s := scalarGo(types[0]); s != "" {
			return ptr(s)
		}
	}
	g.fail("%s: unsupported schema type %s", path, schemaType(n))
	return goAny
}

func resolveAlias(def string) string {
	for {
		t, ok := aliases[def]
		if !ok {
			return def
		}
		def = t
	}
}

func scalarGo(t string) string {
	switch t {
	case "string":
		return "string"
	case "boolean":
		return "bool"
	case "integer":
		return "int"
	case "number":
		return "float64"
	}
	return ""
}

// describe builds the description of the value at path.
func (g *generator) describe(path string, n *jnode) string {
	desc, ok := descriptions[path]
	if !ok {
		desc = n.str("description")
	}
	if desc == "" {
		if r := refName(n); r != "" {
			def := resolveAlias(r)
			if desc = g.defs.get(def).str("description"); desc == "" {
				desc = fallbackDescriptions[def]
			}
		}
	}
	if desc == "" {
		desc = fallbackDescriptions[path]
	}
	if desc == "" {
		desc = "The `" + path[strings.LastIndex(path, ".")+1:] + "` setting (no description in agent-schema.json)."
	}
	desc = strings.TrimSpace(desc)
	switch enum := enumValues(n); {
	case g.isEnum(path):
		// The Pulumi enum type lists the values, with their descriptions.
		desc = strings.TrimSpace(sentence(desc))
	case len(enum) > 0 && !strings.Contains(desc, "Allowed values"):
		desc = sentence(desc) + "Allowed values: " + backtickList(enum) + "."
	}
	if def := n.get("default"); def != nil && def.kind != jObj && def.kind != jArr &&
		!strings.Contains(strings.ToLower(desc), "default") {
		desc = sentence(desc) + "Defaults to `" + def.scalarString() + "`."
	}
	if note, ok := descriptionNotes[path]; ok {
		desc = sentence(desc) + note
	}
	return desc
}

func enumValues(n *jnode) []string {
	for _, c := range []*jnode{n, n.get("items"), n.get("additionalProperties")} {
		if c != nil && c.get("enum") != nil {
			return c.strings("enum")
		}
	}
	return nil
}

func sentence(s string) string {
	if s == "" {
		return ""
	}
	if !strings.HasSuffix(s, ".") && !strings.HasSuffix(s, ")") && !strings.HasSuffix(s, "`") {
		s += "."
	}
	return s + " "
}

func backtickList(vals []string) string {
	out := make([]string, len(vals))
	for i, v := range vals {
		out[i] = "`" + v + "`"
	}
	return strings.Join(out, ", ")
}

// schemaType renders a compact type description of a schema node.
func schemaType(n *jnode) string {
	if n == nil {
		return "?"
	}
	if r := refName(n); r != "" {
		return "ref:" + r
	}
	for _, k := range []string{"oneOf", "anyOf"} {
		if bs := n.list(k); len(bs) > 0 && n.get("type") == nil {
			parts := make([]string, len(bs))
			for i, b := range bs {
				parts[i] = schemaType(b)
			}
			return k + "[" + strings.Join(parts, "|") + "]"
		}
	}
	types := n.types()
	switch {
	case slices.Contains(types, "array"):
		return "array<" + schemaType(n.get("items")) + ">"
	case slices.Contains(types, "object"):
		if p := n.get("properties"); p != nil {
			return "object{" + strings.Join(p.keys, ",") + "}"
		}
		if ap := n.get("additionalProperties"); ap != nil && ap.kind == jObj {
			return "map<" + schemaType(ap) + ">"
		}
		if pp := n.get("patternProperties"); pp != nil && len(pp.keys) == 1 {
			return "map<" + schemaType(pp.get(pp.keys[0])) + ">"
		}
		if len(types) > 1 {
			return strings.Join(types, "|")
		}
		return "object"
	case len(types) > 0:
		s := strings.Join(types, "|")
		if e := n.strings("enum"); len(e) > 0 {
			s += " enum"
		}
		return s
	}
	return goAny
}

// latestConfigVersion returns the highest numeric value of the root `version` enum.
func latestConfigVersion(schema *jnode) (string, error) {
	best := -1
	for _, v := range schema.get("properties").get("version").strings("enum") {
		n, err := strconv.Atoi(v)
		if err != nil {
			continue
		}
		best = max(best, n)
	}
	if best < 0 {
		return "", fmt.Errorf("the schema's root version property has no numeric enum")
	}
	return strconv.Itoa(best), nil
}

// sortedTypes returns the generated types: Document first, the others by name.
func (g *generator) sortedTypes() []*goType {
	names := slices.Sorted(maps.Keys(g.types))
	out := []*goType{g.types["Document"]}
	for _, n := range names {
		if n != "Document" {
			out = append(out, g.types[n])
		}
	}
	return out
}

var initialisms = map[string]string{
	"api": "API", "ast": "AST", "html": "HTML", "http": "HTTP", "id": "ID", "ip": "IP", "ips": "IPs",
	"json": "JSON", "llm": "LLM", "oauth": "OAuth", "pdf": "PDF", "uri": "URI", "url": "URL", "vcs": "VCS",
}

// pascal turns a snake_case or camelCase key into an exported Go identifier.
func pascal(key string) string {
	var b strings.Builder
	for _, part := range splitWords(key) {
		if up, ok := initialisms[strings.ToLower(part)]; ok {
			b.WriteString(up)
			continue
		}
		r := []rune(part)
		r[0] = unicode.ToUpper(r[0])
		b.WriteString(string(r))
	}
	return b.String()
}

// camel turns a snake_case key into the camelCase Pulumi property name. Keys that are
// already camelCase are kept.
func camel(key string) string {
	parts := strings.Split(key, "_")
	var b strings.Builder
	for i, p := range parts {
		if p == "" {
			continue
		}
		if i == 0 {
			b.WriteString(p)
			continue
		}
		r := []rune(p)
		r[0] = unicode.ToUpper(r[0])
		b.WriteString(string(r))
	}
	return b.String()
}

// splitWords splits snake_case and camelCase identifiers into words.
func splitWords(key string) []string {
	var words []string
	for _, part := range strings.Split(key, "_") {
		if part == "" {
			continue
		}
		start := 0
		r := []rune(part)
		for i := 1; i < len(r); i++ {
			if unicode.IsUpper(r[i]) && !unicode.IsUpper(r[i-1]) {
				words = append(words, string(r[start:i]))
				start = i
			}
		}
		words = append(words, string(r[start:]))
	}
	return words
}

// loaderOnlyProp returns the schema of a loader-only property (see loaderOnly).
func (g *generator) loaderOnlyProp(path, key string) *jnode {
	for _, lo := range loaderOnly[path] {
		if lo.key != key {
			continue
		}
		var n *jnode
		if lo.schema != "" {
			var err error
			if n, err = parseOrdered([]byte(lo.schema)); err != nil {
				g.fail("%s.%s: %v", path, key, err)
				return &jnode{kind: jObj, vals: map[string]*jnode{}}
			}
		} else {
			def, prop, _ := strings.Cut(lo.from, ".")
			n = g.defs.get(def).get("properties").get(prop)
			if n == nil {
				g.fail("%s.%s: loaderOnly source %s does not exist", path, key, lo.from)
				return &jnode{kind: jObj, vals: map[string]*jnode{}}
			}
		}
		cp := *n
		cp.vals = maps.Clone(n.vals)
		desc := strings.TrimSpace(n.str("description"))
		cp.vals["description"] = &jnode{kind: jStr, s: sentence(desc) + loaderOnlyNote}
		if !slices.Contains(cp.keys, "description") {
			cp.keys = append(cp.keys, "description")
		}
		return &cp
	}
	g.fail("%s.%s: unknown property", path, key)
	return &jnode{kind: jObj, vals: map[string]*jnode{}}
}
