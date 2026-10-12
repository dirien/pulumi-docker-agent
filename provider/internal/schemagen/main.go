// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

// Command schemagen keeps the provider in sync with the pinned docker-agent release.
//
//	schemagen generate   copy agent-schema.json and the example configurations of the docker-agent module
//	                     in provider/go.mod into the provider tree, and generate the typed
//	                     AgentConfig inputs (pkg/render/config_types.gen.go) and the version,
//	                     toolset and provider constants (pkg/render/version.gen.go,
//	                     pkg/validate/upstream.gen.go)
//	schemagen matrix     print the coverage matrix (schema path -> Pulumi input) as Markdown
//	schemagen diff       print a Markdown report of the changes between two agent-schema.json files
//	schemagen changelog  prepend a CHANGELOG entry for a docker-agent upgrade
//
// It uses the standard library only and runs from anywhere inside the provider module.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

// Paths relative to the provider module root.
const (
	vendoredSchema    = "pkg/render/schema/agent-schema.json"
	examplesDir       = "pkg/render/testdata/upstream"
	typesFile         = "pkg/render/config_types.gen.go"
	renderVersionFile = "pkg/render/version.gen.go"
	validateFile      = "pkg/validate/upstream.gen.go"
	providerModule    = "module github.com/dirien/pulumi-docker-agent/provider"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "generate":
		err = cmdGenerate(os.Args[2:])
	case "matrix":
		err = cmdMatrix(os.Args[2:])
	case "diff":
		err = cmdDiff(os.Args[2:])
	case "changelog":
		err = cmdChangelog(os.Args[2:])
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "schemagen:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: schemagen generate|matrix|diff|changelog [flags]")
	os.Exit(2)
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
			return "", errors.New("run schemagen inside the provider module (provider/go.mod not found)")
		}
		dir = parent
	}
}

func cmdGenerate(args []string) error {
	fs := flag.NewFlagSet("generate", flag.ExitOnError)
	noSync := fs.Bool("no-sync", false, "use the vendored schema and examples as they are (do not copy them from the module cache)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	root, err := providerRoot()
	if err != nil {
		return err
	}
	m, err := goListModule(root)
	if err != nil {
		return err
	}
	if !*noSync {
		if err := syncUpstream(m, filepath.Join(root, vendoredSchema), filepath.Join(root, examplesDir)); err != nil {
			return fmt.Errorf("copying the upstream schema and examples: %w", err)
		}
	}
	g, schema, err := loadGenerator(filepath.Join(root, vendoredSchema))
	if err != nil {
		return err
	}
	configVersion, err := latestConfigVersion(schema)
	if err != nil {
		return err
	}
	types, err := emitTypes(g, m.Version)
	if err != nil {
		return fmt.Errorf("formatting the generated types: %w", err)
	}
	renderVersion, err := emitRenderVersion(m.Version, configVersion)
	if err != nil {
		return err
	}
	toolsets, providers, err := upstreamLists(m)
	if err != nil {
		return err
	}
	validateConsts, err := emitValidateConstants(m.Version, configVersion, toolsets, providers)
	if err != nil {
		return err
	}
	for file, data := range map[string][]byte{typesFile: types, renderVersionFile: renderVersion, validateFile: validateConsts} {
		if err := os.WriteFile(filepath.Join(root, file), data, 0o644); err != nil { //nolint:gosec // generated source
			return err
		}
	}
	fmt.Printf("schemagen: docker-agent %s (config version %s): %d types, %d enums, %d toolset types, %d provider types\n",
		m.Version, configVersion, len(g.types), len(g.enums), len(toolsets), len(providers))
	for _, n := range g.notes {
		fmt.Println("schemagen: note:", n)
	}
	return nil
}

func loadGenerator(schemaFile string) (*generator, *jnode, error) {
	data, err := os.ReadFile(schemaFile)
	if err != nil {
		return nil, nil, err
	}
	schema, err := parseOrdered(data)
	if err != nil {
		return nil, nil, fmt.Errorf("parsing %s: %w", schemaFile, err)
	}
	g := newGenerator(schema)
	if err := g.run(); err != nil {
		return nil, nil, err
	}
	return g, schema, nil
}

func cmdMatrix(args []string) error {
	fs := flag.NewFlagSet("matrix", flag.ExitOnError)
	schemaFile := fs.String("schema", "", "agent-schema.json (default: the vendored copy)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *schemaFile == "" {
		root, err := providerRoot()
		if err != nil {
			return err
		}
		*schemaFile = filepath.Join(root, vendoredSchema)
	}
	g, _, err := loadGenerator(*schemaFile)
	if err != nil {
		return err
	}
	_, err = os.Stdout.WriteString(matrix(g))
	return err
}
