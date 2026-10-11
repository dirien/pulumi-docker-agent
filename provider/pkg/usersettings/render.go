// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

// Package usersettings renders and validates docker-agent's user configuration file
// (`~/.config/cagent/config.yaml`) with docker-agent's own Go types
// (github.com/docker/docker-agent/pkg/userconfig).
//
// The typed provider inputs are encoded into a generic document (Encode), decoded into a
// userconfig.Config with the same YAML library and options docker-agent uses to load the
// file, checked with docker-agent's validators (Validate), and marshalled back the way
// docker-agent saves the file. The output is therefore byte-for-byte what
// `docker agent alias add` or the TUI's /settings dialog would write for the same
// configuration (minus comments), which keeps refreshes stable when docker-agent rewrites
// the file.
package usersettings

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/docker/docker-agent/pkg/userconfig"
	"github.com/goccy/go-yaml"

	"github.com/dirien/pulumi-docker-agent/provider/pkg/validate"
)

// FormatVersion is the user configuration format version docker-agent writes.
const FormatVersion = userconfig.CurrentVersion

// DefaultPath is where docker-agent reads the user configuration on Linux and macOS
// (`paths.GetConfigDir()`), relative to the home directory.
const DefaultPath = "~/.config/cagent/config.yaml"

// Render decodes the encoded document into docker-agent's userconfig.Config and returns
// it together with the YAML docker-agent would write for it. The `version` key is always
// set to FormatVersion, like docker-agent's own save does.
func Render(doc map[string]any) (*userconfig.Config, string, error) {
	if _, ok := doc["version"]; ok {
		return nil, "", fmt.Errorf("extra key %q is managed by the provider (always %q)", "version", FormatVersion)
	}
	data, err := json.Marshal(doc)
	if err != nil {
		return nil, "", fmt.Errorf("encoding user settings: %w", err)
	}
	cfg := &userconfig.Config{}
	// The same decoder docker-agent's userconfig.Load uses (goccy/go-yaml); JSON is YAML.
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, "", errors.New(CleanError(err.Error()))
	}
	cfg.Version = FormatVersion
	out, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, "", fmt.Errorf("rendering user settings: %s", CleanError(err.Error()))
	}
	return cfg, string(out), nil
}

// SHA256 returns the hex-encoded SHA-256 of s.
func SHA256(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// position matches the "[line:column] " prefix goccy/go-yaml adds to errors. The
// positions refer to the provider's internal encoding, not to anything the user wrote.
var position = regexp.MustCompile(`(?m)^\s*\[\d+:\d+\]\s*`)

// CleanError strips ANSI codes, YAML source excerpts (which could echo secret values) and
// internal positions from a docker-agent or YAML error.
func CleanError(msg string) string {
	msg = validate.Clean(msg)
	msg = position.ReplaceAllString(msg, "")
	return strings.TrimSpace(msg)
}
