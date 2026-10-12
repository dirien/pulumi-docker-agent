// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testSchema(t *testing.T) *pkgSchema {
	t.Helper()
	var s pkgSchema
	require.NoError(t, json.Unmarshal([]byte(`{
		"name": "docker-agent",
		"namespace": "dirien",
		"language": {"java": {"basePackage": "io.github.dirien"}, "python": {"packageName": "ediri_docker_agent"}},
		"resources": {"docker-agent:index:AgentRun": {"inputProperties": {
			"logging": {"$ref": "#/types/docker-agent:index:Logging"},
			"prompt": {"type": "string"}
		}}},
		"functions": {"docker-agent:index:getAgent": {"inputs": {"properties": {"reference": {"type": "string"}}}}},
		"types": {
			"docker-agent:index:Logging": {"type": "string", "enum": [{"name": "None", "value": "none"}, {"name": "Tools", "value": "tools"}]},
			"docker-agent:index:SafetyMode": {"type": "string", "enum": [{"name": "Restricted", "value": "restricted"}]},
			"docker-agent:index:HookType": {"type": "string", "enum": [{"name": "Builtin", "value": "builtin"}]},
			"docker-agent:index:RuntimeDefaults": {"type": "object", "properties": {
				"safety": {"$ref": "#/types/docker-agent:index:SafetyMode"},
				"modes": {"type": "array", "items": {"$ref": "#/types/docker-agent:index:SafetyMode"}}
			}},
			"docker-agent:index:HookDefinition": {"type": "object", "properties": {
				"type": {"$ref": "#/types/docker-agent:index:HookType"},
				"command": {"type": "string"}
			}},
			"docker-agent:index:Toolset": {"type": "object", "properties": {"type": {"type": "string"}}}
		}
	}`), &s))
	return &s
}

func TestFixJava(t *testing.T) {
	t.Parallel()
	in := `package generated_program;

import com.pulumi.Context;
import io.github.dirien.dockeragent.AgentRun;
import io.github.dirien.dockeragent.AgentRunArgs;
import com.dirien.dockeragent.inputs.RuntimeDefaultsArgs;
import com.pulumi.dockeragent.DockeragentFunctions;
import java.nio.file.Files;
import java.nio.file.Paths;

public class App {
    public static void stack(Context ctx) {
        final var config = ctx.config();
        final var key = config.requireSecret("key");
        final var file = Files.readString(Paths.get("./agent.yaml"));
        final var agent = DockeragentFunctions.getAgent(com.dirien.dockeragent.inputs.GetAgentArgs.builder()
            .reference("x")
            .build());
        var run = new AgentRun("run", AgentRunArgs.builder()
            .prompt("tools")
            .logging("tools")
            .env(Map.of("KEY", key))
            .runtime(RuntimeDefaultsArgs.builder()
                .safety("restricted")
                .modes(
                    "restricted",
                    "restricted")
                .build())
            .toolsets(ToolsetArgs.builder()
                .type("builtin")
                .build())
            .hooks(HookDefinitionArgs.builder()
                .type("builtin")
                .command("x")
                .build())
            .build());
    }
}
`
	out, err := fixJava(in, testSchema(t))
	require.NoError(t, err)
	for _, want := range []string{
		"import io.github.dirien.dockeragent.inputs.RuntimeDefaultsArgs;",
		"import io.github.dirien.dockeragent.DockeragentFunctions;",
		"import io.github.dirien.dockeragent.inputs.GetAgentArgs;",
		"import io.github.dirien.dockeragent.enums.Logging;",
		"import io.github.dirien.dockeragent.enums.SafetyMode;",
		"import io.github.dirien.dockeragent.enums.HookType;",
		"import java.io.UncheckedIOException;",
		"DockeragentFunctions.getAgent(GetAgentArgs.builder()",
		`.prompt("tools")`, // a string property keeps its string
		".logging(Logging.Tools)",
		".safety(SafetyMode.Restricted)",
		"SafetyMode.Restricted,\n                    SafetyMode.Restricted)",
		`.type("builtin")`, // Toolset.type is a string
		".type(HookType.Builtin)",
		`final var file = readFile("./agent.yaml");`,
		"private static String readFile(String path) {",
		`.env(key.applyValue(_key -> Map.of("KEY", _key)))`,
	} {
		assert.Contains(t, out, want)
	}
	assert.NotContains(t, out, "com.dirien")
	assert.NotContains(t, out, "com.pulumi.dockeragent")
	assert.Equal(t, 1, strings.Count(out, "import java.nio.file.Files;"))
	assert.True(t, strings.HasSuffix(out, "    }\n}\n"), out)
}

// TestFixJavaTextBlocks checks that the backslashes of text blocks, which pulumi-java writes for
// multi-line strings verbatim, are escaped, and that their quotes and parentheses are skipped.
func TestFixJavaTextBlocks(t *testing.T) {
	t.Parallel()
	in := "var r = new AgentRun(\"r\", AgentRunArgs.builder()\n" +
		"    .prompt(\"\"\"\ngrep -q '\\-h' && echo \"a (b)\" \\\n  done\n    \"\"\")\n" +
		"    .logging(\"tools\") // \"\"\" \\ in a comment\n    .build());\n"
	out, err := fixJava(in, testSchema(t))
	require.NoError(t, err)
	assert.Contains(t, out, ".prompt(\"\"\"\ngrep -q '\\\\-h' && echo \"a (b)\" \\\\\n  done\n    \"\"\")\n")
	assert.Contains(t, out, ".logging(Logging.Tools) // \"\"\" \\ in a comment\n")
	_, err = skipJavaString(`"""\nnot closed""`, 0)
	assert.ErrorContains(t, err, "unterminated text block")
}

func TestFixJavaRejectsUnknownEnumValues(t *testing.T) {
	t.Parallel()
	_, err := fixJava("var r = new AgentRun(\"r\", AgentRunArgs.builder()\n    .logging(\"loud\")\n    .build());\n", testSchema(t))
	require.ErrorContains(t, err, `"loud" is not a value of the enum Logging`)
}

func TestFixPythonImports(t *testing.T) {
	t.Parallel()
	out, err := fixPythonImports("import pulumi\nimport ediri_docker_agent as docker_agent\n"+
		"import pulumi_docker_agent as docker_agent\n\nx = 1\n", testSchema(t))
	require.NoError(t, err)
	assert.Equal(t, "import pulumi\nimport ediri_docker_agent as docker_agent\n\nx = 1\n", out)

	out, err = fixPythonImports("import pulumi_docker_agent as docker_agent\n", testSchema(t))
	require.NoError(t, err)
	assert.Equal(t, "import ediri_docker_agent as docker_agent\n", out)
}

func TestFixGoAnyLists(t *testing.T) {
	t.Parallel()
	out, err := fixGoAnyLists("x := pulumi.Map{\n\"required\": pulumi.Any{\n\"a\",\n\"}\",\n},\n}\n")
	require.NoError(t, err)
	assert.Equal(t, "x := pulumi.Map{\n\"required\": pulumi.Any([]interface{}{\n\"a\",\n\"}\",\n}),\n}\n", out)
}

func TestNormalizeHCL(t *testing.T) {
	t.Parallel()
	in := `terraform {
  required_providers {
    docker-agent = {
      source  = "pulumi/dirien/docker-agent"
      version = "0.0.1-alpha.1791750221"
    }
  }
}
resource "docker-agent_agent_artifact" "a" {
  pulumi {
    replace_on_changes = [reference]
  }
  lifecycle {
    create_before_destroy = true
  }
  reference = "localhost:5000/a:v1"
  prompt              ="Hello ${var.who}."
}
output "x" {
  value = docker-agent_agent_artifact.a.digest
}
`
	want := `terraform {
  required_providers {
    docker-agent = {
      source = "pulumi/dirien/docker-agent"
    }
  }
}

resource "docker-agent_agent_artifact" "a" {
  reference = "localhost:5000/a:v1"
  prompt              = "Hello ${var.who}."
}

output "x" {
  value = docker-agent_agent_artifact.a.digest
}
`
	assert.Equal(t, want, normalizeHCL(in))
}

func TestAddComments(t *testing.T) {
	t.Parallel()
	comments := []comment{
		{kind: "config", key: "apiKey", lines: []string{"The key."}},
		{kind: "resources", key: "my-agent", lines: []string{"An agent,", "rendered."}},
		{kind: "variables", key: "agentFile", lines: []string{"A file."}},
	}
	ts := "const config = new pulumi.Config();\nconst apiKey = config.requireSecret(\"apiKey\");\n" +
		"const agentFile = fs.readFileSync(\"a.yaml\", \"utf8\");\nconst myAgent = new docker_agent.AgentConfig(\"my-agent\", {\n});\n"
	out, err := addComments(ts, comments, language{fence: "typescript", comment: "//"})
	require.NoError(t, err)
	assert.Equal(t, "const config = new pulumi.Config();\n\n// The key.\nconst apiKey = config.requireSecret(\"apiKey\");\n\n"+
		"// A file.\nconst agentFile = fs.readFileSync(\"a.yaml\", \"utf8\");\n\n"+
		"// An agent,\n// rendered.\nconst myAgent = new docker_agent.AgentConfig(\"my-agent\", {\n});\n", out)

	py := "agent_file = (lambda path: open(path).read())(\"a.yaml\")\n"
	_, err = addComments(py, comments[2:], language{fence: "python", comment: "#"})
	require.NoError(t, err)
	_, err = addComments(py, comments[1:2], language{fence: "python", comment: "#"})
	require.ErrorContains(t, err, `resources "my-agent"`)
}

func TestCheckResourceNames(t *testing.T) {
	t.Parallel()
	java := language{fence: "java"}
	require.NoError(t, checkResourceNames(`var p = new AgentsIgnore("project", AgentsIgnoreArgs.builder()`, []string{"project"}, java))
	require.ErrorContains(t, checkResourceNames(`var projectResource = new AgentsIgnore("projectResource", `,
		[]string{"project"}, java), "rename the resource")
}

func TestParseSource(t *testing.T) {
	t.Parallel()
	src, err := parseSource("x-examples.yaml", `# header
---
description: First
files:
  a.yaml: "agents: {}"
name: first
runtime: yaml
resources:
  # A comment.
  one:
    type: docker-agent:index:AgentConfig
---
description: Second
notes:
  hcl: A note.
name: second
runtime: yaml
`)
	require.NoError(t, err)
	require.Len(t, src.examples, 2)
	first := src.examples[0]
	assert.Equal(t, "First", first.Description)
	assert.Equal(t, "first", first.name)
	assert.Equal(t, map[string]string{"a.yaml": "agents: {}"}, first.Files)
	assert.True(t, strings.HasPrefix(first.program, "name: first\nruntime: yaml\n"))
	assert.Equal(t, []comment{{kind: "resources", key: "one", lines: []string{"A comment."}}}, first.comments)
	assert.Equal(t, []string{"one"}, first.resources)
	assert.Equal(t, "A note.", src.examples[1].Notes["hcl"])

	_, err = parseSource("x.yaml", "description: x\ntitle: y\nname: a\nruntime: yaml\n")
	require.ErrorContains(t, err, "field title not found")
	_, err = parseSource("x.yaml", "name: a\nruntime: yaml\n")
	require.ErrorContains(t, err, "title")
	_, err = parseSource("x.yaml", "description: x\nnotes:\n  rust: y\nname: a\nruntime: yaml\n")
	require.ErrorContains(t, err, `unknown language "rust"`)
}

func TestReplaceRegions(t *testing.T) {
	t.Parallel()
	ex := &example{name: "one", code: map[string]string{}}
	ex.Notes = map[string]string{"hcl": "A note."}
	for _, lang := range languages {
		ex.code[lang.fence] = lang.fence + " code\n"
	}
	src := &source{file: indexExamples, examples: []*example{ex}}
	page := "# Title\n\n<!-- docsgen begin: one (generated) -->\nold\n<!-- docsgen end -->\n\nText.\n"
	out, err := replaceRegions(page, src)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(out, "# Title\n\n<!-- docsgen begin: one (generated) -->\n"+
		`{{< chooser language "typescript,python,go,csharp,java,yaml,hcl" >}}`+"\n{{% choosable language typescript %}}\n\n"+
		"```typescript\ntypescript code\n```\n\n{{% /choosable %}}\n"), out)
	assert.Contains(t, out, "```hcl\nhcl code\n```\n\nA note.\n\n{{% /choosable %}}\n{{< /chooser >}}\n<!-- docsgen end -->\n\nText.\n")
	assert.NotContains(t, out, "old")

	again, err := replaceRegions(out, src)
	require.NoError(t, err)
	assert.Equal(t, out, again, "replacing is idempotent")

	_, err = replaceRegions("no regions\n", src)
	require.ErrorContains(t, err, "no `<!-- docsgen begin: <name> -->` region for one")
}

// TestSources parses the real docs/yaml files.
func TestSources(t *testing.T) {
	t.Parallel()
	root, err := providerRoot()
	require.NoError(t, err)
	sources, err := readSources(filepath.Join(root, "..", "docs", "yaml"))
	require.NoError(t, err)
	var names []string
	for _, src := range sources {
		names = append(names, src.file)
		if src.file != indexExamples {
			assert.GreaterOrEqual(t, len(src.examples), 2, "%s: at least two examples", src.file)
			_, err := os.Stat(filepath.Join(root, "pkg", "provider", "embed", strings.TrimSuffix(src.file, ".yaml")+".md"))
			assert.NoError(t, err, "run `make docs`")
		}
	}
	assert.Contains(t, names, indexExamples)
}
