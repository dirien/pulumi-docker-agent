package schemadiff

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Classifier tells whether a schema path (in the package's path notation) is rendered as an
// untyped (`any`) input by the provider, and why. The Markdown report checks every added or
// changed path and each of its ancestor prefixes (e.g. for `A.b[].c`: `A`, `A.b`, `A.b[]`,
// `A.b[].c`).
type Classifier func(path string) (untyped bool, reason string)

// UntypedPath is an added or changed path that sits in an untyped part of the provider schema.
type UntypedPath struct {
	Path   string // the added or changed path
	At     string // the outermost untyped prefix of Path (Path itself when equal)
	Reason string
}

// Untyped returns, sorted by path, the added properties and changed paths (type, enum and
// required changes, union constraints) for which classify reports an untyped prefix. It returns
// nil when classify is nil.
func (d *Diff) Untyped(classify Classifier) []UntypedPath {
	if classify == nil {
		return nil
	}
	set := map[string]struct{}{}
	for _, p := range d.AddedProperties {
		set[p.Path] = struct{}{}
	}
	for _, p := range d.AddedConstraints {
		set[p.Path] = struct{}{}
	}
	for _, c := range d.TypeChanges {
		set[c.Path] = struct{}{}
	}
	for _, c := range d.EnumChanges {
		set[c.Path] = struct{}{}
	}
	for _, c := range d.RequiredChanges {
		set[c.Path] = struct{}{}
	}
	var out []UntypedPath
	for _, p := range sortedPaths(set) {
		if at, reason, ok := untypedPrefix(p, classify); ok {
			out = append(out, UntypedPath{Path: p, At: at, Reason: reason})
		}
	}
	return out
}

func untypedPrefix(path string, classify Classifier) (at, reason string, ok bool) {
	for _, prefix := range Ancestors(path) {
		if untyped, why := classify(prefix); untyped {
			return prefix, why, true
		}
	}
	return "", "", false
}

// Ancestors returns the prefixes of path from the outermost (the definition or Config) to path
// itself, splitting before each `.`, `[`, `{` and `|`.
func Ancestors(path string) []string {
	var out []string
	depth := 0 // inside {<pattern>} the separators belong to the pattern
	for i := 1; i < len(path); i++ {
		switch path[i] {
		case '{':
			if depth == 0 {
				out = append(out, path[:i])
			}
			depth++
		case '}':
			if depth > 0 {
				depth--
			}
		case '.', '[', '|':
			if depth == 0 {
				out = append(out, path[:i])
			}
		}
	}
	return append(out, path)
}

// EnumClassifier tells whether the value at a schema path (in the package's path notation)
// is a Pulumi enum of the provider, and its type name.
type EnumClassifier func(path string) (enumType string, ok bool)

// Enums classifies the Pulumi enums of the previous (Old) and the new (New) schema. Either may
// be nil when it is unknown, for example because the generator rejects the new schema.
type Enums struct {
	Old, New EnumClassifier
}

// EnumBreak is a change that breaks programs written against the previous SDKs: values removed
// from a Pulumi enum (a renamed value shows up as removed and added), or a string input that
// became a Pulumi enum. The constants of a Pulumi enum are typed in Go, .NET and Java, and the
// TypeScript type is a union of the literals.
type EnumBreak struct {
	Path           string
	Enum           string // the enum type: the previous one, or the new one for NewEnum
	Removed, Added []string
	NewEnum        bool // the input was a plain string before
}

// SDKBreaking returns the enum changes that break programs written against the previous SDKs,
// sorted by path. It returns nil when enums is nil.
func (d *Diff) SDKBreaking(enums *Enums) []EnumBreak {
	if enums == nil {
		return nil
	}
	var out []EnumBreak
	for _, c := range d.EnumChanges {
		oldEnum, wasEnum := classifyEnum(enums.Old, c.Path)
		newEnum, isEnum := classifyEnum(enums.New, c.Path)
		switch {
		case wasEnum && len(c.Removed) > 0:
			out = append(out, EnumBreak{Path: c.Path, Enum: oldEnum, Removed: c.Removed, Added: c.Added})
		case !wasEnum && isEnum && enums.Old != nil:
			out = append(out, EnumBreak{Path: c.Path, Enum: newEnum, Added: c.Added, NewEnum: true})
		}
	}
	return out
}

func classifyEnum(classify EnumClassifier, path string) (string, bool) {
	if classify == nil {
		return "", false
	}
	return classify(path)
}

// Markdown renders the diff as a report titled
// "# docker-agent <from> → <to>: configuration schema changes". classify and enums may be nil;
// without enums the report has no "SDK-breaking changes" section.
func (d *Diff) Markdown(from, to string, classify Classifier, enums *Enums) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# docker-agent %s → %s: configuration schema changes\n\n", from, to)

	b.WriteString("## Summary\n\n")
	if d.Empty() {
		fmt.Fprintf(&b, "No configuration schema changes between %s and %s.\n\n", from, to)
	} else {
		fmt.Fprintf(&b, "%s.\n\n", upperFirst(d.Summary()))
		counts := []struct {
			label string
			n     int
		}{
			{"Definitions added", len(d.AddedDefinitions)},
			{"Definitions removed", len(d.RemovedDefinitions)},
			{"Properties added", len(d.AddedProperties)},
			{"Properties removed", len(d.RemovedProperties)},
			{"Types changed", len(d.TypeChanges)},
			{"Enums changed", len(d.EnumChanges)},
			{"Required lists changed", len(d.RequiredChanges)},
			{"Union constraints added", len(d.AddedConstraints)},
			{"Union constraints removed", len(d.RemovedConstraints)},
			{"Descriptions changed", len(d.DescriptionChanges)},
		}
		b.WriteString("| Change | Count |\n|---|---:|\n")
		for _, c := range counts {
			if c.n > 0 {
				fmt.Fprintf(&b, "| %s | %d |\n", c.label, c.n)
			}
		}
		b.WriteString("\n")
	}

	if enums != nil {
		writeSDKBreaking(&b, d.SDKBreaking(enums), enums)
	}

	b.WriteString("## Config version\n\n")
	v := d.Version
	switch {
	case v.LatestChanged():
		fmt.Fprintf(&b, "- latest config version: %s → %s\n", orNone(v.OldLatest), orNone(v.NewLatest))
	default:
		fmt.Fprintf(&b, "- latest config version: %s (unchanged)\n", orNone(v.NewLatest))
	}
	if len(v.Added) > 0 {
		fmt.Fprintf(&b, "- added versions: %s\n", codeList(v.Added))
	}
	if len(v.Removed) > 0 {
		fmt.Fprintf(&b, "- removed versions: %s\n", codeList(v.Removed))
	}
	b.WriteString("\n")

	writeDefinitions(&b, "Added definitions", d.AddedDefinitions)
	writeDefinitions(&b, "Removed definitions", d.RemovedDefinitions)

	if len(d.AddedProperties) > 0 {
		b.WriteString("## Added properties\n\n")
		if classify != nil {
			b.WriteString("| Path | Type | Description | Typed? |\n|---|---|---|---|\n")
		} else {
			b.WriteString("| Path | Type | Description |\n|---|---|---|\n")
		}
		for _, p := range d.AddedProperties {
			fmt.Fprintf(&b, "| %s | %s | %s |", tcode(p.Path), tcode(p.Type), cell(firstSentence(p.Description)))
			if classify != nil {
				if at, reason, ok := untypedPrefix(p.Path, classify); ok {
					fmt.Fprintf(&b, " no: %s |", untypedNote(p.Path, at, reason, tcode, cell))
				} else {
					b.WriteString(" yes |")
				}
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	if len(d.RemovedProperties) > 0 {
		b.WriteString("## Removed properties\n\n| Path | Type |\n|---|---|\n")
		for _, p := range d.RemovedProperties {
			fmt.Fprintf(&b, "| %s | %s |\n", tcode(p.Path), tcode(p.Type))
		}
		b.WriteString("\n")
	}

	if len(d.TypeChanges) > 0 {
		b.WriteString("## Changed types\n\n| Path | Old type | New type |\n|---|---|---|\n")
		for _, c := range d.TypeChanges {
			fmt.Fprintf(&b, "| %s | %s | %s |\n", tcode(c.Path), tcode(c.Old), tcode(c.New))
		}
		b.WriteString("\n")
	}

	if len(d.EnumChanges) > 0 {
		b.WriteString("## Changed enums\n\n| Path | Added values | Removed values |\n|---|---|---|\n")
		for _, c := range d.EnumChanges {
			fmt.Fprintf(&b, "| %s | %s | %s |\n", tcode(c.Path), tcodeList(c.Added), tcodeList(c.Removed))
		}
		b.WriteString("\n")
	}

	if len(d.RequiredChanges) > 0 {
		b.WriteString("## Changed required\n\n| Object | Now required | No longer required |\n|---|---|---|\n")
		for _, c := range d.RequiredChanges {
			fmt.Fprintf(&b, "| %s | %s | %s |\n", tcode(c.Path), tcodeList(c.Added), tcodeList(c.Removed))
		}
		b.WriteString("\n")
	}

	if len(d.AddedConstraints)+len(d.RemovedConstraints) > 0 {
		b.WriteString("## Changed union constraints\n\n")
		b.WriteString("Branches of `oneOf`/`anyOf`/`allOf` that re-declare a key of the owning object to constrain it.\n\n")
		for _, p := range d.AddedConstraints {
			fmt.Fprintf(&b, "- added %s: %s\n", code(p.Path), code(p.Type))
		}
		for _, p := range d.RemovedConstraints {
			fmt.Fprintf(&b, "- removed %s: %s\n", code(p.Path), code(p.Type))
		}
		b.WriteString("\n")
	}

	if n := len(d.DescriptionChanges); n > 0 {
		fmt.Fprintf(&b, "## Changed descriptions\n\n%d %s changed", n, pluralize(n, "description", "descriptions"))
		paths := make([]string, n)
		for i, p := range d.DescriptionChanges {
			paths[i] = code(p)
		}
		if n > 10 {
			fmt.Fprintf(&b, ".\n\n<details><summary>Paths</summary>\n\n%s\n\n</details>\n\n", strings.Join(paths, ", "))
		} else {
			fmt.Fprintf(&b, ": %s.\n\n", strings.Join(paths, ", "))
		}
	}

	b.WriteString("## Properties that landed in untyped (`any`) paths\n\n")
	switch untyped := d.Untyped(classify); {
	case classify == nil:
		b.WriteString("Not checked (no classifier).\n")
	case len(untyped) == 0:
		b.WriteString("None: every added or changed property maps to a typed input.\n")
	default:
		for _, u := range untyped {
			fmt.Fprintf(&b, "- %s: %s\n", code(u.Path), untypedNote(u.Path, u.At, u.Reason, code, mdText))
		}
	}
	return b.String()
}

func writeSDKBreaking(b *strings.Builder, breaks []EnumBreak, enums *Enums) {
	b.WriteString("## SDK-breaking changes\n\n")
	switch {
	case enums.Old == nil:
		b.WriteString("Not checked (the Pulumi enums of the previous schema are unknown).\n\n")
		return
	case len(breaks) == 0:
		b.WriteString("None: no Pulumi enum lost a value, and no string input became an enum.\n\n")
		return
	}
	b.WriteString("These changes break Go, .NET and Java programs that use the removed constants or pass a " +
		"string to a new enum, and TypeScript programs that pass a removed literal. A renamed value shows up " +
		"as removed and added. Call them out in the release notes.\n\n")
	b.WriteString("| Path | Pulumi enum | Removed values | Added values |\n|---|---|---|---|\n")
	for _, e := range breaks {
		enum := tcode(e.Enum)
		if e.NewEnum {
			enum += " (new: the input was a string)"
		}
		fmt.Fprintf(b, "| %s | %s | %s | %s |\n", tcode(e.Path), enum, tcodeList(e.Removed), tcodeList(e.Added))
	}
	b.WriteString("\n")
}

func writeDefinitions(b *strings.Builder, title string, defs []Definition) {
	if len(defs) == 0 {
		return
	}
	fmt.Fprintf(b, "## %s\n\n", title)
	for _, d := range defs {
		fmt.Fprintf(b, "- %s (%s)", code(d.Name), code(d.Type))
		if s := firstSentence(d.Description); s != "" {
			fmt.Fprintf(b, ": %s", mdText(s))
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
}

func untypedNote(path, at, reason string, codeFn, textFn func(string) string) string {
	note := textFn(reason)
	if note == "" {
		note = "untyped"
	}
	if at != path {
		note = fmt.Sprintf("under %s (%s)", codeFn(at), note)
	}
	return note
}

// code renders s as an inline code span (outside tables).
func code(s string) string {
	if s == "" {
		return ""
	}
	return "`" + s + "`"
}

// tcode renders s as an inline code span inside a table cell: GFM splits cells before parsing
// code spans, so pipes must be escaped even there.
func tcode(s string) string {
	return strings.ReplaceAll(code(s), "|", `\|`)
}

func codeList(values []string) string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = code(orEmpty(v))
	}
	return strings.Join(out, ", ")
}

func tcodeList(values []string) string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = tcode(orEmpty(v))
	}
	return strings.Join(out, ", ")
}

// orEmpty shows an empty enum value as "".
func orEmpty(v string) string {
	if v == "" {
		return `""`
	}
	return v
}

// cell makes s safe inside a Markdown table cell.
func cell(s string) string {
	return strings.ReplaceAll(mdText(s), "|", `\|`)
}

// mdText collapses whitespace and escapes angle brackets so that placeholders such as <name> are
// not swallowed as HTML by GitHub.
func mdText(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	return strings.NewReplacer("<", "&lt;", ">", "&gt;").Replace(s)
}

var abbreviations = map[string]bool{"e.g": true, "i.e": true, "etc": true, "vs": true, "eg": true, "ie": true, "approx": true}

const maxSentence = 160

// firstSentence returns the first sentence of s (whitespace collapsed, at most maxSentence runes).
func firstSentence(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	for i := 0; i < len(s); i++ {
		if s[i] != '.' || (i+1 < len(s) && s[i+1] != ' ') {
			continue
		}
		word := s[:i]
		if j := strings.LastIndexByte(word, ' '); j >= 0 {
			word = word[j+1:]
		}
		word = strings.ToLower(strings.TrimLeft(word, "(\"'`"))
		if abbreviations[word] {
			continue
		}
		s = s[:i+1]
		break
	}
	if utf8.RuneCountInString(s) > maxSentence {
		r := []rune(s)
		s = strings.TrimRight(string(r[:maxSentence-1]), " ") + "…"
	}
	return s
}

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	r, n := utf8.DecodeRuneInString(s)
	return strings.ToUpper(string(r)) + s[n:]
}
