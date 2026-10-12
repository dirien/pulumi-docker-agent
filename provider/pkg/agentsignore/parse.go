// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package agentsignore

import "strings"

// Parsed is an existing `.agentsignore` file as the inputs that render it.
type Parsed struct {
	// Comment is the comment block at the top of the file, without the `# ` prefixes.
	Comment string
	// Patterns are the patterns in order, trimmed the way docker-agent trims each line.
	Patterns []string
	// DroppedComments counts the comment lines after the first pattern. `comment` is a
	// header, so they cannot be kept.
	DroppedComments int
}

// Parse reads an `.agentsignore` file the way docker-agent does (pkg/fsx/agentsignore.go):
// every line is trimmed, and blank lines and lines starting with `#` are skipped. It is the
// inverse of Render: the comment lines before the first pattern become Comment, a blank line
// between them an empty comment line, and every other line a pattern. Render(p.Comment,
// p.Patterns) equals content for a file Render wrote; for other files it means the same to
// docker-agent.
func Parse(content string) Parsed {
	out := Parsed{Patterns: []string{}}
	var header []string
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if len(out.Patterns) == 0 {
			switch {
			case strings.HasPrefix(trimmed, "#"):
				text := strings.TrimPrefix(strings.TrimPrefix(trimmed, "#"), " ")
				header = append(header, strings.TrimRight(text, " \t"))
				continue
			case trimmed == "":
				if len(header) > 0 {
					header = append(header, "")
				}
				continue
			}
		}
		switch {
		case trimmed == "":
		case strings.HasPrefix(trimmed, "#"):
			out.DroppedComments++
		default:
			out.Patterns = append(out.Patterns, trimmed)
		}
	}
	for len(header) > 0 && header[len(header)-1] == "" {
		header = header[:len(header)-1]
	}
	out.Comment = strings.Join(header, "\n")
	return out
}
