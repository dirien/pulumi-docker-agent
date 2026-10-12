// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const dockerAgentModule = "github.com/docker/docker-agent"

type module struct {
	Version string
	Dir     string
}

// goListModule resolves the docker-agent module required by the provider's go.mod. It
// downloads the module if the module cache does not have it.
func goListModule(providerDir string) (module, error) {
	run := func(args ...string) ([]byte, error) {
		cmd := exec.Command("go", args...)
		cmd.Dir = providerDir
		cmd.Stderr = os.Stderr
		return cmd.Output()
	}
	out, err := run("list", "-m", "-json", dockerAgentModule)
	if err != nil {
		return module{}, fmt.Errorf("go list -m %s: %w", dockerAgentModule, err)
	}
	var m module
	if err := json.Unmarshal(out, &m); err != nil {
		return module{}, err
	}
	if m.Dir == "" {
		if _, err := run("mod", "download", dockerAgentModule); err != nil {
			return module{}, fmt.Errorf("go mod download %s: %w", dockerAgentModule, err)
		}
		if out, err = run("list", "-m", "-json", dockerAgentModule); err != nil {
			return module{}, err
		}
		if err := json.Unmarshal(out, &m); err != nil {
			return module{}, err
		}
	}
	if m.Dir == "" || m.Version == "" {
		return module{}, fmt.Errorf("could not locate %s in the module cache", dockerAgentModule)
	}
	return m, nil
}

// syncUpstream copies the pinned schema and the upstream example configurations from the
// module cache into the provider tree: every `*.yaml` and `*.hcl` file under examples/,
// subdirectories included, the Markdown files next to them (`instruction_file`, the HCL
// `file()` helper), and the text files the HCL `file()` helper reads. The paths of the other
// example files (Go programs, Dockerfiles, RAG documents, ...) are listed in otherFilesList,
// so the example generator can say why they are not converted.
func syncUpstream(m module, schemaOut, examplesOut string) error {
	data, err := os.ReadFile(filepath.Join(m.Dir, "agent-schema.json"))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(schemaOut), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(schemaOut, data, 0o644); err != nil { //nolint:gosec // committed source file
		return err
	}

	if err := os.RemoveAll(examplesOut); err != nil {
		return err
	}
	src, err := os.OpenRoot(filepath.Join(m.Dir, "examples"))
	if err != nil {
		return err
	}
	defer src.Close() //nolint:errcheck // read-only
	var others []string
	err = fs.WalkDir(src.FS(), ".", func(rel string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !vendoredExample(rel) {
			others = append(others, rel)
			return nil
		}
		b, err := src.ReadFile(rel)
		if err != nil {
			return err
		}
		out := filepath.Join(examplesOut, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		return os.WriteFile(out, b, 0o644) //nolint:gosec // testdata
	})
	if err != nil {
		return err
	}
	// The text files HCL examples read with file(), which the example generator copies next to
	// the programs.
	reads, err := hclReads(src.FS(), others)
	if err != nil {
		return err
	}
	for _, rel := range reads {
		b, err := src.ReadFile(rel)
		if err != nil {
			return err
		}
		out := filepath.Join(examplesOut, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(out, b, 0o644); err != nil { //nolint:gosec // testdata
			return err
		}
		others = slices.DeleteFunc(others, func(o string) bool { return o == rel })
	}
	slices.Sort(others)
	list := "# The example files of " + dockerAgentModule + " " + m.Version + " that are not copied here.\n" +
		strings.Join(others, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(examplesOut, otherFilesList), []byte(list), 0o644); err != nil { //nolint:gosec // testdata
		return err
	}
	readme := fmt.Sprintf("These are the example configurations of %s %s (examples/**/*.yaml and *.hcl,\n"+
		"with the Markdown files they read), copied by `make generate_config_types`. %s lists the\n"+
		"other example files. Do not edit them: TestRoundTripUpstreamExamples decodes each configuration\n"+
		"into the typed inputs, renders it again and compares what docker-agent loads, and\n"+
		"provider/internal/examplegen turns each one into the Pulumi programs in\n"+
		"examples/docker-agent-examples.\n",
		dockerAgentModule, m.Version, otherFilesList)
	return os.WriteFile(filepath.Join(examplesOut, "README.md"), []byte(readme), 0o644) //nolint:gosec // testdata
}

// hclFileCall matches the literal path argument of the HCL `file()` helper.
var hclFileCall = regexp.MustCompile(`\bfile\(\s*"([^"\\$]+)"`)

// hclReads returns the files among others (not vendored) that an HCL example of fsys reads with
// `file()`: text files (.txt) below the example's directory, slash-separated and relative to
// fsys. Other files are left to the example generator, which rejects them.
func hclReads(fsys fs.FS, others []string) ([]string, error) {
	var out []string
	err := fs.WalkDir(fsys, ".", func(rel string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || path.Ext(rel) != ".hcl" {
			return err
		}
		b, err := fs.ReadFile(fsys, rel)
		if err != nil {
			return err
		}
		for _, m := range hclFileCall.FindAllStringSubmatch(string(b), -1) {
			local := path.Clean(m[1])
			read := path.Join(path.Dir(rel), local)
			if path.Ext(read) == ".txt" && fs.ValidPath(local) && fs.ValidPath(read) && read != otherFilesList &&
				slices.Contains(others, read) && !slices.Contains(out, read) {
				out = append(out, read)
			}
		}
		return nil
	})
	slices.Sort(out)
	return out, err
}

// otherFilesList is the file in the vendored examples directory that lists the upstream
// example files that are not copied.
const otherFilesList = "other-files.txt"

// vendoredExample reports whether the upstream example file rel (slash-separated, relative to
// examples/) is copied: the configurations and the Markdown files next to them, but not the
// READMEs.
func vendoredExample(rel string) bool {
	switch path.Ext(rel) {
	case ".yaml", ".hcl":
		return true
	case ".md":
		return path.Base(rel) != "README.md"
	}
	return false
}

// mapLiteralKeys returns the sorted string keys of the map literal in function fn of a Go
// source file. It reads the registries of toolset types and model providers from the
// docker-agent sources instead of importing them (which would link every LLM SDK).
func mapLiteralKeys(file, fn string) ([]string, error) {
	f, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
	if err != nil {
		return nil, err
	}
	var keys []string
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		decl, ok := n.(*ast.FuncDecl)
		if !ok || decl.Name.Name != fn || decl.Body == nil {
			return true
		}
		ast.Inspect(decl.Body, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			if _, isMap := lit.Type.(*ast.MapType); !isMap {
				return true
			}
			found = true
			for _, elt := range lit.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				if bl, ok := kv.Key.(*ast.BasicLit); ok && bl.Kind == token.STRING {
					if k, err := strconv.Unquote(bl.Value); err == nil {
						keys = append(keys, k)
					}
				}
			}
			return false
		})
		return false
	})
	if !found {
		return nil, fmt.Errorf("no map literal in %s of %s; update provider/internal/schemagen for the new upstream code", fn, file)
	}
	slices.Sort(keys)
	return keys, nil
}

// upstreamLists returns the registered toolset types and native provider types.
func upstreamLists(m module) (toolsets, providers []string, err error) {
	toolsets, err = mapLiteralKeys(filepath.Join(m.Dir, "pkg", "teamloader", "toolsets", "toolsets.go"), "DefaultToolsetCreators")
	if err != nil {
		return nil, nil, err
	}
	factories, err := mapLiteralKeys(filepath.Join(m.Dir, "pkg", "model", "provider", "providers", "providers.go"), "DefaultFactories")
	if err != nil {
		return nil, nil, err
	}
	for _, f := range factories {
		if !strings.HasPrefix(f, "openai_") {
			providers = append(providers, f)
		}
	}
	return toolsets, providers, nil
}
