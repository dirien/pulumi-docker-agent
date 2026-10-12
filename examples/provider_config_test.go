// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package examples

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pulumi/providertest/pulumitest"
	"github.com/pulumi/providertest/pulumitest/assertrefresh"
	"github.com/pulumi/providertest/pulumitest/opttest"
	"github.com/pulumi/pulumi/sdk/v3/go/auto"
	"github.com/pulumi/pulumi/sdk/v3/go/auto/optpreview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The provider settings tests change the configuration of the default provider of
// testdata/agent-run-provider-config (an AgentConfig, an AgentArtifact and an AgentRun with a
// deletePrompt): the engine must configure the provider again in place, with the new settings,
// and keep every resource. A replacement of the provider would replace every resource, run
// the agent again and then the old resource's deletePrompt.

// TestProviderSettingsUpdateInPlaceE2E rotates the password of docker-agent:registryAuth and
// then runs one command with DOCKER_AGENT_SKIP_VALIDATION=true.
func TestProviderSettingsUpdateInPlaceE2E(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	reg := newAuthRegistry(t, testRegistry(t), "ci", "token-v1")
	llm := newFakeLLM(t)
	pt := providerConfigStack(t, root, llm, reg.Host+"/e2e/settings-in-place:v1")
	setRegistryAuth(t, pt, reg.Host, "ci", "token-v1")

	pt.Up(t)
	require.Equal(t, []string{runPrompt}, userMessages(llm))
	before := stackIDs(t, pt)
	require.Positive(t, reg.authorized(), "the push used registryAuth")

	// The registry accepts only the new password from now on.
	reg.setPassword("token-v2")
	setRegistryAuth(t, pt, reg.Host, "ci", "token-v2")
	up := pt.Up(t)
	assertNoResourceChanges(t, up)
	assertConfigured(t, pt, "token-v2")
	assert.Equal(t, before, stackIDs(t, pt), "the provider was updated in place and no resource was replaced")
	assert.Equal(t, "token-v2", providerRegistryPassword(t, pt), "the state has the new password")
	assert.Equal(t, []string{runPrompt}, userMessages(llm), "the rotation runs neither the prompt nor the deletePrompt")
	// A refresh reads the artifact from the registry, which accepts only the new password now:
	// the provider was configured with it.
	authorized := reg.authorized()
	assertrefresh.HasNoChanges(t, pt.Refresh(t))
	assert.Greater(t, reg.authorized(), authorized, "the refresh read the artifact with the rotated password")

	// One command with DOCKER_AGENT_SKIP_VALIDATION, then the next one without it.
	ws := pt.CurrentStack().Workspace()
	ws.SetEnvVar("DOCKER_AGENT_SKIP_VALIDATION", "true")
	assertNoResourceChanges(t, pt.Up(t))
	assertConfigured(t, pt, `"skipValidation":true`)
	ws.UnsetEnvVar("DOCKER_AGENT_SKIP_VALIDATION")
	assertNoResourceChanges(t, pt.Up(t))
	assertConfigured(t, pt, `"skipValidation":false`)
	pt.Preview(t, optpreview.ExpectNoChanges())
	assert.Equal(t, before, stackIDs(t, pt), "no resource was replaced")
	assert.Equal(t, []string{runPrompt}, userMessages(llm), "the setting runs neither the prompt nor the deletePrompt")

	pt.Destroy(t)
	assert.Equal(t, []string{runPrompt, deletePrompt}, userMessages(llm), "destroy runs the deletePrompt once")
}

// TestProviderSettingsAddRegistryAuthE2E adds docker-agent:registryAuth to a stack, together
// with an artifact in a registry that needs it: the same `pulumi up` must configure the
// provider with the new credentials, or the push fails.
func TestProviderSettingsAddRegistryAuthE2E(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	open := testRegistry(t)
	reg := newAuthRegistry(t, open, "ci", "token-v1")
	llm := newFakeLLM(t)
	pt := providerConfigStack(t, root, llm, open+"/e2e/settings-add-auth:v1")

	pt.Up(t)
	require.Equal(t, []string{runPrompt}, userMessages(llm))
	before := stackIDs(t, pt)

	setRegistryAuth(t, pt, reg.Host, "ci", "token-v1")
	pt.SetConfig(t, "artifact", reg.Host+"/e2e/settings-add-auth:v1")
	up := pt.Up(t)
	// pulumi-go-provider's CheckConfig decodes the new configuration into the provider before
	// Configure, so the push alone cannot tell whether Configure got registryAuth.
	assertConfigured(t, pt, reg.Host)
	assert.NotEmpty(t, stringOutput(t, up.Outputs, "digest"))
	assert.Positive(t, reg.authorized(), "the push to the registry that needs registryAuth succeeded")
	after := stackIDs(t, pt)
	for _, name := range []string{"pulumi:providers:docker-agent::default", "docker-agent:index:AgentConfig::writer",
		"docker-agent:index:AgentRun::notes"} {
		assert.Equal(t, before[name], after[name], "%s keeps its ID", name)
	}
	assert.Equal(t, []string{runPrompt}, userMessages(llm), "adding registryAuth does not run the agent again")

	pt.Destroy(t)
	assert.Equal(t, []string{runPrompt, deletePrompt}, userMessages(llm))
}

const (
	runPrompt    = "Write the release notes into the working directory."
	deletePrompt = "Delete the release notes from the working directory."
)

// providerConfigStack creates a stack of testdata/agent-run-provider-config with the provider
// in bin/, pushing the artifact to artifact.
func providerConfigStack(t *testing.T, root string, llm *fakeLLM, artifact string) *pulumitest.PulumiTest {
	t.Helper()
	pt := pulumitest.NewPulumiTest(t, filepath.Join("testdata", "agent-run-provider-config"),
		opttest.LocalProviderPath(providerName, filepath.Join(root, "bin")),
		opttest.SkipInstall(),
		opttest.SkipStackCreate(),
	)
	pt.NewStack(t, "")
	pt.SetConfig(t, "baseUrl", llm.URL)
	pt.SetConfig(t, "workDir", t.TempDir())
	pt.SetConfig(t, "artifact", artifact)
	require.NoError(t, pt.CurrentStack().SetConfig(pt.Context(), "apiKey",
		auto.ConfigValue{Value: "sk-fake-provider-config-0123456789", Secret: true}))
	return pt
}

// setRegistryAuth sets docker-agent:registryAuth[0], as `pulumi config set --path` does.
func setRegistryAuth(t *testing.T, pt *pulumitest.PulumiTest, address, username, password string) {
	t.Helper()
	stack := pt.CurrentStack()
	for key, v := range map[string]auto.ConfigValue{
		"docker-agent:registryAuth[0].address":  {Value: address},
		"docker-agent:registryAuth[0].username": {Value: username},
		"docker-agent:registryAuth[0].password": {Value: password, Secret: true},
	} {
		require.NoError(t, stack.SetConfigWithOptions(pt.Context(), key, v, &auto.ConfigOptions{Path: true}))
	}
}

// assertNoResourceChanges fails when an update created, replaced or deleted a resource. The
// update of the default provider itself is not counted.
func assertNoResourceChanges(t *testing.T, up auto.UpResult) {
	t.Helper()
	for op, n := range *up.Summary.ResourceChanges {
		assert.True(t, op == "same" || n == 0, "the update changed resources (%s: %d):\n%s", op, n, up.StdOut)
	}
}

// assertConfigured checks the provider configurations of the last command in its gRPC log:
// no DiffConfig reported a replacement, and the engine configured every provider with a
// configuration that contains want (as JSON; secrets are logged in plaintext).
func assertConfigured(t *testing.T, pt *pulumitest.PulumiTest, want string) {
	t.Helper()
	log := pt.GrpcLog(t)
	require.NotNil(t, log)
	diffs, err := log.DiffConfigs()
	require.NoError(t, err)
	for i := range diffs {
		assert.Empty(t, diffs[i].Response.GetReplaces(), "DiffConfig replaced the provider")
	}
	configures, err := log.Configures()
	require.NoError(t, err)
	require.NotEmpty(t, configures, "the command configured no provider")
	for i := range configures {
		args, err := json.Marshal(configures[i].Request.GetArgs().AsMap())
		require.NoError(t, err)
		assert.Contains(t, string(args), want, "Configure got the old configuration")
	}
}

// stackIDs returns the IDs of the stack's resources and providers, by type and name.
func stackIDs(t *testing.T, pt *pulumitest.PulumiTest) map[string]string {
	t.Helper()
	var state struct {
		Resources []stateResource `json:"resources"`
	}
	require.NoError(t, json.Unmarshal(pt.ExportStack(t).Deployment, &state))
	ids := map[string]string{}
	for _, r := range state.Resources {
		if r.Delete || r.ID == "" {
			continue
		}
		parts := strings.Split(r.URN, "::")
		ids[r.Type+"::"+parts[len(parts)-1]] = r.ID
	}
	return ids
}

// providerRegistryPassword returns the registryAuth password stored with the default provider.
func providerRegistryPassword(t *testing.T, pt *pulumitest.PulumiTest) string {
	t.Helper()
	var state struct {
		Resources []stateResource `json:"resources"`
	}
	require.NoError(t, json.Unmarshal(pt.ExportStack(t).Deployment, &state))
	for _, r := range state.Resources {
		if r.Type != "pulumi:providers:"+providerName || r.Delete {
			continue
		}
		var auth []struct {
			Password any `json:"password"`
		}
		raw := r.Inputs["registryAuth"]
		if isSecretJSON(raw) {
			var wrapped struct {
				Plaintext string `json:"plaintext"`
			}
			require.NoError(t, json.Unmarshal(raw, &wrapped))
			raw = json.RawMessage(wrapped.Plaintext)
		}
		require.NoError(t, json.Unmarshal(raw, &auth), "%s", raw)
		require.Len(t, auth, 1)
		switch pw := auth[0].Password.(type) {
		case string:
			return pw
		case map[string]any:
			var s string
			require.NoError(t, json.Unmarshal([]byte(fmt.Sprint(pw["plaintext"])), &s))
			return s
		}
	}
	t.Fatal("no docker-agent provider in the state")
	return ""
}

// authRegistry is a registry that needs HTTP basic authentication: a proxy to a plain
// registry that rejects every request without the current username and password.
type authRegistry struct {
	Host string

	mu                 sync.Mutex
	username, password string
	authorizedCount    int
}

func newAuthRegistry(t *testing.T, upstream, username, password string) *authRegistry {
	t.Helper()
	target, err := url.Parse("http://" + upstream)
	require.NoError(t, err)
	proxy := httputil.NewSingleHostReverseProxy(target)
	a := &authRegistry{username: username, password: password}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		a.mu.Lock()
		good := ok && user == a.username && pass == a.password
		if good {
			a.authorizedCount++
		}
		a.mu.Unlock()
		if !good {
			w.Header().Set("WWW-Authenticate", `Basic realm="docker-agent-e2e"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		r.Header.Del("Authorization")
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	a.Host = strings.TrimPrefix(srv.URL, "http://")
	// Give the registry container a moment if the proxy is the first to reach it.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if resp, err := http.Get("http://" + upstream + "/v2/"); err == nil { //nolint:noctx // a test probe
			_ = resp.Body.Close()
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	return a
}

func (a *authRegistry) setPassword(password string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.password = password
}

func (a *authRegistry) authorized() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.authorizedCount
}
