// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

// Package agentsignore renders and validates docker-agent `.agentsignore` files.
//
// docker-agent (pkg/fsx/agentsignore.go) reads the file line by line, trims every line,
// drops blank lines and lines starting with `#`, and hands the rest to go-git's gitignore
// parser (github.com/go-git/go-git/v5/plumbing/format/gitignore), the same library used
// here. The validation rejects patterns that this pipeline would silently change or that
// go-git can never match, so the file always means what the program says.
package agentsignore

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
)

// FileName is the name docker-agent looks for, at or above the working directory.
const FileName = ".agentsignore"

// Render returns the file content: the optional header comment (each line prefixed with
// `# `), a blank line after it, then one pattern per line in the given order. The result
// always ends with a newline unless it is empty.
func Render(comment string, patterns []string) string {
	var b strings.Builder
	if c := strings.TrimRight(comment, "\r\n\t "); c != "" {
		for _, line := range strings.Split(strings.ReplaceAll(c, "\r\n", "\n"), "\n") {
			line = strings.TrimRight(line, " \t\r")
			if line == "" {
				b.WriteString("#\n")
				continue
			}
			b.WriteString("# " + line + "\n")
		}
		if len(patterns) > 0 {
			b.WriteString("\n")
		}
	}
	for _, p := range patterns {
		b.WriteString(p + "\n")
	}
	return b.String()
}

// SHA256 returns the hex-encoded SHA-256 of s.
func SHA256(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// Issue is a validation problem with one pattern.
type Issue struct {
	// Index is the position of the pattern in the input list.
	Index int
	// Message explains the problem. It quotes the pattern.
	Message string
}

func (i Issue) String() string { return fmt.Sprintf("patterns[%d]: %s", i.Index, i.Message) }

// Validate checks the patterns. Errors are patterns docker-agent would read differently
// from what was written, or that can never match. Warnings are patterns that are valid
// but probably not what was meant.
func Validate(patterns []string) (errs, warnings []Issue) {
	seen := map[string]int{}
	for i, p := range patterns {
		if msg := checkPattern(p); msg != "" {
			errs = append(errs, Issue{i, msg})
			continue
		}
		if j, dup := seen[p]; dup {
			warnings = append(warnings, Issue{i, fmt.Sprintf("%q repeats patterns[%d]", p, j)})
		} else {
			seen[p] = i
		}
		if strings.Contains(p, " #") || strings.Contains(p, "\t#") {
			warnings = append(warnings, Issue{i, fmt.Sprintf("%q: gitignore syntax has no trailing comments, "+
				"so everything after the `#` is part of the pattern; use the `comment` input for notes", p)})
		}
	}
	if len(errs) == 0 {
		warnings = append(warnings, ineffectiveNegations(patterns)...)
	}
	return errs, warnings
}

func checkPattern(p string) string {
	switch {
	case strings.ContainsAny(p, "\n\r"):
		return fmt.Sprintf("%q contains a line break; give each pattern its own list entry", p)
	case strings.TrimSpace(p) == "":
		return "empty pattern; docker-agent skips blank lines"
	case strings.TrimSpace(p) != p:
		return fmt.Sprintf("%q has leading or trailing whitespace, which docker-agent trims; remove it "+
			"(escape a significant leading character with `\\`)", p)
	case strings.HasPrefix(p, "#"):
		return fmt.Sprintf("%q would be read as a comment; escape it as `\\%s` to match a name starting "+
			"with `#`, or use the `comment` input for notes", p, p)
	}
	body := strings.TrimPrefix(p, "!")
	core := strings.TrimSuffix(strings.TrimPrefix(body, "/"), "/")
	if core == "" {
		return fmt.Sprintf("%q matches nothing", p)
	}
	isGlob := strings.Contains(strings.TrimSuffix(body, "/"), "/")
	for _, seg := range strings.Split(core, "/") {
		if seg == "" {
			continue
		}
		if _, err := filepath.Match(seg, ""); err != nil {
			return fmt.Sprintf("%q: invalid glob %q (%v); go-git never matches it", p, seg, err)
		}
		if isGlob && seg != "**" && strings.Contains(seg, "**") {
			return fmt.Sprintf("%q: %q mixes `**` with other characters, which go-git never matches; use "+
				"`**` as a whole path segment (for example `docs/**/*.md`) or a single `*`", p, seg)
		}
	}
	return ""
}

// ineffectiveNegations warns about literal negations (`!name` without wildcards) that no
// earlier pattern excludes. go-git evaluates the last matching pattern, so such a
// negation re-includes nothing. Patterns with wildcards are not checked because no single
// sample path represents them.
func ineffectiveNegations(patterns []string) []Issue {
	var out []Issue
	for i, p := range patterns {
		if !strings.HasPrefix(p, "!") {
			continue
		}
		target := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(p, "!"), "/"), "/")
		if strings.ContainsAny(target, `*?[\`) {
			continue
		}
		isDir := strings.HasSuffix(p, "/")
		path := strings.Split(target, "/")
		excluded := false
		for _, earlier := range patterns[:i] {
			if strings.HasPrefix(earlier, "!") {
				continue
			}
			if gitignore.ParsePattern(earlier, nil).Match(path, isDir) == gitignore.Exclude {
				excluded = true
			}
		}
		if !excluded {
			out = append(out, Issue{i, fmt.Sprintf("%q re-includes %q, but no earlier pattern excludes it; "+
				"negations only take effect after a pattern that matches", p, target)})
		}
	}
	return out
}

// Matcher builds a go-git matcher for the patterns, anchored at the directory that
// contains the `.agentsignore` file, exactly like docker-agent does.
func Matcher(patterns []string) gitignore.Matcher {
	parsed := make([]gitignore.Pattern, 0, len(patterns))
	for _, p := range patterns {
		parsed = append(parsed, gitignore.ParsePattern(p, nil))
	}
	return gitignore.NewMatcher(parsed)
}
