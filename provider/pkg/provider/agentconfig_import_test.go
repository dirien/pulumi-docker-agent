// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docker/docker-agent/pkg/config"
	"github.com/docker/docker-agent/pkg/config/hcl"
	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/integration"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dirien/pulumi-docker-agent/provider/pkg/ociagent"
)

// upstreamDir holds docker-agent's example configurations (see the render package's
// TestRoundTripUpstreamExamples).
var upstreamDir = filepath.Join("..", "render", "testdata", "upstream")

// upstreamConfigs returns the YAML and HCL example configurations of docker-agent,
// subdirectories included, relative to upstreamDir.
func upstreamConfigs(t *testing.T) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(upstreamDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if ext := filepath.Ext(path); ext == ".yaml" || ext == ".hcl" {
			rel, err := filepath.Rel(upstreamDir, path)
			if err != nil {
				return err
			}
			files = append(files, rel)
		}
		return nil
	})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(files), 190, "the upstream examples are missing; run make generate_config_types")
	return files
}

// loadedJSON loads a configuration with the docker-agent library, the way `docker agent run`
// loads a file (HCL included), and returns the loaded config as JSON.
func loadedJSON(t *testing.T, src config.Source) string {
	t.Helper()
	cfg, err := config.Load(t.Context(), hcl.NewSource(src))
	require.NoError(t, err, "%s does not load", src.Name())
	b, err := json.MarshalIndent(cfg, "", "  ")
	require.NoError(t, err)
	return string(b)
}

// importAgentConfigFor runs an AgentConfig import like the engine: Read with the import ID
// and no state, then Check with the read inputs as old and new inputs. It checks that the
// inputs need no further defaults, that the read state is what Create computes for them,
// and that a program with exactly these inputs previews no changes. It returns the read
// response.
func importAgentConfigFor(t *testing.T, s integration.Server, id string) p.ReadResponse {
	t.Helper()
	u := urn(agentConfigToken, "imported")
	read, err := s.Read(p.ReadRequest{ID: id, Urn: u})
	require.NoError(t, err)
	assert.Equal(t, "imported", read.ID, "an imported AgentConfig gets its name as ID, like a created one")

	checked, err := s.Check(p.CheckRequest{Urn: u, State: read.Inputs, Inputs: read.Inputs})
	require.NoError(t, err)
	require.Empty(t, checked.Failures, failureText(checked.Failures))
	assert.True(t, property.New(read.Inputs).Equals(property.New(checked.Inputs)),
		"Check changed the imported inputs:\nread:    %v\nchecked: %v", read.Inputs, checked.Inputs)

	diff, err := s.Diff(p.DiffRequest{ID: read.ID, Urn: u, State: read.Properties, OldInputs: read.Inputs, Inputs: checked.Inputs})
	require.NoError(t, err)
	assert.False(t, diff.HasChanges, "the first preview after the import has changes: %v", diff.DetailedDiff)

	created, err := s.Create(p.CreateRequest{Urn: u, Properties: checked.Inputs})
	require.NoError(t, err)
	assert.True(t, property.New(created.Properties).Equals(property.New(read.Properties)),
		"the imported state differs from what Create computes:\nread:    %v\ncreated: %v", read.Properties, created.Properties)
	return read
}

// TestAgentConfigImportUpstreamExamples imports every example configuration docker-agent
// ships with a `file://` ID. The imported inputs must render a configuration that
// docker-agent loads equal to the original file.
func TestAgentConfigImportUpstreamExamples(t *testing.T) {
	t.Parallel()
	s := server(t)
	for _, rel := range upstreamConfigs(t) {
		t.Run(filepath.ToSlash(rel), func(t *testing.T) {
			t.Parallel()
			file, err := filepath.Abs(filepath.Join(upstreamDir, rel))
			require.NoError(t, err)
			want := loadedJSON(t, config.NewFileSource(file))

			read := importAgentConfigFor(t, s, "file://"+file)
			yaml := read.Properties.Get("yaml").AsString()
			got := loadedJSON(t, config.NewBytesSource("imported.yaml", []byte(yaml)))
			assert.JSONEq(t, want, got, "rendered:\n%s", yaml)
		})
	}
}

// TestAgentConfigImportUpstreamExamplesFromOCI imports every example from an artifact pushed
// to an in-memory registry, with an `oci://` ID.
func TestAgentConfigImportUpstreamExamplesFromOCI(t *testing.T) {
	t.Parallel()
	s := server(t)
	host := newRegistry(t, nil)
	for i, rel := range upstreamConfigs(t) {
		t.Run(filepath.ToSlash(rel), func(t *testing.T) {
			t.Parallel()
			file := filepath.Join(upstreamDir, rel)
			data, err := os.ReadFile(file)
			require.NoError(t, err)
			art, err := ociagent.Build(data, ociagent.Annotations(data, "2026-01-01T00:00:00Z", nil))
			require.NoError(t, err)
			ref := host + "/upstream/example:" + strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel)) + "-" +
				string(rune('a'+i%26))
			_, err = ociagent.Push(t.Context(), ref, art, ociagent.Options{})
			require.NoError(t, err)

			switch {
			case strings.Contains(string(data), "instruction_file"):
				// docker-agent only reads instruction files next to a local configuration.
				_, err := s.Read(p.ReadRequest{ID: "oci://" + ref, Urn: urn(agentConfigToken, "imported")})
				assert.ErrorContains(t, err, "only supported for local file-based configs")
				return
			case filepath.Ext(rel) == ".hcl" && strings.Contains(string(data), "file("):
				// The file() calls of a pulled HCL artifact cannot read local files.
				_, err := s.Read(p.ReadRequest{ID: "oci://" + ref, Urn: urn(agentConfigToken, "imported")})
				assert.ErrorContains(t, err, "reading file")
				return
			}
			want := loadedJSON(t, config.NewFileSource(file))
			read := importAgentConfigFor(t, s, "oci://"+ref)
			yaml := read.Properties.Get("yaml").AsString()
			got := loadedJSON(t, config.NewBytesSource("imported.yaml", []byte(yaml)))
			assert.JSONEq(t, want, got, "rendered:\n%s", yaml)
		})
	}
}

func TestAgentConfigImportIDs(t *testing.T) {
	t.Parallel()
	s := server(t)
	u := urn(agentConfigToken, "imported")
	rel := filepath.Join(upstreamDir, "basic_agent.yaml")
	abs, err := filepath.Abs(rel)
	require.NoError(t, err)
	// Relative paths are relative to the working directory, which Pulumi sets to the project
	// directory (here: the package directory).
	for _, id := range []string{rel, "file://" + rel, "file://" + abs, abs, "FILE://" + abs} {
		read, err := s.Read(p.ReadRequest{ID: id, Urn: u})
		require.NoError(t, err, id)
		assert.Equal(t, "imported", read.ID)
		assert.Equal(t, "root", read.Inputs.Get("agents").AsArray().Get(0).AsMap().Get("name").AsString(), id)
	}

	dir := t.TempDir()
	notConfig := filepath.Join(dir, "agent.txt")
	require.NoError(t, os.WriteFile(notConfig, []byte("agents: {}"), 0o600))
	invalid := filepath.Join(dir, "invalid.yaml")
	require.NoError(t, os.WriteFile(invalid, []byte("agents:\n  root:\n    model: missing\n"), 0o600))
	// The typed inputs would fold the list into one instruction, but docker-agent rejects the
	// original: only what docker-agent loads is imported.
	tooOld := filepath.Join(dir, "too-old.yaml")
	require.NoError(t, os.WriteFile(tooOld, []byte("version: \"11\"\nagents:\n  root:\n    model: openai/gpt-5-mini\n"+
		"    instruction: [a, b]\n"), 0o600))
	for id, want := range map[string]string{
		notConfig:                            "is not a configuration file",
		"file://" + filepath.Join(dir, "no"): "does not exist",
		"oci://":                             "the import ID must be",
		"":                                   "the import ID must be",
		"file://" + invalid:                  "non-existent model 'missing'",
		tooOld:                               "this syntax is supported by config version 15",
	} {
		_, err := s.Read(p.ReadRequest{ID: id, Urn: u})
		assert.ErrorContains(t, err, want, id)
	}
	// A path without a configuration extension works with an explicit file:// scheme.
	read, err := s.Read(p.ReadRequest{ID: "file://" + notConfig, Urn: u})
	assert.ErrorContains(t, err, "agents: at least one agent is required")
	assert.Empty(t, read.ID)
}

// TestAgentConfigImportSkipValidation imports a configuration the pinned docker-agent
// rejects: with the provider's skipValidation, the imported resource sets skipValidation.
func TestAgentConfigImportSkipValidation(t *testing.T) {
	t.Parallel()
	s := server(t)
	require.NoError(t, s.Configure(p.ConfigureRequest{Args: pm(map[string]any{"skipValidation": true})}))
	invalid := filepath.Join(t.TempDir(), "agent.yaml")
	require.NoError(t, os.WriteFile(invalid, []byte("agents:\n  root:\n    model: missing\n    future_key: 42\n"), 0o600))
	read, err := s.Read(p.ReadRequest{ID: invalid, Urn: urn(agentConfigToken, "imported")})
	require.NoError(t, err)
	assert.True(t, read.Inputs.Get("skipValidation").AsBool())
	agent := read.Inputs.Get("agents").AsArray().Get(0).AsMap()
	assert.Equal(t, "missing", agent.Get("model").AsString())
	assert.Equal(t, 42.0, agent.Get("extra").AsMap().Get("future_key").AsNumber(), "untyped keys go to `extra`")
	assert.Contains(t, read.Properties.Get("yaml").AsString(), "future_key: 42")
}

// TestAgentConfigImportDetails checks the parts of an import that the upstream examples do
// not show: empty lists are left out, HCL files are converted, instruction files are
// inlined, and the version defaults to the latest.
func TestAgentConfigImportDetails(t *testing.T) {
	t.Parallel()
	s := server(t)
	u := urn(agentConfigToken, "imported")
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "prompt.md"), []byte("Be brief."), 0o600))
	file := filepath.Join(dir, "agent.yaml")
	require.NoError(t, os.WriteFile(file, []byte(`# A comment.
agents:
  root:
    model: openai/gpt-5-mini
    instruction_file: prompt.md
    toolsets: []
`), 0o600))
	read, err := s.Read(p.ReadRequest{ID: file, Urn: u})
	require.NoError(t, err)
	in := read.Inputs
	assert.Equal(t, "16", in.Get("version").AsString())
	agent := in.Get("agents").AsArray().Get(0).AsMap()
	assert.Equal(t, "Be brief.", agent.Get("instruction").AsString())
	assert.True(t, agent.Get("instructionFile").IsNull())
	assert.True(t, agent.Get("toolsets").IsNull(), "an empty list is left out")

	hclFile := filepath.Join(dir, "agent.hcl")
	require.NoError(t, os.WriteFile(hclFile, []byte(`agent "root" {
  model       = "openai/gpt-5-mini"
  instruction = file("prompt.md")
}
`), 0o600))
	read, err = s.Read(p.ReadRequest{ID: hclFile, Urn: u})
	require.NoError(t, err)
	assert.Equal(t, "Be brief.", read.Inputs.Get("agents").AsArray().Get(0).AsMap().Get("instruction").AsString())
}

// TestAgentConfigRefreshKeepsState checks that a refresh returns the state and inputs the
// engine sent, unchanged, also when they contain secrets.
func TestAgentConfigRefreshKeepsState(t *testing.T) {
	t.Parallel()
	s := server(t)
	u := urn(agentConfigToken, "cfg")
	in := agentConfigInputs("Hi.")
	in["extra"] = map[string]any{"future": map[string]any{"n": 1, "list": []any{"a", true}}}
	in["skipValidation"] = true
	checked := check(t, s, agentConfigToken, "cfg", in)
	require.Empty(t, checked.Failures, failureText(checked.Failures))
	inputs := checked.Inputs.Set("metadata", checked.Inputs.Get("metadata").WithSecret(true))
	created, err := s.Create(p.CreateRequest{Urn: u, Properties: inputs})
	require.NoError(t, err)

	read, err := s.Read(p.ReadRequest{ID: created.ID, Urn: u, Properties: created.Properties, Inputs: inputs})
	require.NoError(t, err)
	assert.Equal(t, created.ID, read.ID)
	assert.Equal(t, created.Properties, read.Properties)
	assert.Equal(t, inputs, read.Inputs)
}

// TestImportedDefaultProviderConfig covers the default provider that `pulumi import` creates:
// the engine checks it with the raw stack configuration (JSON strings for structured values)
// and stores that raw configuration. The program's default provider (checked, and from an SDK
// with a pinned version) must not replace it, while a real configuration change still does.
func TestImportedDefaultProviderConfig(t *testing.T) {
	t.Parallel()
	s := server(t)
	u := urn("pulumi:providers:docker-agent", "default")
	auth := `[{"address":"ghcr.io","username":"me","password":"s3cret"}]`
	raw := property.NewMap(map[string]property.Value{
		"forceHttps":   property.New("true"),
		"registryAuth": property.New(auth).WithSecret(true),
	})
	checked, err := s.CheckConfig(p.CheckRequest{Urn: u, Inputs: raw})
	require.NoError(t, err)
	require.Empty(t, checked.Failures, "the raw configuration of an import is accepted")
	assert.True(t, checked.Inputs.Get("forceHttps").AsBool())
	reg := checked.Inputs.Get("registryAuth")
	require.True(t, reg.IsArray(), "%v", reg)
	assert.Equal(t, "ghcr.io", reg.AsArray().Get(0).AsMap().Get("address").AsString())

	versioned := checked.Inputs.Set("version", property.New("0.1.0"))
	for name, tc := range map[string]struct {
		old, news property.Map
		changes   bool
	}{
		"same configuration":                         {raw, checked.Inputs, false},
		"checked, outputs inherited from the import": {versioned, versioned, false},
		"pinned version":                             {raw, checked.Inputs.Set("version", property.New("0.1.0")), false},
		"nothing recorded":                           {property.Map{}, checked.Inputs.Delete("registryAuth").Delete("forceHttps"), false},
		"changed forceHttps":                         {raw.Set("forceHttps", property.New("false")), checked.Inputs, true},
		"changed registry":                           {raw.Set("registryAuth", property.New(`[{"address":"docker.io"}]`)), checked.Inputs, true},
		"removed registry config":                    {raw, checked.Inputs.Delete("registryAuth"), true},
	} {
		state := property.Map{}
		if v, ok := tc.old.GetOk("version"); ok {
			state = state.Set("version", v)
		}
		diff, err := s.DiffConfig(p.DiffRequest{Urn: u, OldInputs: tc.old, State: state, Inputs: tc.news})
		require.NoError(t, err, name)
		assert.Equal(t, tc.changes, diff.HasChanges, "%s: %v", name, diff.DetailedDiff)
	}
}
