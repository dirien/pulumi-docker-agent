// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"reflect"
	"strings"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi/sdk/v3/go/property"

	"github.com/dirien/pulumi-docker-agent/provider/pkg/agentrun"
	"github.com/dirien/pulumi-docker-agent/provider/pkg/validate"
)

// secretStrings returns every string value in m that is secret or nested in a secret
// value. Messages shown to users are redacted with it as a second line of defence: the
// validator already drops docker-agent's YAML source excerpts.
func secretStrings(m property.Map) []string {
	var out []string
	var walk func(v property.Value, secret bool)
	walk = func(v property.Value, secret bool) {
		secret = secret || v.Secret()
		switch {
		case v.IsString():
			if secret {
				out = append(out, v.AsString())
			}
		case v.IsArray():
			for _, e := range v.AsArray().All {
				walk(e, secret)
			}
		case v.IsMap():
			for _, e := range v.AsMap().All {
				walk(e, secret)
			}
		}
	}
	for _, v := range m.All {
		walk(v, false)
	}
	return out
}

// agentRunSecretStrings is secretStrings for the inputs of an AgentRun, without the values of
// `env` that are configuration, not credentials (agentrun.NonSecretEnvValue: a region, a project,
// a flag, a base URL without credentials): the Pulumi SDKs mark the whole `env` map secret, so
// every value would otherwise be redacted and make an answer that repeats it secret. A value the
// program marked secret itself (an entry that is secret on its own, as Pulumi YAML passes
// `env: {X: ${secretValue}}`, or one nested in the SDKs' secret map) stays secret.
func agentRunSecretStrings(m property.Map) []string {
	var out []string
	for k, v := range m.All {
		if k != "env" || !v.IsMap() {
			out = append(out, secretStrings(property.NewMap(map[string]property.Value{k: v}))...)
			continue
		}
		for name, e := range v.AsMap().All {
			if e.IsString() && !e.Secret() && agentrun.NonSecretEnvValue(name, e.AsString()) {
				continue
			}
			out = append(out, secretStrings(property.NewMap(map[string]property.Value{name: e.WithSecret(e.Secret() || v.Secret())}))...)
		}
	}
	return out
}

// logWarnings reports docker-agent loader warnings as Pulumi warnings.
func logWarnings(ctx context.Context, warnings, secrets []string) {
	for _, w := range warnings {
		userLog(ctx).Warning("docker-agent: " + validate.Redact(w, secrets))
	}
}

// withUserSettingsSecrets marks the values of a UserSettings's inputs and state that the
// schema declares secret (`env` of a hook, `headers` of an identity token source) as
// secrets, and `yaml` and `sha256`, which contain them, too:
//
//   - Read: the engine stores an import's inputs and state as Read returns them, while a
//     program passes those values as secrets and Create returns them as secrets. Without
//     this, `pulumi import` stores them, and the rendered file, in plain text.
//   - Create and Update: pulumi-go-provider v1.7.0 loses the secretness of a value nested
//     below a map entry when the value's struct has fields after it (its decoder records the
//     path of the secret with an append that the next field overwrites), so
//     `providers.<name>.auth.workloadIdentityFederation.identityToken.headers` would be
//     stored in plain text. In a preview, the same mismatch makes the whole `providers`
//     output unknown. Known inputs are mirrored in the state as they are, so a preview takes
//     them from the request instead.
func withUserSettingsSecrets(prov p.Provider) p.Provider {
	create, update, read := prov.Create, prov.Update, prov.Read
	prov.Create = func(ctx context.Context, req p.CreateRequest) (p.CreateResponse, error) {
		resp, err := create(ctx, req)
		if err == nil && req.Urn.Type() == userSettingsType {
			resp.Properties = userSettingsStateSecrets(req.Properties, resp.Properties, req.DryRun)
		}
		return resp, err
	}
	prov.Update = func(ctx context.Context, req p.UpdateRequest) (p.UpdateResponse, error) {
		resp, err := update(ctx, req)
		if err == nil && req.Urn.Type() == userSettingsType {
			resp.Properties = userSettingsStateSecrets(req.Inputs, resp.Properties, req.DryRun)
		}
		return resp, err
	}
	prov.Read = func(ctx context.Context, req p.ReadRequest) (p.ReadResponse, error) {
		resp, err := read(ctx, req)
		if err != nil || req.Urn.Type() != userSettingsType || resp.ID == "" {
			return resp, err
		}
		inputs, _ := markSchemaSecrets(property.New(resp.Inputs), reflect.TypeFor[UserSettingsArgs]())
		resp.Inputs = inputs.AsMap()
		resp.Properties = userSettingsStateSecrets(resp.Inputs, resp.Properties, false)
		return resp, nil
	}
	return prov
}

// userSettingsStateSecrets marks the schema's secrets in the state of a UserSettings, and
// `yaml` and `sha256` when an input is secret. In a preview, an input that is known in the
// request but unknown in the state is taken from the request: the state embeds the inputs.
func userSettingsStateSecrets(inputs, state property.Map, preview bool) property.Map {
	if preview {
		for k, v := range inputs.All {
			if s, ok := state.GetOk(k); ok && s.IsComputed() && !v.HasComputed() {
				state = state.Set(k, v)
			}
		}
	}
	marked, set := markSchemaSecrets(property.New(state), reflect.TypeFor[UserSettingsState]())
	state = marked.AsMap()
	if set || anySecret(inputs) {
		state = markSecret(state, "yaml", "sha256")
	}
	return state
}

// anySecret reports whether a value of m is secret or contains a secret.
func anySecret(m property.Map) bool {
	for _, v := range m.All {
		if v.HasSecrets() {
			return true
		}
	}
	return false
}

// markSchemaSecrets marks the values in v that the schema declares secret: the fields of t
// (and of the types it contains) tagged `provider:"secret"`. It reports whether one of them
// is set to a non-empty value. The engine stores the inputs and the state an import's Read
// returns as they are, so values that a program would pass as secrets, and that Create would
// return as secrets, must be marked here.
func markSchemaSecrets(v property.Value, t reflect.Type) (property.Value, bool) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if v.IsNull() || v.IsComputed() {
		return v, false
	}
	rebuild := func(x property.Value) property.Value {
		return x.WithSecret(v.Secret()).WithDependencies(v.Dependencies())
	}
	found := false
	switch {
	case t.Kind() == reflect.Struct && v.IsMap():
		m := v.AsMap()
		for f := range schemaFields(t) {
			name, _, _ := strings.Cut(f.Tag.Get("pulumi"), ",")
			e, ok := m.GetOk(name)
			if !ok {
				continue
			}
			if strings.Contains(f.Tag.Get("provider"), "secret") {
				if !e.IsNull() {
					m = m.Set(name, e.WithSecret(true))
					found = found || !isEmptyValue(e) && (!e.IsMap() || e.AsMap().Len() > 0)
				}
				continue
			}
			marked, set := markSchemaSecrets(e, f.Type)
			m = m.Set(name, marked)
			found = found || set
		}
		return rebuild(property.New(m)), found
	case (t.Kind() == reflect.Slice || t.Kind() == reflect.Array) && v.IsArray():
		items := make([]property.Value, 0, v.AsArray().Len())
		for _, e := range v.AsArray().All {
			marked, set := markSchemaSecrets(e, t.Elem())
			items = append(items, marked)
			found = found || set
		}
		return rebuild(property.New(items)), found
	case t.Kind() == reflect.Map && v.IsMap():
		m := v.AsMap()
		for k, e := range v.AsMap().All {
			marked, set := markSchemaSecrets(e, t.Elem())
			m = m.Set(k, marked)
			found = found || set
		}
		return rebuild(property.New(m)), found
	}
	return v, false
}

// schemaFields yields the fields of a struct type that are Pulumi properties, including those
// of embedded structs.
func schemaFields(t reflect.Type) func(func(reflect.StructField) bool) {
	return func(yield func(reflect.StructField) bool) {
		for i := range t.NumField() {
			f := t.Field(i)
			if f.Anonymous && f.Type.Kind() == reflect.Struct {
				for ef := range schemaFields(f.Type) {
					if !yield(ef) {
						return
					}
				}
				continue
			}
			if name, _, _ := strings.Cut(f.Tag.Get("pulumi"), ","); name == "" || !f.IsExported() {
				continue
			}
			if !yield(f) {
				return
			}
		}
	}
}
