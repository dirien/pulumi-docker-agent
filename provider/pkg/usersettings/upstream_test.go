// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package usersettings

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/docker/docker-agent/pkg/config/latest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The TUI enums and the keybinding tables live in docker-agent packages that pull in the
// whole TUI (Bubble Tea v2), so the provider keeps copies. These tests read the pinned
// docker-agent module's source and fail when a docker-agent upgrade changes the values.

func dockerAgentDir(t *testing.T) string {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "go", "list", "-m", "-f", "{{.Dir}}",
		"github.com/docker/docker-agent").Output()
	require.NoError(t, err, "locating the docker-agent module")
	dir := strings.TrimSpace(string(out))
	require.NotEmpty(t, dir, "the docker-agent module is not downloaded; run `go mod download`")
	return dir
}

func parseFile(t *testing.T, path string) *ast.File {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	require.NoError(t, err)
	return f
}

// constantsOfType returns the string constants declared with the given type, in order.
func constantsOfType(f *ast.File, typeName string) []string {
	var out []string
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs := spec.(*ast.ValueSpec)
			if id, ok := vs.Type.(*ast.Ident); !ok || id.Name != typeName {
				continue
			}
			for _, v := range vs.Values {
				if lit, ok := v.(*ast.BasicLit); ok && lit.Kind == token.STRING {
					s, _ := strconv.Unquote(lit.Value)
					out = append(out, s)
				}
			}
		}
	}
	return out
}

// mapKeys returns the string keys of the map literal assigned to the package variable name.
func mapKeys(f *ast.File, name string) []string {
	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		vs, ok := n.(*ast.ValueSpec)
		if !ok || len(vs.Names) != 1 || vs.Names[0].Name != name || len(vs.Values) != 1 {
			return true
		}
		lit, ok := vs.Values[0].(*ast.CompositeLit)
		if !ok {
			return false
		}
		for _, e := range lit.Elts {
			if kv, ok := e.(*ast.KeyValueExpr); ok {
				if k, ok := kv.Key.(*ast.BasicLit); ok && k.Kind == token.STRING {
					s, _ := strconv.Unquote(k.Value)
					out = append(out, s)
				}
			}
		}
		return false
	})
	slices.Sort(out)
	return out
}

// actionNames returns the first string of every entry of the slice literal returned by
// the function name (docker-agent's actionMapFor table).
func actionNames(f *ast.File, name string) []string {
	var out []string
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Name.Name != name {
			continue
		}
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			ret, ok := n.(*ast.ReturnStmt)
			if !ok || len(ret.Results) != 1 {
				return true
			}
			lit, ok := ret.Results[0].(*ast.CompositeLit)
			if !ok {
				return true
			}
			for _, e := range lit.Elts {
				entry, ok := e.(*ast.CompositeLit)
				if !ok || len(entry.Elts) == 0 {
					continue
				}
				if s, ok := entry.Elts[0].(*ast.BasicLit); ok && s.Kind == token.STRING {
					v, _ := strconv.Unquote(s.Value)
					out = append(out, v)
				}
			}
			return false
		})
	}
	slices.Sort(out)
	return out
}

func sorted(s []string) []string {
	out := slices.Clone(s)
	slices.Sort(out)
	return out
}

func TestTUIEnumsMatchDockerAgent(t *testing.T) {
	t.Parallel()
	f := parseFile(t, filepath.Join(dockerAgentDir(t), "pkg", "tui", "messages", "settings.go"))
	for typ, ours := range map[string][]string{
		"SendMode":        BusySendModes,
		"InterruptMode":   InterruptConfirmations,
		"SidebarPosition": SidebarPositions,
		"SectionSpacing":  SectionSpacings,
		"SidebarInfoMode": SidebarInfoModes,
	} {
		upstream := constantsOfType(f, typ)
		require.NotEmpty(t, upstream, "docker-agent no longer declares %s constants", typ)
		assert.Equal(t, sorted(upstream), sorted(ours), "values of docker-agent's %s changed", typ)
	}
}

func TestSafetyModesMatchDockerAgent(t *testing.T) {
	t.Parallel()
	f := parseFile(t, filepath.Join(dockerAgentDir(t), "pkg", "config", "latest", "types.go"))
	assert.Equal(t, sorted(constantsOfType(f, "SafetyMode")), sorted(SafetyModes))
	for _, m := range SafetyModes {
		assert.NoError(t, latest.SafetyMode(m).Validate(), m)
	}
}

func TestKeybindingTablesMatchDockerAgent(t *testing.T) {
	t.Parallel()
	f := parseFile(t, filepath.Join(dockerAgentDir(t), "pkg", "tui", "core", "keys.go"))
	assert.Equal(t, actionNames(f, "actionMapFor"), sorted(KeybindingActions), "remappable TUI actions changed")
	assert.Equal(t, mapKeys(f, "keyModifiers"), sorted(keyModifiers), "key modifiers changed")
	assert.Equal(t, mapKeys(f, "namedKeys"), sorted(namedKeys), "named keys changed")
}
