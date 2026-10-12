// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package pcl

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"gopkg.in/yaml.v3"

	"github.com/dirien/pulumi-docker-agent/provider/pkg/render"
)

// LoadOptions select the files Load converts.
type LoadOptions struct {
	// Paths are the files and directories to convert, relative to the source directory. Empty
	// means the whole source directory.
	Paths []string
	// Exclude are directories (absolute) that are not searched, such as the directory the
	// converted program is written to.
	Exclude []string
}

// skipDirs are directories that never hold agent files of the project.
var skipDirs = []string{"node_modules", "venv", "__pycache__", "vendor"}

// Load reads every docker-agent configuration below dir: `*.yaml` and `*.yml` files with a
// top-level `agents` key, and docker-agent HCL files (`*.hcl`). Other files, hidden
// directories and Pulumi project files are skipped. The configurations are named after their
// path and sorted by name. A YAML and an HCL file of the same name in the same directory that
// hold the same configuration are converted once.
func Load(dir string, opts LoadOptions) ([]*Config, hcl.Diagnostics) {
	files, diags := findFiles(dir, opts)
	if diags.HasErrors() {
		return nil, diags
	}
	var configs []*Config
	for _, rel := range files {
		c, ds := loadFile(dir, rel)
		diags = append(diags, ds...)
		if c != nil {
			configs = append(configs, c)
		}
	}
	if diags.HasErrors() {
		return nil, diags
	}
	configs, ds := name(configs)
	return configs, append(diags, ds...)
}

func isConfigFile(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".yaml", ".yml":
		lower := strings.ToLower(name)
		return lower != "pulumi.yaml" && lower != "pulumi.yml" && !strings.HasPrefix(lower, "pulumi.")
	case ".hcl":
		return true
	}
	return false
}

// findFiles lists the candidate files, relative to dir and slash-separated, sorted.
func findFiles(dir string, opts LoadOptions) ([]string, hcl.Diagnostics) {
	roots := opts.Paths
	if len(roots) == 0 {
		roots = []string{"."}
	}
	excluded := func(p string) bool {
		return slices.ContainsFunc(opts.Exclude, func(x string) bool {
			rel, err := filepath.Rel(x, p)
			return err == nil && rel == "."
		})
	}
	seen := map[string]bool{}
	var out []string
	var diags hcl.Diagnostics
	for _, root := range roots {
		start := filepath.Join(dir, filepath.FromSlash(root))
		info, err := os.Stat(start)
		if err != nil {
			diags = append(diags, diagnostic(hcl.DiagError, root, "Cannot read the file", err.Error()))
			continue
		}
		if !info.IsDir() {
			rel, err := filepath.Rel(dir, start)
			if err != nil || strings.HasPrefix(rel, "..") {
				diags = append(diags, diagnostic(hcl.DiagError, root, "Outside the source directory", root+" is not inside "+dir+"."))
				continue
			}
			if rel = filepath.ToSlash(rel); !seen[rel] {
				seen[rel] = true
				out = append(out, rel)
			}
			continue
		}
		err = filepath.WalkDir(start, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if p != start && (strings.HasPrefix(d.Name(), ".") || slices.Contains(skipDirs, d.Name()) || excluded(p)) {
					return filepath.SkipDir
				}
				return nil
			}
			if !d.Type().IsRegular() || !isConfigFile(d.Name()) {
				return nil
			}
			rel, err := filepath.Rel(dir, p)
			if err != nil {
				return err
			}
			if rel = filepath.ToSlash(rel); !seen[rel] {
				seen[rel] = true
				out = append(out, rel)
			}
			return nil
		})
		if err != nil {
			diags = append(diags, diagnostic(hcl.DiagError, root, "Cannot read the file", err.Error()))
		}
	}
	slices.Sort(out)
	return out, diags
}

// diagnostic returns a diagnostic about a file (relative to the source directory). The CLI
// prints it as `<file>:1,1-1: <summary>; <detail>`.
func diagnostic(sev hcl.DiagnosticSeverity, file, summary, detail string) *hcl.Diagnostic {
	start := hcl.Pos{Line: 1, Column: 1}
	return &hcl.Diagnostic{
		Severity: sev,
		Summary:  summary,
		Detail:   detail,
		Subject:  &hcl.Range{Filename: file, Start: start, End: start},
	}
}

// loadFile loads one candidate file. It returns nil without diagnostics for a file that is
// not a docker-agent configuration.
func loadFile(dir, rel string) (*Config, hcl.Diagnostics) {
	file := filepath.Join(dir, filepath.FromSlash(rel))
	data, err := os.ReadFile(file)
	if err != nil {
		return nil, hcl.Diagnostics{diagnostic(hcl.DiagError, rel, "Cannot read the file", err.Error())}
	}
	if strings.EqualFold(filepath.Ext(rel), ".hcl") {
		return loadHCL(dir, rel, data)
	}
	return loadYAML(dir, rel, data)
}

// loadYAML loads a YAML file with a top-level `agents` key.
func loadYAML(dir, rel string, data []byte) (*Config, hcl.Diagnostics) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		if bytes.HasPrefix(data, []byte("agents:")) || bytes.Contains(data, []byte("\nagents:")) {
			return nil, hcl.Diagnostics{diagnostic(hcl.DiagError, rel, "Invalid YAML", err.Error())}
		}
		return nil, nil
	}
	if !hasAgents(&root) {
		return nil, nil
	}
	dec, err := render.DecodeYAML(data)
	if err != nil {
		return nil, hcl.Diagnostics{diagnostic(hcl.DiagError, rel, "Cannot convert the configuration", err.Error())}
	}
	c := &Config{Source: rel, Decoded: dec}
	return c, c.finish(dir)
}

// hasAgents reports whether a YAML document is a mapping with an `agents` key.
func hasAgents(root *yaml.Node) bool {
	if len(root.Content) == 0 {
		return false
	}
	top := root.Content[0]
	if top.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i+1 < len(top.Content); i += 2 {
		if top.Content[i].Value == "agents" {
			return true
		}
	}
	return false
}

// finish leaves out the keys docker-agent never reads, reports the other untyped keys, checks
// that the configuration renders, and handles `instruction_file`.
func (c *Config) finish(dir string) hcl.Diagnostics {
	var diags hcl.Diagnostics
	doc := reflect.ValueOf(&c.Decoded.Document).Elem()
	for _, p := range c.Decoded.Untyped {
		reason, unread := render.UnreadKey(p)
		if !unread {
			diags = append(diags, diagnostic(hcl.DiagWarning, c.Source, "Untyped key "+string(p),
				"No typed input of this provider version covers it, so it is passed through `extra` verbatim. "+
					"A newer provider version may have a typed input for it."))
			continue
		}
		parent, key, err := splitLast(p)
		if err != nil {
			return append(diags, diagnostic(hcl.DiagError, c.Source, "Cannot convert the configuration", err.Error()))
		}
		m, err := lookup(doc, parent)
		if err != nil {
			return append(diags, diagnostic(hcl.DiagError, c.Source, "Cannot convert the configuration", err.Error()))
		}
		line, err := yaml.Marshal(map[string]any{key: m.MapIndex(reflect.ValueOf(key)).Interface()})
		if err != nil {
			return append(diags, diagnostic(hcl.DiagError, c.Source, "Cannot convert the configuration", err.Error()))
		}
		m.SetMapIndex(reflect.ValueOf(key), reflect.Value{})
		c.addNote(p, fmt.Sprintf("Left out: %s. The original line: %s", reason, strings.TrimSpace(string(line))))
	}

	if _, err := render.Render(c.Decoded.Document); err != nil {
		return append(diags, diagnostic(hcl.DiagError, c.Source, "Cannot convert the configuration", err.Error()))
	}

	// docker-agent reads instruction files relative to the agent file, which the provider's
	// in-memory validation cannot do.
	base := path.Dir(c.Source)
	for _, a := range c.Decoded.Document.Agents {
		for _, f := range a.InstructionFile {
			c.SkipValidation = "`instruction_file` is read relative to the agent file when docker-agent loads it. " +
				"The provider validates the rendered YAML in memory, where there are no files, so validation is " +
				"skipped. The converter copied the instruction files next to the program."
			if rel, ok := inside(dir, base, f); ok {
				c.addFile(rel)
			}
		}
	}
	return diags
}

func (c *Config) addNote(p render.Path, text string) {
	if c.Notes == nil {
		c.Notes = map[render.Path][]string{}
	}
	c.Notes[p] = append(c.Notes[p], text)
}

func (c *Config) addFile(rel string) {
	if !slices.Contains(c.Files, rel) {
		c.Files = append(c.Files, rel)
	}
}

// inside resolves file, relative to the directory base of the source directory dir, and
// returns it relative to dir when it is a regular file inside dir.
func inside(dir, base, file string) (string, bool) {
	if path.IsAbs(file) || filepath.IsAbs(file) {
		return "", false
	}
	rel := path.Clean(path.Join(base, filepath.ToSlash(file)))
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return "", false
	}
	info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	return rel, true
}

// formats orders the files of one configuration in different formats: YAML first.
var formats = []string{".yaml", ".yml", ".hcl"}

// name names the configurations after their path, converts a configuration that is in the
// same directory in two formats once, and sorts them by name.
func name(configs []*Config) ([]*Config, hcl.Diagnostics) {
	stem := func(c *Config) string { return strings.TrimSuffix(c.Source, path.Ext(c.Source)) }
	rank := func(c *Config) int { return slices.Index(formats, strings.ToLower(path.Ext(c.Source))) }
	slices.SortFunc(configs, func(a, b *Config) int {
		if s := strings.Compare(stem(a), stem(b)); s != 0 {
			return s
		}
		return rank(a) - rank(b)
	})

	var diags hcl.Diagnostics
	var out []*Config
	for _, c := range configs {
		if n := len(out); n > 0 && stem(out[n-1]) == stem(c) {
			first := out[n-1]
			a, errA := canonical(first.Decoded.Document)
			b, errB := canonical(c.Decoded.Document)
			if errA == nil && errB == nil && a == b && len(c.ReadFiles) == 0 {
				first.Twins = append(first.Twins, c.Source)
				diags = append(diags, diagnostic(hcl.DiagWarning, c.Source, "Skipped",
					c.Source+" holds the same configuration as "+first.Source+", which is converted instead."))
				continue
			}
		}
		out = append(out, c)
	}

	taken := map[string]bool{}
	for _, c := range out {
		base := strings.ReplaceAll(stem(c), "/", "-")
		if taken[base] {
			base += "-" + strings.TrimPrefix(strings.ToLower(path.Ext(c.Source)), ".")
		}
		n := base
		for i := 2; taken[n]; i++ {
			n = base + "-" + strconv.Itoa(i)
		}
		taken[n] = true
		c.Name = n
	}
	slices.SortFunc(out, func(a, b *Config) int { return strings.Compare(a.Name, b.Name) })
	return out, diags
}

// canonical renders a configuration with every command that only has an instruction in the
// short form (`name: instruction`), which docker-agent reads like the object form. An HCL file
// can only write the object form, so this is how its YAML twin is recognised.
func canonical(doc render.Document) (string, error) {
	y, err := render.Render(doc)
	if err != nil {
		return "", err
	}
	dec, err := render.DecodeYAML([]byte(y))
	if err != nil {
		return "", err
	}
	d := dec.Document
	for i := range d.Agents {
		d.Agents[i].Commands, d.Agents[i].SimpleCommands = shortCommands(d.Agents[i].Commands, d.Agents[i].SimpleCommands)
	}
	for k, g := range d.Commands {
		g.Commands, g.SimpleCommands = shortCommands(g.Commands, g.SimpleCommands)
		d.Commands[k] = g
	}
	return render.Render(d)
}

func shortCommands(cmds map[string]render.Command, simple map[string]string) (map[string]render.Command, map[string]string) {
	for name, c := range cmds {
		if c.Instruction == nil || c.Description != nil || c.Agent != nil || c.URL != nil || len(c.Extra) > 0 {
			continue
		}
		if simple == nil {
			simple = map[string]string{}
		}
		simple[name] = *c.Instruction
		delete(cmds, name)
	}
	if len(cmds) == 0 {
		cmds = nil
	}
	return cmds, simple
}
