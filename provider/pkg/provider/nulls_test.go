// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"encoding/json"
	"maps"
	"reflect"
	"testing"

	p "github.com/pulumi/pulumi-go-provider"
	purn "github.com/pulumi/pulumi/sdk/v3/go/common/resource/urn"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A null value where the schema has a map value or a list element (Python `None`, YAML `null`)
// panicked in pulumi-go-provider's decoding and crashed the provider. A null map value is an
// absent key; a null list element fails with its path; a null that an `any` holds is kept.
func TestNullInputs(t *testing.T) {
	s := server(t)
	checkOf := func(t *testing.T, typ string, in map[string]any) p.CheckResponse {
		t.Helper()
		var resp p.CheckResponse
		require.NotPanics(t, func() { resp = check(t, s, typ, "nulls", in) })
		return resp
	}
	failures := func(resp p.CheckResponse) map[string]string {
		out := map[string]string{}
		for _, f := range resp.Failures {
			out[f.Property] = f.Reason
		}
		return out
	}
	run := func(extra map[string]any) map[string]any {
		m := map[string]any{"yaml": "agents:\n  root:\n    model: openai/gpt-5-mini\n", "prompt": "hi"}
		maps.Copy(m, extra)
		return m
	}
	config := func(mod func(m, agent map[string]any)) map[string]any {
		m := agentConfigInputs("Help.")
		mod(m, m["agents"].([]any)[1].(map[string]any))
		return m
	}
	settings := func(mod func(m map[string]any)) map[string]any {
		m := userSettingsInputs(t.TempDir()+"/config.yaml", "dark")
		mod(m)
		return m
	}

	t.Run("AgentRun env (the reported panic)", func(t *testing.T) {
		resp := checkOf(t, agentRunToken, run(map[string]any{"env": map[string]any{"X": nil, "Y": "y"}}))
		require.Empty(t, resp.Failures)
		env := resp.Inputs.Get("env").AsMap()
		assert.False(t, has(env, "X"), "null is an absent key")
		assert.Equal(t, "y", env.Get("Y").AsString())
	})
	t.Run("the reviewer's repro", func(t *testing.T) {
		s, _ := loggingServer(t)
		in := runProps(map[string]any{"yaml": harnessYAML, "prompt": "Write NOTES.md"}).Set("env",
			property.New(property.NewMap(map[string]property.Value{"X": property.New(property.Null)})))
		require.NotPanics(t, func() { _ = runCheck(t, s, in) })
	})
	t.Run("AgentRun triggers hold any value", func(t *testing.T) {
		resp := checkOf(t, agentRunToken, run(map[string]any{"triggers": []any{nil, map[string]any{"a": nil}}}))
		require.Empty(t, resp.Failures)
		assert.Equal(t, 2, resp.Inputs.Get("triggers").AsArray().Len())
	})
	t.Run("AgentConfig maps", func(t *testing.T) {
		resp := checkOf(t, agentConfigToken, config(func(m, agent map[string]any) {
			agent["toolsets"] = []any{map[string]any{"type": "shell", "env": map[string]any{"X": nil, "Y": "y"}}}
			m["extra"] = map[string]any{"x": nil}
		}))
		require.Empty(t, resp.Failures, failureText(resp.Failures))
		created, err := s.Create(p.CreateRequest{Urn: urn(agentConfigToken, "nulls"), Properties: resp.Inputs})
		require.NoError(t, err)
		yaml := created.Properties.Get("yaml").AsString()
		assert.Contains(t, yaml, "Y: y")
		assert.NotContains(t, yaml, "X:", "the rendered configuration has no key for the null value")
		ts := resp.Inputs.Get("agents").AsArray().Get(1).AsMap().Get("toolsets").AsArray().Get(0).AsMap()
		assert.False(t, has(ts.Get("env").AsMap(), "X"))
		assert.True(t, resp.Inputs.Get("extra").AsMap().Get("x").IsNull(), "extra holds any value, also null")

		resp = checkOf(t, agentConfigToken, config(func(m, _ map[string]any) { m["models"] = map[string]any{"fast": nil} }))
		assert.Contains(t, failureText(resp.Failures), "references non-existent model 'fast'", "a null model is absent")
	})
	t.Run("AgentConfig lists", func(t *testing.T) {
		resp := checkOf(t, agentConfigToken, config(func(m, agent map[string]any) {
			agent["toolsets"] = []any{nil}
			agent["subAgents"] = []any{"helper", nil}
			m["metadata"] = map[string]any{"tags": []any{nil}}
			m["agents"] = append(m["agents"].([]any), nil)
		}))
		assert.Equal(t, map[string]string{
			"agents[1].toolsets[0]":  "a list element must not be null",
			"agents[1].subAgents[1]": "a list element must not be null",
			"metadata.tags[0]":       "a list element must not be null",
			"agents[2]":              "a list element must not be null",
		}, failures(resp))
	})
	t.Run("AgentArtifact, UserSettings and AgentsIgnore", func(t *testing.T) {
		resp := checkOf(t, agentArtifactToken, map[string]any{"reference": "localhost:5000/a:v1",
			"yaml": "agents:\n  root:\n    model: openai/gpt-5-mini\n", "annotations": map[string]any{"X": nil}})
		require.Empty(t, resp.Failures, failureText(resp.Failures))
		assert.False(t, has(resp.Inputs.Get("annotations").AsMap(), "X"))

		resp = checkOf(t, userSettingsToken, settings(func(m map[string]any) {
			m["aliases"] = map[string]any{"gone": nil, "reviewer": m["aliases"].(map[string]any)["reviewer"]}
		}))
		require.Empty(t, resp.Failures, failureText(resp.Failures))
		assert.False(t, has(resp.Inputs.Get("aliases").AsMap(), "gone"))
		resp = checkOf(t, userSettingsToken, settings(func(m map[string]any) {
			m["sandboxAllowlist"] = []any{nil}
			m["settings"].(map[string]any)["hooks"].(map[string]any)["sessionStart"] = []any{nil}
		}))
		assert.Equal(t, map[string]string{"sandboxAllowlist[0]": "a list element must not be null",
			"settings.hooks.sessionStart[0]": "a list element must not be null"}, failures(resp))

		resp = checkOf(t, agentsIgnoreToken, map[string]any{"directory": t.TempDir(), "patterns": []any{"a", nil}})
		assert.Equal(t, map[string]string{"patterns[1]": "a list element must not be null"}, failures(resp))
	})
	t.Run("provider configuration", func(t *testing.T) {
		const providerURN = "urn:pulumi:test::proj::pulumi:providers:docker-agent::p"
		var resp p.CheckResponse
		var err error
		require.NotPanics(t, func() {
			resp, err = s.CheckConfig(p.CheckRequest{Urn: providerURN, Inputs: pm(map[string]any{"registryAuth": []any{nil}})})
		})
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"registryAuth[0]": "a list element must not be null"}, failures(resp))
		require.NotPanics(t, func() {
			err = s.Configure(p.ConfigureRequest{Args: pm(map[string]any{"registryAuth": []any{nil}})})
		})
		require.ErrorContains(t, err, "registryAuth[0]: a list element must not be null")
		// `pulumi import` passes the configuration as JSON strings.
		require.NotPanics(t, func() {
			resp, err = s.CheckConfig(p.CheckRequest{Urn: providerURN, Inputs: pm(map[string]any{"registryAuth": "[null]"})})
		})
		require.NoError(t, err)
		assert.Contains(t, failures(resp), "registryAuth[0]")
	})
}

func has(m property.Map, k string) bool {
	_, ok := m.GetOk(k)
	return ok
}

// The walk keeps the secretness and dependencies of everything around a removed null.
func TestWithoutNullsKeepsSecretsAndDependencies(t *testing.T) {
	deps := []purn.URN{purn.URN("urn:pulumi:test::proj::docker-agent:index:AgentConfig::other")}
	env := property.New(property.NewMap(map[string]property.Value{
		"X": property.New(property.Null),
		"Y": property.New("y").WithDependencies(deps),
		"Z": property.New(property.Computed),
	})).WithSecret(true).WithDependencies(deps)
	in := property.NewMap(map[string]property.Value{"agents": property.New([]property.Value{property.New(property.NewMap(
		map[string]property.Value{"name": property.New("root"), "toolsets": property.New([]property.Value{
			property.New(property.NewMap(map[string]property.Value{"type": property.New("shell"), "env": env})),
		})}))}).WithDependencies(deps)})

	out, nulls := withoutNulls(in, reflect.TypeFor[AgentConfigArgs]())
	assert.Empty(t, nulls)
	agents := out.Get("agents")
	assert.Equal(t, deps, agents.Dependencies(), "the list keeps its dependencies")
	got := agents.AsArray().Get(0).AsMap().Get("toolsets").AsArray().Get(0).AsMap().Get("env")
	assert.True(t, got.Secret(), "the map stays secret")
	assert.Equal(t, deps, got.Dependencies(), "the map keeps its dependencies")
	assert.False(t, has(got.AsMap(), "X"))
	assert.Equal(t, deps, got.AsMap().Get("Y").Dependencies(), "a value keeps its dependencies")
	assert.True(t, got.AsMap().Get("Z").IsComputed())

	// Without nulls, nothing changes.
	clean := in.Set("agents", property.New([]property.Value{}).WithDependencies(deps))
	same, _ := withoutNulls(clean, reflect.TypeFor[AgentConfigArgs]())
	assert.True(t, same.Get("agents").Equals(clean.Get("agents")))
}

// Every resource and function of the schema has its input type in inputTypes; without it, its
// nulls would reach the decoder.
func TestInputTypesCoverTheSchema(t *testing.T) {
	resp, err := server(t).GetSchema(p.GetSchemaRequest{})
	require.NoError(t, err)
	var schema struct {
		Resources map[string]any `json:"resources"`
		Functions map[string]any `json:"functions"`
	}
	require.NoError(t, json.Unmarshal([]byte(resp.Schema), &schema))
	var want, got []string
	for tok := range schema.Resources {
		want = append(want, tok)
	}
	for tok := range schema.Functions {
		want = append(want, tok)
	}
	for tok, typ := range inputTypes {
		got = append(got, string(tok))
		assert.Equal(t, reflect.Struct, typ.Kind(), "%s", tok)
	}
	assert.ElementsMatch(t, want, got)
}

// A panic in Check, CheckConfig, Configure or Invoke becomes a failure or an error that says
// what the provider was doing, without blaming the inputs and without a secret value, and its
// stack trace goes to the debug log.
func TestNullInputsRecover(t *testing.T) {
	const secret = "sk-secret-in-a-panic-0123456789"
	boom := func() { panic("boom: " + secret) }
	prov := withNullInputs(p.Provider{
		Check:       func(context.Context, p.CheckRequest) (p.CheckResponse, error) { boom(); return p.CheckResponse{}, nil },
		CheckConfig: func(context.Context, p.CheckRequest) (p.CheckResponse, error) { boom(); return p.CheckResponse{}, nil },
		Configure:   func(context.Context, p.ConfigureRequest) error { boom(); return nil },
		Invoke: func(context.Context, p.InvokeRequest) (p.InvokeResponse, error) {
			boom()
			return p.InvokeResponse{}, nil
		},
	})
	rec := &recordingLogger{}
	ctx := context.WithValue(t.Context(), loggerKey{}, rec)
	inputs := property.NewMap(map[string]property.Value{"prompt": property.New(secret).WithSecret(true)})
	const report = "(please report this at https://github.com/dirien/pulumi-docker-agent/issues; --debug shows the " +
		"stack trace): boom: [secret]"
	resp, err := prov.Check(ctx, p.CheckRequest{Urn: runURN("x"), Inputs: inputs})
	require.NoError(t, err)
	require.Len(t, resp.Failures, 1)
	assert.Equal(t, "the provider failed while checking the inputs of docker-agent:index:AgentRun "+report,
		resp.Failures[0].Reason)
	resp, err = prov.CheckConfig(ctx, p.CheckRequest{Inputs: inputs})
	require.NoError(t, err)
	assert.Equal(t, "the provider failed while checking the provider configuration "+report, resp.Failures[0].Reason)
	assert.EqualError(t, prov.Configure(ctx, p.ConfigureRequest{Args: inputs}),
		"the provider failed while configuring the provider "+report)
	_, err = prov.Invoke(ctx, p.InvokeRequest{Token: "docker-agent:index:getAgent", Args: inputs})
	assert.EqualError(t, err, "the provider failed while running docker-agent:index:getAgent "+report)

	debugLog := rec.messages("debug")
	require.Len(t, debugLog, 4)
	for _, m := range debugLog {
		assert.Contains(t, m, "panic while ")
		assert.Contains(t, m, "goroutine ", "the stack trace")
		assert.Contains(t, m, "nulls_test.go", "the stack trace reaches the panic")
		assert.NotContains(t, m, secret)
	}
	assert.NotContains(t, rec.all(), secret)
}
