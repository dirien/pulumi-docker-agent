// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

// Command docsgen writes the Example Usage sections of the API docs and of docs/_index.md in
// every language the Pulumi Registry shows (TypeScript, Python, Go, C#, Java, YAML and HCL) from
// examples written once in Pulumi YAML.
//
// Every docs/yaml/<name>-examples.yaml holds YAML documents separated by `---` lines:
//
//	---
//	description: Push an agent to a registry   # the example's title (required)
//	skipPreview: the example pushes to ghcr.io # optional, read by examples/docs_examples_test.go
//	files:                                     # optional files next to Pulumi.yaml, for fn::readFile
//	  agent.yaml: |
//	    ...
//	notes:                                     # optional, docs/_index.md only: a paragraph shown
//	  hcl: In HCL, ...                         # after the code of one language
//	name: publish                              # from here on: the Pulumi YAML program
//	runtime: yaml
//	resources:
//	  # A comment directly above a resource, variable or config key is copied into every language.
//	  artifact:
//	    ...
//
// Each program is converted with `pulumi convert --generate-only` against the provider binary in
// bin/ (through `plugins.providers`, so the provider plugin is not downloaded): to PCL first, then
// from PCL to every language. The results are checked and normalized: trailing whitespace, the
// plugin version and Terraform-only boilerplate are removed from HCL, and known program generator
// bugs are fixed (pulumi-java's package names, string enum values, fn::readFile and secret maps;
// Go's list values of untyped maps; a duplicate Python import). A program that a generator cannot
// convert, or that loses a resource name, fails the run; examples avoid such constructs. The YAML
// tab shows the program exactly as written.
//
// docs/yaml/index-examples.yaml fills the regions of docs/_index.md between
// `<!-- docsgen begin: <name> ... -->` and `<!-- docsgen end -->`; every other file becomes
// pkg/provider/embed/<name>-examples.md, which the resources and functions embed in their
// descriptions. `make docs` runs this command; `make codegen` runs `make docs` before `schema`.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
)

const providerModule = "module github.com/dirien/pulumi-docker-agent/provider"

// indexExamples is the source of the examples in docs/_index.md.
const indexExamples = "index-examples.yaml"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "docsgen:", err)
		os.Exit(1)
	}
}

type options struct {
	yamlDir string // docs/yaml
	outDir  string // pkg/provider/embed
	index   string // docs/_index.md
	binDir  string // the directory of pulumi-resource-docker-agent
	pulumi  string // the Pulumi CLI
	jobs    int
}

func run(args []string) error {
	root, err := providerRoot()
	if err != nil {
		return err
	}
	var opts options
	fs := flag.NewFlagSet("docsgen", flag.ContinueOnError)
	fs.StringVar(&opts.yamlDir, "yaml", filepath.Join(root, "..", "docs", "yaml"), "directory of the YAML examples")
	fs.StringVar(&opts.outDir, "out", filepath.Join(root, "pkg", "provider", "embed"), "directory of the generated Markdown")
	fs.StringVar(&opts.index, "index", filepath.Join(root, "..", "docs", "_index.md"), "the package overview page")
	fs.StringVar(&opts.binDir, "provider", filepath.Join(root, "..", "bin"), "directory of the provider binary")
	fs.StringVar(&opts.pulumi, "pulumi", "pulumi", "the Pulumi CLI")
	fs.IntVar(&opts.jobs, "jobs", min(runtime.NumCPU(), 8), "conversions to run in parallel")
	fix := fs.String("fix", "", "only apply the program generator fixes of this language (java, go or python) "+
		"to the program files given as arguments, in place")
	if err := fs.Parse(args); err != nil {
		return err
	}
	for _, p := range []*string{&opts.yamlDir, &opts.outDir, &opts.index, &opts.binDir} {
		if *p, err = filepath.Abs(*p); err != nil {
			return err
		}
	}
	binary := filepath.Join(opts.binDir, "pulumi-resource-docker-agent")
	if _, err := os.Stat(binary); err != nil {
		return fmt.Errorf("%w (run `make provider` first)", err)
	}
	if *fix != "" {
		return fixPrograms(opts.pulumi, binary, *fix, fs.Args())
	}

	sources, err := readSources(opts.yamlDir)
	if err != nil {
		return err
	}
	schema, err := loadSchema(opts.pulumi, binary)
	if err != nil {
		return err
	}
	conv := &converter{pulumi: opts.pulumi, binDir: opts.binDir, schema: schema}
	if err := conv.convertAll(sources, opts.jobs); err != nil {
		return err
	}

	var written []string
	for _, src := range sources {
		if src.file == indexExamples {
			changed, err := updateIndex(opts.index, src)
			if err != nil {
				return err
			}
			if changed {
				written = append(written, opts.index)
			}
			continue
		}
		path := filepath.Join(opts.outDir, strings.TrimSuffix(src.file, ".yaml")+".md")
		changed, err := writeIfChanged(path, []byte(apiExamples(src)))
		if err != nil {
			return err
		}
		if changed {
			written = append(written, path)
		}
	}
	if err := removeStale(opts.outDir, sources); err != nil {
		return err
	}
	for _, path := range written {
		fmt.Println("docsgen: wrote", path)
	}
	return nil
}

// removeStale deletes generated Markdown whose YAML source is gone.
func removeStale(outDir string, sources []*source) error {
	files, err := filepath.Glob(filepath.Join(outDir, "*-examples.md"))
	if err != nil {
		return err
	}
	for _, f := range files {
		yamlName := strings.TrimSuffix(filepath.Base(f), ".md") + ".yaml"
		if !slices.ContainsFunc(sources, func(s *source) bool { return s.file == yamlName }) {
			if err := os.Remove(f); err != nil {
				return err
			}
			fmt.Println("docsgen: removed", f)
		}
	}
	return nil
}

func writeIfChanged(path string, content []byte) (bool, error) {
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, content) {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return false, err
	}
	return true, os.WriteFile(path, content, 0o600)
}

// providerRoot finds the provider module from the working directory.
func providerRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		b, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err == nil && bytes.Contains(b, []byte(providerModule+"\n")) {
			return dir, nil
		}
		if _, err := os.Stat(filepath.Join(dir, "provider", "go.mod")); err == nil {
			return filepath.Join(dir, "provider"), nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("run docsgen inside the repository (provider/go.mod not found)")
		}
		dir = parent
	}
}
