package schemadiff

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// ChangelogEntry returns a one-line Keep-a-Changelog list item ("- Upgrade docker-agent from
// <from> to <to> (configuration version `"17"`, previously `"16"`): <Summary>; the new
// configuration keys are typed inputs of `AgentConfig`."). The configuration version part is
// omitted when the schema has no version enum, and the AgentConfig clause when no property was
// added. d may be nil, which is reported as "no configuration schema changes".
func ChangelogEntry(from, to string, d *Diff) string {
	var b strings.Builder
	fmt.Fprintf(&b, "- Upgrade docker-agent from %s to %s", from, to)
	if d != nil && d.Version.NewLatest != "" {
		if d.Version.LatestChanged() && d.Version.OldLatest != "" {
			fmt.Fprintf(&b, " (configuration version `%q`, previously `%q`)", d.Version.NewLatest, d.Version.OldLatest)
		} else {
			fmt.Fprintf(&b, " (configuration version `%q`)", d.Version.NewLatest)
		}
	}
	b.WriteString(": ")
	if d == nil || d.Empty() {
		b.WriteString("no configuration schema changes.")
		return b.String()
	}
	b.WriteString(d.Summary())
	if len(d.AddedProperties) > 0 {
		b.WriteString("; the new configuration keys are typed inputs of `AgentConfig`")
	}
	b.WriteString(".")
	return b.String()
}

// unreleased is the lower-cased heading prefix of the Unreleased section.
const unreleased = "## [unreleased]"

// linkReference matches Markdown link reference definitions such as `[0.1.0]: https://...`.
var linkReference = regexp.MustCompile(`^\[[^\]]+\]:\s`)

// PrependChangelog inserts entry (one or more Markdown lines) at the top of the `### Changed`
// subsection of the `## [Unreleased]` section of a Keep-a-Changelog document. A missing
// `### Changed` subsection is created (after `### Added` when present, otherwise right below the
// `## [Unreleased]` heading), and a missing `## [Unreleased]` section is created above the first
// release section. The function is idempotent: when the section already contains entry, the input
// is returned unchanged. CRLF line endings are preserved.
func PrependChangelog(changelog []byte, entry string) ([]byte, error) {
	entryLines := splitLines(strings.Trim(strings.ReplaceAll(entry, "\r\n", "\n"), "\n"))
	for i, l := range entryLines {
		entryLines[i] = strings.TrimRight(l, " \t")
	}
	if strings.TrimSpace(strings.Join(entryLines, "")) == "" {
		return nil, errors.New("schemadiff: empty changelog entry")
	}

	crlf := bytes.Contains(changelog, []byte("\r\n"))
	lines := splitLines(strings.ReplaceAll(string(changelog), "\r\n", "\n"))

	start := -1
	for i, l := range lines {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(l)), unreleased) {
			start = i
			break
		}
	}

	var out []string
	if start < 0 {
		pos := len(lines)
		for i, l := range lines {
			if strings.HasPrefix(l, "## ") || linkReference.MatchString(l) {
				pos = i
				break
			}
		}
		block := append([]string{"## [Unreleased]", "", "### Changed", ""}, entryLines...)
		out = spliceBlock(lines, pos, block)
	} else {
		end := len(lines)
		for i := start + 1; i < len(lines); i++ {
			if strings.HasPrefix(lines[i], "## ") || linkReference.MatchString(lines[i]) {
				end = i
				break
			}
		}
		if containsBlock(lines[start+1:end], entryLines) {
			return changelog, nil
		}
		out = insertIntoUnreleased(lines, start, end, entryLines)
	}

	text := strings.Join(out, "\n")
	if crlf {
		text = strings.ReplaceAll(text, "\n", "\r\n")
	}
	return []byte(text), nil
}

func insertIntoUnreleased(lines []string, start, end int, entry []string) []string {
	changed, added := -1, -1
	for i := start + 1; i < end; i++ {
		switch strings.ToLower(strings.TrimSpace(lines[i])) {
		case "### changed":
			if changed < 0 {
				changed = i
			}
		case "### added":
			if added < 0 {
				added = i
			}
		}
	}

	if changed >= 0 {
		// Put the entry at the top of the existing list.
		next := changed + 1
		for next < end && strings.TrimSpace(lines[next]) == "" {
			next++
		}
		out := append([]string{}, lines[:changed+1]...)
		out = append(out, "")
		out = append(out, entry...)
		if next >= end || !isListLine(lines[next]) {
			out = append(out, "")
		}
		return append(out, lines[next:]...)
	}

	pos := start + 1
	if added >= 0 {
		pos = added + 1
		for pos < end && !strings.HasPrefix(strings.TrimSpace(lines[pos]), "### ") {
			pos++
		}
	}
	return spliceBlock(lines, pos, append([]string{"### Changed", ""}, entry...))
}

// spliceBlock inserts block at pos, separated from the surrounding content by exactly one blank
// line (blank lines around pos are absorbed).
func spliceBlock(lines []string, pos int, block []string) []string {
	before := pos
	for before > 0 && strings.TrimSpace(lines[before-1]) == "" {
		before--
	}
	after := pos
	for after < len(lines) && strings.TrimSpace(lines[after]) == "" {
		after++
	}
	out := append([]string{}, lines[:before]...)
	if before > 0 {
		out = append(out, "")
	}
	out = append(out, block...)
	out = append(out, "") // blank separator line, or the final newline at EOF
	return append(out, lines[after:]...)
}

func containsBlock(section, block []string) bool {
	for i := 0; i+len(block) <= len(section); i++ {
		match := true
		for j, want := range block {
			if strings.TrimRight(section[i+j], " \t") != want {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func isListLine(l string) bool {
	t := strings.TrimLeft(l, " \t")
	return strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "* ") || strings.HasPrefix(t, "+ ")
}

func splitLines(s string) []string { return strings.Split(s, "\n") }
