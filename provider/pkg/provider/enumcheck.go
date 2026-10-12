// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"reflect"
	"strings"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
)

// checkInputs is infer.DefaultCheck for inputs with a `skipValidation` escape hatch. The
// enums of the configuration types come from the pinned docker-agent, and DefaultCheck
// rejects every other value. When skip reports that validation is skipped, those failures
// are dropped, so a value that a newer docker-agent added (or that the user insists on) is
// rendered as it is, like any other input docker-agent would reject. Defaults are applied as
// if the value were valid.
//
// Regression tests: TestAgentConfigEnumValues and TestUserSettingsEnumValues.
func checkInputs[I any](ctx context.Context, inputs property.Map, skip func(I) bool) (I, []p.CheckFailure, error) {
	args, failures, err := infer.DefaultCheck[I](ctx, inputs)
	if err != nil || len(failures) == 0 || !skip(args) {
		return args, failures, err
	}
	var rest []p.CheckFailure
	var enumPaths []string
	for _, f := range failures {
		if isEnumFailure(f) {
			enumPaths = append(enumPaths, f.Property)
		} else {
			rest = append(rest, f)
		}
	}
	if len(enumPaths) == 0 || len(rest) > 0 {
		return args, rest, nil
	}
	// DefaultCheck applies defaults only without failures. Check the inputs again with the
	// rejected values unknown (unknowns are not validated) and take the defaults from there.
	// DefaultCheck also records how to encode the checked inputs, so the last call must be
	// on the real inputs.
	if sanitized, ok := withUnknowns(inputs, enumPaths); ok {
		if defaulted, again, err := infer.DefaultCheck[I](ctx, sanitized); err == nil && len(again) == 0 {
			if args, _, err = infer.DefaultCheck[I](ctx, inputs); err != nil {
				return args, nil, err
			}
			fillZero(reflect.ValueOf(&args).Elem(), reflect.ValueOf(defaulted))
		}
	}
	return args, nil, nil
}

// isEnumFailure reports whether f is pulumi-go-provider's failure for a value that is not one
// of an enum's values (infer.checkEnumValue).
func isEnumFailure(f p.CheckFailure) bool {
	return f.Property != "" && strings.Contains(f.Reason, " is not a valid value for the enum ")
}

// withUnknowns returns inputs with the values at paths replaced by unknowns.
func withUnknowns(inputs property.Map, paths []string) (property.Map, bool) {
	obj := resource.ToResourcePropertyValue(property.New(inputs))
	for _, s := range paths {
		path, err := resource.ParsePropertyPath(s)
		if err != nil || !path.Set(obj, resource.MakeComputed(resource.NewProperty(""))) {
			return property.Map{}, false
		}
	}
	return resource.FromResourcePropertyValue(obj).AsMap(), true
}

// fillZero sets the zero values in dst that are set in src, recursively. dst and src are
// decoded from the same inputs, so they only differ where src has defaults.
func fillZero(dst, src reflect.Value) {
	if !src.IsValid() || src.IsZero() {
		return
	}
	if dst.IsZero() {
		dst.Set(src)
		return
	}
	switch dst.Kind() {
	case reflect.Pointer:
		fillZero(dst.Elem(), src.Elem())
	case reflect.Struct:
		for i := range dst.NumField() {
			if dst.Type().Field(i).IsExported() {
				fillZero(dst.Field(i), src.Field(i))
			}
		}
	case reflect.Slice:
		for i := range min(dst.Len(), src.Len()) {
			fillZero(dst.Index(i), src.Index(i))
		}
	case reflect.Map:
		for _, k := range dst.MapKeys() {
			e := reflect.New(dst.Type().Elem()).Elem()
			e.Set(dst.MapIndex(k))
			fillZero(e, src.MapIndex(k))
			dst.SetMapIndex(k, e)
		}
	}
}
