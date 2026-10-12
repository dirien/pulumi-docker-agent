// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// source is one docs/yaml file.
type source struct {
	file     string // base name, e.g. agentconfig-examples.yaml
	examples []*example
}

// example is one document of a source file.
type example struct {
	header
	name      string            // the program's `name`
	program   string            // the Pulumi YAML program: the document from `name:` on
	comments  []comment         // comments to copy into the converted programs
	resources []string          // the resource names
	pcl       string            // the program in PCL, filled by the converter
	code      map[string]string // language -> program, filled by the converter
}

// header holds the keys before `name:`; they are not part of the program.
type header struct {
	Description string            `yaml:"description"`
	SkipPreview string            `yaml:"skipPreview"`
	Files       map[string]string `yaml:"files"`
	Notes       map[string]string `yaml:"notes"`
}

// comment is the comment above a resource, variable or config key of a program.
type comment struct {
	kind  string // resources, variables or config
	key   string
	lines []string // without the leading "# "
}

var (
	separatorRE = regexp.MustCompile(`(?m)^---[ \t]*$`)
	nameLineRE  = regexp.MustCompile(`(?m)^name:`)
)

func readSources(dir string) ([]*source, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no examples in %s", dir)
	}
	slices.Sort(files)
	var sources []*source
	for _, f := range files {
		b, err := os.ReadFile(f) //nolint:gosec // G304: the files of docs/yaml
		if err != nil {
			return nil, err
		}
		src, err := parseSource(filepath.Base(f), string(b))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		sources = append(sources, src)
	}
	return sources, nil
}

func parseSource(file, content string) (*source, error) {
	src := &source{file: file}
	names := map[string]bool{}
	for i, doc := range separatorRE.Split(content, -1) {
		if isBlankYAML(doc) {
			continue
		}
		ex, err := parseExample(doc)
		if err != nil {
			return nil, fmt.Errorf("document %d: %w", i, err)
		}
		if names[ex.name] {
			return nil, fmt.Errorf("two examples are named %q", ex.name)
		}
		names[ex.name] = true
		src.examples = append(src.examples, ex)
	}
	if len(src.examples) == 0 {
		return nil, errors.New("no examples")
	}
	return src, nil
}

// isBlankYAML reports whether a document has nothing but comments and blank lines.
func isBlankYAML(doc string) bool {
	for line := range strings.SplitSeq(doc, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			return false
		}
	}
	return true
}

func parseExample(doc string) (*example, error) {
	loc := nameLineRE.FindStringIndex(doc)
	if loc == nil {
		return nil, errors.New("no top-level `name:` key")
	}
	ex := &example{program: strings.TrimSpace(doc[loc[0]:]) + "\n"}

	dec := yaml.NewDecoder(strings.NewReader(doc[:loc[0]]))
	dec.KnownFields(true)
	if err := dec.Decode(&ex.header); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("the keys before `name:` (description, skipPreview, files, notes): %w", err)
	}
	if ex.Description == "" || strings.Contains(ex.Description, "\n") {
		return nil, errors.New("`description`, the example's title, must be one line")
	}
	for lang := range ex.Notes {
		if !slices.ContainsFunc(languages, func(l language) bool { return l.fence == lang }) {
			return nil, fmt.Errorf("notes: unknown language %q", lang)
		}
	}

	var program yaml.Node
	if err := yaml.Unmarshal([]byte(ex.program), &program); err != nil {
		return nil, fmt.Errorf("the program: %w", err)
	}
	if len(program.Content) != 1 || program.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("the program is not a YAML mapping")
	}
	top := program.Content[0]
	for i := 0; i+1 < len(top.Content); i += 2 {
		key, value := top.Content[i], top.Content[i+1]
		switch key.Value {
		case "name":
			ex.name = value.Value
		case "description":
			return nil, errors.New("put `description` (the title) before `name:`")
		case "plugins":
			return nil, errors.New("the program must not declare plugins: docsgen adds the provider binary")
		case "resources", "variables", "config":
			if value.Kind != yaml.MappingNode {
				continue
			}
			for j := 0; j+1 < len(value.Content); j += 2 {
				k := value.Content[j]
				if key.Value == "resources" {
					ex.resources = append(ex.resources, k.Value)
				}
				if lines := commentLines(k.HeadComment); len(lines) > 0 {
					ex.comments = append(ex.comments, comment{kind: key.Value, key: k.Value, lines: lines})
				}
			}
		}
	}
	if ex.name == "" {
		return nil, errors.New("the program has no name")
	}
	if strings.Contains(ex.program, "```") {
		return nil, errors.New("the program must not contain ``` (it is embedded in Markdown)")
	}
	return ex, nil
}

// commentLines turns a YAML head comment into its text lines.
func commentLines(c string) []string {
	if strings.TrimSpace(c) == "" {
		return nil
	}
	var lines []string
	for line := range strings.SplitSeq(strings.TrimSpace(c), "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "#")
		lines = append(lines, strings.TrimPrefix(line, " "))
	}
	return lines
}

// pulumiYAML is the Pulumi.yaml docsgen converts: the program, using the provider binary.
func (ex *example) pulumiYAML(binDir string) []byte {
	var b bytes.Buffer
	b.WriteString(ex.program)
	fmt.Fprintf(&b, "plugins:\n  providers:\n    - name: docker-agent\n      path: %q\n", binDir)
	return b.Bytes()
}
