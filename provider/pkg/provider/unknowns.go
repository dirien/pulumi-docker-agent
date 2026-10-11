// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
)

// ---------------------------------------------------------------------------------------
// Unknown inputs must survive Check (applies to every resource).
//
// pulumi-go-provider v1.7.0 decodes the inputs into the resource's Go args type in Check
// and encodes the checked args back into a property map. That round trip loses some
// unknown values: a computed required list (AgentConfig `agents`, AgentsIgnore
// `patterns`) comes back as null, and fields of objects inside lists (an agent's `name`,
// a toolset's `type`, a keybinding's `keys`) come back as zero values or disappear. The
// engine sends the checked inputs to the preview's Create/Update, which then fails with
// "Missing required field" or previews made-up values, whenever an input depends on
// another resource's output.
//
// withUnknownInputs puts every value that was unknown in the request back into Check's
// response. It works on raw property maps and does not depend on any resource's input
// types, so it keeps working when those types change (for example when AgentConfig's
// inputs are generated from the docker-agent schema). Check only validates and applies
// defaults, so the request and the response have the same shape; where they differ (the
// response dropped a value that holds unknowns), the request's value is used.
//
// Regression tests: TestUnknownInputsSurviveCheck* in unknowns_test.go.
// ---------------------------------------------------------------------------------------

// withUnknownInputs wraps Check of every resource, see the comment above.
func withUnknownInputs(prov p.Provider) p.Provider {
	check := prov.Check
	prov.Check = func(ctx context.Context, req p.CheckRequest) (p.CheckResponse, error) {
		resp, err := check(ctx, req)
		if err == nil && req.Inputs.HasComputed() {
			resp.Inputs = restoreUnknowns(property.New(req.Inputs), property.New(resp.Inputs)).AsMap()
		}
		return resp, err
	}
	return prov
}

// restoreUnknowns returns checked with every value that is unknown in raw (the inputs the
// engine sent) unknown again. Secretness is kept from both sides.
func restoreUnknowns(raw, checked property.Value) property.Value {
	switch {
	case raw.IsComputed():
		return property.New(property.Computed).WithSecret(raw.Secret() || checked.Secret())
	case !raw.HasComputed():
		return checked
	case checked.IsNull():
		// The checked inputs dropped a value that contains unknowns.
		return raw
	case raw.IsMap() && checked.IsMap():
		out := checked.AsMap()
		for k, v := range raw.AsMap().All {
			if v.HasComputed() {
				out = out.Set(k, restoreUnknowns(v, out.Get(k)))
			}
		}
		return property.New(out).WithSecret(raw.Secret() || checked.Secret())
	case raw.IsArray() && checked.IsArray() && raw.AsArray().Len() == checked.AsArray().Len():
		items := checked.AsArray().AsSlice()
		for i, v := range raw.AsArray().All {
			if v.HasComputed() {
				items[i] = restoreUnknowns(v, items[i])
			}
		}
		return property.New(items).WithSecret(raw.Secret() || checked.Secret())
	}
	return checked
}
