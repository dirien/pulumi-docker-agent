// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"io"
	"log"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/hashicorp/hcl/v2"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource/plugin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/dirien/pulumi-docker-agent/provider/pkg/ociagent"
	"github.com/dirien/pulumi-docker-agent/provider/pkg/pcl"
)

func TestParseProgramArgs(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		args    []string
		version string
		want    programArgs
		err     string
	}{
		{args: nil, version: "0.1.0", want: programArgs{Options: pcl.Options{ProviderVersion: "0.1.0"}}},
		{args: nil, version: "0.0.1-alpha.0", want: programArgs{}},
		{
			args:    []string{"--push-to", "ghcr.io/acme/agents", "--run", "Hi", "agent.yaml", "team"},
			version: "0.1.0",
			want: programArgs{
				Options: pcl.Options{PushTo: "ghcr.io/acme/agents", Run: "Hi", ProviderVersion: "0.1.0"},
				paths:   []string{"agent.yaml", "team"},
			},
		},
		{
			// Paths first, flags between and after them.
			args:    []string{"agent.yaml", "--push-to=localhost:5000/team", "team", "-run", "Hi", "--provider-version", "v0.2.0"},
			version: "0.0.1-alpha.0",
			want: programArgs{
				Options: pcl.Options{PushTo: "localhost:5000/team", Run: "Hi", ProviderVersion: "0.2.0"},
				paths:   []string{"agent.yaml", "team"},
			},
		},
		{
			// `--` ends the flags.
			args:    []string{"--run", "Hi", "--", "--odd-file.yaml", "agent.yaml"},
			version: "0.0.1-alpha.0",
			want:    programArgs{Options: pcl.Options{Run: "Hi"}, paths: []string{"--odd-file.yaml", "agent.yaml"}},
		},
		{args: []string{"--unknown"}, err: "supported arguments: --push-to"},
		{args: []string{"agent.yaml", "--run"}, err: "flag needs an argument"},
		{args: []string{"--provider-version", "latest"}, err: `--provider-version "latest"`},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			t.Parallel()
			got, err := parseProgramArgs(tc.args, tc.version)
			if tc.err != "" {
				assert.ErrorContains(t, err, tc.err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestReleaseVersion(t *testing.T) {
	t.Parallel()
	for version, want := range map[string]string{
		"0.1.0":                       "0.1.0",
		"v0.1.0":                      "0.1.0",
		"1.2.3-beta.1":                "1.2.3-beta.1",
		"0.0.1-alpha.0":               "",
		"0.0.1-alpha.1791750221":      "",
		"0.1.1-dev.0":                 "",
		"0.1.1-SNAPSHOT-abc1234":      "",
		"0.1.0+dirty":                 "",
		"0.1.0-alpha.0+5a6b7c8.dirty": "",
		"":                            "",
		"not-a-version":               "",
	} {
		assert.Equal(t, want, releaseVersion(version), version)
	}
}

// TestConvertProgram converts a directory the way `pulumi convert --from docker-agent` calls the
// plugin: main.pp and the instruction files go to the target directory, and the directory the
// project is generated into is not converted.
func TestConvertProgram(t *testing.T) {
	t.Parallel()
	src, dst := t.TempDir(), t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(src, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
	}
	write("pirate.yaml", "agents:\n  root:\n    model: openai/gpt-5-mini\n    instruction: Talk like a pirate.\n")
	write("team/writer.yaml", "agents:\n  root:\n    model: openai/gpt-5-mini\n    instruction_file: prompts/writer.md\n")
	write("team/prompts/writer.md", "Write well.\n")
	write("out/stale.yaml", "agents:\n  root:\n    model: openai/gpt-5-mini\n")

	c := &converter{version: "0.1.0"}
	resp, err := c.ConvertProgram(context.Background(), &plugin.ConvertProgramRequest{
		SourceDirectory:           src,
		TargetDirectory:           dst,
		GeneratedProjectDirectory: "out",
		Args:                      []string{"--push-to", "localhost:5000/agents", "--run", "Hi"},
	})
	require.NoError(t, err)
	require.False(t, resp.Diagnostics.HasErrors(), "%v", resp.Diagnostics)
	// team/writer.yaml uses instruction_file, so it is neither pushed nor run.
	require.Len(t, resp.Diagnostics, 1)
	assert.Equal(t, "No AgentArtifact or AgentRun", resp.Diagnostics[0].Summary)

	program, err := os.ReadFile(filepath.Join(dst, "main.pp"))
	require.NoError(t, err)
	text := string(program)
	assert.Contains(t, text, `resource pirate "docker-agent:index:AgentConfig"`)
	assert.Contains(t, text, `resource teamWriter "docker-agent:index:AgentConfig"`)
	assert.Contains(t, text, `reference     = "localhost:5000/agents/pirate:latest"`)
	assert.Contains(t, text, `resource pirateRun "docker-agent:index:AgentRun"`)
	assert.Contains(t, text, `version           = "0.1.0"`)
	assert.NotContains(t, text, "stale")
	prompt, err := os.ReadFile(filepath.Join(dst, "team", "prompts", "writer.md"))
	require.NoError(t, err)
	assert.Equal(t, "Write well.\n", string(prompt))

	// Only the given files, and a development build pins no provider version.
	dst = t.TempDir()
	resp, err = (&converter{version: "0.0.1-alpha.0"}).ConvertProgram(context.Background(),
		&plugin.ConvertProgramRequest{SourceDirectory: src, TargetDirectory: dst, Args: []string{"pirate.yaml"}})
	require.NoError(t, err)
	require.Empty(t, resp.Diagnostics)
	program, err = os.ReadFile(filepath.Join(dst, "main.pp"))
	require.NoError(t, err)
	assert.NotContains(t, string(program), "teamWriter")
	assert.NotContains(t, string(program), "version")
}

func TestConvertProgramErrors(t *testing.T) {
	t.Parallel()
	c := &converter{version: "0.1.0"}
	summaries := func(resp *plugin.ConvertProgramResponse) []string {
		var out []string
		for _, d := range resp.Diagnostics {
			assert.Equal(t, hcl.DiagError, d.Severity)
			out = append(out, d.Summary)
		}
		return out
	}

	empty := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(empty, "compose.yaml"), []byte("services: {}\n"), 0o600))
	resp, err := c.ConvertProgram(context.Background(),
		&plugin.ConvertProgramRequest{SourceDirectory: empty, TargetDirectory: t.TempDir()})
	require.NoError(t, err)
	assert.Equal(t, []string{"No docker-agent configuration found"}, summaries(resp))

	resp, err = c.ConvertProgram(context.Background(),
		&plugin.ConvertProgramRequest{SourceDirectory: empty, TargetDirectory: t.TempDir(), Args: []string{"--nope"}})
	require.NoError(t, err)
	assert.Equal(t, []string{"Invalid converter arguments"}, summaries(resp))

	resp, err = c.ConvertProgram(context.Background(),
		&plugin.ConvertProgramRequest{SourceDirectory: empty, TargetDirectory: t.TempDir(), Args: []string{"missing.yaml"}})
	require.NoError(t, err)
	assert.Equal(t, []string{"Cannot read the file"}, summaries(resp))

	two := t.TempDir()
	for _, f := range []string{"a.yaml", "b.yaml"} {
		require.NoError(t, os.WriteFile(filepath.Join(two, f), []byte("agents:\n  root:\n    model: openai/gpt-5-mini\n"), 0o600))
	}
	resp, err = c.ConvertProgram(context.Background(), &plugin.ConvertProgramRequest{
		SourceDirectory: two, TargetDirectory: t.TempDir(), Args: []string{"--push-to", "ghcr.io/acme/agent:v1"},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"Cannot write the program"}, summaries(resp))
	assert.Contains(t, resp.Diagnostics[0].Detail, "only works for a single configuration")
}

// newRegistry starts an in-process OCI registry and returns its host:port.
func newRegistry(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(registry.New(registry.Logger(log.New(io.Discard, "", 0))))
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "http://")
}

func mustTag(t *testing.T, ref string) name.Tag {
	t.Helper()
	tag, err := name.NewTag(ref)
	require.NoError(t, err)
	return tag
}

// TestImportArtifacts lists a repository with two agent artifacts, a container image and a
// multi-platform index, as `pulumi import --from docker-agent <repository>` does.
func TestImportArtifacts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	host := newRegistry(t)
	repo := host + "/team/assistant"

	const doc = "agents:\n  root:\n    model: openai/gpt-5-mini\n"
	a, err := ociagent.Build([]byte(doc), ociagent.Annotations([]byte(doc), "2026-01-02T03:04:05Z", nil))
	require.NoError(t, err)
	for _, tag := range []string{"v1", "latest"} {
		_, err := ociagent.Push(ctx, repo+":"+tag, a, ociagent.Options{})
		require.NoError(t, err)
	}
	img, err := random.Image(64, 1)
	require.NoError(t, err)
	require.NoError(t, remote.Write(mustTag(t, repo+":image"), img))
	idx, err := random.Index(64, 1, 2)
	require.NoError(t, err)
	require.NoError(t, remote.WriteIndex(mustTag(t, repo+":multi"), idx))

	resources, diags, err := importArtifacts(ctx, repo, false, "0.1.0", remote.WithContext(ctx))
	require.NoError(t, err)
	assert.Equal(t, []plugin.ResourceImport{
		{Type: pcl.ArtifactType, Name: "assistantLatest", ID: repo + ":latest", Version: "0.1.0", PluginDownloadURL: pcl.PluginDownloadURL},
		{Type: pcl.ArtifactType, Name: "assistantV1", ID: repo + ":v1", Version: "0.1.0", PluginDownloadURL: pcl.PluginDownloadURL},
	}, resources)
	var skipped []string
	for _, d := range diags {
		assert.Equal(t, hcl.DiagWarning, d.Severity)
		skipped = append(skipped, d.Summary)
	}
	assert.Equal(t, []string{"Skipped " + repo + ":image", "Skipped " + repo + ":multi"}, skipped)

	// A repository without agent artifacts is an error diagnostic.
	other := host + "/team/images"
	require.NoError(t, remote.Write(mustTag(t, other+":v1"), img))
	resources, diags, err = importArtifacts(ctx, other, false, "", remote.WithContext(ctx))
	require.NoError(t, err)
	assert.Empty(t, resources)
	require.True(t, diags.HasErrors())
	assert.Equal(t, "No agent artifacts in "+other, diags[len(diags)-1].Summary)

	_, _, err = importArtifacts(ctx, host+"/team/missing", false, "", remote.WithContext(ctx))
	assert.ErrorContains(t, err, "listing the tags of")
	_, _, err = importArtifacts(ctx, "Not A Repository", false, "", remote.WithContext(ctx))
	assert.ErrorContains(t, err, "is not a repository")
}

func TestConvertStateArguments(t *testing.T) {
	t.Parallel()
	c := &converter{version: "0.1.0"}
	for _, args := range [][]string{nil, {"a", "b"}, {"--nope", "a"}} {
		_, err := c.ConvertState(context.Background(), &plugin.ConvertStateRequest{Args: args})
		assert.Equal(t, codes.InvalidArgument, status.Code(err), "%v: %v", args, err)
	}
	_, err := c.ConvertSnippet(context.Background(), &plugin.ConvertSnippetRequest{})
	assert.Equal(t, codes.Unimplemented, status.Code(err))
}
