// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// apiExamples renders the Example Usage section of a resource or function description, in the
// shortcode format the Registry's API docs generator reads.
func apiExamples(src *source) string {
	var b strings.Builder
	b.WriteString("{{% examples %}}\n## Example Usage\n")
	for _, ex := range src.examples {
		fmt.Fprintf(&b, "{{%% example %%}}\n### %s\n\n", ex.Description)
		for _, lang := range languages {
			fmt.Fprintf(&b, "```%s\n%s```\n", lang.fence, ex.code[lang.fence])
		}
		b.WriteString("{{% /example %}}\n")
	}
	b.WriteString("{{% /examples %}}\n")
	return b.String()
}

// chooser renders an example as a language chooser of the package overview page.
func chooser(ex *example) string {
	var b strings.Builder
	names := make([]string, len(languages))
	for i, lang := range languages {
		names[i] = lang.fence
	}
	fmt.Fprintf(&b, "{{< chooser language %q >}}\n", strings.Join(names, ","))
	for _, lang := range languages {
		fmt.Fprintf(&b, "{{%% choosable language %s %%}}\n\n```%s\n%s```\n\n", lang.fence, lang.fence, ex.code[lang.fence])
		if note := strings.TrimSpace(ex.Notes[lang.fence]); note != "" {
			b.WriteString(note + "\n\n")
		}
		b.WriteString("{{% /choosable %}}\n")
	}
	b.WriteString("{{< /chooser >}}\n")
	return b.String()
}

var (
	indexBeginRE = regexp.MustCompile(`(?m)^<!-- docsgen begin: ([\w-]+)\b[^\n]*-->\n`)
	indexEndRE   = regexp.MustCompile(`(?m)^<!-- docsgen end -->\n`)
)

// updateIndex replaces the generated regions of the package overview page.
func updateIndex(path string, src *source) (bool, error) {
	old, err := os.ReadFile(path) //nolint:gosec // G304: docs/_index.md
	if err != nil {
		return false, err
	}
	content, err := replaceRegions(string(old), src)
	if err != nil {
		return false, fmt.Errorf("%s: %w", path, err)
	}
	return writeIfChanged(path, []byte(content))
}

func replaceRegions(content string, src *source) (string, error) {
	byName := map[string]*example{}
	for _, ex := range src.examples {
		byName[ex.name] = ex
	}
	used := map[string]bool{}
	var b strings.Builder
	rest := content
	for {
		begin := indexBeginRE.FindStringSubmatchIndex(rest)
		if begin == nil {
			break
		}
		name := rest[begin[2]:begin[3]]
		ex, ok := byName[name]
		if !ok {
			return "", fmt.Errorf("no example named %q in %s", name, src.file)
		}
		used[name] = true
		end := indexEndRE.FindStringIndex(rest[begin[1]:])
		if end == nil {
			return "", fmt.Errorf("`<!-- docsgen begin: %s -->` has no `<!-- docsgen end -->`", name)
		}
		b.WriteString(rest[:begin[1]])
		b.WriteString(chooser(ex))
		rest = rest[begin[1]+end[0]:]
	}
	b.WriteString(rest)
	var missing []string
	for _, ex := range src.examples {
		if !used[ex.name] {
			missing = append(missing, ex.name)
		}
	}
	if len(missing) > 0 {
		return "", errors.New("no `<!-- docsgen begin: <name> -->` region for " + strings.Join(missing, ", "))
	}
	return b.String(), nil
}
