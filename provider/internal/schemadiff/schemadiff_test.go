package schemadiff

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"
)

const oldFixture = `{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "properties": {
    "version": {"type": "string", "enum": ["0", "1", "2", "9", "10"]},
    "agents": {"type": "object", "additionalProperties": {"$ref": "#/definitions/Agent"}},
    "legacy": {"type": "boolean", "description": "Old switch."}
  },
  "required": ["agents"],
  "definitions": {
    "Agent": {
      "type": "object",
      "description": "An agent.",
      "required": ["model"],
      "properties": {
        "model": {"type": "string", "description": "Model name."},
        "max_iterations": {"type": "string"},
        "mode": {"type": "string", "enum": ["fast", "slow", "legacy"]},
        "hooks": {"$ref": "#/definitions/Hooks"},
        "budget": {
          "oneOf": [
            {"type": "integer"},
            {"type": "object", "properties": {"total": {"type": "integer"}}, "required": ["total"]}
          ]
        },
        "tags": {"type": "array", "items": {"type": "string"}},
        "strategies": {
          "type": "array",
          "items": {
            "type": "object",
            "properties": {
              "type": {"type": "string"},
              "chunking": {"type": "object", "properties": {"size": {"type": "integer"}}}
            }
          }
        },
        "args": {
          "type": "object",
          "additionalProperties": {"type": "object", "properties": {"type": {"type": "string"}}}
        },
        "shell": {
          "type": "object",
          "additionalProperties": false,
          "patternProperties": {"^[a-z]+$": {"type": "object", "properties": {"cmd": {"type": "string"}}}}
        }
      }
    },
    "Hooks": {"type": "object", "properties": {"pre": {"type": "array", "items": {"type": "string"}}}},
    "Gone": {"type": "object", "description": "Removed later.", "properties": {"x": {"type": "string"}}}
  }
}`

const newFixture = `{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "properties": {
    "version": {"type": "string", "enum": ["0", "1", "2", "9", "10", "11"]},
    "agents": {"type": "object", "additionalProperties": {"$ref": "#/definitions/Agent"}},
    "routing": {"$ref": "#/definitions/Routing"}
  },
  "required": ["agents", "version"],
  "definitions": {
    "Agent": {
      "type": "object",
      "description": "An agent. Now with routing.",
      "required": ["model", "mode"],
      "properties": {
        "model": {"type": "string", "description": "Model name."},
        "max_iterations": {"type": "integer"},
        "mode": {"type": "string", "enum": ["fast", "slow", "auto"]},
        "hooks": {"$ref": "#/definitions/Hooks"},
        "budget": {
          "oneOf": [
            {"type": "integer"},
            {"type": "object", "properties": {"total": {"type": "integer"}, "type": {"type": "string", "enum": ["tokens"]}},
             "required": ["total"]}
          ]
        },
        "tags": {"type": "array", "items": {"type": "string"}},
        "strategies": {
          "type": "array",
          "items": {
            "type": "object",
            "properties": {
              "type": {"type": "string"},
              "chunking": {"type": "object", "properties": {"size": {"type": "integer"}, "overlap": {"type": "integer"}}}
            }
          }
        },
        "args": {
          "type": "object",
          "additionalProperties": {"type": "object", "properties": {"type": {"type": "string"}, "required": {"type": "boolean"}}}
        },
        "shell": {
          "type": "object",
          "additionalProperties": false,
          "patternProperties": {"^[a-z]+$": {"type": "object", "properties": {"cmd": {"type": "string"}, "env": {"type": "object"}}}}
        }
      }
    },
    "Hooks": {"type": "object", "properties": {"pre": {"type": "array", "items": {"type": "string"}}}},
    "Routing": {
      "type": "object",
      "description": "Routing policy. Maps choices to agents.",
      "properties": {"routes": {"type": "object", "additionalProperties": {"type": "string"}, "description": "Choice to agent (e.g. yes: a)."}}
    }
  }
}`

func mustCompare(t *testing.T, oldRaw, newRaw string) *Diff {
	t.Helper()
	d, err := Compare([]byte(oldRaw), []byte(newRaw))
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	return d
}

func propertyPaths(props []Property) []string {
	out := make([]string, len(props))
	for i, p := range props {
		out[i] = p.Path
	}
	return out
}

func TestCompareFixture(t *testing.T) {
	d := mustCompare(t, oldFixture, newFixture)

	wantVersion := VersionChange{
		Old: []string{"0", "1", "2", "9", "10"}, New: []string{"0", "1", "2", "9", "10", "11"},
		Added: []string{"11"}, OldLatest: "10", NewLatest: "11",
	}
	if !reflect.DeepEqual(d.Version, wantVersion) {
		t.Errorf("Version = %+v, want %+v", d.Version, wantVersion)
	}
	if !d.Version.Changed() || !d.Version.LatestChanged() {
		t.Error("version change not detected")
	}

	if got := []string{d.AddedDefinitions[0].Name}; len(d.AddedDefinitions) != 1 || got[0] != "Routing" {
		t.Errorf("AddedDefinitions = %+v", d.AddedDefinitions)
	}
	if len(d.RemovedDefinitions) != 1 || d.RemovedDefinitions[0].Name != "Gone" ||
		d.RemovedDefinitions[0].Type != "object{x}" || d.RemovedDefinitions[0].Description != "Removed later." {
		t.Errorf("RemovedDefinitions = %+v", d.RemovedDefinitions)
	}

	wantAdded := []string{
		"Agent.args{}.required",               // additionalProperties map value
		"Agent.budget|1.type",                 // oneOf branch
		"Agent.shell{}.env",                   // patternProperties map value
		"Agent.strategies[].chunking.overlap", // array items, nested inline object
		"Config.routing",                      // root property
		"Routing.routes",                      // property of an added definition
	}
	if got := propertyPaths(d.AddedProperties); !reflect.DeepEqual(got, wantAdded) {
		t.Errorf("AddedProperties = %q, want %q", got, wantAdded)
	}
	if got := propertyPaths(d.RemovedProperties); !reflect.DeepEqual(got, []string{"Config.legacy", "Gone.x"}) {
		t.Errorf("RemovedProperties = %q", got)
	}
	routes := d.AddedProperties[len(d.AddedProperties)-1]
	if routes.Type != "map<string>" || routes.Description != "Choice to agent (e.g. yes: a)." {
		t.Errorf("Routing.routes = %+v", routes)
	}
	// A $ref without a description borrows the definition's.
	if p := d.AddedProperties[4]; p.Type != "ref:Routing" || p.Description != "Routing policy. Maps choices to agents." {
		t.Errorf("Config.routing = %+v", p)
	}

	// Adding properties to inline objects is not a type change of the parents.
	wantTypes := []TypeChange{{Path: "Agent.max_iterations", Old: "string", New: "integer"}}
	if !reflect.DeepEqual(d.TypeChanges, wantTypes) {
		t.Errorf("TypeChanges = %+v, want %+v", d.TypeChanges, wantTypes)
	}

	wantEnums := []EnumChange{
		{Path: "Agent.mode", Added: []string{"auto"}, Removed: []string{"legacy"}},
		{Path: "Config.version", Added: []string{"11"}},
	}
	if !reflect.DeepEqual(d.EnumChanges, wantEnums) {
		t.Errorf("EnumChanges = %+v, want %+v", d.EnumChanges, wantEnums)
	}

	wantRequired := []RequiredChange{
		{Path: "Agent", Added: []string{"mode"}},
		{Path: "Config", Added: []string{"version"}},
	}
	if !reflect.DeepEqual(d.RequiredChanges, wantRequired) {
		t.Errorf("RequiredChanges = %+v, want %+v", d.RequiredChanges, wantRequired)
	}

	if !reflect.DeepEqual(d.DescriptionChanges, []string{"Agent"}) {
		t.Errorf("DescriptionChanges = %q", d.DescriptionChanges)
	}
	if d.Empty() {
		t.Error("Empty() = true")
	}
	const wantSummary = "config version 10 → 11, 1 definition added, 1 definition removed, 6 properties added, " +
		"2 properties removed, 1 type changed, 2 enums changed, 2 required lists changed, 1 description changed"
	if got := d.Summary(); got != wantSummary {
		t.Errorf("Summary() =\n %q\nwant\n %q", got, wantSummary)
	}
}

func TestCompareIdentical(t *testing.T) {
	d := mustCompare(t, newFixture, newFixture)
	if !d.Empty() {
		t.Fatalf("identical schemas: %s", d.Summary())
	}
	if got := d.Summary(); got != "no configuration schema changes" {
		t.Errorf("Summary() = %q", got)
	}
	if d.Version.LatestChanged() || d.Version.NewLatest != "11" {
		t.Errorf("Version = %+v", d.Version)
	}
}

func TestCompareDoesNotFollowRefs(t *testing.T) {
	oldRaw := `{"properties": {"a": {"$ref": "#/definitions/A"}}, "definitions": {"A": {"type": "object", "properties": {"x": {"type": "string"}}}}}`
	// The definition gains a property: reported once, at A.y, never at Config.a.y.
	newRaw := `{"properties": {"a": {"$ref": "#/definitions/A"}}, "definitions": {"A": {"type": "object", "properties": {"x": {"type": "string"}, "y": {"type": "string"}}}}}`
	d := mustCompare(t, oldRaw, newRaw)
	if got := propertyPaths(d.AddedProperties); !reflect.DeepEqual(got, []string{"A.y"}) {
		t.Errorf("AddedProperties = %q", got)
	}
	if len(d.TypeChanges) != 0 {
		t.Errorf("TypeChanges = %+v", d.TypeChanges)
	}

	// Inline object replaced by a reference: a type change plus removed inline properties.
	inline := `{"properties": {"a": {"type": "object", "properties": {"x": {"type": "string"}}}}, "definitions": {"A": {"type": "object"}}}`
	d = mustCompare(t, inline, oldRaw)
	// A free-form object that gains properties is a type change (object → object{x}).
	want := []TypeChange{{Path: "A", Old: "object", New: "object{x}"}, {Path: "Config.a", Old: "object{x}", New: "ref:A"}}
	if !reflect.DeepEqual(d.TypeChanges, want) {
		t.Errorf("TypeChanges = %+v", d.TypeChanges)
	}
	if got := propertyPaths(d.RemovedProperties); !reflect.DeepEqual(got, []string{"Config.a.x"}) {
		t.Errorf("RemovedProperties = %q", got)
	}
	if got := propertyPaths(d.AddedProperties); !reflect.DeepEqual(got, []string{"A.x"}) {
		t.Errorf("AddedProperties = %q", got)
	}
}

// Toolset-style unions re-declare `type` in anyOf branches to tie each toolset type to its
// required fields; those are constraints, not configuration keys.
func TestCompareUnionConstraints(t *testing.T) {
	oldRaw := `{"definitions": {"Toolset": {
	  "type": "object",
	  "properties": {"type": {"type": "string", "enum": ["mcp", "api"]}, "url": {"type": "string"}},
	  "anyOf": [
	    {"allOf": [{"properties": {"type": {"const": "mcp"}}}, {"required": ["command"]}]},
	    {"properties": {"type": {"enum": ["mcp", "api"]}}}
	  ]}}}`
	newRaw := `{"definitions": {"Toolset": {
	  "type": "object",
	  "properties": {"type": {"type": "string", "enum": ["mcp", "api", "a2a"]}, "url": {"type": "string"}},
	  "anyOf": [
	    {"allOf": [{"properties": {"type": {"const": "mcp"}}}, {"required": ["command"]}]},
	    {"properties": {"type": {"enum": ["mcp", "api", "a2a"]}}},
	    {"allOf": [{"properties": {"type": {"const": "a2a"}}}, {"required": ["url"]}]}
	  ]}}}`
	d := mustCompare(t, oldRaw, newRaw)
	if len(d.AddedProperties) != 0 {
		t.Errorf("AddedProperties = %+v", d.AddedProperties)
	}
	want := []Property{{Path: "Toolset|2|0.type", Type: `const:a2a`, Constraint: true}}
	if !reflect.DeepEqual(d.AddedConstraints, want) {
		t.Errorf("AddedConstraints = %+v, want %+v", d.AddedConstraints, want)
	}
	wantEnums := []EnumChange{
		{Path: "Toolset.type", Added: []string{"a2a"}},
		{Path: "Toolset|1.type", Added: []string{"a2a"}},
	}
	if !reflect.DeepEqual(d.EnumChanges, wantEnums) {
		t.Errorf("EnumChanges = %+v, want %+v", d.EnumChanges, wantEnums)
	}
	if len(d.TypeChanges) != 0 {
		t.Errorf("constraint-only unions changed the type: %+v", d.TypeChanges)
	}
	if !strings.Contains(d.Summary(), "1 union constraint changed") {
		t.Errorf("Summary() = %q", d.Summary())
	}

	// A const change inside a branch is an enum change, not a type change.
	constChanged := strings.Replace(newRaw, `"const": "a2a"`, `"const": "openapi"`, 1)
	d = mustCompare(t, newRaw, constChanged)
	if want := []EnumChange{{Path: "Toolset|2|0.type", Added: []string{"openapi"}, Removed: []string{"a2a"}}}; !reflect.DeepEqual(d.EnumChanges, want) {
		t.Errorf("EnumChanges = %+v", d.EnumChanges)
	}
	if len(d.TypeChanges) != 0 {
		t.Errorf("TypeChanges = %+v", d.TypeChanges)
	}
}

// Properties of union branches that the owning object does not declare are real keys.
func TestCompareDiscriminatedUnionBranches(t *testing.T) {
	oldRaw := `{"definitions": {"Src": {"oneOf": [
	  {"type": "object", "properties": {"kind": {"const": "file"}, "path": {"type": "string"}}}
	]}}}`
	newRaw := `{"definitions": {"Src": {"oneOf": [
	  {"type": "object", "properties": {"kind": {"const": "file"}, "path": {"type": "string"}}},
	  {"type": "object", "properties": {"kind": {"const": "url"}, "url": {"type": "string"}}}
	]}}}`
	d := mustCompare(t, oldRaw, newRaw)
	if got := propertyPaths(d.AddedProperties); !reflect.DeepEqual(got, []string{"Src|1.kind", "Src|1.url"}) {
		t.Errorf("AddedProperties = %q", got)
	}
	if want := []TypeChange{{Path: "Src", Old: "oneOf[object{kind,path}]", New: "oneOf[object{kind,path}|object{kind,url}]"}}; !reflect.DeepEqual(d.TypeChanges, want) {
		t.Errorf("TypeChanges = %+v", d.TypeChanges)
	}
}

func TestCompareErrors(t *testing.T) {
	if _, err := Compare([]byte(`{`), []byte(`{}`)); err == nil || !strings.Contains(err.Error(), "old schema") {
		t.Errorf("invalid old schema: err = %v", err)
	}
	if _, err := Compare([]byte(`{}`), []byte(`[1]`)); err == nil || !strings.Contains(err.Error(), "new schema") {
		t.Errorf("non-object new schema: err = %v", err)
	}
	if _, err := Compare([]byte(`{}`), []byte(`null`)); err == nil {
		t.Error("null schema accepted")
	}
	d, err := Compare([]byte(`{}`), []byte(`{}`))
	if err != nil || !d.Empty() || d.Version.NewLatest != "" {
		t.Errorf("empty schemas: %+v, %v", d, err)
	}
}

func TestVersionLatest(t *testing.T) {
	for _, tc := range []struct {
		values []string
		want   string
	}{
		{[]string{"0", "1", "2", "10", "9"}, "10"},
		{[]string{"16", "2"}, "16"},
		{[]string{"latest"}, "latest"},
		{[]string{"1.5", "1.10"}, "1.5"},
		{nil, ""},
	} {
		if got := latest(tc.values); got != tc.want {
			t.Errorf("latest(%q) = %q, want %q", tc.values, got, tc.want)
		}
	}
	v := compareVersions([]string{"0", "1", "2"}, []string{"1", "2", "3"})
	if !reflect.DeepEqual(v.Added, []string{"3"}) || !reflect.DeepEqual(v.Removed, []string{"0"}) || v.OldLatest != "2" || v.NewLatest != "3" {
		t.Errorf("compareVersions = %+v", v)
	}
}

func TestTypeString(t *testing.T) {
	for _, tc := range []struct{ schema, want string }{
		{`{"type": "string"}`, "string"},
		{`{"type": "integer", "minimum": 0}`, "integer"},
		{`{"type": "array", "items": {"type": "string"}}`, "array<string>"},
		{`{"type": "array"}`, "array<any>"},
		{`{"type": "array", "items": [{"type": "string"}]}`, "array<tuple>"},
		{`{"type": "object", "additionalProperties": {"$ref": "#/definitions/Toolset"}}`, "map<ref:Toolset>"},
		{`{"type": "object", "patternProperties": {"^a$": {"type": "string"}}, "additionalProperties": false}`, "map<string>"},
		{`{"type": "object", "additionalProperties": {"type": ["object", "null"]}}`, "map<object|null>"},
		{`{"oneOf": [{"type": "boolean"}, {"type": "array", "items": {"type": "string"}}]}`, "oneOf[boolean|array<string>]"},
		{`{"$ref": "#/definitions/HooksConfig", "description": "x"}`, "ref:HooksConfig"},
		{`{"type": "object", "properties": {"b": {}, "a": {}}}`, "object{a,b}"},
		{`{"properties": {"a": {}}}`, "object{a}"},
		{`{"type": "object", "properties": {"a": {}}, "additionalProperties": {"type": "integer"}}`, "object{a}+map<integer>"},
		{`{"type": "object", "properties": {"a": {}, "b": {}, "c": {}, "d": {}, "e": {}, "f": {}, "g": {}, "h": {}, "i": {}, "j": {}}}`,
			"object{a,b,c,d,e,f,g,h,…+2}"},
		{`{"type": "object"}`, "object"},
		{`{"type": ["string", "null"]}`, "string|null"},
		{`{}`, "any"},
		{`{"description": "free-form"}`, "any"},
		{`{"const": "mcp"}`, "const:mcp"},
		{`{"const": 3}`, "const:3"},
		{`{"enum": ["a", "b"]}`, "string"},
		{`{"enum": [1, 2]}`, "integer"},
		{`{"enum": ["a", 1]}`, "enum"},
		// Constraint-only unions on a typed node are not part of the type...
		{`{"type": "object", "properties": {"a": {}}, "oneOf": [{"required": ["a"]}, {"not": {"required": ["a"]}}]}`, "object{a}"},
		// ...but typed branches are, and a free-form object defers to them.
		{`{"type": "object", "allOf": [{"$ref": "#/definitions/RAGConfig"}, {"type": "object", "properties": {"x": {}}}]}`,
			"allOf[ref:RAGConfig|object{x}]"},
		{`{"type": "object", "properties": {"a": {}}, "anyOf": [{"$ref": "#/definitions/X"}]}`, "object{a}&anyOf[ref:X]"},
		// allOf drops pure constraints next to typed branches.
		{`{"allOf": [{"$ref": "#/definitions/HookDefinition"}, {"not": {"required": ["x"]}}]}`, "allOf[ref:HookDefinition]"},
		{`{"oneOf": [{"required": ["a"]}, {"required": ["b"]}]}`, "oneOf[any|any]"},
		{`{"type": "array", "items": {"oneOf": [{"type": "string"}, {"$ref": "#/definitions/Remote"}]}}`, "array<oneOf[string|ref:Remote]>"},
	} {
		var m map[string]any
		dec := json.NewDecoder(strings.NewReader(tc.schema))
		dec.UseNumber()
		if err := dec.Decode(&m); err != nil {
			t.Fatalf("%s: %v", tc.schema, err)
		}
		if got := TypeString(m); got != tc.want {
			t.Errorf("TypeString(%s) = %q, want %q", tc.schema, got, tc.want)
		}
	}
}

func TestPathOrderIsNatural(t *testing.T) {
	paths := []string{"Toolset|10.type", "Toolset|2.type", "A.b", "A", "Toolset|1.type", "A.b[].c", "A.a"}
	sort.Slice(paths, func(i, j int) bool { return lessPath(paths[i], paths[j]) })
	want := []string{"A", "A.a", "A.b", "A.b[].c", "Toolset|1.type", "Toolset|2.type", "Toolset|10.type"}
	if !reflect.DeepEqual(paths, want) {
		t.Errorf("sorted = %q, want %q", paths, want)
	}
	if lessPath("a1", "a1") || !lessPath("a01", "a1") && !lessPath("a1", "a01") {
		t.Error("lessPath is not a strict order on equal-valued digit runs")
	}
}

func TestPropertyPaths(t *testing.T) {
	paths, err := PropertyPaths([]byte(newFixture))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Config.version", "Config.routing", "Agent.args{}.required", "Agent.budget|1.total",
		"Agent.strategies[].chunking.size", "Agent.shell{}.cmd", "Routing.routes",
	} {
		if !contains(paths, want) {
			t.Errorf("PropertyPaths misses %s (got %q)", want, paths)
		}
	}
	if _, err := PropertyPaths([]byte(`nope`)); err == nil {
		t.Error("invalid JSON accepted")
	}
}

func TestMultipleMapValueSchemas(t *testing.T) {
	raw := `{"definitions": {"M": {"type": "object",
	  "additionalProperties": {"type": "object", "properties": {"a": {}}},
	  "patternProperties": {"^x.y|z$": {"type": "object", "properties": {"b": {}}}}}}}`
	paths, err := PropertyPaths([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"M{^x.y|z$}.b", "M{}.a"}; !reflect.DeepEqual(paths, want) {
		t.Errorf("PropertyPaths = %q, want %q", paths, want)
	}
}
