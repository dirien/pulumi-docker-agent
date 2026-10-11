// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

// Package validate checks docker-agent configurations with the docker-agent library
// (github.com/docker/docker-agent/pkg/config), the same code `docker agent run`,
// `docker agent debug config` and `docker agent share push` use.
package validate

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/docker/docker-agent/pkg/config"
	"github.com/docker/docker-agent/pkg/config/latest"
	"github.com/docker/docker-agent/pkg/environment"
	"gopkg.in/yaml.v3"
)

// DockerAgentVersion, LatestConfigVersion, ToolsetTypes and ProviderTypes are generated
// from the docker-agent module in go.mod (upstream.gen.go, `make generate_config_types`).

// allFeatures enables every optional docker-agent feature: the provider validates
// configurations for the docker-agent CLI, not for a restricted embedder.
var allFeatures = []config.Feature{
	config.FeatureCodeMode, config.FeatureDeferredTools, config.FeatureExternalAgents,
	config.FeatureHarness, config.FeatureAgentRouting, config.FeatureHooks, config.FeatureEvaluators,
	config.FeatureSkills, config.FeatureToon,
}

// Result is the outcome of validating a configuration.
type Result struct {
	// Valid is true when the configuration loads and only uses known toolsets and providers.
	Valid bool
	// Errors explains why the configuration is invalid. ANSI escape codes are removed.
	Errors []string
	// AgentNames lists the agents in document order.
	AgentNames []string
	// DefaultAgent is `root` if an agent has that name, else the first agent.
	DefaultAgent string
	// RequiredEnvVars lists the environment variables the configured models need
	// (best effort, sorted). It is only computed for valid configurations.
	RequiredEnvVars []string
	// Warnings are the warnings docker-agent's loader logged (for example a suspicious
	// max_tokens). They never reach the process's stderr.
	Warnings []string
}

// Validate loads yamlDoc with the docker-agent library and checks it. name is used in
// error messages only. It never returns an error for an invalid configuration; the
// problems are reported in Result.Errors.
func Validate(ctx context.Context, name string, yamlDoc []byte) (res Result) {
	if name == "" {
		name = "agent.yaml"
	}
	ctx, warnings := collectWarnings(ctx)
	defer func() { res.Warnings = warnings.messages() }()
	return validate(ctx, name, yamlDoc)
}

func validate(ctx context.Context, name string, yamlDoc []byte) Result {
	res := Result{}
	cfg, err := config.Load(ctx, config.NewBytesSource(name, yamlDoc))
	if err != nil {
		res.Errors = []string{Clean(err.Error())}
		// Still report the agent names when the document is at least well-formed YAML.
		res.AgentNames = AgentNames(yamlDoc)
		res.DefaultAgent = defaultAgent(res.AgentNames)
		return res
	}

	for _, a := range cfg.Agents {
		res.AgentNames = append(res.AgentNames, a.Name)
	}
	res.DefaultAgent = defaultAgent(res.AgentNames)

	if err := checkRequirements(cfg); err != nil {
		res.Errors = err
		return res
	}

	res.Valid = true
	res.RequiredEnvVars = requiredEnvVars(ctx, cfg)
	return res
}

// Error joins the errors of an invalid result into one error, or returns nil.
func (r Result) Error() error {
	if r.Valid {
		return nil
	}
	if len(r.Errors) == 0 {
		return errors.New("invalid docker-agent configuration")
	}
	return errors.New(strings.Join(r.Errors, "\n"))
}

func checkRequirements(cfg *latest.Config) []string {
	req := config.Requires(cfg)
	err := req.Check(hasProvider, hasToolset, allFeatures)
	if err == nil {
		return nil
	}
	var ue *config.UnsupportedError
	if !errors.As(err, &ue) {
		return []string{Clean(err.Error())}
	}
	var out []string
	for _, p := range ue.Providers {
		out = append(out, fmt.Sprintf("unknown model provider %q at %s: docker-agent %s supports %s, "+
			"the built-in aliases (mistral, openrouter, ollama, ...) and custom providers declared under `providers`",
			p.Name, strings.Join(p.Locations, ", "), DockerAgentVersion, strings.Join(ProviderTypes, ", ")))
	}
	for _, t := range ue.Toolsets {
		out = append(out, fmt.Sprintf("unknown toolset type %q at %s: valid types are %s",
			t.Name, strings.Join(t.Locations, ", "), strings.Join(ToolsetTypes, ", ")))
	}
	for _, f := range ue.Features {
		out = append(out, fmt.Sprintf("unsupported feature %q at %s", f.Name, strings.Join(f.Locations, ", ")))
	}
	return out
}

func hasProvider(p string) bool {
	return slices.Contains(ProviderTypes, p) || strings.HasPrefix(p, "openai_")
}

func hasToolset(t string) bool {
	return slices.Contains(ToolsetTypes, t)
}

// requiredEnvVars computes the credentials the models need. It deliberately uses an empty
// environment so that the result does not depend on the machine running Pulumi.
func requiredEnvVars(ctx context.Context, cfg *latest.Config) []string {
	vars := config.GatherEnvVarsForModels(ctx, cfg, environment.NewNoEnvProvider())
	vars = append(vars, config.GatherEnvVarsForEvaluators(cfg)...)
	vars = slices.DeleteFunc(vars, func(s string) bool { return s == "" })
	slices.Sort(vars)
	return slices.Compact(vars)
}

func defaultAgent(names []string) string {
	if slices.Contains(names, "root") {
		return "root"
	}
	if len(names) > 0 {
		return names[0]
	}
	return ""
}

// AgentNames returns the agent names of a YAML document in document order, without
// validating it. It returns nil if the document cannot be parsed.
func AgentNames(doc []byte) []string {
	var raw struct {
		Agents yaml.Node `yaml:"agents"`
	}
	if err := yaml.Unmarshal(doc, &raw); err != nil {
		return nil
	}
	if raw.Agents.Kind != yaml.MappingNode {
		return nil
	}
	var names []string
	for i := 0; i+1 < len(raw.Agents.Content); i += 2 {
		names = append(names, raw.Agents.Content[i].Value)
	}
	return names
}

// DefaultAgent returns the default agent for a list of agent names.
func DefaultAgent(names []string) string { return defaultAgent(names) }

var (
	ansi = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)
	// sourceLine matches the source excerpt lines goccy/go-yaml appends to errors, for
	// example ">  5 |   tolls: [x]" or "   4 |     Authorization: Bearer ...".
	sourceLine = regexp.MustCompile(`^\s*>?\s*\d+\s*\|`)
	// caretLine matches the line that points at the error column ("      ^").
	caretLine = regexp.MustCompile(`^\s*\^+\s*$`)
)

// Clean removes ANSI escape codes and the YAML source excerpt from a docker-agent error
// message, keeping the "[line:col] message" part. The excerpt is dropped because it
// echoes configuration content, which can contain secrets.
func Clean(msg string) string {
	msg = ansi.ReplaceAllString(msg, "")
	lines := strings.Split(msg, "\n")
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		l = strings.TrimRight(l, " \t\r")
		if sourceLine.MatchString(l) || caretLine.MatchString(l) {
			continue
		}
		out = append(out, l)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// Redacted is the replacement for secret values in messages.
const Redacted = "[secret]"

// Redact replaces every occurrence of the given secret values in msg with [secret]. For
// multi-line secrets (such as a secret YAML document) each line and each `key: value`
// value of at least 8 characters is redacted as well.
func Redact(msg string, secrets []string) string {
	var candidates []string
	add := func(s string, minLen int) {
		s = strings.TrimSpace(s)
		if len(s) >= minLen {
			candidates = append(candidates, s)
		}
	}
	for _, s := range secrets {
		add(s, 4)
		if !strings.Contains(s, "\n") {
			continue
		}
		for _, line := range strings.Split(s, "\n") {
			add(line, 8)
			if _, v, ok := strings.Cut(line, ": "); ok {
				add(strings.Trim(strings.TrimSpace(v), `"'`), 8)
			}
		}
	}
	// Longest first, so that a value is not partially replaced by a shorter one.
	slices.SortFunc(candidates, func(a, b string) int { return len(b) - len(a) })
	for _, c := range candidates {
		msg = strings.ReplaceAll(msg, c, Redacted)
	}
	return msg
}
