package schemadiff

import (
	"reflect"
	"strings"
	"testing"
)

// classifyFixture treats Agent.args (a map of free-form objects) and the whole Routing
// definition as untyped.
func classifyFixture(path string) (bool, string) {
	switch path {
	case "Agent.args":
		return true, "rendered as map<any>"
	case "Routing":
		return true, "definition not generated"
	}
	return false, ""
}

func TestMarkdownReport(t *testing.T) {
	d := mustCompare(t, oldFixture, newFixture)
	md := d.Markdown("v1.149.0", "v1.150.0", classifyFixture)

	if again := d.Markdown("v1.149.0", "v1.150.0", classifyFixture); again != md {
		t.Fatal("Markdown is not deterministic")
	}
	for _, want := range []string{
		"# docker-agent v1.149.0 → v1.150.0: configuration schema changes\n",
		"## Summary\n\nConfig version 10 → 11, 1 definition added,",
		"| Properties added | 6 |",
		"## Config version\n\n- latest config version: 10 → 11\n- added versions: `11`\n",
		"## Added definitions\n\n- `Routing` (`object{routes}`): Routing policy.\n",
		"## Removed definitions\n\n- `Gone` (`object{x}`): Removed later.\n",
		"| Path | Type | Description | Typed? |",
		"| `Agent.budget\\|1.type` | `string` |  | yes |",
		"| `Agent.args{}.required` | `boolean` |  | no: under `Agent.args` (rendered as map&lt;any&gt;) |",
		"| `Routing.routes` | `map<string>` | Choice to agent (e.g. yes: a). | no: under `Routing` (definition not generated) |",
		"## Removed properties\n\n| Path | Type |\n|---|---|\n| `Config.legacy` | `boolean` |\n| `Gone.x` | `string` |\n",
		"## Changed types\n\n| Path | Old type | New type |\n|---|---|---|\n| `Agent.max_iterations` | `string` | `integer` |\n",
		"| `Agent.mode` | `auto` | `legacy` |",
		"| `Config.version` | `11` |  |",
		"## Changed required\n\n| Object | Now required | No longer required |\n|---|---|---|\n| `Agent` | `mode` |  |\n",
		"## Changed descriptions\n\n1 description changed: `Agent`.\n",
		"## Properties that landed in untyped (`any`) paths\n\n" +
			"- `Agent.args{}.required`: under `Agent.args` (rendered as map&lt;any&gt;)\n" +
			"- `Routing.routes`: under `Routing` (definition not generated)\n",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("report misses %q\n---\n%s", want, md)
		}
	}
	// Section order follows the specification.
	order := []string{
		"## Summary", "## Config version", "## Added definitions", "## Removed definitions", "## Added properties",
		"## Removed properties", "## Changed types", "## Changed enums", "## Changed required", "## Changed descriptions",
		"## Properties that landed in untyped",
	}
	last := -1
	for _, h := range order {
		i := strings.Index(md, h)
		if i <= last {
			t.Errorf("section %q out of order", h)
		}
		last = i
	}
}

func TestMarkdownWithoutClassifier(t *testing.T) {
	d := mustCompare(t, oldFixture, newFixture)
	md := d.Markdown("a", "b", nil)
	if strings.Contains(md, "Typed?") {
		t.Error("Typed? column without classifier")
	}
	if !strings.Contains(md, "## Properties that landed in untyped (`any`) paths\n\nNot checked (no classifier).\n") {
		t.Errorf("missing untyped placeholder:\n%s", md)
	}
	if d.Untyped(nil) != nil {
		t.Error("Untyped(nil) != nil")
	}
}

func TestMarkdownAllTyped(t *testing.T) {
	d := mustCompare(t, oldFixture, newFixture)
	md := d.Markdown("a", "b", func(string) (bool, string) { return false, "" })
	if !strings.Contains(md, "None: every added or changed property maps to a typed input.") {
		t.Errorf("missing all-typed note:\n%s", md)
	}
}

func TestMarkdownNoChanges(t *testing.T) {
	d := mustCompare(t, newFixture, newFixture)
	md := d.Markdown("v1.150.0", "v1.150.1", classifyFixture)
	for _, want := range []string{
		"## Summary\n\nNo configuration schema changes between v1.150.0 and v1.150.1.\n",
		"- latest config version: 11 (unchanged)\n",
		"None: every added or changed property maps to a typed input.",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("report misses %q\n---\n%s", want, md)
		}
	}
	for _, unwanted := range []string{"## Added properties", "## Changed types", "| Change | Count |"} {
		if strings.Contains(md, unwanted) {
			t.Errorf("empty report contains %q", unwanted)
		}
	}
}

func TestMarkdownConstraintsAndManyDescriptions(t *testing.T) {
	d := &Diff{
		AddedConstraints:   []Property{{Path: "Toolset|11|0.type", Type: "const:webhook", Constraint: true}},
		RemovedConstraints: []Property{{Path: "Toolset|3|0.type", Type: "const:api", Constraint: true}},
		EnumChanges:        []EnumChange{{Path: "Toolset|1.type", Added: []string{"webhook", ""}}},
	}
	for i := 0; i < 11; i++ {
		d.DescriptionChanges = append(d.DescriptionChanges, "D.p"+string(rune('a'+i)))
	}
	md := d.Markdown("a", "b", func(p string) (bool, string) { return p == "Toolset|1", "" })
	for _, want := range []string{
		"## Changed union constraints\n",
		"- added `Toolset|11|0.type`: `const:webhook`\n",
		"- removed `Toolset|3|0.type`: `const:api`\n",
		"| `Toolset\\|1.type` | `webhook`, `\"\"` |  |",
		"11 descriptions changed.\n\n<details><summary>Paths</summary>",
		"- `Toolset|1.type`: under `Toolset|1` (untyped)\n",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("report misses %q\n---\n%s", want, md)
		}
	}
}

func TestUntypedChecksEveryAncestor(t *testing.T) {
	d := &Diff{
		AddedProperties: []Property{{Path: "RAGConfig.strategies[].chunking.size"}, {Path: "ModelConfig.task_budget|1.total"}},
		TypeChanges:     []TypeChange{{Path: "ApiConfig.args{}.type"}},
		RequiredChanges: []RequiredChange{{Path: "Typed"}},
	}
	var asked []string
	got := d.Untyped(func(p string) (bool, string) {
		asked = append(asked, p)
		switch p {
		case "RAGConfig.strategies[]":
			return true, "items are any"
		case "RAGConfig.strategies[].chunking": // shadowed by the outer untyped prefix
			return true, "inner"
		case "ModelConfig.task_budget|1.total":
			return true, "leaf"
		case "ApiConfig.args{}":
			return true, ""
		}
		return false, ""
	})
	want := []UntypedPath{
		{Path: "ApiConfig.args{}.type", At: "ApiConfig.args{}"},
		{Path: "ModelConfig.task_budget|1.total", At: "ModelConfig.task_budget|1.total", Reason: "leaf"},
		{Path: "RAGConfig.strategies[].chunking.size", At: "RAGConfig.strategies[]", Reason: "items are any"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Untyped = %+v, want %+v", got, want)
	}
	if !contains(asked, "Typed") {
		t.Errorf("required change not classified: %q", asked)
	}
}

func TestAncestors(t *testing.T) {
	for _, tc := range []struct {
		path string
		want []string
	}{
		{"Config.agents", []string{"Config", "Config.agents"}},
		{"RAGConfig.strategies[].chunking.size", []string{
			"RAGConfig", "RAGConfig.strategies", "RAGConfig.strategies[]", "RAGConfig.strategies[].chunking",
			"RAGConfig.strategies[].chunking.size",
		}},
		{"ApiConfig.args{}.type", []string{"ApiConfig", "ApiConfig.args", "ApiConfig.args{}", "ApiConfig.args{}.type"}},
		{"ModelConfig.task_budget|1.total", []string{
			"ModelConfig", "ModelConfig.task_budget", "ModelConfig.task_budget|1", "ModelConfig.task_budget|1.total",
		}},
		{"Toolset|0|0.type", []string{"Toolset", "Toolset|0", "Toolset|0|0", "Toolset|0|0.type"}},
		{"M{^x.y|z$}.b", []string{"M", "M{^x.y|z$}", "M{^x.y|z$}.b"}},
		{"Agent", []string{"Agent"}},
	} {
		if got := Ancestors(tc.path); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("Ancestors(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

func TestFirstSentence(t *testing.T) {
	long := strings.Repeat("word ", 60)
	for _, tc := range []struct{ in, want string }{
		{"", ""},
		{"One. Two.", "One."},
		{"No period", "No period"},
		{"Use e.g. this. Then that.", "Use e.g. this."},
		{"Values (i.e. 1.5) work. More.", "Values (i.e. 1.5) work."},
		{"Version 1.2 is fine.\nNext line.", "Version 1.2 is fine."},
		{"Multi\n  line\twhitespace. x", "Multi line whitespace."},
		{long, strings.TrimRight(long[:159], " ") + "…"},
	} {
		if got := firstSentence(tc.in); got != tc.want {
			t.Errorf("firstSentence(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestCellEscaping(t *testing.T) {
	if got := cell("a | b <name>\n c"); got != `a \| b &lt;name&gt; c` {
		t.Errorf("cell = %q", got)
	}
	if got := tcode("oneOf[a|b]"); got != "`oneOf[a\\|b]`" {
		t.Errorf("tcode = %q", got)
	}
	if got := code("oneOf[a|b]"); got != "`oneOf[a|b]`" {
		t.Errorf("code = %q", got)
	}
}
