// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

// Command examplegen turns every example configuration of the pinned docker-agent release into
// a Pulumi program in TypeScript and Python that builds it with AgentConfig and typed inputs.
//
// It reads the example configurations that `schemagen generate` vendors into
// pkg/render/testdata/upstream, decodes each one with render.DecodeYAML (keeping the YAML
// comments, the key order and the anchors), and writes examples/docker-agent-examples:
//
//	README.md                      what the programs are, and a table of every example
//	typescript/index.ts            imports every example and exports the stack output `configs`
//	typescript/examples/<name>.ts  one AgentConfig per example
//	python/__main__.py             the same for Python
//	python/examples/<module>.py
//	{typescript,python}/examples/<path>  copies of the .md and .txt files HCL examples read
//	testdata/<name>.yaml           what each program renders: render.Render of the decoded example
//
// `make generate_config_types` runs it after schemagen, so the programs follow every
// docker-agent upgrade. The hand-written project files (Pulumi.yaml, package.json, ...) are
// left alone.
package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// Paths relative to the provider module root.
const (
	upstreamDir    = "pkg/render/testdata/upstream"
	outDir         = "../examples/docker-agent-examples"
	otherFilesList = "other-files.txt"
	providerModule = "module github.com/dirien/pulumi-docker-agent/provider"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "examplegen:", err)
		os.Exit(1)
	}
}

func run() error {
	root, err := providerRoot()
	if err != nil {
		return err
	}
	files, examples, err := generate(filepath.Join(root, upstreamDir))
	if err != nil {
		return err
	}
	if err := write(filepath.Join(root, outDir), files); err != nil {
		return err
	}
	fmt.Printf("examplegen: %d docker-agent examples -> %s\n", len(examples), filepath.Clean(filepath.Join(root, outDir)))
	return nil
}

// providerRoot finds the provider module root from the working directory.
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
			return "", errors.New("run examplegen inside the provider module (provider/go.mod not found)")
		}
		dir = parent
	}
}

// generatedFile reports whether rel (slash-separated, relative to the output directory) is a
// file examplegen writes. Only these are removed before writing, so the hand-written project
// files and local state (node_modules, venv) stay. The publish job of
// .github/workflows/upgrade-docker-agent.yml accepts the same paths from an upgrade
// (TestUpgradeWorkflowAcceptsGeneratedFiles checks that both agree).
func generatedFile(rel string) bool {
	dir, base := path.Dir(rel), path.Base(rel)
	ext := path.Ext(base)
	if lang, read, ok := strings.Cut(rel, "/examples/"); ok && (lang == "typescript" || lang == "python") &&
		copiedRead.MatchString(read) {
		return true // a copy of a file an HCL example reads
	}
	switch dir {
	case ".":
		return base == "README.md"
	case "testdata":
		return ext == ".yaml"
	case "typescript":
		return base == "index.ts"
	case "python":
		return base == "__main__.py"
	case "typescript/examples":
		return ext == ".ts"
	case "python/examples":
		return ext == ".py"
	}
	return false
}

// write replaces the generated files under out with files (relative, slash-separated paths).
func write(out string, files map[string][]byte) error {
	var stale []string
	err := filepath.WalkDir(out, func(file string, d fs.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(out, file)
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "node_modules" || d.Name() == "venv" {
				return filepath.SkipDir
			}
			return nil
		}
		rel = filepath.ToSlash(rel)
		if _, keep := files[rel]; generatedFile(rel) && !keep {
			stale = append(stale, file)
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, f := range stale {
		if err := os.Remove(f); err != nil {
			return err
		}
		// Remove the directories of copied files that are left empty.
		for dir := filepath.Dir(f); dir != out && strings.HasPrefix(dir, out+string(filepath.Separator)); dir = filepath.Dir(dir) {
			if entries, err := os.ReadDir(dir); err != nil || len(entries) > 0 || os.Remove(dir) != nil {
				break
			}
		}
	}
	names := make([]string, 0, len(files))
	for rel := range files {
		names = append(names, rel)
	}
	slices.Sort(names)
	for _, rel := range names {
		if !generatedFile(rel) {
			return fmt.Errorf("%s is not a generated path; update generatedFile and the allowed paths of "+
				".github/workflows/upgrade-docker-agent.yml", rel)
		}
		file := filepath.Join(out, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(file, files[rel], 0o644); err != nil { //nolint:gosec // generated example programs
			return err
		}
	}
	return nil
}
