// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNames(t *testing.T) {
	t.Parallel()
	for key, want := range map[string][2]string{
		"base_url":            {"BaseURL", "baseUrl"},
		"api_type":            {"APIType", "apiType"},
		"allow_private_ips":   {"AllowPrivateIPs", "allowPrivateIps"},
		"before_llm_call":     {"BeforeLLMCall", "beforeLlmCall"},
		"callbackRedirectURL": {"CallbackRedirectURL", "callbackRedirectURL"},
		"clientId":            {"ClientID", "clientId"},
		"oauth":               {"OAuth", "oauth"},
		"k1":                  {"K1", "k1"},
	} {
		if got := pascal(key); got != want[0] {
			t.Errorf("pascal(%q) = %q, want %q", key, got, want[0])
		}
		if got := camel(key); got != want[1] {
			t.Errorf("camel(%q) = %q, want %q", key, got, want[1])
		}
	}
}

func TestParseOrderedKeepsKeyOrder(t *testing.T) {
	t.Parallel()
	n, err := parseOrdered([]byte(`{"z": 1, "a": {"y": true, "b": null}, "m": ["x"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(n.keys, ","); got != "z,a,m" {
		t.Errorf("keys = %s", got)
	}
	if got := strings.Join(n.get("a").keys, ","); got != "y,b" {
		t.Errorf("nested keys = %s", got)
	}
	if _, err := parseOrdered([]byte(`{} {}`)); err == nil {
		t.Error("trailing data must fail")
	}
}

const miniSchema = `{
  "properties": {
    "version": {"type": "string", "enum": ["1", "2", "10"]},
    "agents": {"type": "object", "additionalProperties": {"$ref": "#/definitions/AgentConfig"}},
    "runtime": {"$ref": "#/definitions/RuntimeDefaults"}
  },
  "definitions": {
    "AgentConfig": {"type": "object", "description": "An agent.", "properties": {
      "model": {"type": "string", "description": "The model"},
      "readonly": {"type": "boolean", "default": false}
    }},
    "RuntimeDefaults": {"type": "object", "properties": {
      "sandbox": {"type": "boolean"},
      "safety": {"type": "string", "enum": ["strict", "balanced"]},
      "network_allowlist": {"type": "array", "items": {"type": "string"}}
    }, "required": ["sandbox"]}
  }
}`

func generate(t *testing.T, schema string) (*generator, error) {
	t.Helper()
	n, err := parseOrdered([]byte(schema))
	if err != nil {
		t.Fatal(err)
	}
	g := newGenerator(n)
	return g, g.run()
}

func TestGenerateMiniSchema(t *testing.T) {
	t.Parallel()
	g, err := generate(t, miniSchema)
	if err != nil {
		t.Fatal(err)
	}
	src, err := emitTypes(g, "v0.0.0")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Agents   []Agent          `pulumi:\"agents\" yaml:\"agents\" render:\"keyed=name\"`",
		"Name     string         `pulumi:\"name\" yaml:\"-\" render:\"key\"`",
		"Readonly *bool          `pulumi:\"readonly,optional\" yaml:\"readonly\"`",
		"Sandbox          bool           `pulumi:\"sandbox\" yaml:\"sandbox\"`",
		"NetworkAllowlist []string       `pulumi:\"networkAllowlist,optional\" yaml:\"network_allowlist\"`",
		"Extra            map[string]any `pulumi:\"extra,optional\" yaml:\"-\" render:\"extra\"`",
		"Allowed values: `strict`, `balanced`.",
		"Defaults to `false`.",
		"a.SetDefault(&x.Version, DefaultConfigVersion)",
	} {
		if !strings.Contains(strings.Join(strings.Fields(string(src)), " "), strings.Join(strings.Fields(want), " ")) {
			t.Errorf("generated source lacks %q:\n%s", want, src)
		}
	}
	v, err := latestConfigVersion(g.root)
	if err != nil || v != "10" {
		t.Errorf("latestConfigVersion = %q, %v; want 10 (numeric, not lexical, maximum)", v, err)
	}
}

func TestGenerateRequiresRules(t *testing.T) {
	t.Parallel()
	for name, schema := range map[string]string{
		"union": strings.Replace(miniSchema, `"model": {"type": "string", "description": "The model"}`,
			`"model": {"oneOf": [{"type": "string"}, {"type": "integer"}]}`, 1),
		"inline object": strings.Replace(miniSchema, `"model": {"type": "string", "description": "The model"}`,
			`"model": {"type": "object", "properties": {"name": {"type": "string"}}}`, 1),
		"free-form object": strings.Replace(miniSchema, `"model": {"type": "string", "description": "The model"}`,
			`"model": {"type": "object"}`, 1),
		"unnamed definition": strings.Replace(miniSchema, `"RuntimeDefaults": {`, `"Unknown": {"type": "object", "properties": {}}, "RuntimeDefaults": {`, 1),
	} {
		if _, err := generate(t, schema); err == nil || !strings.Contains(err.Error(), "without a generator rule") {
			t.Errorf("%s: want a missing-rule error, got %v", name, err)
		}
	}
}

// TestGeneratedFilesAreCurrent regenerates the types from the vendored schema and compares
// them with the committed file, so a hand edit of the generated code is caught by `go test`.
func TestGeneratedFilesAreCurrent(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..")
	g, _, err := loadGenerator(filepath.Join(root, vendoredSchema))
	if err != nil {
		t.Fatal(err)
	}
	committed, err := os.ReadFile(filepath.Join(root, typesFile))
	if err != nil {
		t.Fatal(err)
	}
	// The header names the docker-agent version: "... github.com/docker/docker-agent vX.Y.Z. DO NOT EDIT."
	header, _, _ := strings.Cut(string(committed), ". DO NOT EDIT.")
	version := header[strings.LastIndex(header, " ")+1:]
	fresh, err := emitTypes(g, version)
	if err != nil {
		t.Fatal(err)
	}
	if string(fresh) != string(committed) {
		t.Errorf("%s is stale or was edited by hand; run make generate_config_types", typesFile)
	}
}

func TestUntypedPath(t *testing.T) {
	t.Parallel()
	for path, want := range map[string]bool{
		"ModelConfig.provider_opts":                     true,
		"ModelConfig.provider_opts.extra_body":          true,
		"Config.flavors{}":                              true,
		"AgentConfig.structured_output.schema.required": true,
		"AgentConfig.structured_output.name":            false,
		"ModelConfig.provider":                          false,
	} {
		if got, _ := untypedPath(path); got != want {
			t.Errorf("untypedPath(%q) = %v, want %v", path, got, want)
		}
	}
}
