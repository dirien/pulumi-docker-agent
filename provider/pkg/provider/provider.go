// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

// Package provider implements the Docker Agent Pulumi provider with pulumi-go-provider's
// infer framework.
package provider

import (
	"context"
	"fmt"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
	"github.com/pulumi/pulumi/sdk/v3/go/common/tokens"

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
	return withUnknownInputs(withLocalFilePreview(withPreviewOutputs(withConfigVersionDiff(prov))))
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
		WithKeywords("pulumi", "docker", "docker-agent", "ai", "agents", "category/utility", "kind/native").
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
			"java": map[string]any{
				"basePackage":                     "io.github.dirien",
				"buildFiles":                      "gradle",
				"gradleNexusPublishPluginVersion": "2.0.0",
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
