// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/blang/semver"
	"github.com/google/go-containerregistry/pkg/registry"
	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/integration"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/common/tokens"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dirien/pulumi-docker-agent/provider/pkg/agentrun/fakeopenai"
	"github.com/dirien/pulumi-docker-agent/provider/pkg/ociagent"
)

// AgentRun executions are serialized by the provider, so these tests do not use
// t.Parallel.

const (
	agentRunToken = "docker-agent:index:AgentRun"
	runKey        = "sk-fake-provider-test-key-42"
)

func runServer(t *testing.T) integration.Server {
	t.Helper()
	s, err := integration.NewServer(t.Context(), Name, semver.MustParse("0.1.0"), integration.WithProvider(Provider()))
	require.NoError(t, err)
	return s
}

func runURN(name string) resource.URN {
	return resource.NewURN("test", "proj", "", tokens.Type(agentRunToken), name)
}

func runProps(m map[string]any) property.Map {
	return resource.FromResourcePropertyValue(resource.NewObjectProperty(resource.NewPropertyMapFromMap(m))).AsMap()
}

func runYAML(srv *fakeopenai.Server, agentExtra string) string {
	return fmt.Sprintf(`providers:
  fake:
    api_type: openai_chatcompletions
    base_url: %s
    token_key: FAKE_API_KEY
models:
  m:
    provider: fake
    model: fake-model
agents:
  root:
    model: m
    instruction: You review changes.
%s`, srv.URL, agentExtra)
}

const runSchema = `    structured_output:
      name: review
      schema:
        type: object
        properties:
          risk: {type: string, enum: [low, medium, high]}
        required: [risk]
        additionalProperties: false
`

// runInputs returns AgentRun inputs with a secret env.
func runInputs(srv *fakeopenai.Server, agentExtra string, extra map[string]any) property.Map {
	m := map[string]any{"yaml": runYAML(srv, agentExtra), "prompt": "Upgrade the database."}
	for k, v := range extra {
		m[k] = v
	}
	in := runProps(m)
	return in.Set("env", property.New(property.NewMap(map[string]property.Value{
		"FAKE_API_KEY": property.New(runKey),
	})).WithSecret(true))
}

func runCheck(t *testing.T, s integration.Server, in property.Map) p.CheckResponse {
	t.Helper()
	resp, err := s.Check(p.CheckRequest{Urn: runURN("run"), Inputs: in})
	require.NoError(t, err)
	return resp
}

func runFailures(fs []p.CheckFailure) string {
	var b strings.Builder
	for _, f := range fs {
		b.WriteString(f.Property + ": " + f.Reason + "\n")
	}
	return b.String()
}

func TestAgentRunSchema(t *testing.T) {
	resp, err := runServer(t).GetSchema(p.GetSchemaRequest{})
	require.NoError(t, err)
	var schema struct {
		Resources map[string]struct {
			InputProperties map[string]map[string]any `json:"inputProperties"`
			Properties      map[string]map[string]any `json:"properties"`
			RequiredInputs  []string                  `json:"requiredInputs"`
		} `json:"resources"`
		Types map[string]map[string]any `json:"types"`
	}
	require.NoError(t, json.Unmarshal([]byte(resp.Schema), &schema))
	res, ok := schema.Resources[agentRunToken]
	require.True(t, ok)
	assert.Equal(t, []string{"prompt"}, res.RequiredInputs)
	assert.Equal(t, true, res.InputProperties["env"]["secret"])
	for _, in := range []string{"yaml", "reference", "agent", "prompt", "env", "inheritEnvironment", "workingDir",
		"toolApproval", "timeoutSeconds", "triggers", "outputSecret", "allowHookApprovals"} {
		assert.Contains(t, res.InputProperties, in)
	}
	for _, out := range []string{"output", "structuredOutput", "agentName", "usage", "toolCalls", "ranAt"} {
		assert.Contains(t, res.Properties, out)
	}
	assert.Equal(t, "deny", res.InputProperties["toolApproval"]["default"])
	assert.Equal(t, 300.0, res.InputProperties["timeoutSeconds"]["default"])
	enum := schema.Types["docker-agent:index:ToolApproval"]
	require.NotNil(t, enum)
	assert.Len(t, enum["enum"], 2)
}

func TestAgentRunCheck(t *testing.T) {
	s := runServer(t)
	srv := fakeopenai.New(nil)
	defer srv.Close()

	ok := runCheck(t, s, runInputs(srv, "", nil))
	require.Empty(t, ok.Failures, runFailures(ok.Failures))
	assert.Equal(t, "deny", ok.Inputs.Get("toolApproval").AsString())
	assert.True(t, ok.Inputs.Get("inheritEnvironment").AsBool())

	cases := []struct {
		name   string
		in     property.Map
		expect string
	}{
		{"both sources", runInputs(srv, "", map[string]any{"reference": "localhost:5000/a/b:v1"}), "set either `yaml` or `reference`"},
		{"no source", runInputs(srv, "", nil).Delete("yaml"), "one of `yaml` and `reference` is required"},
		{"empty prompt", runInputs(srv, "", map[string]any{"prompt": "  "}), "`prompt` must not be empty"},
		{"bad approval", runInputs(srv, "", map[string]any{"toolApproval": "sometimes"}), "toolApproval"},
		{"bad timeout", runInputs(srv, "", map[string]any{"timeoutSeconds": 0}), "`timeoutSeconds` must be positive"},
		{"unknown agent", runInputs(srv, "", map[string]any{"agent": "nope"}), `agent "nope" is not defined`},
		{"invalid yaml", runInputs(srv, "    tolsets: []\n", nil), "invalid docker-agent configuration"},
		{"harness", runInputs(srv, "    harness:\n      type: claude-code\n", nil), "coding-harness agents"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resp := runCheck(t, s, c.in)
			assert.Contains(t, runFailures(resp.Failures), c.expect)
		})
	}

	// An unknown yaml (an output of another resource during preview) is not validated.
	unknown := runInputs(srv, "", nil).Set("yaml", property.New(property.Computed))
	resp := runCheck(t, s, unknown)
	assert.Empty(t, resp.Failures, runFailures(resp.Failures))
}

func TestAgentRunLifecycle(t *testing.T) {
	s := runServer(t)
	var calls atomic.Int32
	srv := fakeopenai.New(func(r fakeopenai.Request) fakeopenai.Reply {
		calls.Add(1)
		return fakeopenai.Default(r)
	})
	defer srv.Close()
	u := runURN("run")

	first := runCheck(t, s, runInputs(srv, runSchema, nil))
	require.Empty(t, first.Failures, runFailures(first.Failures))

	// Preview: nothing runs, every output is unknown.
	preview, err := s.Create(p.CreateRequest{Urn: u, Properties: first.Inputs, DryRun: true})
	require.NoError(t, err)
	for _, k := range agentRunOutputs {
		assert.True(t, preview.Properties.Get(k).IsComputed(), k)
	}
	assert.Zero(t, calls.Load(), "a preview never runs the agent")

	created, err := s.Create(p.CreateRequest{Urn: u, Properties: first.Inputs})
	require.NoError(t, err)
	assert.Equal(t, int32(1), calls.Load())
	state := created.Properties
	out := state.Get("output")
	assert.JSONEq(t, `{"risk":"low"}`, out.AsString())
	assert.False(t, out.Secret(), "a secret env does not make the answer secret")
	so := state.Get("structuredOutput")
	assert.False(t, so.Secret())
	assert.Equal(t, "low", so.AsMap().Get("risk").AsString())
	assert.Equal(t, "root", state.Get("agentName").AsString())
	assert.Equal(t, 10.0, state.Get("usage").AsMap().Get("inputTokens").AsNumber())
	assert.Equal(t, 5.0, state.Get("usage").AsMap().Get("outputTokens").AsNumber())
	assert.Equal(t, 0, state.Get("toolCalls").AsArray().Len())
	assert.NotEmpty(t, state.Get("ranAt").AsString())
	assert.True(t, state.Get("env").Secret(), "env stays secret in the state")
	assert.Equal(t, "Bearer "+runKey, srv.Requests()[0].Authorization)

	// No input change: no diff, so `pulumi up` does not run the agent again.
	same := runCheck(t, s, runInputs(srv, runSchema, nil))
	diff, err := s.Diff(p.DiffRequest{ID: created.ID, Urn: u, State: state, Inputs: same.Inputs, OldInputs: first.Inputs})
	require.NoError(t, err)
	assert.False(t, diff.HasChanges, "%+v", diff.DetailedDiff)

	// Refresh keeps the stored answer.
	read, err := s.Read(p.ReadRequest{ID: created.ID, Urn: u, Properties: state, Inputs: first.Inputs})
	require.NoError(t, err)
	assert.Equal(t, created.ID, read.ID)
	assert.Equal(t, out.AsString(), read.Properties.Get("output").AsString())
	assert.Equal(t, int32(1), calls.Load())

	// A new trigger: an update that runs the agent again.
	triggered := runCheck(t, s, runInputs(srv, runSchema, map[string]any{"triggers": []any{"v2"}}))
	diff, err = s.Diff(p.DiffRequest{ID: created.ID, Urn: u, State: state, Inputs: triggered.Inputs, OldInputs: first.Inputs})
	require.NoError(t, err)
	assert.True(t, diff.HasChanges)
	assert.Equal(t, p.Update, diff.DetailedDiff["triggers"].Kind)

	upPreview, err := s.Update(p.UpdateRequest{ID: created.ID, Urn: u, State: state, Inputs: triggered.Inputs,
		OldInputs: first.Inputs, DryRun: true})
	require.NoError(t, err)
	for _, k := range agentRunOutputs {
		assert.True(t, upPreview.Properties.Get(k).IsComputed(), k)
	}
	assert.Equal(t, int32(1), calls.Load(), "an update preview does not run the agent")

	updated, err := s.Update(p.UpdateRequest{ID: created.ID, Urn: u, State: state, Inputs: triggered.Inputs, OldInputs: first.Inputs})
	require.NoError(t, err)
	assert.Equal(t, int32(2), calls.Load())
	assert.JSONEq(t, `{"risk":"low"}`, updated.Properties.Get("output").AsString())
	assert.Equal(t, "v2", updated.Properties.Get("triggers").AsArray().Get(0).AsString())

	// The same triggers again: no change.
	again := runCheck(t, s, runInputs(srv, runSchema, map[string]any{"triggers": []any{"v2"}}))
	diff, err = s.Diff(p.DiffRequest{ID: created.ID, Urn: u, State: updated.Properties, Inputs: again.Inputs, OldInputs: triggered.Inputs})
	require.NoError(t, err)
	assert.False(t, diff.HasChanges, "%+v", diff.DetailedDiff)

	// Any other input change reruns too.
	prompt := runCheck(t, s, runInputs(srv, runSchema, map[string]any{"triggers": []any{"v2"}, "prompt": "Another change."}))
	diff, err = s.Diff(p.DiffRequest{ID: created.ID, Urn: u, State: updated.Properties, Inputs: prompt.Inputs, OldInputs: triggered.Inputs})
	require.NoError(t, err)
	assert.Equal(t, p.Update, diff.DetailedDiff["prompt"].Kind)

	require.NoError(t, s.Delete(p.DeleteRequest{ID: created.ID, Urn: u, Properties: updated.Properties}))
	assert.Equal(t, int32(2), calls.Load())
}

func TestAgentRunSecretness(t *testing.T) {
	s := runServer(t)
	srv := fakeopenai.New(nil)
	defer srv.Close()

	t.Run("outputSecret", func(t *testing.T) {
		in := runCheck(t, s, runInputs(srv, runSchema, map[string]any{"outputSecret": true}))
		require.Empty(t, in.Failures, runFailures(in.Failures))
		preview, err := s.Create(p.CreateRequest{Urn: runURN("run"), Properties: in.Inputs, DryRun: true})
		require.NoError(t, err)
		assert.True(t, preview.Properties.Get("output").Secret())
		created, err := s.Create(p.CreateRequest{Urn: runURN("run"), Properties: in.Inputs})
		require.NoError(t, err)
		assert.True(t, created.Properties.Get("output").Secret())
		assert.True(t, created.Properties.Get("structuredOutput").Secret())
		assert.False(t, created.Properties.Get("usage").Secret())
		assert.False(t, created.Properties.Get("agentName").Secret())
	})

	t.Run("secret prompt", func(t *testing.T) {
		in := runInputs(srv, "", nil)
		in = in.Set("prompt", property.New("a confidential change").WithSecret(true))
		checked := runCheck(t, s, in)
		require.Empty(t, checked.Failures, runFailures(checked.Failures))
		created, err := s.Create(p.CreateRequest{Urn: runURN("run"), Properties: checked.Inputs})
		require.NoError(t, err)
		assert.True(t, created.Properties.Get("output").Secret(), "the answer can repeat the prompt")
		assert.True(t, created.Properties.Get("prompt").Secret())
		assert.False(t, created.Properties.Get("ranAt").Secret())
	})
}

func TestAgentRunReference(t *testing.T) {
	s := runServer(t)
	srv := fakeopenai.New(func(r fakeopenai.Request) fakeopenai.Reply { return fakeopenai.Reply{Content: "from the registry"} })
	defer srv.Close()

	reg := httptest.NewServer(registry.New(registry.Logger(log.New(io.Discard, "", 0))))
	defer reg.Close()
	ref := strings.TrimPrefix(reg.URL, "http://") + "/agents/reviewer:v1"
	doc := []byte(runYAML(srv, ""))
	art, err := ociagent.Build(doc, ociagent.Annotations(doc, ociagent.FormatCreated(time.Now()), nil))
	require.NoError(t, err)
	_, err = ociagent.Push(t.Context(), ref, art, ociagent.Options{})
	require.NoError(t, err)

	in := runInputs(srv, "", map[string]any{"reference": ref}).Delete("yaml")
	checked := runCheck(t, s, in)
	require.Empty(t, checked.Failures, runFailures(checked.Failures))
	created, err := s.Create(p.CreateRequest{Urn: runURN("run"), Properties: checked.Inputs})
	require.NoError(t, err)
	assert.Equal(t, "from the registry", created.Properties.Get("output").AsString())
}

func TestAgentRunFailureDoesNotLeakTheKey(t *testing.T) {
	s := runServer(t)
	srv := fakeopenai.New(nil, fakeopenai.WithToken("the-right-key"))
	defer srv.Close()

	checked := runCheck(t, s, runInputs(srv, "", nil))
	require.Empty(t, checked.Failures, runFailures(checked.Failures))
	_, err := s.Create(p.CreateRequest{Urn: runURN("run"), Properties: checked.Inputs})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
	assert.NotContains(t, err.Error(), runKey)
}

func TestAgentRunToolApproval(t *testing.T) {
	s := runServer(t)
	srv := fakeopenai.New(func(r fakeopenai.Request) fakeopenai.Reply {
		if len(r.ToolResults()) == 0 {
			return fakeopenai.Reply{ToolCalls: []fakeopenai.ToolCall{{Name: "write_marker", Arguments: "{}"}}}
		}
		return fakeopenai.Reply{Content: "done"}
	})
	defer srv.Close()

	dir := t.TempDir()
	tools := fmt.Sprintf(`    toolsets:
      - type: script
        shell:
          write_marker:
            cmd: "echo ran > marker.txt"
            description: Writes a marker file
            working_dir: %s
`, dir)

	deny := runCheck(t, s, runInputs(srv, tools, nil))
	created, err := s.Create(p.CreateRequest{Urn: runURN("run"), Properties: deny.Inputs})
	require.NoError(t, err)
	assert.Equal(t, 0, created.Properties.Get("toolCalls").AsArray().Len())
	assert.NoFileExists(t, dir+"/marker.txt")

	allow := runCheck(t, s, runInputs(srv, tools, map[string]any{"toolApproval": "allow"}))
	created, err = s.Create(p.CreateRequest{Urn: runURN("run"), Properties: allow.Inputs})
	require.NoError(t, err)
	assert.Equal(t, "write_marker", created.Properties.Get("toolCalls").AsArray().Get(0).AsString())
	assert.FileExists(t, dir+"/marker.txt")
}

func TestAgentRunPreviewWithUnknownInputs(t *testing.T) {
	s := runServer(t)
	srv := fakeopenai.New(nil)
	defer srv.Close()

	// yaml from AgentConfig.yaml and prompt from another resource, both unknown in a preview.
	in := runInputs(srv, "", map[string]any{"agent": "root"}).
		Set("yaml", property.New(property.Computed)).
		Set("prompt", property.New(property.Computed))
	checked := runCheck(t, s, in)
	require.Empty(t, checked.Failures, runFailures(checked.Failures))
	assert.True(t, checked.Inputs.Get("yaml").IsComputed(), "withUnknownInputs keeps yaml unknown")
	assert.True(t, checked.Inputs.Get("prompt").IsComputed(), "withUnknownInputs keeps prompt unknown")
	assert.Equal(t, "deny", checked.Inputs.Get("toolApproval").AsString(), "defaults still apply")

	preview, err := s.Create(p.CreateRequest{Urn: runURN("run"), Properties: checked.Inputs, DryRun: true})
	require.NoError(t, err)
	for _, k := range agentRunOutputs {
		assert.True(t, preview.Properties.Get(k).IsComputed(), k)
	}
	assert.True(t, preview.Properties.Get("prompt").IsComputed())
	assert.Empty(t, srv.Requests(), "a preview never runs the agent")

	// An update preview with unknown inputs works the same way.
	first := runCheck(t, s, runInputs(srv, "", nil))
	created, err := s.Create(p.CreateRequest{Urn: runURN("run"), Properties: first.Inputs})
	require.NoError(t, err)
	diff, err := s.Diff(p.DiffRequest{ID: created.ID, Urn: runURN("run"), State: created.Properties, Inputs: checked.Inputs,
		OldInputs: first.Inputs})
	require.NoError(t, err)
	assert.True(t, diff.HasChanges)
	up, err := s.Update(p.UpdateRequest{ID: created.ID, Urn: runURN("run"), State: created.Properties, Inputs: checked.Inputs,
		OldInputs: first.Inputs, DryRun: true})
	require.NoError(t, err)
	for _, k := range agentRunOutputs {
		assert.True(t, up.Properties.Get(k).IsComputed(), k)
	}
	assert.Len(t, srv.Requests(), 1, "only the create ran the agent")
}

func TestAgentRunRedactsSecretInputsInErrors(t *testing.T) {
	s := runServer(t)
	const confidential = "the confidential merger plan"
	srv := fakeopenai.New(func(r fakeopenai.Request) fakeopenai.Reply {
		// A model API that quotes the request in its error.
		return fakeopenai.Reply{Status: 400, ErrorMessage: "cannot process: " + r.LastUser()}
	})
	defer srv.Close()

	in := runInputs(srv, "", nil).Set("prompt", property.New(confidential).WithSecret(true))
	checked := runCheck(t, s, in)
	require.Empty(t, checked.Failures, runFailures(checked.Failures))
	_, err := s.Create(p.CreateRequest{Urn: runURN("run"), Properties: checked.Inputs})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot process: [secret]")
	assert.NotContains(t, err.Error(), confidential)
}

// Review finding 5: an answer that repeats a secret `env` value is stored as a secret.
func TestAgentRunAnswerWithSecretIsStoredAsSecret(t *testing.T) {
	s := runServer(t)
	srv := fakeopenai.New(func(r fakeopenai.Request) fakeopenai.Reply {
		return fakeopenai.Reply{Content: "system was: " + r.System()}
	})
	defer srv.Close()

	leaky := strings.Replace(runYAML(srv, ""), "instruction: You review changes.",
		`instruction: "Use token ${env.FAKE_API_KEY} for calls."`, 1)
	in := runInputs(srv, "", nil).Set("yaml", property.New(leaky))
	checked := runCheck(t, s, in)
	require.Empty(t, checked.Failures, runFailures(checked.Failures))
	created, err := s.Create(p.CreateRequest{Urn: runURN("run"), Properties: checked.Inputs})
	require.NoError(t, err)
	out := created.Properties.Get("output")
	assert.True(t, out.Secret(), "the answer contains the env value")
	assert.Contains(t, out.AsString(), runKey)

	// A state-only update (timeoutSeconds) keeps it secret.
	longer := checked.Inputs.Set("timeoutSeconds", property.New(600.0))
	up, err := s.Update(p.UpdateRequest{ID: created.ID, Urn: runURN("run"), State: created.Properties, Inputs: longer,
		OldInputs: checked.Inputs})
	require.NoError(t, err)
	assert.True(t, up.Properties.Get("output").Secret())
	assert.Len(t, srv.Requests(), 1)

	// An answer without secrets stays public.
	plain := runCheck(t, s, runInputs(srv, "", nil))
	created, err = s.Create(p.CreateRequest{Urn: runURN("plain"), Properties: plain.Inputs})
	require.NoError(t, err)
	assert.False(t, created.Properties.Get("output").Secret())
}

// Review finding 6: inputs that become unknown are a pending rerun in the preview.
func TestAgentRunDiffUnknownInputs(t *testing.T) {
	s := runServer(t)
	srv := fakeopenai.New(nil)
	defer srv.Close()
	first := runCheck(t, s, runInputs(srv, "", nil))
	created, err := s.Create(p.CreateRequest{Urn: runURN("run"), Properties: first.Inputs})
	require.NoError(t, err)

	for _, key := range []string{"triggers", "agent", "workingDir", "env", "prompt"} {
		t.Run(key, func(t *testing.T) {
			in := first.Inputs.Set(key, property.New(property.Computed))
			diff, err := s.Diff(p.DiffRequest{ID: created.ID, Urn: runURN("run"), State: created.Properties, Inputs: in,
				OldInputs: first.Inputs})
			require.NoError(t, err)
			assert.True(t, diff.HasChanges)
			assert.Equal(t, p.Update, diff.DetailedDiff[key].Kind)

			up, err := s.Update(p.UpdateRequest{ID: created.ID, Urn: runURN("run"), State: created.Properties, Inputs: in,
				OldInputs: first.Inputs, DryRun: true})
			require.NoError(t, err)
			assert.True(t, up.Properties.Get("output").IsComputed(), "the preview shows a rerun")
		})
	}
	assert.Len(t, srv.Requests(), 1)
}

// Review finding 9: there is nothing to import.
func TestAgentRunImportIsNotSupported(t *testing.T) {
	_, err := runServer(t).Read(p.ReadRequest{ID: "imported", Urn: runURN("run")})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "importing an AgentRun is not supported")
}

// Review finding 14: outputSecret and timeoutSeconds alone update the state without a
// rerun; inputs that affect the run (here allowHookApprovals) rerun the agent.
func TestAgentRunStateOnlyChanges(t *testing.T) {
	s := runServer(t)
	var calls atomic.Int32
	srv := fakeopenai.New(func(r fakeopenai.Request) fakeopenai.Reply {
		calls.Add(1)
		return fakeopenai.Reply{Content: fmt.Sprintf("answer %d", calls.Load())}
	})
	defer srv.Close()
	u := runURN("run")
	first := runCheck(t, s, runInputs(srv, "", nil))
	created, err := s.Create(p.CreateRequest{Urn: u, Properties: first.Inputs})
	require.NoError(t, err)
	ranAt := created.Properties.Get("ranAt").AsString()

	for _, change := range []map[string]any{{"outputSecret": true}, {"timeoutSeconds": 30}} {
		next := runCheck(t, s, runInputs(srv, "", change))
		diff, err := s.Diff(p.DiffRequest{ID: created.ID, Urn: u, State: created.Properties, Inputs: next.Inputs, OldInputs: first.Inputs})
		require.NoError(t, err)
		assert.True(t, diff.HasChanges, "%v", change)

		preview, err := s.Update(p.UpdateRequest{ID: created.ID, Urn: u, State: created.Properties, Inputs: next.Inputs,
			OldInputs: first.Inputs, DryRun: true})
		require.NoError(t, err)
		assert.Equal(t, "answer 1", preview.Properties.Get("output").AsString(), "no rerun in the preview")

		up, err := s.Update(p.UpdateRequest{ID: created.ID, Urn: u, State: created.Properties, Inputs: next.Inputs, OldInputs: first.Inputs})
		require.NoError(t, err)
		assert.Equal(t, "answer 1", up.Properties.Get("output").AsString())
		assert.Equal(t, ranAt, up.Properties.Get("ranAt").AsString())
		for k := range change {
			assert.False(t, up.Properties.Get(k).IsNull(), k)
		}
		if _, ok := change["outputSecret"]; ok {
			assert.True(t, up.Properties.Get("output").Secret())
		}
	}
	assert.Equal(t, int32(1), calls.Load())

	hooks := runCheck(t, s, runInputs(srv, "", map[string]any{"allowHookApprovals": true}))
	diff, err := s.Diff(p.DiffRequest{ID: created.ID, Urn: u, State: created.Properties, Inputs: hooks.Inputs, OldInputs: first.Inputs})
	require.NoError(t, err)
	assert.Equal(t, p.Update, diff.DetailedDiff["allowHookApprovals"].Kind)
	up, err := s.Update(p.UpdateRequest{ID: created.ID, Urn: u, State: created.Properties, Inputs: hooks.Inputs, OldInputs: first.Inputs})
	require.NoError(t, err)
	assert.Equal(t, "answer 2", up.Properties.Get("output").AsString())
	assert.Equal(t, int32(2), calls.Load())
}
