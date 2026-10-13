// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

// Package provider implements the Docker Agent Pulumi provider with pulumi-go-provider's
// infer framework.
package provider

import (
	"context"
	"fmt"
	"strings"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
	"github.com/pulumi/pulumi/sdk/v3/go/common/tokens"
	"github.com/pulumi/pulumi/sdk/v3/go/property"

	"github.com/dirien/pulumi-docker-agent/provider/pkg/agentrun"
	"github.com/dirien/pulumi-docker-agent/provider/pkg/validate"
)

// Name is the Pulumi package name.
const Name = "docker-agent"

// DockerAgentVersion is the docker-agent library version the provider validates against
// and writes to artifact annotations.
const DockerAgentVersion = validate.DockerAgentVersion

const agentConfigType = tokens.Type(Name + ":index:AgentConfig")

const (
	repository = "https://github.com/dirien/pulumi-docker-agent"
	// Docker's logo as a PNG (the avatar of the docker GitHub organization). It must be a
	// PNG or JPEG because the .NET SDK embeds it as the NuGet package icon.
	logoURL = "https://avatars.githubusercontent.com/u/5429470?s=200&v=4"
)

const description = "A Pulumi provider for Docker Agent (docker-agent): define AI agent configurations " +
	"as typed, validated code and share them as OCI artifacts, the same way `docker agent share push` does."

// Provider returns the Docker Agent provider.
func Provider() p.Provider {
	prov, err := Builder().Build()
	if err != nil {
		panic(fmt.Errorf("unable to build provider: %w", err))
	}
	prov = withImportedProviderConfig(withAgentConfigRead(withUserSettingsSecrets(withFrameworkKeysDiffConfig(prov))))
	prov = withUnknownInputs(withLocalFilePreview(withPreviewOutputs(withConfigVersionDiff(prov))))
	return withNullInputs(withConfigUpdatesInPlace(withShutdownWatch(withCommandScopedState(prov))))
}

// withFrameworkKeysDiffConfig makes the inferred DiffConfig compare the same keys on both
// sides. CheckConfig stores `__pulumi-go-provider-infer` and `__pulumi-go-provider-version`
// in the provider's inputs, and the engine adds `version`; the inferred DiffConfig drops
// them from the new inputs only (pulumi-go-provider v1.7.0, infer/resource.go) and diffs
// `version` from the old state itself. So every DiffConfig saw them deleted from the old
// inputs and reported a replacement: on a provider version upgrade of an SDK program (its
// default provider is default_<version>) and on a move to another provider instance, which
// replaced every resource of the provider.
//
// It wraps the inferred DiffConfig directly: withImportedProviderConfig needs the `__`
// marker to tell a checked configuration from the raw one of `pulumi import`.
func withFrameworkKeysDiffConfig(prov p.Provider) p.Provider {
	inner := prov.DiffConfig
	prov.DiffConfig = func(ctx context.Context, req p.DiffRequest) (p.DiffResponse, error) {
		req.OldInputs = withoutFrameworkKeys(req.OldInputs).Delete("version")
		req.State = withoutFrameworkKeys(req.State)
		return inner(ctx, req)
	}
	return prov
}

// withConfigUpdatesInPlace reports every change of the provider configuration as an update
// of the provider, never as a replacement. pulumi-go-provider replaces a provider on any
// configuration change (pulumi-go-provider#409), and the engine then replaces every resource
// of the provider: every AgentRun ran again, and the old resource's deletePrompt ran after
// the new run, in another provider process. No setting of this provider identifies a
// resource (`registryAuth`, `skipValidation` and `forceHttps` only change how the provider
// works), so the engine can configure the provider again in place: a rotated registry
// password, a toggled setting and DOCKER_AGENT_SKIP_VALIDATION or DOCKER_AGENT_FORCE_HTTPS in
// the environment of one command keep every resource.
func withConfigUpdatesInPlace(prov p.Provider) p.Provider {
	inner := prov.DiffConfig
	prov.DiffConfig = func(ctx context.Context, req p.DiffRequest) (p.DiffResponse, error) {
		resp, err := inner(ctx, req)
		if err != nil {
			return resp, err
		}
		for k, d := range resp.DetailedDiff {
			switch d.Kind {
			case p.AddReplace:
				d.Kind = p.Add
			case p.UpdateReplace:
				d.Kind = p.Update
			case p.DeleteReplace:
				d.Kind = p.Delete
			}
			resp.DetailedDiff[k] = d
		}
		resp.DeleteBeforeReplace = false
		return resp, nil
	}
	return prov
}

// withShutdownWatch starts agentrun.WatchShutdownSignals when the engine configures the
// provider: from then on, a SIGINT (Pulumi stops a provider with SIGINT) refuses new agent
// runs and stops the running ones (their run children, which stop their Claude Code CLIs and
// tools). The engine configures a provider before it creates,
// updates or deletes resources with it, and the Pulumi SDK installed its own SIGINT handler
// before it served the first request, so the watcher never takes a SIGINT that would have
// ended the provider.
func withShutdownWatch(prov p.Provider) p.Provider {
	inner := prov.Configure
	prov.Configure = func(ctx context.Context, req p.ConfigureRequest) error {
		agentrun.WatchShutdownSignals()
		return inner(ctx, req)
	}
	return prov
}

// withoutFrameworkKeys returns m without the keys that start with `__`.
func withoutFrameworkKeys(m property.Map) property.Map {
	for k := range m.All {
		if strings.HasPrefix(k, "__") {
			m = m.Delete(k)
		}
	}
	return m
}

// withConfigVersionDiff adds the diff of AgentConfig's `version` input. pulumi-go-provider
// removes an input called `version` before diffing (the name is reserved for provider
// versions), so AgentConfig.Diff never sees it.
func withConfigVersionDiff(prov p.Provider) p.Provider {
	inner := prov.Diff
	prov.Diff = func(ctx context.Context, req p.DiffRequest) (p.DiffResponse, error) {
		resp, err := inner(ctx, req)
		if err != nil || req.Urn.Type() != agentConfigType {
			return resp, err
		}
		olds := req.OldInputs
		if olds.Len() == 0 {
			olds = req.State
		}
		oldVersion, newVersion := olds.Get("version"), req.Inputs.Get("version")
		if newVersion.IsComputed() || !newVersion.Equals(oldVersion) {
			if resp.DetailedDiff == nil {
				resp.DetailedDiff = map[string]p.PropertyDiff{}
			}
			resp.DetailedDiff["version"] = p.PropertyDiff{Kind: p.Update, InputDiff: true}
			resp.HasChanges = true
		}
		return resp, nil
	}
	return prov
}

// Builder returns the configured provider builder. It is exported for tests.
func Builder() *infer.ProviderBuilder {
	return infer.NewProviderBuilder().
		WithDisplayName("Docker Agent").
		WithDescription(description).
		WithHomepage(repository).
		WithRepository(repository).
		WithPublisher("dirien").
		WithLogoURL(logoURL).
		WithLicense("Apache-2.0").
		// The Registry matches its search box against these; it drops "pulumi" and the
		// category/ and kind/ tags, and has no AI category (utility is the closest).
		WithKeywords("pulumi", "docker", "docker-agent", "cagent", "ai", "agents", "genai", "llm", "mcp", "oci",
			"category/utility", "kind/native").
		WithNamespace("dirien").
		WithPluginDownloadURL("github://api.github.com/dirien/pulumi-docker-agent").
		WithLanguageMap(map[string]any{
			"nodejs": map[string]any{
				"packageName":          "@ediri/pulumi-docker-agent",
				"respectSchemaVersion": true,
			},
			"python": map[string]any{
				"packageName":          "ediri_docker_agent",
				"respectSchemaVersion": true,
				"pyproject":            map[string]any{"enabled": true},
			},
			"csharp": map[string]any{
				"rootNamespace":        "ediri",
				"respectSchemaVersion": true,
			},
			"go": map[string]any{
				"generateResourceContainerTypes": true,
				"importBasePath":                 "github.com/dirien/pulumi-docker-agent/sdk/go/dockeragent",
				"respectSchemaVersion":           true,
			},
			// Maven coordinates io.github.dirien:docker-agent (the Registry derives them from
			// basePackage and the package name), Java package io.github.dirien.dockeragent. The
			// io.github.dirien namespace is verifiable on the Maven Central Portal through the
			// dirien GitHub account. The dependencies pin pulumi-java's defaults, so a CLI upgrade
			// cannot change them silently.
			"java": map[string]any{
				"basePackage":                     "io.github.dirien",
				"buildFiles":                      "gradle",
				"gradleNexusPublishPluginVersion": "2.0.0",
				"dependencies": map[string]any{
					"com.pulumi:pulumi":               "1.0.0",
					"com.google.code.gson:gson":       "2.8.9",
					"com.google.code.findbugs:jsr305": "3.0.2",
				},
			},
		}).
		WithConfig(infer.Config(&Config{})).
		WithResources(
			infer.Resource(&AgentConfig{}),
			infer.Resource(&AgentArtifact{}),
			infer.Resource(&UserSettings{}),
			infer.Resource(&AgentsIgnore{}),
			NewAgentRunResource(),
		).
		WithFunctions(
			infer.Function(&GetAgent{}),
			infer.Function(&ValidateAgentConfig{}),
		).
		WithModuleMap(map[tokens.ModuleName]tokens.ModuleName{
			"provider": "index",
			"render":   "index",
		})
}
