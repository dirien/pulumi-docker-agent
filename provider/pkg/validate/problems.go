// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package validate

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/docker/docker-agent/pkg/config"
	"github.com/docker/docker-agent/pkg/config/latest"
	goyaml "github.com/goccy/go-yaml"
	"gopkg.in/yaml.v3"
)

// docker-agent's loader stops at the first problem of a configuration, and only some of its
// messages say where the problem is (`agents.root.safety: ...`); others name an agent
// (`agent 'root' references non-existent sub-agent 'x'`) or nothing at all. problems turns
// the first error into a located Problem, removes the value it is about from the document and
// loads the rest again, so that one validation reports every independent problem in
// docker-agent's own words:
//
//   - a message that starts with a location (`models.claude: ...`) is located there, and
//     at the key its text starts with (`compaction_threshold must be ...`);
//   - a message about an agent's reference (sub-agent, handoff, model, ...) is located at
//     that agent's key;
//   - other messages are located by unmarshalling the document's values one by one into
//     docker-agent's own types until the smallest value that still fails with the same
//     message is found (for example the toolset of an agent that sets `ref` on a `shell`
//     toolset).
//
// A problem is only removed when that cannot change what the rest of the document means:
// never a whole section, an agent or another named definition (removing one would turn its
// references into new problems). Then the search stops, as it does after maxRounds problems.

// maxRounds bounds the number of loads of one validation.
const maxRounds = 16

// agentsKey is the configuration's `agents` section.
const agentsKey = "agents"

// Problem is one reason a configuration is invalid.
type Problem struct {
	// Message is docker-agent's message, without ANSI escape codes, YAML excerpts and the
	// "parsing config file" header.
	Message string
	// Path is where the problem is: the mapping keys and, for sequences, item indexes in
	// decimal of the YAML value it is about, for example [agentsKey, "root", "toolsets", "2",
	// "ref"]. It is nil when the problem cannot be located.
	Path []string

	line int // where Path starts in the document, for ordering
}

// message is the text of a loader error.
func message(err error) string {
	return strings.TrimPrefix(Clean(err.Error()), "parsing config file\n")
}

// problems locates the loader error err of data and looks for further independent problems.
func problems(ctx context.Context, name string, data []byte, err error) []Problem {
	t, ok := parseTree(data)
	if !ok {
		return []Problem{{Message: message(err)}}
	}
	// Loads of the reduced document must not report its warnings.
	quiet, _ := collectWarnings(ctx)
	var out []Problem
	// elided are the removed values and emptied the lists and mappings a removal left
	// empty: a problem inside the former or at the latter is a consequence of the removal.
	var elided, emptied [][]string
	consequence := func(p Problem) bool {
		return p.Path != nil && (slices.ContainsFunc(elided, func(e []string) bool { return hasPrefix(p.Path, e) }) ||
			slices.ContainsFunc(emptied, func(e []string) bool { return hasPrefix(e, p.Path) }))
	}
	for round := 1; ; round++ {
		msg := message(err)
		loc := t.locate(msg)
		// The locations in the message are in the reduced document: give them the indexes of
		// the validated one, like Path.
		p := Problem{Message: t.relocateMessage(msg), Path: t.original(loc.path), line: t.line(loc.path)}
		if consequence(p) || slices.ContainsFunc(out, func(q Problem) bool {
			return q.Message == p.Message && slices.Equal(q.Path, p.Path)
		}) {
			break
		}
		out = append(out, p)
		if round == maxRounds || len(loc.elide) == 0 {
			break
		}
		for _, e := range loc.elide {
			elided = append(elided, t.original(e))
		}
		empty, ok := t.remove(loc.elide)
		if !ok {
			break
		}
		for _, e := range empty {
			emptied = append(emptied, t.original(e))
		}
		cfg, lerr := config.Load(quiet, config.NewBytesSource(name, t.bytes()))
		if lerr == nil {
			for _, rp := range requirementProblems(cfg, t) {
				if !consequence(rp) {
					out = append(out, rp)
				}
			}
			break
		}
		err = lerr
	}
	slices.SortStableFunc(out, func(a, b Problem) int {
		switch {
		case a.Path == nil || b.Path == nil:
			return boolInt(a.Path == nil) - boolInt(b.Path == nil)
		default:
			return a.line - b.line
		}
	})
	return out
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func hasPrefix(path, prefix []string) bool {
	return len(path) >= len(prefix) && slices.Equal(path[:len(prefix)], prefix)
}

// requirementProblems reports the model providers, toolset types and features of a loaded
// configuration that docker-agent does not support, one per location.
func requirementProblems(cfg *latest.Config, t *tree) []Problem {
	req := config.Requires(cfg)
	err := req.Check(hasProvider, hasToolset, allFeatures)
	if err == nil {
		return nil
	}
	var ue *config.UnsupportedError
	if !errors.As(err, &ue) {
		return []Problem{{Message: Clean(err.Error())}}
	}
	var out []Problem
	// add reports a problem at each path loc resolves to. The message names the location in
	// the validated document (msg formats it), not in the reduced one docker-agent loaded.
	add := func(loc string, msg func(where string) string) {
		paths := t.locations(loc)
		if len(paths) == 0 {
			out = append(out, Problem{Message: msg(loc)})
			return
		}
		for _, p := range paths {
			out = append(out, Problem{Message: msg(t.displayAs(loc, p)), Path: t.original(p), line: t.line(p)})
		}
	}
	for _, p := range ue.Providers {
		for _, loc := range p.Locations {
			add(loc, func(where string) string {
				return fmt.Sprintf("unknown model provider %q at %s: docker-agent %s supports %s, the built-in "+
					"aliases (mistral, openrouter, ollama, ...) and custom providers declared under `providers`",
					p.Name, where, DockerAgentVersion, strings.Join(ProviderTypes, ", "))
			})
		}
	}
	for _, ts := range ue.Toolsets {
		for _, loc := range ts.Locations {
			add(loc, func(where string) string {
				return fmt.Sprintf("unknown toolset type %q at %s: valid types are %s",
					ts.Name, where, strings.Join(ToolsetTypes, ", "))
			})
		}
	}
	for _, f := range ue.Features {
		for _, loc := range f.Locations {
			add(loc, func(where string) string { return fmt.Sprintf("unsupported feature %q at %s", f.Name, where) })
		}
	}
	return out
}

// locationToken matches a docker-agent location with list indexes in a message, for example
// `agents.root.toolsets[0]` or, relative to an agent, `hooks.pre_tool_use[0].hooks[1]`.
var locationToken = regexp.MustCompile(`[A-Za-z_][\w./-]*(?:\[\d+\][\w./-]*)+`)

// listIndexes matches the list indexes of a location.
var listIndexes = regexp.MustCompile(`\[\d+\]`)

// relocateMessage rewrites the locations with list indexes in a message of the current
// document with the indexes of the validated document. docker-agent reports a problem of the
// reduced document that problems loads after removing earlier problems, so the second of
// three invalid hooks says `hooks.pre_tool_use[0].hooks[0]` there. Locations that do not
// resolve to exactly one value are left as they are.
func (t *tree) relocateMessage(msg string) string {
	if len(t.removed) == 0 {
		return msg // nothing was removed from a list, the indexes are the same
	}
	return locationToken.ReplaceAllStringFunc(msg, func(loc string) string {
		if paths := t.locations(loc); len(paths) == 1 {
			return t.displayAs(loc, paths[0])
		}
		return loc
	})
}

// displayAs writes path p of the current document, which the docker-agent location loc
// resolves to, as a location of the validated document in loc's form: from the top of the
// document or, when loc is relative to an agent, from the agent. It returns loc when that
// form cannot be kept.
func (t *tree) displayAs(loc string, p []string) string {
	skip := 0
	if _, ok := t.resolve(t.top, loc, nil); !ok {
		skip = 2 // relative to agents.<name>
	}
	s := t.display(p, skip)
	if s == "" || listIndexes.ReplaceAllString(s, "[]") != listIndexes.ReplaceAllString(loc, "[]") {
		return loc
	}
	return s
}

// display writes path p of the current document as a docker-agent location (`a.b[2].c`) with
// the list indexes of the validated document, leaving out its first skip segments.
func (t *tree) display(p []string, skip int) string {
	var b strings.Builder
	n := t.top
	for k, seg := range p {
		n = deref(n)
		switch {
		case n == nil:
			return ""
		case n.Kind == yaml.SequenceNode:
			i, err := strconv.Atoi(seg)
			if err != nil || i < 0 || i >= len(n.Content) {
				return ""
			}
			if k >= skip {
				fmt.Fprintf(&b, "[%d]", t.originalIndex(n, i))
			}
			n = n.Content[i]
		case n.Kind == yaml.MappingNode:
			if k >= skip {
				if b.Len() > 0 {
					b.WriteByte('.')
				}
				b.WriteString(seg)
			}
			n, _ = child(n, seg)
		default:
			return ""
		}
	}
	return b.String()
}

// originalIndex translates the index of an item of the sequence seq in the current document
// into its index in the validated document.
func (t *tree) originalIndex(seq *yaml.Node, i int) int {
	for _, r := range t.removed[seq] { // ascending
		if r <= i {
			i++
		}
	}
	return i
}

// tree is a parsed configuration that problems removes values from. Paths into it are in
// the current document; original translates them back to the document as it was validated.
type tree struct {
	top *yaml.Node
	// removed lists, per sequence, the original indexes of the items removed from it.
	removed map[*yaml.Node][]int
}

func parseTree(data []byte) (*tree, bool) {
	var root yaml.Node
	if yaml.Unmarshal(data, &root) != nil || len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return nil, false
	}
	return &tree{top: root.Content[0], removed: map[*yaml.Node][]int{}}, true
}

func (t *tree) bytes() []byte {
	b, err := yaml.Marshal(t.top)
	if err != nil {
		return nil
	}
	return b
}

func deref(n *yaml.Node) *yaml.Node {
	for n != nil && n.Kind == yaml.AliasNode {
		n = n.Alias
	}
	return n
}

// child returns the value of key in a mapping and the key's node.
func child(n *yaml.Node, key string) (value, keyNode *yaml.Node) {
	n = deref(n)
	if n == nil || n.Kind != yaml.MappingNode {
		return nil, nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1], n.Content[i]
		}
	}
	return nil, nil
}

// walk returns the node at path and the node of the last step (a key, or the item itself).
func (t *tree) walk(path []string) (n, at *yaml.Node) {
	n = t.top
	for _, seg := range path {
		n = deref(n)
		switch {
		case n == nil:
			return nil, nil
		case n.Kind == yaml.MappingNode:
			n, at = child(n, seg)
		case n.Kind == yaml.SequenceNode:
			i, err := strconv.Atoi(seg)
			if err != nil || i < 0 || i >= len(n.Content) {
				return nil, nil
			}
			n, at = n.Content[i], n.Content[i]
		default:
			return nil, nil
		}
	}
	return n, at
}

// line returns the line path starts at, or 0.
func (t *tree) line(path []string) int {
	if _, at := t.walk(path); at != nil {
		return at.Line
	}
	return 0
}

// original translates a path of the current document into the validated document.
func (t *tree) original(path []string) []string {
	if path == nil {
		return nil
	}
	out := make([]string, 0, len(path))
	n := t.top
	for k, seg := range path {
		n = deref(n)
		if n != nil && n.Kind == yaml.SequenceNode {
			if i, err := strconv.Atoi(seg); err == nil && i < len(n.Content) {
				out = append(out, strconv.Itoa(t.originalIndex(n, i)))
				n = n.Content[i]
				continue
			}
		}
		if n == nil || n.Kind != yaml.MappingNode {
			return append(out, path[k:]...)
		}
		out = append(out, seg)
		n, _ = child(n, seg)
	}
	return out
}

// remove deletes the values at paths (items of one sequence are removed together). It
// returns the paths of the lists and mappings left empty, and whether anything was removed.
func (t *tree) remove(paths [][]string) (emptied [][]string, removed bool) {
	type item struct {
		seq *yaml.Node
		i   int
	}
	var items []item
	for _, p := range paths {
		if len(p) == 0 {
			continue
		}
		parent, _ := t.walk(p[:len(p)-1])
		parent = deref(parent)
		last := p[len(p)-1]
		switch {
		case parent == nil:
		case parent.Kind == yaml.MappingNode:
			for i := 0; i+1 < len(parent.Content); i += 2 {
				if parent.Content[i].Value == last {
					parent.Content = slices.Delete(parent.Content, i, i+2)
					removed = true
					break
				}
			}
		case parent.Kind == yaml.SequenceNode:
			if i, err := strconv.Atoi(last); err == nil && i >= 0 && i < len(parent.Content) {
				items = append(items, item{parent, i})
			}
		}
	}
	// Remove items from the back, so that the indexes stay valid, and remember their
	// original indexes.
	slices.SortFunc(items, func(a, b item) int { return b.i - a.i })
	items = slices.Compact(items)
	for _, it := range items {
		orig := t.originalIndex(it.seq, it.i)
		it.seq.Content = slices.Delete(it.seq.Content, it.i, it.i+1)
		t.removed[it.seq] = append(t.removed[it.seq], orig)
		slices.Sort(t.removed[it.seq])
		removed = true
	}
	for _, p := range paths {
		if len(p) == 0 {
			continue
		}
		if parent, _ := t.walk(p[:len(p)-1]); parent != nil && len(deref(parent).Content) == 0 &&
			!slices.ContainsFunc(emptied, func(e []string) bool { return slices.Equal(e, p[:len(p)-1]) }) {
			emptied = append(emptied, p[:len(p)-1])
		}
	}
	return emptied, removed
}

// location is where a problem is, and what to remove to look for further problems (nil:
// stop).
type location struct {
	path  []string
	elide [][]string
}

var (
	refProblems = []struct {
		re  *regexp.Regexp
		key string
	}{
		{regexp.MustCompile(`^agent '(.+?)' references non-existent sub-agent '(.*)'$`), "sub_agents"},
		{regexp.MustCompile(`^agent '(.+?)': external sub-agent '(.*?)' resolves to name `), "sub_agents"},
		{regexp.MustCompile(`^agent '(.+?)' references non-existent handoff agent '(.*)'$`), "handoffs"},
		{regexp.MustCompile(`^agent '(.+?)': external handoff '(.*?)' resolves to name `), "handoffs"},
		{regexp.MustCompile(`^agent '(.+?)' references non-existent force_handoff agent '(.*)'$`), "force_handoff"},
		{regexp.MustCompile(`^agent '(.+?)': external force_handoff '(.*?)' resolves to name `), "force_handoff"},
		{regexp.MustCompile(`^agent '(.+?)' cannot force_handoff to itself$`), "force_handoff"},
		{regexp.MustCompile(`^force_handoff cycle detected involving agent '(.+)'$`), "force_handoff"},
		{regexp.MustCompile(`^agent '(.+?)' references non-existent model '(.*)'$`), "model"},
		{regexp.MustCompile(`^agent '(.+?)' references non-existent toolset '(.*)'$`), "use_toolsets"},
		{regexp.MustCompile(`^agent '(.+?)' references non-existent command group '(.*)'$`), "use_commands"},
		{regexp.MustCompile(`^agent '(.+?)' references non-existent skill group '(.*)'$`), "use_skills"},
	}
	// toolsetRef matches a toolset `ref` to an undefined top-level definition.
	toolsetRef = regexp.MustCompile(`^agent '(.+?)' references non-existent (?:MCP|RAG) definition '(.*)'$`)
	// definitionProblem matches a message about a named definition: `provider 'p': ...`.
	definitionProblem = regexp.MustCompile(`^(provider|model|MCP definition) '(.+?)': (.*)$`)
	definitionSection = map[string]string{"provider": "providers", "model": "models", "MCP definition": "mcps"}
	agentProblem      = regexp.MustCompile(`^agent ['"](.+?)['"]`)
	unknownField      = regexp.MustCompile(`^\[(\d+):(\d+)\] unknown field "(.+?)"`)
	positions         = regexp.MustCompile(`\[\d+:\d+\]\s*`)
	leadingKey        = regexp.MustCompile(`^([a-z_][a-z0-9_.\[\]]*)(?:\s|:|$)`)
	words             = regexp.MustCompile(`[a-z_][a-z0-9_]*`)
	quotedValue       = regexp.MustCompile(`["']([^"']+)["']`)
	agentUnmarshal    = "failed to unmarshal agent config for "
)

// locate finds where the problem msg is in the current document.
func (t *tree) locate(msg string) location {
	for _, rp := range refProblems {
		m := rp.re.FindStringSubmatch(msg)
		if m == nil {
			continue
		}
		agent := []string{agentsKey, m[1]}
		if v, _ := t.walk(agent); v == nil {
			continue
		}
		path := append(slices.Clone(agent), rp.key)
		if v, _ := t.walk(path); v != nil && deref(v).Kind == yaml.SequenceNode && len(m) > 2 {
			return location{path: path, elide: itemsEqual(path, deref(v), m[2])}
		}
		return t.leaf(path)
	}

	if m := toolsetRef.FindStringSubmatch(msg); m != nil {
		path := []string{agentsKey, m[1], "toolsets"}
		if v, _ := t.walk(path); v != nil && deref(v).Kind == yaml.SequenceNode {
			for i, ts := range deref(v).Content {
				if ref, _ := child(ts, "ref"); ref != nil && deref(ref).Value == m[2] {
					// Remove the whole toolset: without its ref it would be invalid too.
					item := append(slices.Clone(path), strconv.Itoa(i))
					return location{path: append(slices.Clone(item), "ref"), elide: [][]string{item}}
				}
			}
		}
	}

	if m := definitionProblem.FindStringSubmatch(msg); m != nil {
		if path := []string{definitionSection[m[1]], m[2]}; t.exists(path) {
			return t.leaf(t.refine(path, m[3]))
		}
	}

	if rest, ok := strings.CutPrefix(msg, agentUnmarshal); ok {
		if agents, _ := child(t.top, agentsKey); agents != nil {
			for i := 0; i+1 < len(deref(agents).Content); i += 2 {
				name := deref(agents).Content[i].Value
				if inner, ok := strings.CutPrefix(rest, name+": "); ok {
					path := t.localize(deref(agents).Content[i+1], reflect.TypeFor[latest.AgentConfig](), inner,
						[]string{agentsKey, name})
					return t.leaf(t.refine(path, innermost(inner)))
				}
			}
		}
	}

	if m := unknownField.FindStringSubmatch(msg); m != nil {
		if path := t.key(m[3], m[1], m[2]); path != nil {
			return t.leaf(path)
		}
	}

	if strings.HasPrefix(msg, "unsupported config version") {
		return location{path: []string{"version"}}
	}
	if strings.HasPrefix(msg, "at least one agent must be configured") {
		return location{path: []string{agentsKey}}
	}

	// A message that starts with a location: `models.claude: ...`, `agents.root.safety: ...`,
	// or relative to an agent: `hooks.pre_tool_use[0].hooks[0]: ...`.
	for i := strings.Index(msg, ": "); i > 0 && i < 200; {
		if paths := t.locations(msg[:i]); len(paths) == 1 {
			rest := msg[i+2:]
			path := t.refine(paths[0], rest)
			if v, _ := t.walk(path); v != nil && deref(v).Kind == yaml.SequenceNode {
				// A list of references: remove only the items the message names.
				for _, m := range quotedValue.FindAllStringSubmatch(rest, -1) {
					if items := itemsEqual(path, deref(v), m[1]); len(items) > 0 {
						return location{path: path, elide: items}
					}
				}
			}
			return t.leaf(path)
		}
		j := strings.Index(msg[i+2:], ": ")
		if j < 0 {
			break
		}
		i += j + 2
	}

	if m := agentProblem.FindStringSubmatch(msg); m != nil {
		if v, _ := t.walk([]string{agentsKey, m[1]}); v != nil {
			return location{path: []string{agentsKey, m[1]}}
		}
	}

	if path := t.localize(t.top, reflect.TypeFor[latest.Config](), msg, []string{}); len(path) > 0 {
		return t.leaf(t.refine(path, innermost(msg)))
	}
	return location{}
}

// leaf locates a problem at path and removes the value there to look for further problems,
// unless that would remove a section or a named definition.
func (t *tree) leaf(path []string) location {
	loc := location{path: path}
	if removable(path) {
		loc.elide = [][]string{path}
	}
	return loc
}

// removable reports whether the value at path can be removed without creating problems
// elsewhere: it is not a section, an agent or another named definition (models, providers,
// toolsets, ...) that could be referenced.
func removable(path []string) bool {
	switch len(path) {
	case 0, 1:
		return false
	case 2:
		f, ok := yamlField(reflect.TypeFor[latest.Config](), path[0])
		return ok && derefType(f).Kind() == reflect.Struct
	}
	return true
}

// itemsEqual returns the paths of the items of the sequence seq at path whose value is v.
func itemsEqual(path []string, seq *yaml.Node, v string) [][]string {
	var out [][]string
	for i, item := range seq.Content {
		if item = deref(item); item.Kind == yaml.ScalarNode && item.Value == v {
			out = append(out, append(slices.Clone(path), strconv.Itoa(i)))
		}
	}
	return out
}

// refine moves a problem located at a mapping to the key its message is about: the key or
// location the message starts with (from `models.claude` to
// `models.claude.compaction_threshold` for "compaction_threshold must be greater than 0", or
// to `models.claude.cost.input` for "cost.input must not be negative"), else the only key of
// the mapping the message mentions ("invalid api_type 'x'").
func (t *tree) refine(path []string, msg string) []string {
	v, _ := t.walk(path)
	if v = deref(v); v == nil || v.Kind != yaml.MappingNode {
		return path
	}
	if m := leadingKey.FindStringSubmatch(msg); m != nil {
		if p, ok := t.resolve(v, m[1], slices.Clone(path)); ok {
			return p
		}
	}
	var mentioned []string
	for _, w := range words.FindAllString(msg, -1) {
		if c, _ := child(v, w); c != nil && !slices.Contains(mentioned, w) {
			mentioned = append(mentioned, w)
		}
	}
	if len(mentioned) == 1 {
		return append(slices.Clone(path), mentioned[0])
	}
	return path
}

// exists reports whether the document has a value at path.
func (t *tree) exists(path []string) bool {
	v, _ := t.walk(path)
	return v != nil
}

// innermost strips the wrappers and positions from a nested unmarshalling message.
func innermost(msg string) string {
	msg = positions.ReplaceAllString(msg, "")
	for {
		rest, ok := strings.CutPrefix(msg, agentUnmarshal)
		if !ok {
			return strings.TrimSpace(msg)
		}
		_, msg, _ = strings.Cut(rest, ": ")
	}
}

// key locates the key name of an "unknown field" error: at line:col when the key there has
// that name (the positions of nested errors are relative to the value being unmarshalled),
// else the only key of that name in the document.
func (t *tree) key(name, line, col string) []string {
	var found [][]string
	var at []string
	var visit func(n *yaml.Node, path []string)
	visit = func(n *yaml.Node, path []string) {
		n = deref(n)
		switch n.Kind {
		case yaml.MappingNode:
			for i := 0; i+1 < len(n.Content); i += 2 {
				k := n.Content[i]
				p := append(slices.Clone(path), k.Value)
				if k.Value == name {
					found = append(found, p)
					if strconv.Itoa(k.Line) == line && strconv.Itoa(k.Column) == col {
						at = p
					}
				}
				visit(n.Content[i+1], p)
			}
		case yaml.SequenceNode:
			for i, item := range n.Content {
				visit(item, append(slices.Clone(path), strconv.Itoa(i)))
			}
		}
	}
	visit(t.top, nil)
	switch {
	case at != nil:
		return at
	case len(found) == 1:
		return found[0]
	}
	return nil
}

// locations resolves a docker-agent location (`agents.root.toolsets[2]`, `models.claude`,
// `models.openai/gpt-5 (provider "x")`) to paths of the current document. A location that
// does not start at the top of the document (`hooks.session_start[0]`) is matched against
// every agent. An inline model reference (`models.openai/gpt-5`), which docker-agent lists
// as a model of its own, resolves to the values that use it.
func (t *tree) locations(loc string) [][]string {
	loc, _, _ = strings.Cut(loc, " (provider ")
	if loc == "" || strings.ContainsAny(loc, " '\"\n") {
		return nil
	}
	if p, ok := t.resolve(t.top, loc, nil); ok {
		return [][]string{p}
	}
	var out [][]string
	if agents, _ := child(t.top, agentsKey); agents != nil {
		agents = deref(agents)
		for i := 0; agents.Kind == yaml.MappingNode && i+1 < len(agents.Content); i += 2 {
			if p, ok := t.resolve(agents.Content[i+1], loc, []string{agentsKey, agents.Content[i].Value}); ok {
				out = append(out, p)
			}
		}
	}
	if ref, ok := strings.CutPrefix(loc, "models."); ok && len(out) == 0 && strings.Contains(ref, "/") {
		out = t.uses(ref)
	}
	return out
}

// resolve resolves a dotted location below n. Mapping keys may contain dots: the longest
// key that matches wins.
func (t *tree) resolve(n *yaml.Node, loc string, path []string) ([]string, bool) {
	n = deref(n)
	if loc == "" {
		return path, true
	}
	if n == nil {
		return nil, false
	}
	switch {
	case strings.HasPrefix(loc, "["):
		end := strings.IndexByte(loc, ']')
		i, err := strconv.Atoi(loc[1:max(end, 1)])
		if end < 0 || err != nil || n.Kind != yaml.SequenceNode || i < 0 || i >= len(n.Content) {
			return nil, false
		}
		return t.resolve(n.Content[i], strings.TrimPrefix(loc[end+1:], "."), append(slices.Clone(path), strconv.Itoa(i)))
	case n.Kind == yaml.MappingNode:
		best := -1
		for i := 0; i+1 < len(n.Content); i += 2 {
			k := n.Content[i].Value
			if !strings.HasPrefix(loc, k) {
				continue
			}
			if rest := loc[len(k):]; rest == "" || rest[0] == '.' || rest[0] == '[' {
				if best < 0 || len(k) > len(n.Content[best].Value) {
					best = i
				}
			}
		}
		if best < 0 {
			return nil, false
		}
		k := n.Content[best].Value
		return t.resolve(n.Content[best+1], strings.TrimPrefix(loc[len(k):], "."), append(slices.Clone(path), k))
	}
	return nil, false
}

// uses returns the paths of the `model` values and model lists that reference model.
func (t *tree) uses(model string) [][]string {
	var out [][]string
	var visit func(n *yaml.Node, path []string)
	visit = func(n *yaml.Node, path []string) {
		n = deref(n)
		switch n.Kind {
		case yaml.MappingNode:
			for i := 0; i+1 < len(n.Content); i += 2 {
				visit(n.Content[i+1], append(slices.Clone(path), n.Content[i].Value))
			}
		case yaml.SequenceNode:
			for i, item := range n.Content {
				visit(item, append(slices.Clone(path), strconv.Itoa(i)))
			}
		case yaml.ScalarNode:
			if len(path) > 0 && slices.Contains(strings.Split(n.Value, ","), model) {
				out = append(out, path)
			}
		}
	}
	visit(t.top, nil)
	return out
}

// localize returns the path of the smallest value below n (of docker-agent type typ, at
// path) that fails to unmarshal with the problem msg on its own.
func (t *tree) localize(n *yaml.Node, typ reflect.Type, msg string, path []string) []string {
	n, typ = deref(n), derefType(typ)
	want := innermost(msg)
	try := func(v *yaml.Node, typ reflect.Type, seg string) ([]string, bool) {
		if !reproduces(v, typ, want) {
			return nil, false
		}
		return t.localize(v, typ, msg, append(slices.Clone(path), seg)), true
	}
	switch {
	case n == nil:
	case n.Kind == yaml.MappingNode && typ.Kind() == reflect.Struct:
		for i := 0; i+1 < len(n.Content); i += 2 {
			if ft, ok := yamlField(typ, n.Content[i].Value); ok {
				if p, ok := try(n.Content[i+1], ft, n.Content[i].Value); ok {
					return p
				}
			}
		}
	case n.Kind == yaml.MappingNode && (typ.Kind() == reflect.Map || typ.Kind() == reflect.Slice):
		// A map, or a keyed list such as latest.Agents.
		for i := 0; i+1 < len(n.Content); i += 2 {
			if p, ok := try(n.Content[i+1], typ.Elem(), n.Content[i].Value); ok {
				return p
			}
		}
	case n.Kind == yaml.SequenceNode && typ.Kind() == reflect.Slice:
		for i, item := range n.Content {
			if p, ok := try(item, typ.Elem(), strconv.Itoa(i)); ok {
				return p
			}
		}
	}
	return path
}

// reproduces reports whether the value n alone fails to unmarshal into typ with the
// message want.
func reproduces(n *yaml.Node, typ reflect.Type, want string) bool {
	b, err := yaml.Marshal(n)
	if err != nil {
		return false
	}
	v := reflect.New(typ)
	err = goyaml.UnmarshalWithOptions(b, v.Interface(), goyaml.Strict())
	if err == nil {
		return false
	}
	got := innermost(Clean(err.Error()))
	return got != "" && strings.Contains(want, got)
}

func derefType(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

// yamlField returns the type of the field of struct typ that the YAML key name decodes
// into (docker-agent's types use `json` tags, which goccy/go-yaml honours).
func yamlField(typ reflect.Type, name string) (reflect.Type, bool) {
	typ = derefType(typ)
	if typ.Kind() != reflect.Struct {
		return nil, false
	}
	for _, f := range reflect.VisibleFields(typ) {
		if f.Anonymous || !f.IsExported() {
			continue
		}
		tag := f.Tag.Get("yaml")
		if tag == "" {
			tag = f.Tag.Get("json")
		}
		key, _, _ := strings.Cut(tag, ",")
		if key == "" {
			key = strings.ToLower(f.Name)
		}
		if key == name {
			return f.Type, true
		}
	}
	return nil, false
}
