// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"maps"
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
		"Safety           *SafetyMode    `pulumi:\"safety,optional\" yaml:\"safety\"`",
		"SafetyModeStrict SafetyMode = \"strict\"",
		"func (SafetyMode) Values() []infer.EnumValue[SafetyMode] {",
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

const enumSchema = `{
  "properties": {
    "version": {"type": "string", "enum": ["1", "2"]},
    "agents": {"type": "object", "additionalProperties": {"$ref": "#/definitions/AgentConfig"}},
    "runtime": {"$ref": "#/definitions/RuntimeDefaults"},
    "toolsets": {"type": "array", "items": {"$ref": "#/definitions/Toolset"}},
    "lifecycle": {"$ref": "#/definitions/Lifecycle"}
  },
  "definitions": {
    "AgentConfig": {"type": "object", "properties": {
      "safety": {"type": "string", "enum": ["strict", "balanced"],
        "description": "The mode. 'strict' asks first. 'balanced' (default): asks less, e.g. for reads, and"},
      "mode": {"type": "string", "enum": ["fast-path", "GET", "on_failure"]},
      "single": {"type": "string", "enum": ["only"]},
      "empty": {"type": "string", "enum": ["", "set"]},
      "levels": {"type": "array", "items": {"type": "string", "enum": ["a", "b"]}},
      "decisions": {"type": "object", "additionalProperties": {"type": "string", "enum": ["allow", "deny"]}}
    }},
    "RuntimeDefaults": {"type": "object", "properties": {
      "safety": {"type": "string", "enum": ["strict", "balanced", "yolo"]}
    }},
    "Toolset": {"type": "object", "properties": {"type": {"type": "string", "enum": ["mcp", "shell"]}}},
    "Lifecycle": {"type": "object", "properties": {"backoff": {"type": "string", "enum": ["x", "y"]}}}
  }
}`

// TestGenerateEnums covers which enums become Pulumi enums, their names, value names and
// value descriptions.
func TestGenerateEnums(t *testing.T) {
	t.Parallel()
	g, err := generate(t, enumSchema)
	if err != nil {
		t.Fatal(err)
	}
	src, err := emitTypes(g, "v0.0.0")
	if err != nil {
		t.Fatal(err)
	}
	flat := strings.Join(strings.Fields(string(src)), " ")
	for _, want := range []string{
		// Named in enumTypes and shared by two paths: the union of their values.
		"Safety *SafetyMode `pulumi:\"safety,optional\" yaml:\"safety\"`",
		"SafetyModeYolo SafetyMode = \"yolo\"",
		`{Name: "Strict", Value: SafetyModeStrict, Description: "Prompt for every tool call."}`,
		// Default names: owner type + property; value names in PascalCase.
		"Mode *AgentMode `pulumi:\"mode,optional\" yaml:\"mode\"`",
		`AgentModeFastPath AgentMode = "fast-path"`, `AgentModeGet AgentMode = "GET"`,
		`AgentModeOnFailure AgentMode = "on_failure"`,
		"Levels []AgentLevels `pulumi:\"levels,optional\" yaml:\"levels\"`",
		"Decisions map[string]AgentDecisions `pulumi:\"decisions,optional\" yaml:\"decisions\"`",
		// A default name that is taken by an object type gets a number.
		"Backoff *LifecycleBackoff2 `pulumi:\"backoff,optional\" yaml:\"backoff\"`",
		// Plain strings: a single value, an empty value, numeric values and a volatile set.
		"Single *string `pulumi:\"single,optional\" yaml:\"single\"`", "Allowed values: `only`.",
		"Empty *string `pulumi:\"empty,optional\" yaml:\"empty\"`",
		"Version *string `pulumi:\"version,optional\" yaml:\"version\"`",
		"Type string `pulumi:\"type\" yaml:\"type\"`", "Allowed values: `mcp`, `shell`.",
		// The property description no longer lists the values of a Pulumi enum.
		`a.Describe(&x.Mode, "The ` + "`mode`" + ` setting (no description in agent-schema.json).")`,
	} {
		if !strings.Contains(flat, strings.Join(strings.Fields(want), " ")) {
			t.Errorf("generated source lacks %q:\n%s", want, src)
		}
	}
	if len(g.notes) != 1 || !strings.Contains(g.notes[0], "enum SafetyMode: AgentConfig.safety has the values") {
		t.Errorf("notes = %q, want one about the values of SafetyMode", g.notes)
	}
}

func TestEnumValueNames(t *testing.T) {
	t.Parallel()
	for value, want := range map[string]string{
		"claude-code": "ClaudeCode", "on_failure": "OnFailure", "GET": "Get", "best-effort": "BestEffort",
		"openai_chatcompletions": "OpenAIChatCompletions", "xhigh": "XHigh", "2fa": "Value2fa", "a.b/c d": "ABCD",
	} {
		if got := enumValueName(value); got != want {
			t.Errorf("enumValueName(%q) = %q, want %q", value, got, want)
		}
	}
}

func TestValueDocs(t *testing.T) {
	t.Parallel()
	for desc, want := range map[string]map[string]string{
		"Shorthand. 'resilient' (default): auto-restart on failure. 'strict': fail fast — no start. 'best-effort': once.": {
			"resilient": "Auto-restart on failure (the default).", "strict": "Fail fast — no start.", "best-effort": "Once.",
		},
		"Policy. 'warn' reports and continues (default), 'ignore' continues silently, and 'block' denies events. Other.": {
			"warn": "Reports and continues (default).", "ignore": "Continues silently.", "block": "Denies events.",
		},
		"Type. 'command' runs a shell, e.g. bash; 'model' asks an LLM (used for judges, etc., no code). Other 'x' text.": {
			"command": "Runs a shell, e.g. bash.", "model": "Asks an LLM (used for judges, etc., no code).",
		},
		`Mode. Trust warning: "restricted" is the fail-closed profile: safe calls pass.`: {
			"restricted": "The fail-closed profile: safe calls pass.",
		},
		"No quoted values here.": {},
	} {
		got := valueDocs(desc, []string{"resilient", "strict", "best-effort", "warn", "ignore", "block", "command",
			"model", "restricted"})
		if !maps.Equal(got, want) {
			t.Errorf("valueDocs(%q) = %q, want %q", desc, got, want)
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
