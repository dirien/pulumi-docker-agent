// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"os"
	"slices"
	"strings"
	"testing"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dirien/pulumi-docker-agent/provider/pkg/agentrun"
	"github.com/dirien/pulumi-docker-agent/provider/pkg/agentrun/fakeopenai"
)

const missingKeyWarning = "the configuration needs FAKE_API_KEY, which neither env nor the environment of the Pulumi " +
	"CLI sets; set it in env (for example from a secret config value)"

// TestAgentRunModelChecksOnlyWhenTheAgentRuns: the model checks (agentrun.CheckModels) warn only in
// an operation that runs the agent, so a preview of an unchanged AgentRun stays quiet (U#7).
func TestAgentRunModelChecksOnlyWhenTheAgentRuns(t *testing.T) {
	resetRunState(t)
	t.Setenv("FAKE_API_KEY", "")
	require.NoError(t, os.Unsetenv("FAKE_API_KEY"))
	srv := fakeopenai.New(func(fakeopenai.Request) fakeopenai.Reply { return fakeopenai.Reply{Content: "ok"} })
	defer srv.Close()
	s, rec := loggingServer(t)
	without := runInputs(srv, "", nil).Delete("env")
	check := func(old property.Map, in property.Map) []string {
		t.Helper()
		rec.reset()
		resp, err := s.Check(p.CheckRequest{Urn: runURN("run"), State: old, Inputs: in})
		require.NoError(t, err)
		require.Empty(t, resp.Failures, runFailures(resp.Failures))
		return rec.messages("warning")
	}
	assert.Equal(t, []string{missingKeyWarning}, check(property.Map{}, without), "a create runs the agent")
	assert.Empty(t, check(without, without), "an unchanged AgentRun does not run")
	assert.Equal(t, []string{missingKeyWarning}, check(without, without.Set("prompt", property.New("Another one."))),
		"a changed prompt runs it again")
	assert.Equal(t, []string{missingKeyWarning}, check(without, without.Set("rerun", property.New("always"))))
	assert.Equal(t, []string{missingKeyWarning}, check(without, without.Set("triggers",
		property.New([]property.Value{property.New(property.Computed)}))), "an unknown trigger may run it")
	assert.Empty(t, check(without, without.Set("timeoutSeconds", property.New(60.0))), "a state-only change")
	assert.Empty(t, check(property.Map{}, runInputs(srv, "", nil)), "env has the key")
}

// TestAgentRunUnknownProviderHint: user-level providers do not apply to AgentRun.
func TestAgentRunUnknownProviderHint(t *testing.T) {
	s := runServer(t)
	in := runProps(map[string]any{"prompt": "Hi", "yaml": "agents:\n  root:\n    model: myendpoint/llama\n"})
	resp := runCheck(t, s, in)
	require.Len(t, resp.Failures, 1)
	assert.Contains(t, resp.Failures[0].Reason, `unknown model provider "myendpoint"`)
	assert.True(t, strings.HasSuffix(resp.Failures[0].Reason, "; "+agentrun.UserProvidersHint), resp.Failures[0].Reason)
}

// TestAgentRunDeletePromptCredentialsHint: a delete prompt whose stored inputs lack a credential
// names it and the v0.2.0 change (U#5).
func TestAgentRunDeletePromptCredentialsHint(t *testing.T) {
	resetRunState(t)
	srv := fakeopenai.New(func(fakeopenai.Request) fakeopenai.Reply { return fakeopenai.Reply{Content: "done"} })
	defer srv.Close()
	s := runServer(t)
	checked := runCheck(t, s, runInputs(srv, "", map[string]any{"deletePrompt": "Clean up.", "inheritEnvironment": false}))
	require.Empty(t, checked.Failures, runFailures(checked.Failures))
	created, err := s.Create(p.CreateRequest{Urn: runURN("del"), Properties: checked.Inputs})
	require.NoError(t, err)
	// A stack created by v0.1.2 that relied on the credential of the Pulumi CLI's environment.
	t.Setenv("FAKE_API_KEY", runKey)
	state := created.Properties.Set("env", property.New(property.NewMap(nil)).WithSecret(true))
	err = s.Delete(p.DeleteRequest{ID: created.ID, Urn: runURN("del"), Properties: state})
	require.Error(t, err)
	msg := err.Error()
	assert.Contains(t, msg, "deletePrompt failed: loading the agent configuration: the configuration needs FAKE_API_KEY")
	assert.Contains(t, msg, "FAKE_API_KEY is set in the environment of the Pulumi CLI, but inheritEnvironment: false keeps "+
		"it from the run")
	assert.Contains(t, msg, "; the stored inputs do not give the delete prompt FAKE_API_KEY (since v0.2.0, "+
		"inheritEnvironment: false keeps the environment of the Pulumi CLI from the run, also from the model SDKs); run "+
		"pulumi up with the credentials in env first (that runs the agent again and stores them), or to delete without "+
		"running it, set DOCKER_AGENT_SKIP_DELETE_PROMPTS=true for this command")
	assert.NotContains(t, msg, runKey)
}

// TestAgentRunSecretStringsSkipConfiguration: `env` values that are configuration are not redacted
// (the SDKs mark the whole map secret), other secret inputs are (D6).
func TestAgentRunSecretStringsSkipConfiguration(t *testing.T) {
	in := runProps(map[string]any{"prompt": "Hi"}).
		Set("env", property.New(property.NewMap(map[string]property.Value{
			"AWS_REGION":                property.New("us-east-1"),
			"GOOGLE_GENAI_USE_VERTEXAI": property.New("true"),
			"FAKE_API_KEY":              property.New(runKey),
		})).WithSecret(true)).
		Set("yaml", property.New("agents: {}").WithSecret(true))
	got := agentRunSecretStrings(in)
	assert.True(t, slices.Contains(got, runKey))
	assert.True(t, slices.Contains(got, "agents: {}"))
	assert.False(t, slices.Contains(got, "us-east-1"))
	assert.False(t, slices.Contains(got, "true"))
	assert.ElementsMatch(t, append(got, "us-east-1", "true"), secretStrings(in), "only those values are left out")
}
