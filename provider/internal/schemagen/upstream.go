// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
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
// module cache into the provider tree.
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

	if err := os.MkdirAll(examplesOut, 0o755); err != nil {
		return err
	}
	old, err := filepath.Glob(filepath.Join(examplesOut, "*.yaml"))
	if err != nil {
		return err
	}
	for _, f := range old {
		if err := os.Remove(f); err != nil {
			return err
		}
	}
	examples, err := filepath.Glob(filepath.Join(m.Dir, "examples", "*.yaml"))
	if err != nil {
		return err
	}
	for _, src := range examples {
		b, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(examplesOut, filepath.Base(src)), b, 0o644); err != nil { //nolint:gosec // testdata
			return err
		}
	}
	readme := fmt.Sprintf("These are the example configurations of %s %s (examples/*.yaml),\n"+
		"copied by `make generate_config_types`. Do not edit them: TestRoundTripUpstreamExamples\n"+
		"decodes each one into the typed inputs, renders it again and compares what docker-agent loads.\n",
		dockerAgentModule, m.Version)
	return os.WriteFile(filepath.Join(examplesOut, "README.md"), []byte(readme), 0o644) //nolint:gosec // testdata
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
