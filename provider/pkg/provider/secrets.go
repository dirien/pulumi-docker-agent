// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi/sdk/v3/go/property"

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

// logWarnings reports docker-agent loader warnings as Pulumi warnings.
func logWarnings(ctx context.Context, warnings, secrets []string) {
	for _, w := range warnings {
		p.GetLogger(ctx).Warning("docker-agent: " + validate.Redact(w, secrets))
	}
}
