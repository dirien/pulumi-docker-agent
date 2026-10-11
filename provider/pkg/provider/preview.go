// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi/sdk/v3/go/common/tokens"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
)

const agentArtifactType = tokens.Type(Name + ":index:AgentArtifact")

// withPreviewOutputs post-processes Create and Update results on the raw property maps.
//
// pulumi-go-provider v1.7.0 marks an output unknown during an update preview only when an
// input it depends on is unknown or changed, and it ignores inputs that were absent
// before (infer/resource.go:589-592). Adding `metadata` to an AgentConfig or
// `annotations` to an AgentArtifact would therefore preview stale outputs. This wrapper
// decides computedness itself:
//
//   - AgentConfig computes its real outputs during previews; they are replaced by unknowns
//     only when an input is unknown. It also sets the secretness of agentNames,
//     defaultAgent and requiredEnvVars from the specific inputs they come from.
//   - AgentArtifact update previews mark digest, pinnedReference, created and
//     manifestAnnotations unknown whenever the YAML or the annotations change, because a
//     new manifest will be pushed.
func withPreviewOutputs(prov p.Provider) p.Provider {
	create, update := prov.Create, prov.Update
	prov.Create = func(ctx context.Context, req p.CreateRequest) (p.CreateResponse, error) {
		resp, err := create(ctx, req)
		if err == nil && req.Urn.Type() == agentConfigType {
			resp.Properties = agentConfigOutputs(req.Properties, resp.Properties, req.DryRun)
		}
		return resp, err
	}
	prov.Update = func(ctx context.Context, req p.UpdateRequest) (p.UpdateResponse, error) {
		resp, err := update(ctx, req)
		if err != nil {
			return resp, err
		}
		switch req.Urn.Type() {
		case agentConfigType:
			resp.Properties = agentConfigOutputs(req.Inputs, resp.Properties, req.DryRun)
		case agentArtifactType:
			if req.DryRun {
				resp.Properties = agentArtifactPreviewOutputs(req.State, req.Inputs, resp.Properties)
			}
		}
		return resp, nil
	}
	return prov
}

func agentConfigOutputs(inputs, out property.Map, dryRun bool) property.Map {
	names, namesKnown, namesSecret := agentNamesFrom(inputs)
	if dryRun && (inputs.HasComputed() || !out.Get("yaml").IsString() || out.Get("yaml").AsString() == "") {
		out = markUnknown(out, "yaml", "sha256", "requiredEnvVars")
		if namesKnown {
			out = out.Set("agentNames", property.New(names))
			def := ""
			if len(names) > 0 {
				def = defaultAgentOf(names)
			}
			out = out.Set("defaultAgent", property.New(def))
		} else {
			out = markUnknown(out, "agentNames", "defaultAgent")
		}
	}
	if namesSecret {
		out = markSecret(out, "agentNames", "defaultAgent")
	}
	if tokenKeySecret(inputs) {
		out = markSecret(out, "requiredEnvVars")
	}
	return out
}

func agentArtifactPreviewOutputs(state, inputs, out property.Map) property.Map {
	yamlChanged := !sameString(state.Get("yaml"), inputs.Get("yaml"))
	annotationsChanged := !sameStringMap(state.Get("annotations"), inputs.Get("annotations"))
	neverPushed := !state.Get("digest").IsString() || state.Get("digest").AsString() == ""
	if yamlChanged || annotationsChanged || neverPushed {
		out = markUnknown(out, "digest", "pinnedReference", "created", "manifestAnnotations")
	}
	if yamlChanged {
		out = markUnknown(out, "contentSha256")
	}
	return out
}

// agentNamesFrom returns the agent names from raw inputs, whether they are all known, and
// whether any of them is secret.
func agentNamesFrom(inputs property.Map) (names []property.Value, known, secret bool) {
	agents := inputs.Get("agents")
	if agents.IsComputed() || !agents.IsArray() {
		return nil, false, agents.Secret()
	}
	known = true
	secret = agents.Secret()
	for _, a := range agents.AsArray().All {
		secret = secret || a.Secret()
		if a.IsComputed() || !a.IsMap() {
			known = false
			continue
		}
		n := a.AsMap().Get("name")
		secret = secret || n.Secret()
		if !n.IsString() {
			known = false
			continue
		}
		names = append(names, property.New(n.AsString()))
	}
	return names, known, secret
}

func defaultAgentOf(names []property.Value) string {
	for _, n := range names {
		if n.AsString() == "root" {
			return "root"
		}
	}
	return names[0].AsString()
}

// tokenKeySecret reports whether a `tokenKey` (an environment variable name that ends up
// in requiredEnvVars) of a model or provider is secret.
func tokenKeySecret(inputs property.Map) bool {
	for _, section := range []string{"models", "providers"} {
		v := inputs.Get(section)
		if v.Secret() {
			return true
		}
		if !v.IsMap() {
			continue
		}
		for _, entry := range v.AsMap().All {
			if entry.Secret() || (entry.IsMap() && entry.AsMap().Get("tokenKey").Secret()) {
				return true
			}
		}
	}
	return false
}

func markUnknown(m property.Map, keys ...string) property.Map {
	for _, k := range keys {
		m = m.Set(k, property.New(property.Computed).WithSecret(m.Get(k).Secret()))
	}
	return m
}

func markSecret(m property.Map, keys ...string) property.Map {
	for _, k := range keys {
		if v, ok := m.GetOk(k); ok {
			m = m.Set(k, v.WithSecret(true))
		}
	}
	return m
}

func sameString(a, b property.Value) bool {
	if a.HasComputed() || b.HasComputed() {
		return false
	}
	if !a.IsString() || !b.IsString() {
		return (a.IsNull() || (a.IsString() && a.AsString() == "")) && (b.IsNull() || (b.IsString() && b.AsString() == ""))
	}
	return a.AsString() == b.AsString()
}

func sameStringMap(a, b property.Value) bool {
	if a.HasComputed() || b.HasComputed() {
		return false
	}
	toMap := func(v property.Value) map[string]string {
		out := map[string]string{}
		if v.IsMap() {
			for k, e := range v.AsMap().All {
				if e.IsString() {
					out[k] = e.AsString()
				}
			}
		}
		return out
	}
	am, bm := toMap(a), toMap(b)
	if len(am) != len(bm) {
		return false
	}
	for k, v := range am {
		if w, ok := bm[k]; !ok || w != v {
			return false
		}
	}
	return true
}
