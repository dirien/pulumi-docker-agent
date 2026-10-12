// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

// Package pcl turns docker-agent configurations into a Pulumi program in PCL, the
// language-neutral program format from which `pulumi convert` generates TypeScript, Python, Go,
// C#, Java, YAML and HCL programs. It is the core of the converter plugin
// (`pulumi convert --from docker-agent`).
//
// Load reads the agent files of a directory (YAML and docker-agent HCL) with
// render.DecodeYAML, and Program writes one `docker-agent:index:AgentConfig` resource per file,
// with typed inputs: the generated input types are walked by their `pulumi` struct tags, so
// every key becomes the camelCase input the provider schema declares, in the order of the
// file. Maps keyed by the user (models, env, ...) keep their keys verbatim, multi-line
// strings become heredocs, and YAML anchors and merge keys are expanded.
//
// Pulumi's program generators keep only the comments in front of a resource, so the comments
// of a file are collected there, each labelled with the YAML key it was written at.
package pcl

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"gopkg.in/yaml.v3"

	"github.com/dirien/pulumi-docker-agent/provider/pkg/render"
)

// The resource type tokens of the provider, and where the Pulumi CLI downloads it from.
const (
	ConfigType        = "docker-agent:index:AgentConfig"
	ArtifactType      = "docker-agent:index:AgentArtifact"
	RunType           = "docker-agent:index:AgentRun"
	PluginDownloadURL = "github://api.github.com/dirien/pulumi-docker-agent"
)

// Config is one docker-agent configuration of the program.
type Config struct {
	// Name is the logical name of the AgentConfig: the file path relative to the source
	// directory, without extension, with `/` replaced by `-`.
	Name string
	// Source is the file, relative to the source directory and slash-separated. Twins are
	// files of the same configuration in another format, which are not converted separately.
	Source string
	Twins  []string
	// Decoded is the configuration with its comments, key order and anchors.
	Decoded *render.Decoded
	// ReadFiles are inputs the program reads from a file with readFile, by input path. The
	// files are relative to the program directory.
	ReadFiles map[render.Path]string
	// Notes are explanations of the converter, by input path. They are placed like the
	// comments of that path.
	Notes map[render.Path][]string
	// Comments are the comments of a configuration whose comments are not in Decoded (HCL),
	// in source order.
	Comments []Note
	// SkipValidation explains why the AgentConfig skips validation, or is empty.
	SkipValidation string
	// Files are the files, relative to the source directory and slash-separated, that the
	// program or the configuration reads. The converter copies them next to the program.
	Files []string
}

// Note is a block of comments of a configuration and where it belongs.
type Note struct {
	// Label is the YAML key the comments were written at, for example `agents.root.toolsets[0]`.
	Label string
	// Lines are comment lines (`# text`). An empty line separates two groups.
	Lines []string
}

// Options are the converter arguments that add resources to the program.
type Options struct {
	// PushTo adds an AgentArtifact per configuration: `<registry>/<namespace>` pushes each one
	// to `<registry>/<namespace>/<name>:latest`; a single configuration may also be pushed to
	// a full reference with a tag.
	PushTo string
	// Run adds an AgentRun per configuration that sends this prompt to the default agent.
	Run string
	// ProviderVersion pins the docker-agent provider version the program is generated for.
	ProviderVersion string
}

// commentWidth is the width the converter's own comments are wrapped at.
const commentWidth = 96

// Program returns the PCL program (main.pp) for configs, and warnings about configurations it
// adds no AgentArtifact or AgentRun for.
func Program(configs []*Config, opts Options) ([]byte, hcl.Diagnostics, error) {
	if len(configs) == 0 {
		return nil, nil, errors.New("no docker-agent configuration to convert")
	}
	refs, err := artifactReferences(configs, opts.PushTo)
	if err != nil {
		return nil, nil, err
	}
	w := &writer{taken: map[string]bool{}}
	var diags hcl.Diagnostics
	for _, c := range configs {
		if c.SkipValidation != "" && (opts.PushTo != "" || opts.Run != "") {
			// docker-agent reads instruction files only next to a local agent file, not from
			// an OCI artifact or a configuration in memory.
			delete(refs, c)
			diags = append(diags, diagnostic(hcl.DiagWarning, c.Source, "No AgentArtifact or AgentRun",
				c.Source+" uses `instruction_file`, which docker-agent only reads next to a local agent file, "+
					"not from an OCI artifact or a configuration in memory. Write the instructions in `instruction` "+
					"to push or run it."))
		}
		e, err := w.config(c, refs[c], opts)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", c.Source, err)
		}
		if len(e.yamlIssues) > 0 {
			diags = append(diags, diagnostic(hcl.DiagWarning, c.Source, YAMLLimitation,
				strings.Join(e.yamlIssues, " ")+" Only a conversion to YAML is affected."))
		}
		if len(e.goIssues) > 0 {
			diags = append(diags, diagnostic(hcl.DiagWarning, c.Source, GoLimitation,
				strings.Join(e.goIssues, " ")+" Only a conversion to Go is affected."))
		}
	}
	out := hclwrite.Format([]byte(w.b.String()))
	if _, d := hclsyntax.ParseConfig(out, "main.pp", hcl.InitialPos); d.HasErrors() {
		return nil, nil, fmt.Errorf("the generated program does not parse: %w", d)
	}
	return out, diags, nil
}

// node is a PCL expression: a lit (written as is), a str, a list or an obj.
type node any

type (
	lit  string
	str  string // a string value, written as a heredoc where possible
	list []node
	obj  []entry
)

type entry struct {
	key string // an attribute name or a formatted object key
	val node
}

// writer writes the program text.
type writer struct {
	b     strings.Builder
	taken map[string]bool
}

// name returns id, or id with a number when it is taken, and takes it.
func (w *writer) name(id string) string {
	name := id
	for i := 2; w.taken[name]; i++ {
		name = id + strconv.Itoa(i)
	}
	w.taken[name] = true
	return name
}

// The summaries of the warnings about values that the program generator of one language
// writes incorrectly (see emitter.yamlIssues and emitter.goIssues).
const (
	YAMLLimitation = "Pulumi YAML limitation"
	GoLimitation   = "Pulumi Go limitation"
)

// config writes the resources of one configuration. The emitter it returns has the values of
// the configuration that a YAML or Go program cannot hold as generated.
func (w *writer) config(c *Config, ref string, opts Options) (*emitter, error) {
	e := &emitter{c: c, dec: c.Decoded, used: map[render.Path]bool{}, usedNotes: map[render.Path]bool{}}
	doc := e.object(reflect.ValueOf(&c.Decoded.Document).Elem(), "", "")
	if e.err != nil {
		return nil, e.err
	}
	e.leftovers()
	if c.SkipValidation != "" {
		doc = append(doc, entry{"skipValidation", lit("true")})
		e.notes = append(e.notes, note("skipValidation", c.SkipValidation))
	}

	id := w.name(Identifier(c.Name))
	w.resource(header(c, slices.Concat(c.Comments, e.notes)), id, c.Name, ConfigType, doc, opts)

	if ref != "" {
		aid := w.name(id + "Artifact")
		attrs := obj{{"reference", lit(quote(ref))}, {"yaml", lit(id + ".yaml")}}
		text := fmt.Sprintf("Pushes the configuration of %s to %s as an OCI artifact. Registry credentials "+
			"come from the provider's `registryAuth` or the Docker credential store (`docker login`).", c.Source, ref)
		w.resource(comment(text), aid, c.Name, ArtifactType, attrs, opts)
		w.output(w.name(id+"PinnedReference"), aid+".pinnedReference")
	}
	if opts.Run != "" && c.SkipValidation == "" {
		rid := w.name(id + "Run")
		attrs := obj{{"yaml", lit(id + ".yaml")}, {"prompt", str(opts.Run)}}
		text := "Sends the prompt to the default agent of " + c.Source + " during `pulumi up` (never during a " +
			"preview) and exports the answer. The model credentials, such as the variables in the AgentConfig's " +
			"`requiredEnvVars` output, are read from the environment of the Pulumi CLI (`inheritEnvironment` " +
			"defaults to true), so the program holds no secrets. To keep a key in the stack instead, store it with " +
			"`pulumi config set --secret` and pass it in `env`. Tool calls that docker-agent would ask to confirm " +
			"are denied (`toolApproval` defaults to `deny`)."
		w.resource(comment(text), rid, c.Name, RunType, attrs, opts)
		w.output(w.name(id+"Output"), rid+".output")
	}
	return e, nil
}

// header returns the comments in front of a configuration's AgentConfig: where it was
// converted from, the comment block at the top of the file, and the other comments, each
// labelled with the YAML key it was written at.
func header(c *Config, notes []Note) []string {
	from := "# Converted from " + c.Source
	for _, t := range c.Twins {
		from += ", the same configuration as " + t
	}
	lines := []string{from + "."}
	if head := dropDirectives(c.Decoded.Head, true); len(head) > 0 {
		lines = append(lines, "#")
		lines = append(lines, commentLines(head, "")...)
	}
	if foot := trimBlank(c.Decoded.Foot); len(foot) > 0 {
		notes = append(notes, Note{Label: "end of " + c.Source, Lines: foot})
	}
	for _, n := range notes {
		body := commentLines(trimBlank(n.Lines), "  ")
		if len(body) == 0 {
			continue
		}
		lines = append(lines, "#", "# "+n.Label+":")
		lines = append(lines, body...)
	}
	return lines
}

// commentLines writes comment lines with their text indented by indent. An empty line
// becomes an empty comment line.
func commentLines(lines []string, indent string) []string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		t := commentText(l)
		if strings.TrimSpace(t) == "" {
			out = append(out, "#")
			continue
		}
		out = append(out, "# "+indent+t)
	}
	return out
}

// note returns a converter note, word-wrapped.
func note(label, text string) Note {
	var lines []string
	for _, l := range wrap(text, commentWidth-4) {
		lines = append(lines, "# "+l)
	}
	return Note{Label: label, Lines: lines}
}

// comment returns text as word-wrapped comment lines.
func comment(text string) []string {
	var lines []string
	for _, l := range wrap(text, commentWidth) {
		lines = append(lines, "# "+l)
	}
	return lines
}

func (w *writer) resource(comments []string, id, logical, token string, attrs obj, opts Options) {
	if w.b.Len() > 0 {
		w.b.WriteString("\n")
	}
	for _, l := range comments {
		w.b.WriteString(l + "\n")
	}
	fmt.Fprintf(&w.b, "resource %s %s {\n", id, quote(token))
	if logical != id {
		attrs = append(obj{{"__logicalName", lit(quote(logical))}}, attrs...)
	}
	for _, a := range attrs {
		w.entry(a, 1)
	}
	options := obj{{"pluginDownloadURL", lit(quote(PluginDownloadURL))}}
	if opts.ProviderVersion != "" {
		options = append(options, entry{"version", lit(quote(opts.ProviderVersion))})
	}
	w.b.WriteString("\n  options {\n")
	for _, o := range options {
		w.entry(o, 2)
	}
	w.b.WriteString("  }\n}\n")
}

func (w *writer) output(name, value string) {
	fmt.Fprintf(&w.b, "\noutput %s {\n  value = %s\n}\n", name, value)
}

func (w *writer) indent(n int) { w.b.WriteString(strings.Repeat("  ", n)) }

func (w *writer) entry(e entry, indent int) {
	w.indent(indent)
	w.b.WriteString(e.key + " = ")
	w.value(e.val, indent, false)
	w.b.WriteString("\n")
}

// value writes a node. A multi-line string is a heredoc, except in a list, where it is a
// quoted string (a heredoc's closing marker must be followed by a newline).
func (w *writer) value(n node, indent int, inList bool) {
	switch n := n.(type) {
	case lit:
		w.b.WriteString(string(n))
	case str:
		if m := heredocMarker(string(n)); m != "" && !inList {
			w.b.WriteString("<<" + m + "\n" + string(n) + m)
			return
		}
		w.b.WriteString(quote(string(n)))
	case list:
		if s, ok := inline(n); ok && 2*indent+len(s) <= 100 {
			w.b.WriteString(s)
			return
		}
		w.b.WriteString("[\n")
		for _, item := range n {
			w.indent(indent + 1)
			w.value(item, indent+1, true)
			w.b.WriteString(",\n")
		}
		w.indent(indent)
		w.b.WriteString("]")
	case obj:
		if len(n) == 0 {
			w.b.WriteString("{}")
			return
		}
		w.b.WriteString("{\n")
		for _, e := range n {
			w.entry(e, indent+1)
		}
		w.indent(indent)
		w.b.WriteString("}")
	default:
		panic(fmt.Sprintf("pcl: cannot write %T", n))
	}
}

// inline returns the one-line form of a list of scalars.
func inline(l list) (string, bool) {
	parts := make([]string, 0, len(l))
	for _, item := range l {
		switch item := item.(type) {
		case lit:
			parts = append(parts, string(item))
		case str:
			if strings.Contains(string(item), "\n") {
				return "", false
			}
			parts = append(parts, quote(string(item)))
		default:
			return "", false
		}
	}
	return "[" + strings.Join(parts, ", ") + "]", true
}

// emitter builds the AgentConfig inputs of one configuration from its typed Document.
type emitter struct {
	c         *Config
	dec       *render.Decoded
	used      map[render.Path]bool
	usedNotes map[render.Path]bool
	notes     []Note
	err       error
	// yamlIssues describe the values a Pulumi YAML program cannot hold although the other
	// languages can. Pulumi YAML's program generator writes a string such as `~` or `null`
	// unquoted, so it is read back as null (or a bool or number), and its type checker rejects
	// a typed map with a key that is also an input name of the map's values (a model named
	// `model`).
	yamlIssues []string
	// goIssues describe the values Pulumi's Go program generator writes as code that does not
	// compile: a list that is a value of a free-form map (`pulumi.Any{...}`).
	goIssues []string
}

// anyList notes a list that is a value of a free-form map.
func (e *emitter) anyList(val any, label string) {
	if _, ok := val.([]any); ok {
		e.goIssues = append(e.goIssues, fmt.Sprintf("`%s` is a list in a free-form map, which Pulumi's Go "+
			"program generator writes as `pulumi.Any{...}`; write `pulumi.Any([]interface{}{...})` in main.go.", label))
	}
}

// string returns a string value, noting when Pulumi YAML misreads it.
func (e *emitter) string(s, label string) node {
	if t := misreadAs(s); t != "" {
		e.yamlIssues = append(e.yamlIssues, fmt.Sprintf("`%s` is the string %s, which Pulumi YAML's program "+
			"generator writes unquoted, so the program reads it as %s; quote it in Main.yaml.", label, quote(s), t))
	}
	return str(s)
}

// key notes a map key that Pulumi YAML misreads, or that its type checker confuses with an input
// of the map's values (elem).
func (e *emitter) key(k, label string, elem reflect.Type) {
	if t := misreadAs(k); t != "" {
		e.yamlIssues = append(e.yamlIssues, fmt.Sprintf("The key %s of `%s` is written unquoted by Pulumi YAML's "+
			"program generator, so the program reads it as %s; quote it in Main.yaml.", quote(k), label, t))
	}
	for elem != nil && elem.Kind() == reflect.Pointer {
		elem = elem.Elem()
	}
	if elem != nil && elem.Kind() == reflect.Struct {
		if _, ok := fieldByName(elem, k); ok {
			e.yamlIssues = append(e.yamlIssues, fmt.Sprintf("Pulumi YAML's type checker rejects the key %s of `%s`, "+
				"because its values also have an input named `%s`; rename the key (and where it is used).", quote(k), label, k))
		}
	}
}

// misreadAs returns what a YAML parser reads a string as when Pulumi YAML's program generator
// writes it as a plain scalar (it only quotes numbers that strconv parses, `true` and `false`),
// or "" when it reads it back as the string.
func misreadAs(s string) string {
	if s == "" || s == "true" || s == "false" || strings.ContainsAny(s, "\n\r") {
		return ""
	}
	if _, err := strconv.ParseFloat(s, 32); err == nil {
		return ""
	}
	var n yaml.Node
	if err := yaml.Unmarshal([]byte(s), &n); err != nil || len(n.Content) != 1 ||
		n.Content[0].Kind != yaml.ScalarNode || n.Content[0].Style != 0 {
		return ""
	}
	var v any
	if err := n.Content[0].Decode(&v); err != nil {
		return ""
	}
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "a bool"
	case int, int64, uint64, float64:
		return "a number"
	}
	return ""
}

// place collects the comments and notes of the value at p, labelled with its YAML key.
func (e *emitter) place(p render.Path, label string) {
	var lines []string
	if c := e.dec.Comments[p]; c != nil && !e.used[p] {
		lines = slices.Concat(c.Head, c.Line, c.Foot)
	}
	e.used[p] = true
	if texts, ok := e.c.Notes[p]; ok && !e.usedNotes[p] {
		for _, t := range texts {
			if len(trimBlank(lines)) > 0 {
				lines = append(lines, "")
			}
			lines = append(lines, note("", t).Lines...)
		}
	}
	e.usedNotes[p] = true
	if len(trimBlank(lines)) == 0 {
		return
	}
	if label == "" {
		label = "the document"
	}
	if n := len(e.notes); n > 0 && e.notes[n-1].Label == label {
		e.notes[n-1].Lines = append(append(e.notes[n-1].Lines, ""), lines...)
		return
	}
	e.notes = append(e.notes, Note{Label: label, Lines: lines})
}

// leftovers collects the comments and notes the walk did not reach (a value that is not
// written out), labelled with their input path.
func (e *emitter) leftovers() {
	var rest []render.Path
	for p, c := range e.dec.Comments {
		if !e.used[p] && len(trimBlank(slices.Concat(c.Head, c.Line, c.Foot))) > 0 {
			rest = append(rest, p)
		}
	}
	for p := range e.c.Notes {
		if !e.usedNotes[p] && !slices.Contains(rest, p) {
			rest = append(rest, p)
		}
	}
	slices.Sort(rest)
	for _, p := range rest {
		e.place(p, string(p))
	}
}

func (e *emitter) fail(p render.Path, format string, args ...any) node {
	if e.err == nil {
		e.err = fmt.Errorf("%s: %s", p, fmt.Sprintf(format, args...))
	}
	return lit("null")
}

var plainKey = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func fieldLabel(y, k string) string {
	if y == "" {
		return k
	}
	return y + "." + k
}

func keyLabel(y, k string) string {
	if plainKey.MatchString(k) {
		return fieldLabel(y, k)
	}
	return y + "[" + strconv.Quote(k) + "]"
}

func indexLabel(y string, i int) string { return y + "[" + strconv.Itoa(i) + "]" }

// object builds a typed object: its key (Agent.name) first, then the inputs in YAML order,
// then any other input that is set (keys merged from a YAML anchor).
func (e *emitter) object(v reflect.Value, p render.Path, y string) obj {
	t := v.Type()
	var o obj
	done := map[string]bool{}
	if f := keyField(t); f != nil {
		done[f.pulumi] = true
		o = append(o, entry{f.pulumi, lit(quote(v.Field(f.index).String()))})
	}
	for _, name := range e.dec.Order[p] {
		f, ok := fieldByName(t, name)
		if !ok || done[name] {
			continue
		}
		done[name] = true
		o = e.field(o, v, f, p, y, true)
	}
	for _, f := range fieldsOf(t) {
		if !done[f.pulumi] {
			o = e.field(o, v, f, p, y, false)
		}
	}
	return o
}

// field adds input f of the object v at p. inYAML is true when the file sets its key.
func (e *emitter) field(o obj, v reflect.Value, f fieldInfo, p render.Path, y string, inYAML bool) obj {
	fp := p.Field(f.pulumi)
	fv := v.Field(f.index)
	if f.extra {
		return e.extra(o, f, fv, fp, y)
	}
	label := fieldLabel(y, f.yamlKey)
	var set bool
	switch fv.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Slice, reflect.Map:
		set = !fv.IsNil()
	case reflect.Struct:
		set = true
	default:
		set = inYAML || f.required || !fv.IsZero()
	}
	e.place(fp, label)
	if !set {
		return o
	}
	return append(o, entry{f.pulumi, e.value(fv, fp, label)})
}

// extra adds the untyped keys of an object, verbatim.
func (e *emitter) extra(o obj, f fieldInfo, fv reflect.Value, p render.Path, y string) obj {
	m, _ := fv.Interface().(map[string]any)
	var x obj
	for _, k := range ordered(e.dec.Order[p], keysOf(reflect.ValueOf(m))) {
		kp := p.Key(k)
		label := keyLabel(y, k)
		e.place(kp, label)
		if val, ok := m[k]; ok {
			e.key(k, y, nil)
			e.anyList(val, label)
			x = append(x, entry{objectKey(k), e.free(val, kp, label)})
		}
	}
	if len(x) == 0 {
		return o
	}
	return append(o, entry{f.pulumi, x})
}

// value builds a typed value.
func (e *emitter) value(v reflect.Value, p render.Path, y string) node {
	if file, ok := e.c.ReadFiles[p]; ok {
		return lit("readFile(" + quote(file) + ")")
	}
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return lit("null")
		}
		return e.value(v.Elem(), p, y)
	case reflect.Interface:
		if v.IsNil() {
			return lit("null")
		}
		return e.free(v.Elem().Interface(), p, y)
	case reflect.Struct:
		return e.object(v, p, y)
	case reflect.Slice:
		keyed := keyField(v.Type().Elem())
		l := list{}
		for i := range v.Len() {
			ip := p.Index(i)
			label := indexLabel(y, i)
			if keyed != nil {
				label = keyLabel(y, reflect.Indirect(v.Index(i)).Field(keyed.index).String())
			}
			e.place(ip, label)
			l = append(l, e.value(v.Index(i), ip, label))
		}
		return l
	case reflect.Map:
		o := obj{}
		for _, k := range ordered(e.dec.Order[p], keysOf(v)) {
			ev := v.MapIndex(reflect.ValueOf(k).Convert(v.Type().Key()))
			if !ev.IsValid() {
				continue
			}
			kp := p.Key(k)
			label := keyLabel(y, k)
			e.place(kp, label)
			e.key(k, y, v.Type().Elem())
			if v.Type().Elem().Kind() == reflect.Interface {
				e.anyList(ev.Interface(), label)
			}
			o = append(o, entry{objectKey(k), e.value(ev, kp, label)})
		}
		return o
	case reflect.String:
		return e.string(v.String(), y)
	case reflect.Bool:
		return lit(strconv.FormatBool(v.Bool()))
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return lit(strconv.FormatInt(v.Int(), 10))
	case reflect.Float32, reflect.Float64:
		return lit(formatFloat(v.Float()))
	}
	return e.fail(p, "unsupported %s", v.Type())
}

// free builds a free-form value (a JSON schema, provider options, a flavor), as decoded from
// YAML.
func (e *emitter) free(x any, p render.Path, y string) node {
	switch x := x.(type) {
	case nil:
		return lit("null")
	case map[string]any:
		o := obj{}
		for _, k := range ordered(e.dec.Order[p], keysOf(reflect.ValueOf(x))) {
			val, ok := x[k]
			if !ok {
				continue
			}
			kp := p.Key(k)
			label := keyLabel(y, k)
			e.place(kp, label)
			e.key(k, y, nil)
			o = append(o, entry{objectKey(k), e.free(val, kp, label)})
		}
		return o
	case []any:
		l := list{}
		for i, item := range x {
			ip := p.Index(i)
			label := indexLabel(y, i)
			e.place(ip, label)
			l = append(l, e.free(item, ip, label))
		}
		return l
	case string:
		return e.string(x, y)
	case bool:
		return lit(strconv.FormatBool(x))
	case int:
		return lit(strconv.Itoa(x))
	case int64:
		return lit(strconv.FormatInt(x, 10))
	case uint64:
		return lit(strconv.FormatUint(x, 10))
	case float64:
		return lit(formatFloat(x))
	}
	return e.fail(p, "unsupported free-form value %T", x)
}

// keysOf returns the sorted keys of a map with string keys.
func keysOf(v reflect.Value) []string {
	if !v.IsValid() || v.Kind() != reflect.Map {
		return nil
	}
	keys := make([]string, 0, v.Len())
	for _, k := range v.MapKeys() {
		keys = append(keys, k.String())
	}
	sort.Strings(keys)
	return keys
}

// ordered returns the keys in YAML order, then the others (sorted).
func ordered(order, keys []string) []string {
	out := slices.Clone(order)
	for _, k := range keys {
		if !slices.Contains(out, k) {
			out = append(out, k)
		}
	}
	return out
}

// artifactReferences returns where each configuration is pushed to.
func artifactReferences(configs []*Config, pushTo string) (map[*Config]string, error) {
	out := map[*Config]string{}
	pushTo = strings.TrimSpace(pushTo)
	if pushTo == "" {
		return out, nil
	}
	if strings.Contains(pushTo, "@") || strings.Contains(pushTo, "://") {
		return nil, fmt.Errorf("--push-to %q: expected <registry>/<namespace> or, for a single configuration, "+
			"<registry>/<repository>:<tag>", pushTo)
	}
	last := pushTo[strings.LastIndex(pushTo, "/")+1:]
	if strings.Contains(last, ":") {
		if len(configs) > 1 {
			return nil, fmt.Errorf("--push-to %q has a tag, which only works for a single configuration; "+
				"pass <registry>/<namespace> to push each of the %d configurations to <registry>/<namespace>/<name>:latest",
				pushTo, len(configs))
		}
		out[configs[0]] = pushTo
		return out, nil
	}
	prefix := strings.TrimSuffix(pushTo, "/")
	seen := map[string]bool{}
	for _, c := range configs {
		repo := repositoryName(c.Name)
		name := repo
		for i := 2; seen[name]; i++ {
			name = repo + "-" + strconv.Itoa(i)
		}
		seen[name] = true
		out[c] = prefix + "/" + name + ":latest"
	}
	return out, nil
}

var nonRepoChars = regexp.MustCompile(`[^a-z0-9]+`)

// repositoryName returns a valid OCI repository path component for a configuration name.
func repositoryName(name string) string {
	s := strings.Trim(nonRepoChars.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if s == "" {
		return "agent"
	}
	return s
}
