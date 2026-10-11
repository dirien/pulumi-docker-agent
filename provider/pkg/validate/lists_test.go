// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package validate

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestHardcodedListsMatchPinnedDockerAgent fails when the docker-agent module in go.mod
// (for example after a Dependabot bump) registers toolset types or provider types that
// differ from ToolsetTypes and ProviderTypes. It reads the module's sources instead of
// importing the toolset and provider registries, which would link every LLM SDK into the
// test binary.
func TestHardcodedListsMatchPinnedDockerAgent(t *testing.T) {
	t.Parallel()
	out, err := exec.Command("go", "list", "-m", "-json", "github.com/docker/docker-agent").Output()
	require.NoError(t, err, "go list -m github.com/docker/docker-agent")
	var mod struct{ Version, Dir string }
	require.NoError(t, json.Unmarshal(out, &mod))
	require.NotEmpty(t, mod.Dir, "the docker-agent module is not downloaded")
	require.Equal(t, DockerAgentVersion, mod.Version, "DockerAgentVersion must match the docker-agent version in go.mod")

	want := slices.Sorted(slices.Values(ToolsetTypes))

	// The registry `docker agent toolsets` lists.
	creators := mapLiteralKeys(t, filepath.Join(mod.Dir, "pkg", "teamloader", "toolsets", "toolsets.go"), "DefaultToolsetCreators")
	require.Equal(t, want, creators, "validate.ToolsetTypes differs from toolsets.DefaultToolsetCreators()")

	// The JSON schema's Toolset.type enum.
	raw, err := os.ReadFile(filepath.Join(mod.Dir, "agent-schema.json"))
	require.NoError(t, err)
	var schema struct {
		Definitions struct {
			Toolset struct {
				Properties struct {
					Type struct {
						Enum []string `json:"enum"`
					} `json:"type"`
				} `json:"properties"`
			} `json:"Toolset"`
		} `json:"definitions"`
	}
	require.NoError(t, json.Unmarshal(raw, &schema))
	require.Equal(t, want, slices.Sorted(slices.Values(schema.Definitions.Toolset.Properties.Type.Enum)),
		"validate.ToolsetTypes differs from the Toolset.type enum in agent-schema.json")

	// The native model provider implementations. The OpenAI API flavours are accepted
	// through the "openai_" prefix in hasProvider.
	factories := mapLiteralKeys(t, filepath.Join(mod.Dir, "pkg", "model", "provider", "providers", "providers.go"), "DefaultFactories")
	var native []string
	for _, k := range factories {
		if !hasProvider(k) {
			t.Errorf("provider type %q of providers.DefaultFactories() is not accepted by hasProvider", k)
		}
		if k != "openai_chatcompletions" && k != "openai_responses" {
			native = append(native, k)
		}
	}
	require.Equal(t, slices.Sorted(slices.Values(ProviderTypes)), native,
		"validate.ProviderTypes differs from providers.DefaultFactories()")
}

// mapLiteralKeys returns the sorted string keys of the map literal returned by function fn
// in file.
func mapLiteralKeys(t *testing.T, file, fn string) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
	require.NoError(t, err)
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
					k, err := strconv.Unquote(bl.Value)
					require.NoError(t, err)
					keys = append(keys, k)
				}
			}
			return false
		})
		return false
	})
	require.True(t, found, "no map literal found in %s of %s; update this test for the new upstream code", fn, file)
	slices.Sort(keys)
	return keys
}
