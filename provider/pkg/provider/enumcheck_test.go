// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dirien/pulumi-docker-agent/provider/pkg/render"
	"github.com/dirien/pulumi-docker-agent/provider/pkg/usersettings"
)

func failureProperties(fs []p.CheckFailure) []string {
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		out = append(out, f.Property)
	}
	slices.Sort(out)
	return out
}

// TestAgentConfigEnumValues checks that a value outside an enum fails Check on its property,
// and that `skipValidation` accepts it (with the defaults still applied), so the escape hatch
// covers enum values that a newer docker-agent added.
func TestAgentConfigEnumValues(t *testing.T) {
	t.Parallel()
	s := server(t)
	inputs := func(skip bool, extra map[string]any) map[string]any {
		agent := map[string]any{
			"name": "root", "model": "openai/gpt-5-mini", "safety": "yolo",
			"hooks": map[string]any{"sessionStart": []any{map[string]any{"type": "webhook", "command": "x"}}},
		}
		for k, v := range extra {
			agent[k] = v
		}
		in := map[string]any{"agents": []any{agent}}
		if skip {
			in["skipValidation"] = true
		}
		return in
	}

	strict := check(t, s, agentConfigToken, "enums", inputs(false, nil))
	assert.Equal(t, []string{"agents[0].hooks.sessionStart[0].type", "agents[0].safety"},
		failureProperties(strict.Failures), failureText(strict.Failures))
	assert.Contains(t, failureText(strict.Failures), `"yolo" is not a valid value for the enum "SafetyMode"`)

	skipped := check(t, s, agentConfigToken, "enums", inputs(true, nil))
	require.Empty(t, skipped.Failures, failureText(skipped.Failures))
	assert.Equal(t, render.DefaultConfigVersion, skipped.Inputs.Get("version").AsString(), "defaults are applied")
	agent := skipped.Inputs.Get("agents").AsArray().Get(0).AsMap()
	assert.Equal(t, "yolo", agent.Get("safety").AsString())
	created, err := s.Create(p.CreateRequest{Urn: urn(agentConfigToken, "enums"), Properties: skipped.Inputs})
	require.NoError(t, err)
	assert.Contains(t, created.Properties.Get("yaml").AsString(), "safety: yolo")
	assert.Contains(t, created.Properties.Get("yaml").AsString(), "type: webhook")

	// Other failures are still reported, without the enum ones.
	mixed := check(t, s, agentConfigToken, "enums", inputs(true, map[string]any{"maxIterations": "many"}))
	require.Len(t, mixed.Failures, 1, failureText(mixed.Failures))
	assert.Contains(t, mixed.Failures[0].Reason, "maxIterations")
}

// TestUserSettingsEnumValues is TestAgentConfigEnumValues for UserSettings, including the
// default hook type.
func TestUserSettingsEnumValues(t *testing.T) {
	t.Parallel()
	s := server(t)
	inputs := func(skip bool) map[string]any {
		in := map[string]any{
			"path": filepath.Join(t.TempDir(), "config.yaml"),
			"settings": map[string]any{
				"safety": "yolo",
				"layout": map[string]any{"sidebarPosition": "middle"},
				"hooks":  map[string]any{"sessionStart": []any{map[string]any{"command": "echo hi"}}},
			},
		}
		if skip {
			in["skipValidation"] = true
		}
		return in
	}

	strict := check(t, s, userSettingsToken, "us", inputs(false))
	assert.Equal(t, []string{"settings.layout.sidebarPosition", "settings.safety"}, failureProperties(strict.Failures),
		failureText(strict.Failures))

	skipped := check(t, s, userSettingsToken, "us", inputs(true))
	require.Empty(t, skipped.Failures, failureText(skipped.Failures))
	settings := skipped.Inputs.Get("settings").AsMap()
	assert.Equal(t, "yolo", settings.Get("safety").AsString())
	assert.Equal(t, "middle", settings.Get("layout").AsMap().Get("sidebarPosition").AsString())
	hook := settings.Get("hooks").AsMap().Get("sessionStart").AsArray().Get(0).AsMap()
	assert.Equal(t, "command", hook.Get("type").AsString(), "defaults are applied")
}

func enumStrings[T ~string](values []infer.EnumValue[T]) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = string(v.Value)
	}
	return out
}

// TestUserSettingsEnumsMatchDockerAgent ties the UserSettings enums to the value lists that
// pkg/usersettings checks against the docker-agent source (TestTUIEnumsMatchDockerAgent,
// TestSafetyModesMatchDockerAgent).
func TestUserSettingsEnumsMatchDockerAgent(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct{ ours, upstream []string }{
		"SafetyMode":            {enumStrings(render.SafetyMode("").Values()), usersettings.SafetyModes},
		"BusySendMode":          {enumStrings(BusySendMode("").Values()), usersettings.BusySendModes},
		"InterruptConfirmation": {enumStrings(InterruptConfirmation("").Values()), usersettings.InterruptConfirmations},
		"SidebarPosition":       {enumStrings(SidebarPosition("").Values()), usersettings.SidebarPositions},
		"SectionSpacing":        {enumStrings(SectionSpacing("").Values()), usersettings.SectionSpacings},
		"SidebarInfoMode":       {enumStrings(SidebarInfoMode("").Values()), usersettings.SidebarInfoModes},
	} {
		assert.Equal(t, tc.upstream, tc.ours, name)
	}
}

func TestFillZero(t *testing.T) {
	t.Parallel()
	type inner struct {
		Kind  *string
		Count int
	}
	type outer struct {
		Name   string
		Flag   *bool
		Items  []inner
		ByName map[string]inner
		Ptr    *inner
	}
	ptr := func(s string) *string { return &s }
	yes := true
	dst := outer{
		Name:   "kept",
		Items:  []inner{{Kind: ptr("odd")}},
		ByName: map[string]inner{"a": {Count: 2}},
		Ptr:    &inner{Count: 1},
	}
	src := outer{
		Name:   "kept",
		Flag:   &yes,
		Items:  []inner{{Kind: ptr("default"), Count: 3}},
		ByName: map[string]inner{"a": {Kind: ptr("default"), Count: 2}},
		Ptr:    &inner{Kind: ptr("default"), Count: 1},
	}
	fillZero(reflect.ValueOf(&dst).Elem(), reflect.ValueOf(src))
	want := outer{
		Name:   "kept",
		Flag:   &yes,
		Items:  []inner{{Kind: ptr("odd"), Count: 3}},
		ByName: map[string]inner{"a": {Kind: ptr("default"), Count: 2}},
		Ptr:    &inner{Kind: ptr("default"), Count: 1},
	}
	assert.Equal(t, want, dst)
}
