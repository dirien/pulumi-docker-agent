// Package provider holds build metadata for the Docker Agent Pulumi provider.
//
// The provider implementation lives in pkg/provider. This package only exists so
// the release build can inject the version with
//
//	-ldflags "-X github.com/dirien/pulumi-docker-agent/provider.Version=<version>"
package provider

// Version is the provider version. It is set at build time through ldflags.
var Version = "0.0.1-alpha.0"
