// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// language is a language of the Registry's language chooser.
type language struct {
	fence   string // the Markdown fence and chooser name
	convert string // `pulumi convert --language`; empty for YAML, the source language
	file    string // the program file of the converted project
	comment string // the line comment prefix
}

// The languages of the Registry's language chooser.
const (
	langTypeScript = "typescript"
	langPython     = "python"
	langGo         = "go"
	langCSharp     = "csharp"
	langJava       = "java"
	langYAML       = "yaml"
	langHCL        = "hcl"
)

// languages in the order of the Registry's language chooser.
var languages = []language{
	{fence: langTypeScript, convert: langTypeScript, file: "index.ts", comment: "//"},
	{fence: langPython, convert: langPython, file: "__main__.py", comment: "#"},
	{fence: langGo, convert: langGo, file: "main.go", comment: "//"},
	{fence: langCSharp, convert: langCSharp, file: "Program.cs", comment: "//"},
	{fence: langJava, convert: langJava, file: filepath.Join("src", "main", "java", "generated_program", "App.java"), comment: "//"},
	{fence: langYAML, comment: "#"},
	{fence: langHCL, convert: langHCL, file: "program.tf", comment: "#"},
}

// converter converts examples with `pulumi convert`.
type converter struct {
	pulumi string
	binDir string
	schema *pkgSchema
}

func (c *converter) convertAll(sources []*source, jobs int) error {
	var examples []*example
	for _, src := range sources {
		examples = append(examples, src.examples...)
	}
	// Pulumi YAML -> PCL, once per example.
	err := parallel(jobs, len(examples), func(i int) error {
		ex := examples[i]
		pcl, err := c.toPCL(ex)
		if err != nil {
			return fmt.Errorf("example %q: %w", ex.name, err)
		}
		ex.pcl = pcl
		ex.code = map[string]string{}
		return nil
	})
	if err != nil {
		return err
	}
	// PCL -> every language.
	type task struct {
		ex   *example
		lang language
	}
	var tasks []task
	for _, ex := range examples {
		for _, lang := range languages {
			tasks = append(tasks, task{ex, lang})
		}
	}
	var mu sync.Mutex
	return parallel(jobs, len(tasks), func(i int) error {
		t := tasks[i]
		code, err := c.convert(t.ex, t.lang)
		if err != nil {
			return fmt.Errorf("example %q, %s: %w", t.ex.name, t.lang.fence, err)
		}
		mu.Lock()
		t.ex.code[t.lang.fence] = code
		mu.Unlock()
		return nil
	})
}

// parallel runs f(0) ... f(n-1) on up to jobs goroutines and joins the errors.
func parallel(jobs, n int, f func(int) error) error {
	var (
		mu   sync.Mutex
		errs []error
		wg   sync.WaitGroup
	)
	queue := make(chan int)
	for range max(jobs, 1) {
		wg.Go(func() {
			for i := range queue {
				if err := f(i); err != nil {
					mu.Lock()
					errs = append(errs, err)
					mu.Unlock()
				}
			}
		})
	}
	for i := range n {
		queue <- i
	}
	close(queue)
	wg.Wait()
	return errors.Join(errs...)
}

// quotedKeyRE matches an object key that the YAML converter quotes although it is an identifier.
var quotedKeyRE = regexp.MustCompile(`(?m)^(\s*)"([A-Za-z_][A-Za-z0-9_]*)" =`)

// toPCL converts the example to PCL. The YAML converter quotes the keys of objects inside maps
// (`"provider" = "anthropic"` in `models`), which crashes pulumi-java's program generator, so the
// keys that are identifiers are unquoted; an identifier key means the same string in PCL.
func (c *converter) toPCL(ex *example) (string, error) {
	out, err := c.run(ex.pulumiYAML(c.binDir), "", "pcl")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(filepath.Dir(out))
	pcl, err := os.ReadFile(filepath.Join(out, "program.pp")) //nolint:gosec // G304: the converted program
	if err != nil {
		return "", err
	}
	return quotedKeyRE.ReplaceAllString(string(pcl), "$1$2 ="), nil
}

// convert returns the example's program in a language, normalized.
func (c *converter) convert(ex *example, lang language) (string, error) {
	if lang.convert == "" {
		return ex.program, nil
	}
	project := fmt.Appendf(nil, "name: %s\nruntime: yaml\nplugins:\n  providers:\n    - name: docker-agent\n      path: %q\n",
		ex.name, c.binDir)
	out, err := c.run(project, ex.pcl, lang.convert)
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(filepath.Dir(out))
	code, err := os.ReadFile(filepath.Join(out, lang.file)) //nolint:gosec // G304: the converted program
	if err != nil {
		return "", err
	}
	return normalize(string(code), ex, lang, c.schema)
}

// run writes Pulumi.yaml (and program.pp, when converting from PCL) into a temporary directory,
// runs `pulumi convert --generate-only` and returns the output directory.
func (c *converter) run(pulumiYAML []byte, pcl, lang string) (string, error) {
	dir, err := os.MkdirTemp("", "docsgen-")
	if err != nil {
		return "", err
	}
	fail := func(err error) (string, error) {
		os.RemoveAll(dir)
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "Pulumi.yaml"), pulumiYAML, 0o600); err != nil {
		return fail(err)
	}
	args := []string{"convert", "--language", lang, "--out", filepath.Join(dir, "out"), "--generate-only"}
	if pcl != "" {
		if err := os.WriteFile(filepath.Join(dir, "program.pp"), []byte(pcl), 0o600); err != nil {
			return fail(err)
		}
		args = append(args, "--from", "pcl")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.pulumi, args...) //nolint:gosec // G204: the Pulumi CLI with fixed arguments
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PULUMI_SKIP_UPDATE_CHECK=true")
	if output, err := cmd.CombinedOutput(); err != nil {
		return fail(fmt.Errorf("pulumi %s: %w\n%s", strings.Join(args, " "), err, output))
	}
	return filepath.Join(dir, "out"), nil
}

// pkgSchema is the part of the provider's schema docsgen needs.
type pkgSchema struct {
	Name      string                     `json:"name"`
	Namespace string                     `json:"namespace"`
	Language  map[string]json.RawMessage `json:"language"`
	Resources map[string]struct {
		InputProperties map[string]*schemaProperty `json:"inputProperties"`
	} `json:"resources"`
	Functions map[string]struct {
		Inputs *struct {
			Properties map[string]*schemaProperty `json:"properties"`
		} `json:"inputs"`
	} `json:"functions"`
	Types map[string]struct {
		Type       string                     `json:"type"`
		Properties map[string]*schemaProperty `json:"properties"`
		Enum       []struct {
			Name  string `json:"name"`
			Value any    `json:"value"`
		} `json:"enum"`
	} `json:"types"`
}

type schemaProperty struct {
	Ref                  string          `json:"$ref"`
	Type                 string          `json:"type"`
	Items                *schemaProperty `json:"items"`
	AdditionalProperties *schemaProperty `json:"additionalProperties"`
}

// loadSchema reads the schema of the provider binary.
func loadSchema(pulumi, binary string) (*pkgSchema, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	//nolint:gosec // G204: the Pulumi CLI with fixed arguments
	cmd := exec.CommandContext(ctx, pulumi, "package", "get-schema", binary)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Env = append(os.Environ(), "PULUMI_SKIP_UPDATE_CHECK=true")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("pulumi package get-schema: %w\n%s", err, stderr.String())
	}
	var s pkgSchema
	if err := json.Unmarshal(out, &s); err != nil {
		return nil, fmt.Errorf("the provider schema: %w", err)
	}
	return &s, nil
}

// languageInfo decodes the schema's `language.<lang>` section.
func (s *pkgSchema) languageInfo(lang string, v any) error {
	raw, ok := s.Language[lang]
	if !ok {
		return nil
	}
	return json.Unmarshal(raw, v)
}

// tokenName is the last part of a schema token: docker-agent:index:AgentConfig -> AgentConfig.
func tokenName(token string) string {
	return token[strings.LastIndex(token, ":")+1:]
}
