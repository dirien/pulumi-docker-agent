// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"slices"
	"strings"

	p "github.com/pulumi/pulumi-go-provider"

	"github.com/dirien/pulumi-docker-agent/provider/pkg/render"
	"github.com/dirien/pulumi-docker-agent/provider/pkg/validate"
)

// configFailures turns the problems docker-agent finds in a rendered configuration into
// check failures on the inputs that produced them: a problem docker-agent reports at
// `agents.root.sub_agents` fails `agents[0].subAgents`. Problems at an input that already
// has a failure (an enum value docker-agent rejects too) are not repeated.
func configFailures(yamlDoc string, res validate.Result, secrets []string, existing []p.CheckFailure) []p.CheckFailure {
	if len(res.Problems) == 0 {
		var out []p.CheckFailure
		for _, e := range res.Errors {
			out = append(out, p.CheckFailure{Reason: "invalid docker-agent configuration: " + validate.Redact(e, secrets)})
		}
		return out
	}
	ix, _ := render.IndexYAML([]byte(yamlDoc))
	var out []p.CheckFailure
	for _, pr := range res.Problems {
		property := ""
		if pr.Path != nil {
			if path, ok := ix.Lookup(pr.Path); ok {
				property = string(path)
			}
		}
		if property != "" && slices.ContainsFunc(existing, func(f p.CheckFailure) bool {
			return overlaps(f.Property, property)
		}) {
			continue
		}
		out = append(out, p.CheckFailure{
			Property: property,
			Reason:   "invalid docker-agent configuration: " + validate.Redact(pr.Message, secrets),
		})
	}
	return out
}

// overlaps reports whether one of two input paths contains the other.
func overlaps(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	within := func(inner, outer string) bool {
		return inner == outer || strings.HasPrefix(inner, outer+".") || strings.HasPrefix(inner, outer+"[")
	}
	return within(a, b) || within(b, a)
}
