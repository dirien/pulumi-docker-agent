// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"testing"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource/plugin"
	pulumirpc "github.com/pulumi/pulumi/sdk/v3/proto/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/structpb"
)

// TestProviderConfigChangesKeepResources diffs provider configurations the way the engine
// does: through the provider's gRPC server, with the inputs CheckConfig stored (they carry
// pulumi-go-provider's `__` keys) plus the engine's `version`. The engine diffs a provider's
// configuration when it changes (Registry.Diff, served by a provider that is not configured
// yet) and when a resource moves to another provider instance, for example to
// default_<version> after an SDK upgrade (served by the configured new provider, which gets
// old inputs; integration.Server sends none, so it cannot show this). A replacement there
// replaces every resource of the provider, so no change may report one, and every real change
// must be reported, so that the engine configures the provider with it.
func TestProviderConfigChangesKeepResources(t *testing.T) {
	const providerURN = "urn:pulumi:dev::proj::pulumi:providers:docker-agent::default_0_1_2"
	marshal := func(t *testing.T, m resource.PropertyMap) *structpb.Struct {
		t.Helper()
		s, err := plugin.MarshalProperties(m, plugin.MarshalOptions{KeepSecrets: true})
		require.NoError(t, err)
		return s
	}
	auth := func(password string) resource.PropertyValue {
		return resource.NewProperty([]resource.PropertyValue{resource.NewProperty(resource.PropertyMap{
			"address":  resource.NewProperty("registry.example.com"),
			"username": resource.NewProperty("ci"),
			"password": resource.MakeSecret(resource.NewProperty(password)),
		})})
	}
	base := resource.PropertyMap{"skipValidation": resource.NewProperty(false)}
	with := func(k string, v resource.PropertyValue) resource.PropertyMap {
		m := base.Copy()
		m[resource.PropertyKey(k)] = v
		return m
	}
	cases := []struct {
		name       string
		oldVersion string
		old, new   resource.PropertyMap
		changed    bool
		diffs      []string
	}{
		{name: "same configuration and version (a move to another provider instance)", oldVersion: "0.1.2",
			old: base, new: base},
		{name: "version upgrade", oldVersion: "0.1.1", old: base, new: base, changed: true, diffs: []string{"version"}},
		{name: "version upgrade with registryAuth", oldVersion: "0.1.1", old: with("registryAuth", auth("a")),
			new: with("registryAuth", auth("a")), changed: true, diffs: []string{"version"}},
		{name: "registryAuth added", oldVersion: "0.1.2", old: base, new: with("registryAuth", auth("a")), changed: true,
			diffs: []string{"registryAuth"}},
		{name: "registryAuth password rotated", oldVersion: "0.1.2", old: with("registryAuth", auth("a")),
			new: with("registryAuth", auth("b")), changed: true, diffs: []string{"registryAuth"}},
		{name: "registryAuth removed", oldVersion: "0.1.2", old: with("registryAuth", auth("a")), new: base, changed: true,
			diffs: []string{"registryAuth"}},
		{name: "skipValidation set (DOCKER_AGENT_SKIP_VALIDATION)", oldVersion: "0.1.2", old: base,
			new: with("skipValidation", resource.NewProperty(true)), changed: true, diffs: []string{"skipValidation"}},
		{name: "forceHttps set (DOCKER_AGENT_FORCE_HTTPS)", oldVersion: "0.1.2", old: with("forceHttps", resource.NewProperty(false)),
			new: with("forceHttps", resource.NewProperty(true)), changed: true, diffs: []string{"forceHttps"}},
	}
	for _, configured := range []bool{true, false} {
		for _, c := range cases {
			name := c.name
			if !configured {
				name += " (provider not configured)"
			}
			t.Run(name, func(t *testing.T) {
				server := func() pulumirpc.ResourceProviderServer {
					srv, err := p.RawServer(Name, "0.1.2", Provider())(nil)
					require.NoError(t, err)
					return srv
				}
				// The engine checks each configuration with a provider of its own: pulumi-go-provider's
				// CheckConfig decodes into the provider's configuration and keeps keys of a previous one.
				checked := func(version string, config resource.PropertyMap) *structpb.Struct {
					resp, err := server().CheckConfig(t.Context(), &pulumirpc.CheckRequest{Urn: providerURN,
						News: marshal(t, config), Olds: marshal(t, resource.PropertyMap{})})
					require.NoError(t, err)
					require.Empty(t, resp.GetFailures())
					inputs, err := plugin.UnmarshalProperties(resp.GetInputs(), plugin.MarshalOptions{KeepSecrets: true})
					require.NoError(t, err)
					require.Contains(t, inputs, resource.PropertyKey("__pulumi-go-provider-version"))
					inputs["version"] = resource.NewProperty(version)
					return marshal(t, inputs)
				}
				olds, news := checked(c.oldVersion, c.old), checked("0.1.2", c.new)
				srv := server()
				var err error
				if configured {
					_, err = srv.Configure(t.Context(), &pulumirpc.ConfigureRequest{Args: news, AcceptSecrets: true,
						AcceptResources: true, SendsOldInputs: true, SendsOldInputsToDelete: true})
					require.NoError(t, err)
				}
				diff, err := srv.DiffConfig(t.Context(), &pulumirpc.DiffRequest{Urn: providerURN, OldInputs: olds,
					Olds: olds, News: news})
				require.NoError(t, err)
				assert.Empty(t, diff.GetReplaces(), "no configuration change replaces the provider and its resources")
				assert.False(t, diff.GetDeleteBeforeReplace())
				if !c.changed {
					assert.Equal(t, pulumirpc.DiffResponse_DIFF_NONE, diff.GetChanges(), "diffs %v", diff.GetDiffs())
					return
				}
				assert.Equal(t, pulumirpc.DiffResponse_DIFF_SOME, diff.GetChanges(),
					"a change is an update, so that the engine configures the provider with it")
				assert.ElementsMatch(t, c.diffs, diff.GetDiffs())
			})
		}
	}
}
