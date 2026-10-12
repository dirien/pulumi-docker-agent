// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

const indentUnit = "    "

// syntax is what differs between the TypeScript and the Python programs. Both are written in
// the style of the usual formatter (Prettier, Black): four-space indentation, double quotes,
// collections on one line when they fit and one item per line with a trailing comma
// otherwise.
type syntax struct {
	ts    bool
	width int
}

var (
	typescript = &syntax{ts: true, width: 100}
	python     = &syntax{ts: false, width: 88}
)

var tsIdent = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*$`)

func (s *syntax) key(k *propKey) string {
	if s.ts {
		if tsIdent.MatchString(k.ts) {
			return k.ts + ": "
		}
		return quote(k.ts, true) + ": "
	}
	return quote(k.py, false) + ": "
}

func (s *syntax) name(v *varExpr) string {
	if s.ts {
		return v.ts
	}
	return v.py
}

func (s *syntax) spread(v *varExpr) string {
	if s.ts {
		return "..." + v.ts
	}
	return "**" + v.py
}

func (s *syntax) scalar(e *scalarExpr) string {
	switch v := e.value.(type) {
	case nil:
		return map[bool]string{true: "null", false: "None"}[s.ts]
	case bool:
		if s.ts {
			return strconv.FormatBool(v)
		}
		return map[bool]string{true: "True", false: "False"}[v]
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		return formatFloat(v)
	case string:
		if e.enum && s.ts {
			return quote(v, true) + " as const"
		}
		return quote(v, s.ts)
	}
	panic(fmt.Sprintf("examplegen: unsupported scalar %T", e.value))
}

// formatFloat writes a float the way the provider renders it, with a decimal point.
func formatFloat(f float64) string {
	if math.IsInf(f, 0) || math.IsNaN(f) {
		panic("examplegen: non-finite float")
	}
	s := strconv.FormatFloat(f, 'f', -1, 64)
	if !strings.Contains(s, ".") {
		s += ".0"
	}
	return s
}

// commentLine writes a YAML comment line (`# text`) as a comment of the language.
func (s *syntax) commentLine(l string) string {
	if s.ts {
		return "//" + strings.TrimPrefix(l, "#")
	}
	return l
}

func (s *syntax) object(parts []string) string {
	if len(parts) == 0 {
		return "{}"
	}
	if s.ts {
		return "{ " + strings.Join(parts, ", ") + " }"
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

// isBlock reports whether a value is written as a multi-line string.
func isBlock(e expr) bool {
	s, ok := e.(*scalarExpr)
	if !ok {
		return false
	}
	str, ok := s.value.(string)
	return ok && strings.Contains(str, "\n") && strings.TrimSpace(str) != ""
}

// block writes a multi-line string: a template literal (TypeScript) or a triple-quoted string
// (Python) whose content starts on the next line, so every line of it starts at column 0 and
// reads exactly like the YAML block. The closing delimiter ends the last line.
func (s *syntax) block(str string) []string {
	var b strings.Builder
	if s.ts {
		b.WriteString("`\\\n")
		for i, r := range str {
			switch {
			case r == '\\':
				b.WriteString(`\\`)
			case r == '`':
				b.WriteString("\\`")
			case r == '$' && strings.HasPrefix(str[i:], "${"):
				b.WriteString(`\$`)
			case r == '\n' || r == '\t':
				b.WriteRune(r)
			default:
				writeEscaped(&b, r)
			}
		}
		b.WriteString("`")
		return strings.Split(b.String(), "\n")
	}
	b.WriteString(`"""\` + "\n")
	for i, r := range str {
		switch {
		case r == '\\':
			b.WriteString(`\\`)
		case r == '"' && (i == len(str)-1 || str[i+1] == '"'):
			// Never two quotes in a row, and never a quote right before the closing ones.
			b.WriteString(`\"`)
		case r == '\n' || r == '\t' || r == '"':
			b.WriteRune(r)
		default:
			writeEscaped(&b, r)
		}
	}
	b.WriteString(`"""`)
	return strings.Split(b.String(), "\n")
}

// quote writes a single-line string literal, in double quotes unless the string contains
// double quotes and no single quotes (as Prettier and Black do).
func quote(str string, ts bool) string {
	q := '"'
	if strings.ContainsRune(str, '"') && !strings.ContainsRune(str, '\'') {
		q = '\''
	}
	var b strings.Builder
	b.WriteRune(q)
	for _, r := range str {
		switch r {
		case q, '\\':
			b.WriteRune('\\')
			b.WriteRune(r)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		default:
			writeEscaped(&b, r)
		}
	}
	b.WriteRune(q)
	return b.String()
}

// writeEscaped writes r, escaping control characters and the line and paragraph separators.
func writeEscaped(b *strings.Builder, r rune) {
	switch {
	case r == '\r':
		b.WriteString(`\r`)
	case r < 0x20 || r == 0x7f:
		fmt.Fprintf(b, `\x%02x`, r)
	case r == 0x2028 || r == 0x2029:
		fmt.Fprintf(b, `\u%04x`, r)
	default:
		b.WriteRune(r)
	}
}

// printer writes a program, line by line.
type printer struct {
	syn   *syntax
	skip  *skipReason // the reason for skipValidation of the program being written
	lines []string
}

func (p *printer) out(indent int, s string) {
	p.lines = append(p.lines, strings.Repeat(indentUnit, indent)+s)
}

// blank adds an empty line, unless the previous line is empty or opens a bracket.
func (p *printer) blank() {
	if len(p.lines) == 0 {
		return
	}
	last := strings.TrimSpace(p.lines[len(p.lines)-1])
	if last == "" || strings.HasSuffix(last, "{") || strings.HasSuffix(last, "[") || strings.HasSuffix(last, "(") {
		return
	}
	p.lines = append(p.lines, "")
}

func (p *printer) String() string {
	for len(p.lines) > 0 && p.lines[len(p.lines)-1] == "" {
		p.lines = p.lines[:len(p.lines)-1]
	}
	return strings.Join(p.lines, "\n") + "\n"
}

// comments writes comment lines: YAML comments, empty lines between groups of them, and the
// generated notes (word-wrapped).
func (p *printer) comments(indent int, lines []string) {
	lines = trimBlank(lines)
	marker := map[bool]string{true: "//", false: "#"}[p.syn.ts]
	for _, l := range lines {
		switch {
		case l == "":
			// Black removes empty lines inside brackets.
			if p.syn.ts || indent == 0 {
				p.blank()
			}
		case l == skipMarker || strings.HasPrefix(l, noteMarker):
			text := strings.TrimPrefix(l, noteMarker)
			if l == skipMarker {
				text = map[bool]string{true: p.skip.ts, false: p.skip.py}[p.syn.ts]
			}
			for _, w := range wrap(text, p.syn.width-len(indentUnit)*indent-len(marker)-1) {
				p.out(indent, marker+" "+w)
			}
		default:
			p.out(indent, p.syn.commentLine(l))
		}
	}
}

func trimBlank(lines []string) []string {
	for len(lines) > 0 && lines[0] == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// wrap splits text into lines of at most width runes at spaces.
func wrap(text string, width int) []string {
	var out []string
	line := ""
	for _, w := range strings.Fields(text) {
		if line != "" && utf8.RuneCountInString(line)+1+utf8.RuneCountInString(w) > width {
			out = append(out, line)
			line = ""
		}
		if line != "" {
			line += " "
		}
		line += w
	}
	if line != "" {
		out = append(out, line)
	}
	return out
}

// trailing returns a line comment for the end of a line.
func (p *printer) trailing(line []string) string {
	if len(line) == 0 {
		return ""
	}
	if p.syn.ts {
		return " " + p.syn.commentLine(line[0])
	}
	return "  " + line[0]
}

// entries writes the entries of an object or list body. kwargs writes object entries as
// Python keyword arguments.
func (p *printer) entries(es []*entry, indent int, kwargs bool) {
	for i, e := range es {
		// TypeScript: an empty line before a commented entry, as before a commented key in the
		// YAML (Prettier keeps it). A comment that stands for a left-out key stays with its
		// neighbours. Black removes empty lines inside brackets, so Python has none.
		if p.syn.ts && i > 0 && len(trimBlank(e.comment.Head)) > 0 && (e.value != nil || e.spread != nil) {
			p.blank()
		}
		p.entry(e, indent, kwargs)
	}
}

func (p *printer) entry(e *entry, indent int, kwargs bool) {
	head, line, foot := e.comment.Head, e.comment.Line, e.comment.Foot
	if e.value == nil && e.spread == nil {
		p.comments(indent, append(append(append([]string{}, head...), line...), foot...))
		return
	}
	if len(line) > 1 || (e.value != nil && isBlock(e.value)) {
		head, line = append(append([]string{}, head...), line...), nil
	}
	p.comments(indent, head)
	switch {
	case e.spread != nil:
		p.out(indent, p.syn.spread(e.spread)+","+p.trailing(line))
	case e.key == nil:
		p.value(indent, "", e.value, ",", line)
	case kwargs:
		p.value(indent, e.key.py+"=", e.value, ",", line)
	case p.syn.ts && isVar(e.value, e.key.ts):
		// TypeScript's shorthand for `name: name`.
		p.out(indent, e.key.ts+","+p.trailing(line))
	default:
		p.value(indent, p.syn.key(e.key), e.value, ",", line)
	}
	p.comments(indent, foot)
}

// value writes prefix, the value and suffix, starting at indent: on one line when it fits,
// otherwise with one entry per line.
func (p *printer) value(indent int, prefix string, v expr, suffix string, line []string) {
	if s, ok := p.inline(v); ok {
		text := prefix + s + suffix
		_, scalar := v.(*scalarExpr)
		_, ref := v.(*varExpr)
		switch {
		case utf8.RuneCountInString(strings.Repeat(indentUnit, indent)+text) <= p.syn.width:
			p.out(indent, text+p.trailing(line))
			return
		case p.syn.ts && scalar && strings.HasSuffix(prefix, ": ") && utf8.RuneCountInString(prefix)-2 >= len(indentUnit)+3:
			// Prettier moves a string that does not fit to the next line, unless the key is
			// short (shorter than the indentation plus three).
			p.out(indent, strings.TrimSuffix(prefix, " "))
			p.out(indent+1, s+suffix+p.trailing(line))
			return
		case scalar || ref:
			p.out(indent, text+p.trailing(line))
			return
		}
	}
	open := func(bracket string) {
		if p.syn.ts && len(line) > 0 {
			// Prettier puts a comment after an opening bracket on the next line.
			p.out(indent, prefix+bracket)
			p.comments(indent+1, line)
			return
		}
		p.out(indent, prefix+bracket+p.trailing(line))
	}
	switch v := v.(type) {
	case *objectExpr:
		open("{")
		p.entries(v.entries, indent+1, false)
		p.out(indent, "}"+suffix)
	case *listExpr:
		open("[")
		p.entries(v.items, indent+1, false)
		p.out(indent, "]"+suffix)
	case *scalarExpr:
		lines := p.syn.block(v.value.(string))
		p.out(indent, prefix+lines[0])
		p.lines = append(p.lines, lines[1:len(lines)-1]...)
		p.lines = append(p.lines, lines[len(lines)-1]+suffix)
	default:
		panic(fmt.Sprintf("examplegen: cannot write %T", v))
	}
}

// inline returns the one-line form of v, if it has one: no comments, no multi-line strings,
// no objects that must be written on several lines.
func (p *printer) inline(v expr) (string, bool) {
	switch v := v.(type) {
	case *scalarExpr:
		if isBlock(v) {
			return "", false
		}
		return p.syn.scalar(v), true
	case *varExpr:
		return p.syn.name(v), true
	case *objectExpr:
		if v.multiline {
			return "", false
		}
		parts := make([]string, 0, len(v.entries))
		for _, e := range v.entries {
			if hasComments(e) {
				return "", false
			}
			if e.spread != nil {
				parts = append(parts, p.syn.spread(e.spread))
				continue
			}
			s, ok := p.inline(e.value)
			if !ok {
				return "", false
			}
			parts = append(parts, p.syn.key(e.key)+s)
		}
		return p.syn.object(parts), true
	case *listExpr:
		if p.syn.ts && breaksArray(v) {
			return "", false
		}
		parts := make([]string, 0, len(v.items))
		for _, e := range v.items {
			if hasComments(e) {
				return "", false
			}
			s, ok := p.inline(e.value)
			if !ok {
				return "", false
			}
			parts = append(parts, s)
		}
		return "[" + strings.Join(parts, ", ") + "]", true
	}
	return "", false
}

// breaksArray reports whether Prettier always writes a list with one item per line: when it
// has several items and all of them are objects with more than one entry (or all are lists
// with more than one item).
func breaksArray(l *listExpr) bool {
	if len(l.items) < 2 {
		return false
	}
	objects, lists := true, true
	for _, e := range l.items {
		o, isObject := e.value.(*objectExpr)
		objects = objects && isObject && len(o.entries) > 1
		ll, isList := e.value.(*listExpr)
		lists = lists && isList && len(ll.items) > 1
	}
	return objects || lists
}

// isVar reports whether e refers to the variable name.
func isVar(e expr, name string) bool {
	v, ok := e.(*varExpr)
	return ok && v.ts == name
}

func hasComments(e *entry) bool {
	return e.value == nil && e.spread == nil || len(e.comment.Head)+len(e.comment.Line)+len(e.comment.Foot) > 0
}
