// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package pcl

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// quote returns s as a PCL string literal. PCL strings are templates, so `${` and `%{` are
// escaped as `$${` and `%%{`; the other escapes are the HCL ones.
func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i, r := range s {
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case (r == '$' || r == '%') && strings.HasPrefix(s[i+1:], "{"):
			b.WriteRune(r)
			b.WriteRune(r)
		case r == utf8.RuneError || r < 0x20 || r == 0x7f || r == 0x2028 || r == 0x2029:
			fmt.Fprintf(&b, `\u%04X`, r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// heredocMarker returns the marker of a heredoc for s, or "" when s is not written as a heredoc:
// only a string that ends with a newline can be one (the heredoc's value always ends with the
// newline before the closing marker), and it must not contain characters a heredoc cannot hold
// literally. Nor may it contain a template sequence (`${`, `%{`): the HCL program generator
// (pulumi-hcl) drops their escapes in a heredoc, while it keeps them in a quoted string.
func heredocMarker(s string) string {
	if !strings.HasSuffix(s, "\n") || strings.TrimSpace(s) == "" || !utf8.ValidString(s) ||
		strings.ContainsFunc(s, notInHeredoc) || strings.Contains(s, "${") || strings.Contains(s, "%{") {
		return ""
	}
	lines := strings.Split(s, "\n")
	for i := 0; ; i++ {
		marker := "EOT"
		if i > 0 {
			marker += strconv.Itoa(i + 1)
		}
		if !slices.ContainsFunc(lines, func(l string) bool { return strings.TrimSpace(l) == marker }) {
			return marker
		}
	}
}

// notInHeredoc reports whether a heredoc cannot hold r literally: a carriage return, an
// invalid byte or a control character other than a newline or a tab.
func notInHeredoc(r rune) bool {
	return r == '\r' || r == utf8.RuneError || (r < 0x20 && r != '\n' && r != '\t') || r == 0x7f
}

var identifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// objectKey returns a map key as a PCL object key: as is when it is a plain identifier,
// otherwise quoted.
func objectKey(k string) string {
	if identifier.MatchString(k) && k != "true" && k != "false" && k != "null" {
		return k
	}
	return quote(k)
}

// formatFloat writes a float with a decimal point, so it stays a float.
func formatFloat(f float64) string {
	s := strconv.FormatFloat(f, 'f', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}

// Identifier returns the camelCase PCL identifier of a resource name (a file name):
// `basic_agent` -> `basicAgent`, `dhi/dhi-agent` -> `dhiDhiAgent`. Characters that are not
// letters or digits separate words; a name that starts with a digit gets the prefix `agent`.
func Identifier(name string) string {
	var b strings.Builder
	upper := false
	for _, r := range name {
		switch {
		case r < utf8.RuneSelf && (unicode.IsLetter(r) || unicode.IsDigit(r)):
			if upper && b.Len() > 0 {
				r = unicode.ToUpper(r)
			}
			b.WriteRune(r)
			upper = false
		default:
			upper = true
		}
	}
	id := b.String()
	switch {
	case id == "":
		return "agent"
	case unicode.IsDigit(rune(id[0])):
		return "agent" + id
	case unicode.IsUpper(rune(id[0])):
		return strings.ToLower(id[:1]) + id[1:]
	}
	return id
}

// wrap splits text into lines of at most width runes, at spaces.
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

// commentText returns the text of a comment line (`# text` -> `text`), keeping any further
// indentation.
func commentText(line string) string {
	line = strings.TrimRight(line, " \t")
	switch {
	case strings.HasPrefix(line, "#"):
		line = line[1:]
	case strings.HasPrefix(line, "//"):
		line = line[2:]
	}
	return strings.TrimPrefix(line, " ")
}

func trimBlank(lines []string) []string {
	for len(lines) > 0 && strings.TrimSpace(commentText(lines[0])) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(commentText(lines[len(lines)-1])) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

var languageServer = regexp.MustCompile(`^#\s*yaml-language-server:`)

// dropDirectives removes the editor and shell directives from a comment block: a
// `yaml-language-server` modeline, or a shebang on the first line of the file.
func dropDirectives(lines []string, first bool) []string {
	var out []string
	for i, l := range lines {
		if languageServer.MatchString(l) || (first && i == 0 && strings.HasPrefix(l, "#!")) {
			continue
		}
		out = append(out, l)
	}
	return trimBlank(out)
}
