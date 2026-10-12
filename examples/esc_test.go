// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package examples

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/pulumi/providertest/pulumitest"
	"github.com/pulumi/providertest/pulumitest/opttest"
	"github.com/pulumi/pulumi/sdk/v3/go/auto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

const (
	escExample = "esc-yaml"
	// escOrgEnv selects the Pulumi Cloud organization for TestExampleESCEnvironment; by default it is
	// the organization of the user that PULUMI_ACCESS_TOKEN belongs to.
	escOrgEnv = "DOCKER_AGENT_TEST_ESC_ORG"
)

// escEnvironment is the part of examples/esc-yaml/environment.yaml that the tests check.
type escEnvironment struct {
	Values struct {
		PulumiConfig         map[string]any    `yaml:"pulumiConfig"`
		EnvironmentVariables map[string]string `yaml:"environmentVariables"`
		AWS                  struct {
			Login map[string]any `yaml:"login"`
		} `yaml:"aws"`
	} `yaml:"values"`
}

// TestExampleESCProgram checks examples/esc-yaml without Pulumi Cloud: environment.yaml must provide
// every setting the program declares (and the provider's registryAuth), Pulumi.dev.yaml must import
// the environment, and the program must preview with those settings given as plain stack
// configuration in a file backend, which cannot import environments. It needs no registry and no
// credentials: previews neither push nor run agents.
func TestExampleESCProgram(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)

	var env escEnvironment
	require.NoError(t, yaml.Unmarshal([]byte(readFixture(t, filepath.Join(escExample, "environment.yaml"))), &env))
	assert.Contains(t, env.Values.AWS.Login, "fn::open::aws-login")
	assert.ElementsMatch(t, []string{
		"ANTHROPIC_API_KEY", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN", "AWS_REGION",
	}, keysOf(env.Values.EnvironmentVariables))

	var program struct {
		Config map[string]struct {
			Default any `yaml:"default"`
		} `yaml:"config"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(readFixture(t, filepath.Join(escExample, "Pulumi.yaml"))), &program))
	for name, decl := range program.Config {
		if decl.Default == nil {
			assert.Contains(t, env.Values.PulumiConfig, name, "environment.yaml does not set the program's %q", name)
		}
	}
	auth, ok := env.Values.PulumiConfig["docker-agent:registryAuth"].([]any)
	require.True(t, ok && len(auth) == 1, "environment.yaml must set docker-agent:registryAuth")
	assert.ElementsMatch(t, []string{"address", "password", "username"}, keysOf(auth[0].(map[string]any)))

	var stack struct {
		Environment []string `yaml:"environment"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(readFixture(t, filepath.Join(escExample, "Pulumi.dev.yaml"))), &stack))
	assert.Equal(t, []string{"docker-agent/dev"}, stack.Environment)

	start := time.Now()
	pt := pulumitest.NewPulumiTest(t, escExample,
		opttest.LocalProviderPath(providerName, filepath.Join(root, "bin")),
		opttest.SkipInstall(),
		opttest.SkipStackCreate(),
	)
	// Any stack name but dev: Pulumi.dev.yaml imports the environment, which needs Pulumi Cloud.
	pt.NewStack(t, "")
	s, ctx := pt.CurrentStack(), pt.Context()
	path := &auto.ConfigOptions{Path: true}
	require.NoError(t, s.SetConfigWithOptions(ctx, "docker-agent:registryAuth[0].address",
		auto.ConfigValue{Value: "localhost:5000"}, path))
	require.NoError(t, s.SetConfigWithOptions(ctx, "docker-agent:registryAuth[0].username",
		auto.ConfigValue{Value: "esc-user"}, path))
	require.NoError(t, s.SetConfigWithOptions(ctx, "docker-agent:registryAuth[0].password",
		auto.ConfigValue{Value: "esc-registry-password", Secret: true}, path))
	pt.SetConfig(t, "registryPrefix", "localhost:5000/esc")
	require.NoError(t, s.SetConfig(ctx, "anthropicApiKey", auto.ConfigValue{Value: "sk-ant-fake", Secret: true}))

	preview := pt.Preview(t)
	assert.Equal(t, 6, preview.ChangeSummary["create"], "the stack, two AgentConfigs, an AgentArtifact and two AgentRuns:\n%s",
		preview.StdOut)
	assert.NotContains(t, preview.StdOut+preview.StdErr, "esc-registry-password")
	t.Logf("[%s] preview: %s", escExample, time.Since(start).Round(time.Millisecond))
}

// TestExampleESCEnvironment checks examples/esc-yaml/environment.yaml with Pulumi Cloud's
// environment check API (`POST /api/esc/environments/{org}/yaml/check`, which the CLI uses for
// `pulumi config env init`). The check evaluates the definition, including the input schema of
// `fn::open::aws-login`, without saving the environment or opening providers, so the test creates
// and changes nothing. It runs only when PULUMI_ACCESS_TOKEN is set.
func TestExampleESCEnvironment(t *testing.T) {
	t.Parallel()
	token := os.Getenv("PULUMI_ACCESS_TOKEN")
	if token == "" {
		t.Skip("PULUMI_ACCESS_TOKEN is not set; skipping the Pulumi ESC environment check")
	}
	api := "https://api.pulumi.com"
	if u := os.Getenv("PULUMI_BACKEND_URL"); strings.HasPrefix(u, "https://") {
		api = strings.TrimSuffix(u, "/")
	}
	org := os.Getenv(escOrgEnv)
	if org == "" {
		var user struct {
			GithubLogin string `json:"githubLogin"`
		}
		status, body := escRequest(t, http.MethodGet, api+"/api/user", token, nil)
		require.Equal(t, http.StatusOK, status, "GET /api/user: %s", body)
		require.NoError(t, json.Unmarshal(body, &user))
		org = user.GithubLogin
	}

	definition := []byte(readFixture(t, filepath.Join(escExample, "environment.yaml")))
	status, body := escRequest(t, http.MethodPost, fmt.Sprintf("%s/api/esc/environments/%s/yaml/check", api, org),
		token, definition)
	require.Equal(t, http.StatusOK, status, "environment.yaml has errors: %s", body)

	var checked struct {
		Properties map[string]struct {
			Value map[string]json.RawMessage `json:"value"`
		} `json:"properties"`
		Diagnostics []struct {
			Summary string `json:"summary"`
		} `json:"diagnostics"`
	}
	require.NoError(t, json.Unmarshal(body, &checked))
	assert.Empty(t, checked.Diagnostics)
	assert.ElementsMatch(t, []string{"anthropicApiKey", "docker-agent:registryAuth", "registryPrefix"},
		keysOf(checked.Properties["pulumiConfig"].Value))
	assert.ElementsMatch(t, []string{
		"ANTHROPIC_API_KEY", "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN", "AWS_REGION",
	}, keysOf(checked.Properties["environmentVariables"].Value))
}

// escRequest sends a request to the Pulumi Cloud API and returns the status and body.
func escRequest(t *testing.T, method, url, token string, body []byte) (int, []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Accept", "application/vnd.pulumi+8")
	// Like the CLI, which sends the YAML definition verbatim.
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err, "%s %s", method, url)
	defer resp.Body.Close() //nolint:errcheck
	data, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, data
}

// keysOf returns the keys of a map in sorted order.
func keysOf[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
