// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"runtime/debug"
	"strconv"
	"strings"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi/sdk/v3/go/common/tokens"
	"github.com/pulumi/pulumi/sdk/v3/go/property"

	"github.com/dirien/pulumi-docker-agent/provider/pkg/validate"
)

// Null values in inputs. A program can pass null where the schema has a map value or a list
// element, for example `env: {X: None}` in Python or `X: null` in YAML. pulumi-go-provider
// v1.7.0 panics when it decodes such a null into a typed Go map or slice (`reflect: call of
// reflect.Value.Type on zero Value`), which crashed the provider. withNullInputs handles them
// for every resource, function and the provider configuration, before anything decodes them:
//
//   - a null value of a map is an absent key, Pulumi's meaning of null, and is dropped;
//   - a null element of a list fails the Check (or the call) with the property's path: a list
//     has no absent elements;
//   - values that the Go type holds as `any` (`extra`, `providerOpts`, `triggers`, ...) are
//     passed on as they are: a null there is valid and reaches the YAML;
//   - a panic that still happens in Check, CheckConfig, Configure or Invoke becomes a failure
//     or an error instead of ending the provider, with the stack trace in the provider's debug
//     log (see panicked).

// inputTypes are the Go types that the inputs of each resource and function are decoded into.
var inputTypes = map[tokens.Type]reflect.Type{
	agentConfigType:                                  reflect.TypeFor[AgentConfigArgs](),
	agentArtifactType:                                reflect.TypeFor[AgentArtifactArgs](),
	userSettingsType:                                 reflect.TypeFor[UserSettingsArgs](),
	agentsIgnoreType:                                 reflect.TypeFor[AgentsIgnoreArgs](),
	tokens.Type(Name + ":index:AgentRun"):            reflect.TypeFor[AgentRunArgs](),
	tokens.Type(Name + ":index:getAgent"):            reflect.TypeFor[GetAgentArgs](),
	tokens.Type(Name + ":index:validateAgentConfig"): reflect.TypeFor[ValidateAgentConfigArgs](),
}

var configInputType = reflect.TypeFor[Config]()

const nullElementReason = "a list element must not be null"

func withNullInputs(prov p.Provider) p.Provider {
	check, checkConfig, configure, invoke := prov.Check, prov.CheckConfig, prov.Configure, prov.Invoke
	prov.Check = func(ctx context.Context, req p.CheckRequest) (resp p.CheckResponse, err error) {
		defer recoverCheck(ctx, &resp, &err, "checking the inputs of "+string(req.Urn.Type()), req.Inputs)
		t := inputTypes[req.Urn.Type()]
		var nulls []string
		req.Inputs, nulls = withoutNulls(req.Inputs, t)
		req.State, _ = withoutNulls(req.State, t)
		if len(nulls) > 0 {
			return p.CheckResponse{Inputs: req.Inputs, Failures: nullFailures(nulls)}, nil
		}
		return check(ctx, req)
	}
	prov.CheckConfig = func(ctx context.Context, req p.CheckRequest) (resp p.CheckResponse, err error) {
		defer recoverCheck(ctx, &resp, &err, "checking the provider configuration", req.Inputs)
		// `pulumi import` passes structured values as JSON strings (see withImportedProviderConfig).
		var nulls []string
		req.Inputs, nulls = withoutNulls(typedConfig(req.Inputs), configInputType)
		req.State, _ = withoutNulls(typedConfig(req.State), configInputType)
		if len(nulls) > 0 {
			return p.CheckResponse{Inputs: req.Inputs, Failures: nullFailures(nulls)}, nil
		}
		return checkConfig(ctx, req)
	}
	prov.Configure = func(ctx context.Context, req p.ConfigureRequest) (err error) {
		defer recoverError(ctx, &err, "configuring the provider", req.Args)
		var nulls []string
		req.Args, nulls = withoutNulls(typedConfig(req.Args), configInputType)
		if len(nulls) > 0 {
			return fmt.Errorf("invalid provider configuration: %s: %s", strings.Join(nulls, ", "), nullElementReason)
		}
		return configure(ctx, req)
	}
	prov.Invoke = func(ctx context.Context, req p.InvokeRequest) (resp p.InvokeResponse, err error) {
		defer recoverError(ctx, &err, "running "+string(req.Token), req.Args)
		var nulls []string
		req.Args, nulls = withoutNulls(req.Args, inputTypes[req.Token])
		if len(nulls) > 0 {
			return p.InvokeResponse{Failures: nullFailures(nulls)}, nil
		}
		return invoke(ctx, req)
	}
	return prov
}

func nullFailures(paths []string) []p.CheckFailure {
	out := make([]p.CheckFailure, 0, len(paths))
	for _, path := range paths {
		out = append(out, p.CheckFailure{Property: path, Reason: nullElementReason})
	}
	return out
}

// recoverCheck turns a panic of a Check into a failure (see panicked).
func recoverCheck(ctx context.Context, resp *p.CheckResponse, err *error, doing string, inputs property.Map) {
	if r := recover(); r != nil {
		*resp, *err = p.CheckResponse{Failures: []p.CheckFailure{{Reason: panicked(ctx, r, doing, inputs)}}}, nil
	}
}

// recoverError turns a panic into an error (see panicked).
func recoverError(ctx context.Context, err *error, doing string, inputs property.Map) {
	if r := recover(); r != nil {
		*err = errors.New(panicked(ctx, r, doing, inputs))
	}
}

// panicked reports a panic, which is a bug of the provider or a library it uses, not
// necessarily of the inputs: the stack trace goes to the provider's debug log (`pulumi up
// --debug` shows it), and the returned message names what the provider was doing. Both are
// redacted with the secret input values; a panic message names types and paths, not values.
func panicked(ctx context.Context, r any, doing string, inputs property.Map) string {
	secrets := secretStrings(inputs)
	msg := validate.Redact(fmt.Sprint(r), secrets)
	userLog(ctx).Debug(validate.Redact(fmt.Sprintf("panic while %s: %s\n%s", doing, msg, debug.Stack()), secrets))
	return fmt.Sprintf("the provider failed while %s (please report this at %s/issues; --debug shows the "+
		"stack trace): %s", doing, repository, msg)
}

// withoutNulls drops the null map values that t cannot hold and returns the paths of the null
// list elements it cannot hold. Without a type (an unknown token), m is returned as it is.
func withoutNulls(m property.Map, t reflect.Type) (property.Map, []string) {
	if t == nil || m.Len() == 0 {
		return m, nil
	}
	var nulls []string
	v := withoutNullValues(property.New(m), t, "", &nulls)
	if !v.IsMap() {
		return m, nulls
	}
	return v.AsMap(), nulls
}

// withoutNullValues returns v, for decoding into t, without the null map values t cannot hold,
// and adds the paths of the null list elements to nulls. Secretness and dependencies are kept.
func withoutNullValues(v property.Value, t reflect.Type, path string, nulls *[]string) property.Value {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if v.IsComputed() || v.IsNull() {
		return v
	}
	keep := func(nv property.Value) property.Value {
		return nv.WithSecret(v.Secret()).WithDependencies(v.Dependencies())
	}
	switch t.Kind() {
	case reflect.Map:
		if !v.IsMap() {
			return v
		}
		elem := t.Elem()
		out := map[string]property.Value{}
		for k, e := range v.AsMap().All {
			if e.IsNull() && !holdsNull(elem) {
				continue
			}
			out[k] = withoutNullValues(e, elem, path+"["+strconv.Quote(k)+"]", nulls)
		}
		return keep(property.New(property.NewMap(out)))
	case reflect.Slice, reflect.Array:
		if !v.IsArray() {
			return v
		}
		elem := t.Elem()
		out := make([]property.Value, 0, v.AsArray().Len())
		for i, e := range v.AsArray().All {
			p := path + "[" + strconv.Itoa(i) + "]"
			if e.IsNull() && !holdsNull(elem) {
				*nulls = append(*nulls, p)
			}
			out = append(out, withoutNullValues(e, elem, p, nulls))
		}
		return keep(property.New(out))
	case reflect.Struct:
		if !v.IsMap() {
			return v
		}
		fields := pulumiFields(t)
		out := map[string]property.Value{}
		for k, e := range v.AsMap().All {
			if ft, ok := fields[k]; ok {
				sub := k
				if path != "" {
					sub = path + "." + k
				}
				e = withoutNullValues(e, ft, sub, nulls)
			}
			out[k] = e
		}
		return keep(property.New(property.NewMap(out)))
	}
	return v
}

// holdsNull reports whether a value of type t can be decoded from null: an `any`.
func holdsNull(t reflect.Type) bool { return t.Kind() == reflect.Interface }

// pulumiFields returns the field types of a struct by their `pulumi` names, including those
// of embedded structs.
func pulumiFields(t reflect.Type) map[string]reflect.Type {
	out := map[string]reflect.Type{}
	for i := range t.NumField() {
		f := t.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("pulumi"), ",")
		switch {
		case name != "":
			out[name] = f.Type
		case f.Anonymous:
			ft := f.Type
			for ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				for k, v := range pulumiFields(ft) {
					if _, ok := out[k]; !ok {
						out[k] = v
					}
				}
			}
		}
	}
	return out
}
