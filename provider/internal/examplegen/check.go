// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"math"
	"reflect"
	"strings"

	"github.com/dirien/pulumi-docker-agent/provider/pkg/render"
)

// check proves that a program describes its example exactly: it evaluates the AgentConfig
// arguments the way Pulumi hands them to the provider (objects by input name, every number a
// float64), decodes them into a Document by input name as the provider does, renders it, and
// compares the result with the golden YAML.
func check(prog *program) error {
	vars := map[string]any{}
	for _, r := range prog.ex.reads {
		content := string(r.content)
		if r.vars != nil {
			content = placeholder.ReplaceAllStringFunc(content, func(m string) string {
				return r.vars[placeholder.FindStringSubmatch(m)[1]]
			})
		}
		vars[readVar(r).ts] = content
	}
	for _, c := range prog.consts {
		v, err := eval(c.expr, vars)
		if err != nil {
			return err
		}
		vars[c.name.ts] = v
	}
	args, err := eval(prog.args, vars)
	if err != nil {
		return err
	}
	m := args.(map[string]any)
	delete(m, "skipValidation")
	var doc render.Document
	if err := assign(reflect.ValueOf(&doc).Elem(), m, ""); err != nil {
		return err
	}
	got, err := render.Render(doc)
	if err != nil {
		return err
	}
	if got != prog.ex.golden {
		return fmt.Errorf("the program renders different YAML than the example:\n%s\nwant:\n%s", got, prog.ex.golden)
	}
	return nil
}

// eval returns the value of an expression as Pulumi passes it to the provider.
func eval(e expr, vars map[string]any) (any, error) {
	switch e := e.(type) {
	case *objectExpr:
		out := map[string]any{}
		for _, en := range e.entries {
			switch {
			case en.spread != nil:
				src, ok := vars[en.spread.ts].(map[string]any)
				if !ok {
					return nil, fmt.Errorf("spread of %s, which is not an object", en.spread.ts)
				}
				for k, v := range src {
					out[k] = v
				}
			case en.key != nil:
				v, err := eval(en.value, vars)
				if err != nil {
					return nil, err
				}
				out[en.key.ts] = v
			}
		}
		return out, nil
	case *listExpr:
		out := []any{}
		for _, en := range e.items {
			v, err := eval(en.value, vars)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	case *scalarExpr:
		if i, ok := e.value.(int64); ok {
			return float64(i), nil
		}
		return e.value, nil
	case *varExpr:
		v, ok := vars[e.ts]
		if !ok {
			return nil, fmt.Errorf("undefined variable %s", e.ts)
		}
		return v, nil
	}
	return nil, fmt.Errorf("cannot evaluate %T", e)
}

// assign decodes a Pulumi value into a typed input, by input name.
func assign(dst reflect.Value, v any, p render.Path) error {
	if v == nil {
		return nil
	}
	wrong := func() error { return fmt.Errorf("%s: %T for %s", p, v, dst.Type()) }
	switch dst.Kind() {
	case reflect.Pointer:
		ptr := reflect.New(dst.Type().Elem())
		if err := assign(ptr.Elem(), v, p); err != nil {
			return err
		}
		dst.Set(ptr)
	case reflect.Struct:
		m, ok := v.(map[string]any)
		if !ok {
			return wrong()
		}
		for k, val := range m {
			f, ok := fieldByName(dst.Type(), k)
			if !ok {
				return fmt.Errorf("%s: %s has no input %q", p, dst.Type().Name(), k)
			}
			if err := assign(dst.Field(f.index), val, p.Field(k)); err != nil {
				return err
			}
		}
	case reflect.Slice:
		l, ok := v.([]any)
		if !ok {
			return wrong()
		}
		s := reflect.MakeSlice(dst.Type(), len(l), len(l))
		for i, item := range l {
			if err := assign(s.Index(i), item, p.Index(i)); err != nil {
				return err
			}
		}
		dst.Set(s)
	case reflect.Map:
		m, ok := v.(map[string]any)
		if !ok {
			return wrong()
		}
		out := reflect.MakeMap(dst.Type())
		for k, val := range m {
			elem := reflect.New(dst.Type().Elem()).Elem()
			if err := assign(elem, val, p.Key(k)); err != nil {
				return err
			}
			out.SetMapIndex(reflect.ValueOf(k), elem)
		}
		dst.Set(out)
	case reflect.Interface:
		dst.Set(reflect.ValueOf(v))
	case reflect.String:
		s, ok := v.(string)
		if !ok {
			return wrong()
		}
		dst.SetString(s)
	case reflect.Bool:
		b, ok := v.(bool)
		if !ok {
			return wrong()
		}
		dst.SetBool(b)
	case reflect.Int, reflect.Int64:
		f, ok := v.(float64)
		if !ok || f != math.Trunc(f) {
			return wrong()
		}
		dst.SetInt(int64(f))
	case reflect.Float64:
		f, ok := v.(float64)
		if !ok {
			return wrong()
		}
		dst.SetFloat(f)
	default:
		return fmt.Errorf("%s: unsupported %s", p, strings.ToLower(dst.Kind().String()))
	}
	return nil
}
