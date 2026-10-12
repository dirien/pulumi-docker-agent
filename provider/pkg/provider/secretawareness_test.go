// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"path/filepath"
	"strings"
	"testing"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAgentArtifactWarnsAboutSecretYAML checks that a secret `yaml` produces exactly one
// warning naming the registry it is pushed to in plaintext, and a public one none.
func TestAgentArtifactWarnsAboutSecretYAML(t *testing.T) {
	t.Parallel()
	s, rec := loggingServer(t)
	doc := "agents:\n  root:\n    model: openai/gpt-5-mini\n    instruction: Use the token s3cret-value-123.\n"
	inputs := property.NewMap(map[string]property.Value{
		"reference": property.New("ghcr.io/my-org/agents/assistant:v1"),
		"yaml":      property.New(doc).WithSecret(true),
	})
	resp, err := s.Check(p.CheckRequest{Urn: urn("docker-agent:index:AgentArtifact", "art"), Inputs: inputs})
	require.NoError(t, err)
	require.Empty(t, resp.Failures, failureText(resp.Failures))
	warnings := rec.messages("warning")
	require.Len(t, warnings, 1, rec.all())
	assert.Contains(t, warnings[0], "the configuration contains secret values and is pushed in plaintext to ghcr.io")
	assert.NotContains(t, rec.all(), "s3cret-value-123")

	rec.reset()
	resp, err = s.Check(p.CheckRequest{Urn: urn("docker-agent:index:AgentArtifact", "art"),
		Inputs: inputs.Set("yaml", property.New(doc))})
	require.NoError(t, err)
	require.Empty(t, resp.Failures, failureText(resp.Failures))
	assert.Empty(t, rec.messages("warning"), rec.all())

	// A secret reference is not named.
	rec.reset()
	_, err = s.Check(p.CheckRequest{Urn: urn("docker-agent:index:AgentArtifact", "art"),
		Inputs: inputs.Set("reference", inputs.Get("reference").WithSecret(true))})
	require.NoError(t, err)
	require.Len(t, rec.messages("warning"), 1, rec.all())
	assert.Contains(t, rec.messages("warning")[0], "pushed in plaintext to the registry")
}

// TestUserSettingsSensitiveInputsAreSecret checks that identity token headers and hook
// environment variables are secret inputs, so that `yaml` and `sha256` are secret too, and
// that they stay secret in the state (withUserSettingsSecrets: pulumi-go-provider loses the
// secretness of the identity token headers of a provider).
func TestUserSettingsSensitiveInputsAreSecret(t *testing.T) {
	t.Parallel()
	for name, inputs := range map[string]map[string]any{
		"identity token headers": {"providers": map[string]any{"corp": map[string]any{
			"provider": "anthropic",
			"auth": map[string]any{"type": "workload_identity_federation", "workloadIdentityFederation": map[string]any{
				"federationRuleId": "fdrl_123", "organizationId": "org",
				"identityToken": map[string]any{
					"url":     "https://idp.example.com/token",
					"headers": map[string]any{"Authorization": "Bearer abcdefghijklmnop"},
				},
			}},
		}}},
		"hook env": {"settings": map[string]any{"hooks": map[string]any{"sessionStart": []any{map[string]any{
			"command": "notify", "env": map[string]any{"SLACK_TOKEN": "xoxb-0123456789"},
		}}}}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			l := newFileLifecycle(t, userSettingsToken)
			inputs["path"] = filepath.Join(t.TempDir(), "config.yaml")
			checked := l.check(inputs)
			var secret []string
			var walk func(path string, v property.Value)
			walk = func(path string, v property.Value) {
				if v.Secret() {
					secret = append(secret, path)
					return
				}
				switch {
				case v.IsMap():
					for k, e := range v.AsMap().All {
						walk(path+"."+k, e)
					}
				case v.IsArray():
					for _, e := range v.AsArray().All {
						walk(path+"[]", e)
					}
				}
			}
			walk("", property.New(checked))
			require.Len(t, secret, 1, "%v", secret)
			assert.True(t, strings.HasSuffix(secret[0], ".headers") || strings.HasSuffix(secret[0], ".env"), secret[0])
			inputPath := secret[0]

			// The state embeds the inputs: the same value is secret there, in a preview, after
			// Create and after Update, and nothing that a known input mirrors is unknown.
			for _, step := range []string{"preview", "create", "update preview", "update"} {
				var out property.Map
				switch step {
				case "preview":
					out = l.create(checked, true)
				case "create":
					out = l.create(checked, false)
				case "update preview":
					out = l.update(checked, true)
				case "update":
					out = l.update(checked, false)
				}
				assert.True(t, out.Get("yaml").Secret(), "%s: yaml contains the secret", step)
				assert.True(t, out.Get("sha256").Secret(), step)
				secret = nil
				walk("", property.New(out.Delete("yaml", "sha256")))
				assert.Equal(t, []string{inputPath}, secret, step)
				for k := range checked.All {
					assert.False(t, out.Get(k).IsComputed(), "%s: %s is unknown", step, k)
				}
			}
			l.delete()
		})
	}
}

// TestUserSettingsCheckFailureProperty checks that docker-agent's user settings errors fail
// the input they are about.
func TestUserSettingsCheckFailureProperty(t *testing.T) {
	t.Parallel()
	for msg, want := range map[string]string{
		`settings.layout.sidebar_position: invalid value "x" (valid: right)`:      "settings.layout.sidebarPosition",
		"settings.tab_title_max_length must not be negative, got -1":              "settings.tabTitleMaxLength",
		"aliases.dev: path is required":                                           `aliases["dev"]`,
		"board.columns[1]: id or name is required":                                "board.columns[1]",
		"credential_helper.command is required":                                   "credentialHelper.command",
		"settings.hooks.session_end: on_error block is not supported":             "settings.hooks.sessionEnd",
		"default_model: provider and model are required":                          "defaultModel",
		"providers.p: thinking_budget: unknown":                                   `providers["p"]`,
		"settings.hooks.unknown_event[0]: x":                                      "settings.hooks",
		`invalid alias name "bad name"`:                                           "",
		"sandbox_allowlist[0]: host must not be empty":                            "sandboxAllowlist[0]",
		"providers: provider name cannot be empty":                                "providers",
		"something else entirely, with no location":                               "",
		"settings.extra: extra key \"theme\" is a typed input; set theme instead": "settings.extra",
	} {
		assert.Equal(t, want, userSettingsProperty(msg), msg)
	}

	resp := check(t, server(t), userSettingsToken, "us", map[string]any{
		"path":     filepath.Join(t.TempDir(), "config.yaml"),
		"settings": map[string]any{"tabTitleMaxLength": -1},
	})
	require.Len(t, resp.Failures, 1, failureText(resp.Failures))
	assert.Equal(t, "settings.tabTitleMaxLength", resp.Failures[0].Property)
}
