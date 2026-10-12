// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"

	"github.com/pulumi/pulumi-go-provider/infer"

	rootversion "github.com/dirien/pulumi-docker-agent/provider"
	"github.com/dirien/pulumi-docker-agent/provider/pkg/ociagent"
)

// Config is the provider configuration (`docker-agent:*`).
type Config struct {
	RegistryAuth   []RegistryAuth `pulumi:"registryAuth,optional"`
	SkipValidation *bool          `pulumi:"skipValidation,optional"`
	ForceHTTPS     *bool          `pulumi:"forceHttps,optional"`
}

// Annotate describes the provider configuration.
func (c *Config) Annotate(a infer.Annotator) {
	a.Describe(&c.RegistryAuth, "Credentials for OCI registries, used to push, pull and delete agent "+
		"artifacts. They are sent from the first request to a matching registry. Registries without an entry "+
		"fall back to the Docker credential store (`~/.docker/config.json` or `$DOCKER_CONFIG`, including "+
		"credential helpers), so `docker login` is enough for most setups.")
	a.Describe(&c.SkipValidation, "Skip validating agent configurations and user settings with the bundled "+
		"docker-agent library for every resource of this provider. Use it when your configuration relies on "+
		"docker-agent features newer than the library version the provider was built with.")
	a.SetDefault(&c.SkipValidation, false, "DOCKER_AGENT_SKIP_VALIDATION")
	a.Describe(&c.ForceHTTPS, "Refuse plain-HTTP connections to registries. Like the `docker agent` CLI, the provider "+
		"tries HTTPS first and falls back to plain HTTP for `localhost`, `*.localhost`, loopback and private "+
		"(RFC 1918: 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16) addresses, which sends registry credentials "+
		"unencrypted. With `forceHttps` only localhost and loopback registries may use plain HTTP; set "+
		"`insecure: true` on a resource or `getAgent` call to allow it for a specific registry.")
	a.SetDefault(&c.ForceHTTPS, false, "DOCKER_AGENT_FORCE_HTTPS")
}

// RegistryAuth holds credentials for one registry.
type RegistryAuth struct {
	Address  string  `pulumi:"address"`
	Username *string `pulumi:"username,optional"`
	Password *string `pulumi:"password,optional" provider:"secret"`
}

// Annotate describes RegistryAuth.
func (r *RegistryAuth) Annotate(a infer.Annotator) {
	a.Describe(&r, "Credentials for an OCI registry.")
	a.Describe(&r.Address, "The registry host, for example `ghcr.io`, `docker.io` or `localhost:5000`.")
	a.Describe(&r.Username, "The registry username.")
	a.Describe(&r.Password, "The registry password or access token.")
}

// skipValidation reports whether validation is disabled, either for one resource or for
// the whole provider.
func skipValidation(ctx context.Context, resource *bool) bool {
	if resource != nil && *resource {
		return true
	}
	cfg := configFrom(ctx)
	return cfg.SkipValidation != nil && *cfg.SkipValidation
}

// configFrom returns the provider configuration, or an empty one when none is available
// (for example in unit tests that construct a resource directly).
func configFrom(ctx context.Context) (cfg Config) {
	defer func() {
		if recover() != nil {
			cfg = Config{}
		}
	}()
	return infer.GetConfig[Config](ctx)
}

// registryOptions builds the registry options for a request.
func registryOptions(ctx context.Context, insecure *bool) ociagent.Options {
	return optionsFor(configFrom(ctx), insecure)
}

func optionsFor(cfg Config, insecure *bool) ociagent.Options {
	creds := make([]ociagent.Credential, 0, len(cfg.RegistryAuth))
	for _, r := range cfg.RegistryAuth {
		c := ociagent.Credential{Address: r.Address}
		if r.Username != nil {
			c.Username = *r.Username
		}
		if r.Password != nil {
			c.Password = *r.Password
		}
		creds = append(creds, c)
	}
	return ociagent.Options{
		Credentials: creds,
		Insecure:    insecure != nil && *insecure,
		ForceHTTPS:  cfg.ForceHTTPS != nil && *cfg.ForceHTTPS,
		UserAgent:   "pulumi-docker-agent/" + rootversion.Version,
	}
}
