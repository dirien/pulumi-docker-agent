// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package ociagent

import (
	"net/url"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
)

// Credential is a username and password for one registry.
type Credential struct {
	// Address is the registry host, for example `ghcr.io`, `docker.io` or
	// `localhost:5000`. A scheme and path are ignored.
	Address  string
	Username string
	Password string
}

// StaticKeychain serves fixed credentials by registry host.
type StaticKeychain []Credential

// Resolve implements authn.Keychain.
func (k StaticKeychain) Resolve(target authn.Resource) (authn.Authenticator, error) {
	host := NormalizeRegistry(target.RegistryStr())
	for _, c := range k {
		if NormalizeRegistry(c.Address) == host {
			return authn.FromConfig(authn.AuthConfig{Username: c.Username, Password: c.Password}), nil
		}
	}
	return authn.Anonymous, nil
}

// Keychain returns a keychain that tries creds first and then falls back to the Docker
// keychain (~/.docker/config.json or $DOCKER_CONFIG, including credential helpers).
func Keychain(creds []Credential) authn.Keychain {
	if len(creds) == 0 {
		return authn.DefaultKeychain
	}
	return authn.NewMultiKeychain(StaticKeychain(creds), authn.DefaultKeychain)
}

// NormalizeRegistry turns a registry address into the host[:port] form
// go-containerregistry uses, mapping the Docker Hub aliases to index.docker.io.
func NormalizeRegistry(addr string) string {
	addr = strings.TrimSpace(addr)
	if strings.Contains(addr, "://") {
		if u, err := url.Parse(addr); err == nil {
			addr = u.Host
		}
	}
	if i := strings.Index(addr, "/"); i >= 0 {
		addr = addr[:i]
	}
	addr = strings.ToLower(addr)
	switch addr {
	case "docker.io", "index.docker.io", "registry-1.docker.io", "registry.hub.docker.com":
		return name.DefaultRegistry
	}
	return addr
}
