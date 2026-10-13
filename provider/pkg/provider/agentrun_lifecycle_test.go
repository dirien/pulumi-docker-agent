// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"maps"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/registry"
	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/integration"
	"github.com/pulumi/pulumi/sdk/v3/go/common/resource"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dirien/pulumi-docker-agent/provider/pkg/agentrun/fakeopenai"
	"github.com/dirien/pulumi-docker-agent/provider/pkg/ociagent"
)

// resetRunState forgets the process-local AgentRun state after a test: the `rerun: always`
// marks and the working directories runs used. A provider process serves one operation;
// the tests share one process.
func resetRunState(t *testing.T) {
	t.Helper()
	forget := func() {
		alwaysChecked.Clear()
		usedDirs.Clear()
	}
	forget()
	t.Cleanup(forget)
}

// with returns m plus extra.
func with(m map[string]any, extra map[string]any) map[string]any {
	out := maps.Clone(m)
	maps.Copy(out, extra)
	return out
}

// prompts returns the last user message of every request the fake model got.
func prompts(srv *fakeopenai.Server) []string {
	var out []string
	for _, r := range srv.Requests() {
		out = append(out, r.LastUser())
	}
	return out
}

// runCheckAt checks AgentRun inputs for a resource.
func runCheckAt(t *testing.T, s interface {
	Check(p.CheckRequest) (p.CheckResponse, error)
}, urn resource.URN, in property.Map) p.CheckResponse {
	t.Helper()
	resp, err := s.Check(p.CheckRequest{Urn: urn, Inputs: in})
	require.NoError(t, err)
	return resp
}

func diffKeys(d p.DiffResponse) []string {
	return slices.Sorted(maps.Keys(d.DetailedDiff))
}

func TestAgentRunPromptSelection(t *testing.T) {
	resetRunState(t)
	s := runServer(t)
	srv := fakeopenai.New(func(fakeopenai.Request) fakeopenai.Reply { return fakeopenai.Reply{Content: "ok"} })
	defer srv.Close()
	u := runURN("prompts")
	base := map[string]any{"updatePrompt": "Update the summary."}

	first := runCheck(t, s, runInputs(srv, "", base))
	require.Empty(t, first.Failures, runFailures(first.Failures))
	created, err := s.Create(p.CreateRequest{Urn: u, Properties: first.Inputs})
	require.NoError(t, err)
	assert.Equal(t, []string{"Upgrade the database."}, prompts(srv), "create sends prompt")

	step := func(state, olds property.Map, extra map[string]any) (p.UpdateResponse, property.Map) {
		t.Helper()
		next := runCheck(t, s, runInputs(srv, "", extra))
		require.Empty(t, next.Failures, runFailures(next.Failures))
		up, err := s.Update(p.UpdateRequest{ID: created.ID, Urn: u, State: state, Inputs: next.Inputs, OldInputs: olds})
		require.NoError(t, err)
		return up, next.Inputs
	}

	up, olds := step(created.Properties, first.Inputs, with(base, map[string]any{"triggers": []any{"v2"}}))
	assert.Equal(t, "Update the summary.", prompts(srv)[1], "a trigger change reruns with updatePrompt")

	up, olds = step(up.Properties, olds, with(base, map[string]any{"triggers": []any{"v3"}, "prompt": "A new task."}))
	assert.Equal(t, "A new task.", prompts(srv)[2], "a changed prompt wins over updatePrompt")

	up, olds = step(up.Properties, olds, map[string]any{"triggers": []any{"v4"}, "prompt": "A new task."})
	assert.Equal(t, "A new task.", prompts(srv)[3], "without updatePrompt the rerun sends prompt")

	// Changing updatePrompt alone is state-only.
	changed := runCheck(t, s, runInputs(srv, "", map[string]any{"triggers": []any{"v4"}, "prompt": "A new task.",
		"updatePrompt": "Something else."}))
	diff, err := s.Diff(p.DiffRequest{ID: created.ID, Urn: u, State: up.Properties, Inputs: changed.Inputs, OldInputs: olds})
	require.NoError(t, err)
	assert.True(t, diff.HasChanges)
	assert.Equal(t, []string{"updatePrompt"}, diffKeys(diff))
	preview, err := s.Update(p.UpdateRequest{ID: created.ID, Urn: u, State: up.Properties, Inputs: changed.Inputs,
		OldInputs: olds, DryRun: true})
	require.NoError(t, err)
	assert.Equal(t, "ok", preview.Properties.Get("output").AsString(), "no rerun in the preview")
	final, err := s.Update(p.UpdateRequest{ID: created.ID, Urn: u, State: up.Properties, Inputs: changed.Inputs, OldInputs: olds})
	require.NoError(t, err)
	assert.Len(t, srv.Requests(), 4, "no call")
	assert.Equal(t, "Something else.", final.Properties.Get("updatePrompt").AsString())
	assert.Equal(t, up.Properties.Get("ranAt").AsString(), final.Properties.Get("ranAt").AsString())
}

func TestAgentRunRerunAlwaysMarker(t *testing.T) {
	resetRunState(t)
	s := runServer(t)
	var calls atomic.Int32
	srv := fakeopenai.New(func(fakeopenai.Request) fakeopenai.Reply {
		return fakeopenai.Reply{Content: fmt.Sprintf("answer %d", calls.Add(1))}
	})
	defer srv.Close()
	u := runURN("always")
	raw := runInputs(srv, "", map[string]any{"rerun": "always", "updatePrompt": "Look again."})

	checked := runCheckAt(t, s, u, raw)
	require.Empty(t, checked.Failures, runFailures(checked.Failures))
	created, err := s.Create(p.CreateRequest{Urn: u, Properties: checked.Inputs})
	require.NoError(t, err)
	assert.Equal(t, int32(1), calls.Load())
	diff := func(urn resource.URN) p.DiffResponse {
		t.Helper()
		d, err := s.Diff(p.DiffRequest{ID: created.ID, Urn: urn, State: created.Properties, Inputs: checked.Inputs,
			OldInputs: checked.Inputs})
		require.NoError(t, err)
		return d
	}

	// A Diff without a Check of the resource in this process is a refresh: no change.
	alwaysChecked.Delete(u)
	assert.False(t, diff(u).HasChanges, "no Check, no rerun")

	// After a Check (`pulumi up` registers the resource), `always` is a change.
	runCheckAt(t, s, u, raw)
	d := diff(u)
	assert.True(t, d.HasChanges)
	assert.Equal(t, []string{"rerun"}, diffKeys(d))
	assert.Equal(t, p.PropertyDiff{Kind: p.Update, InputDiff: true}, d.DetailedDiff["rerun"])
	assert.False(t, diff(runURN("other")).HasChanges, "the mark is per resource")

	// Read (a refresh) clears the mark.
	_, err = s.Read(p.ReadRequest{ID: created.ID, Urn: u, Properties: created.Properties, Inputs: checked.Inputs})
	require.NoError(t, err)
	assert.False(t, diff(u).HasChanges, "a refresh reports no drift")

	// Check, Diff and the update of `pulumi up`.
	runCheckAt(t, s, u, raw)
	require.True(t, diff(u).HasChanges)
	preview, err := s.Update(p.UpdateRequest{ID: created.ID, Urn: u, State: created.Properties, Inputs: checked.Inputs,
		OldInputs: checked.Inputs, DryRun: true})
	require.NoError(t, err)
	for _, k := range agentRunOutputs {
		assert.True(t, preview.Properties.Get(k).IsComputed(), k)
	}
	assert.Equal(t, int32(1), calls.Load(), "a preview never runs the agent")
	up, err := s.Update(p.UpdateRequest{ID: created.ID, Urn: u, State: created.Properties, Inputs: checked.Inputs,
		OldInputs: checked.Inputs})
	require.NoError(t, err)
	assert.Equal(t, int32(2), calls.Load())
	assert.Equal(t, "answer 2", up.Properties.Get("output").AsString())
	assert.Equal(t, "Look again.", prompts(srv)[1], "the rerun sends updatePrompt")

	// An unknown `rerun` in a Check sets no mark; the unknown input itself is a change.
	alwaysChecked.Delete(u)
	unknown := runCheckAt(t, s, u, raw.Set("rerun", property.New(property.Computed)))
	_, marked := alwaysChecked.Load(u)
	assert.False(t, marked)
	d, err = s.Diff(p.DiffRequest{ID: created.ID, Urn: u, State: created.Properties, Inputs: unknown.Inputs,
		OldInputs: checked.Inputs})
	require.NoError(t, err)
	assert.True(t, d.HasChanges)
}

func TestAgentRunRerunPolicyChanges(t *testing.T) {
	resetRunState(t)
	s := runServer(t)
	var calls atomic.Int32
	srv := fakeopenai.New(func(fakeopenai.Request) fakeopenai.Reply {
		return fakeopenai.Reply{Content: fmt.Sprintf("answer %d", calls.Add(1))}
	})
	defer srv.Close()
	u := runURN("policy")

	always := runCheckAt(t, s, u, runInputs(srv, "", map[string]any{"rerun": "always"}))
	created, err := s.Create(p.CreateRequest{Urn: u, Properties: always.Inputs})
	require.NoError(t, err)

	// always -> onChange: state-only.
	onChange := runCheckAt(t, s, u, runInputs(srv, "", map[string]any{"rerun": "onChange"}))
	diff, err := s.Diff(p.DiffRequest{ID: created.ID, Urn: u, State: created.Properties, Inputs: onChange.Inputs,
		OldInputs: always.Inputs})
	require.NoError(t, err)
	assert.Equal(t, []string{"rerun"}, diffKeys(diff))
	up, err := s.Update(p.UpdateRequest{ID: created.ID, Urn: u, State: created.Properties, Inputs: onChange.Inputs,
		OldInputs: always.Inputs})
	require.NoError(t, err)
	assert.Equal(t, int32(1), calls.Load())
	assert.Equal(t, "onChange", up.Properties.Get("rerun").AsString())
	assert.Equal(t, created.Properties.Get("ranAt"), up.Properties.Get("ranAt"))

	// onChange -> always: reruns.
	again := runCheckAt(t, s, u, runInputs(srv, "", map[string]any{"rerun": "always"}))
	diff, err = s.Diff(p.DiffRequest{ID: created.ID, Urn: u, State: up.Properties, Inputs: again.Inputs,
		OldInputs: onChange.Inputs})
	require.NoError(t, err)
	assert.Equal(t, []string{"rerun"}, diffKeys(diff))
	_, err = s.Update(p.UpdateRequest{ID: created.ID, Urn: u, State: up.Properties, Inputs: again.Inputs,
		OldInputs: onChange.Inputs})
	require.NoError(t, err)
	assert.Equal(t, int32(2), calls.Load())
}

func TestAgentRunDelete(t *testing.T) {
	var calls atomic.Int32
	var fail atomic.Bool
	srv := fakeopenai.New(func(r fakeopenai.Request) fakeopenai.Reply {
		calls.Add(1)
		if fail.Load() {
			// A model API that quotes the rejected key.
			return fakeopenai.Reply{Status: 401, ErrorMessage: "Incorrect API key provided: " + strings.TrimPrefix(r.Authorization, "Bearer ")}
		}
		return fakeopenai.Reply{Content: "cleaned up"}
	})
	defer srv.Close()
	create := func(t *testing.T, s interface {
		Create(p.CreateRequest) (p.CreateResponse, error)
	}, checked p.CheckResponse, name string) p.CreateResponse {
		t.Helper()
		require.Empty(t, checked.Failures, runFailures(checked.Failures))
		created, err := s.Create(p.CreateRequest{Urn: runURN(name), Properties: checked.Inputs})
		require.NoError(t, err)
		return created
	}
	withPrompt := map[string]any{"deletePrompt": "Delete what you created."}

	t.Run("runs deletePrompt", func(t *testing.T) {
		resetRunState(t)
		s, rec := loggingServer(t)
		created := create(t, s, runCheck(t, s, runInputs(srv, "", withPrompt)), "del")
		before := len(srv.Requests())
		rec.reset()
		require.NoError(t, s.Delete(p.DeleteRequest{ID: created.ID, Urn: runURN("del"), Properties: created.Properties}))
		reqs := srv.Requests()
		require.Len(t, reqs, before+1)
		assert.Equal(t, "Delete what you created.", reqs[before].LastUser())
		assert.Equal(t, "Bearer "+runKey, reqs[before].Authorization, "the stored secret env is used")
		assert.Contains(t, strings.Join(rec.messages("info"), "\n"), "agent run finished", "the summary is logged")
	})
	t.Run("nothing without deletePrompt", func(t *testing.T) {
		resetRunState(t)
		s := runServer(t)
		created := create(t, s, runCheck(t, s, runInputs(srv, "", nil)), "plain")
		before := len(srv.Requests())
		require.NoError(t, s.Delete(p.DeleteRequest{ID: created.ID, Urn: runURN("plain"), Properties: created.Properties}))
		assert.Len(t, srv.Requests(), before)
	})
	t.Run("a failure fails the delete", func(t *testing.T) {
		resetRunState(t)
		s := runServer(t)
		created := create(t, s, runCheck(t, s, runInputs(srv, "", withPrompt)), "fails")
		fail.Store(true)
		defer fail.Store(false)
		err := s.Delete(p.DeleteRequest{ID: created.ID, Urn: runURN("fails"), Properties: created.Properties})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "deletePrompt failed: ")
		assert.Contains(t, err.Error(), "set DOCKER_AGENT_SKIP_DELETE_PROMPTS=true for this command (for example "+
			"`DOCKER_AGENT_SKIP_DELETE_PROMPTS=true pulumi destroy`)")
		assert.Contains(t, err.Error(), "401")
		assert.NotContains(t, err.Error(), runKey)
	})
	t.Run("DOCKER_AGENT_SKIP_DELETE_PROMPTS", func(t *testing.T) {
		resetRunState(t)
		s, rec := loggingServer(t)
		created := create(t, s, runCheck(t, s, runInputs(srv, "", withPrompt)), "skip")
		before := len(srv.Requests())
		for _, v := range []string{"true", "1", "TRUE"} {
			t.Setenv(skipDeletePromptsEnv, v)
			rec.reset()
			require.NoError(t, s.Delete(p.DeleteRequest{ID: created.ID, Urn: runURN("skip"), Properties: created.Properties}))
			assert.Equal(t, []string{"deletePrompt not run: DOCKER_AGENT_SKIP_DELETE_PROMPTS is set"}, rec.messages("warning"))
		}
		assert.Len(t, srv.Requests(), before)
		t.Setenv(skipDeletePromptsEnv, "false")
		require.NoError(t, s.Delete(p.DeleteRequest{ID: created.ID, Urn: runURN("skip"), Properties: created.Properties}))
		assert.Len(t, srv.Requests(), before+1, "a false value runs it")
	})
	t.Run("workingDir is gone", func(t *testing.T) {
		resetRunState(t)
		s, rec := loggingServer(t)
		dir := filepath.Join(t.TempDir(), "work")
		require.NoError(t, os.Mkdir(dir, 0o700))
		created := create(t, s, runCheck(t, s, runInputs(srv, "", with(withPrompt, map[string]any{"workingDir": dir}))), "gone")
		require.NoError(t, os.RemoveAll(dir))
		before := len(srv.Requests())
		rec.reset()
		require.NoError(t, s.Delete(p.DeleteRequest{ID: created.ID, Urn: runURN("gone"), Properties: created.Properties}))
		assert.Len(t, srv.Requests(), before)
		assert.Equal(t, []string{fmt.Sprintf("deletePrompt not run: workingDir %q no longer exists", dir)}, rec.messages("warning"))
	})
	t.Run("another run used the workingDir", func(t *testing.T) {
		resetRunState(t)
		s, rec := loggingServer(t)
		dir := t.TempDir()
		// The new resource of a replacement ran in dir; the old one's delete comes next.
		created := create(t, s, runCheck(t, s, runInputs(srv, "", with(withPrompt, map[string]any{"workingDir": dir}))), "new")
		before := len(srv.Requests())
		rec.reset()
		require.NoError(t, s.Delete(p.DeleteRequest{ID: "old-0123abcd", Urn: runURN("old"), Properties: created.Properties}))
		assert.Len(t, srv.Requests(), before)
		warnings := rec.messages("warning")
		require.Len(t, warnings, 1)
		assert.Contains(t, warnings[0], fmt.Sprintf("deletePrompt not run: an AgentRun already ran in workingDir %q during "+
			"this pulumi command (a replacement, or a rename without aliases, creates the new run before it deletes the old "+
			"one), so this delete prompt would undo its work; use the deleteBeforeReplace resource option or aliases", dir))

		// A later command (another provider process) runs it.
		usedDirs.Clear()
		require.NoError(t, s.Delete(p.DeleteRequest{ID: "old-0123abcd", Urn: runURN("old"), Properties: created.Properties}))
		assert.Len(t, srv.Requests(), before+1)
	})
	t.Run("a secret workingDir is not shown", func(t *testing.T) {
		resetRunState(t)
		s, rec := loggingServer(t)
		dir := filepath.Join(t.TempDir(), "confidential-project-name")
		require.NoError(t, os.Mkdir(dir, 0o700))
		in := runInputs(srv, "", withPrompt).Set("workingDir", property.New(dir).WithSecret(true))
		created := create(t, s, runCheck(t, s, in), "new")
		require.True(t, created.Properties.Get("workingDir").Secret())
		before := len(srv.Requests())
		for _, gone := range []bool{false, true} {
			if gone {
				require.NoError(t, os.RemoveAll(dir))
			}
			rec.reset()
			require.NoError(t, s.Delete(p.DeleteRequest{ID: "old-0123abcd", Urn: runURN("old"), Properties: created.Properties}))
			warnings := rec.messages("warning")
			require.Len(t, warnings, 1)
			assert.Contains(t, warnings[0], "[secret]")
			assert.NotContains(t, warnings[0], "confidential-project-name", "the skip reason names the secret workingDir")
		}
		assert.Len(t, srv.Requests(), before)
	})
	t.Run("workingDir is no longer a directory", func(t *testing.T) {
		resetRunState(t)
		s, rec := loggingServer(t)
		dir := filepath.Join(t.TempDir(), "work")
		require.NoError(t, os.Mkdir(dir, 0o700))
		created := create(t, s, runCheck(t, s, runInputs(srv, "", with(withPrompt, map[string]any{"workingDir": dir}))), "file")
		require.NoError(t, os.Remove(dir))
		require.NoError(t, os.WriteFile(dir, []byte("x"), 0o600))
		usedDirs.Clear()
		before := len(srv.Requests())
		rec.reset()
		require.NoError(t, s.Delete(p.DeleteRequest{ID: created.ID, Urn: runURN("file"), Properties: created.Properties}))
		assert.Len(t, srv.Requests(), before)
		assert.Equal(t, []string{fmt.Sprintf("deletePrompt not run: workingDir %q is no longer a directory", dir)},
			rec.messages("warning"))
	})
	t.Run("an unreadable workingDir is not skipped", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root reads every directory")
		}
		resetRunState(t)
		s, rec := loggingServer(t)
		parent := t.TempDir()
		dir := filepath.Join(parent, "work")
		require.NoError(t, os.Mkdir(dir, 0o700))
		created := create(t, s, runCheck(t, s, runInputs(srv, "", with(withPrompt, map[string]any{"workingDir": dir}))), "locked")
		usedDirs.Clear()
		require.NoError(t, os.Chmod(parent, 0)) //nolint:gosec // a directory nobody may enter
		t.Cleanup(func() { _ = os.Chmod(parent, 0o700) })
		rec.reset()
		err := s.Delete(p.DeleteRequest{ID: created.ID, Urn: runURN("locked"), Properties: created.Properties})
		require.Error(t, err, "the delete fails and says why, rather than skipping the cleanup")
		assert.Contains(t, err.Error(), "deletePrompt failed: ")
		assert.Empty(t, rec.messages("warning"))
	})

	// The delete prompt of B waits for the run slot while A runs in the same workingDir: B's
	// prompt would undo A's work.
	sameDir := func(t *testing.T, srv *fakeopenai.Server) (s integration.Server, rec *recordingLogger, b p.CreateResponse,
		in func(string) property.Map) {
		t.Helper()
		resetRunState(t)
		s, rec = loggingServer(t)
		dir := t.TempDir()
		in = func(prompt string) property.Map {
			return runInputs(srv, "", with(withPrompt, map[string]any{"workingDir": dir, "prompt": prompt}))
		}
		b = create(t, s, runCheck(t, s, in("Prompt B")), "b")
		usedDirs.Clear() // B ran in an earlier command
		return s, rec, b, in
	}
	blockingModel := func(t *testing.T, blocked string) (*fakeopenai.Server, chan struct{}, *atomic.Int32) {
		t.Helper()
		release, deletes := make(chan struct{}), &atomic.Int32{}
		srv := fakeopenai.New(func(r fakeopenai.Request) fakeopenai.Reply {
			switch r.LastUser() {
			case blocked:
				<-release
			case withPrompt["deletePrompt"]:
				deletes.Add(1)
			}
			return fakeopenai.Reply{Content: "done"}
		})
		t.Cleanup(srv.Close)
		return srv, release, deletes
	}
	t.Run("another run is in progress in the workingDir", func(t *testing.T) {
		model, release, deletes := blockingModel(t, "Prompt A")
		s, rec, b, in := sameDir(t, model)
		a := runCheck(t, s, in("Prompt A"))
		created := make(chan error, 1)
		go func() {
			_, err := s.Create(p.CreateRequest{Urn: runURN("a"), Properties: a.Inputs})
			created <- err
		}()
		require.Eventually(t, func() bool { return slices.Contains(prompts(model), "Prompt A") }, 10*time.Second,
			10*time.Millisecond)
		rec.reset()
		deleted := make(chan error, 1)
		go func() { deleted <- s.Delete(p.DeleteRequest{ID: b.ID, Urn: runURN("b"), Properties: b.Properties}) }()
		time.Sleep(200 * time.Millisecond)
		close(release)
		require.NoError(t, <-created)
		require.NoError(t, <-deleted)
		assert.Zero(t, deletes.Load(), "B's delete prompt did not run")
		assert.Contains(t, strings.Join(rec.messages("warning"), "\n"), "an AgentRun already ran in workingDir")
	})
	t.Run("the check is repeated when the delete prompt's run starts", func(t *testing.T) {
		model, release, deletes := blockingModel(t, "Prompt C")
		s, rec, b, _ := sameDir(t, model)
		// C, in another directory, holds the run slot.
		c := runCheck(t, s, runInputs(model, "", map[string]any{"prompt": "Prompt C"}))
		created := make(chan error, 1)
		go func() {
			_, err := s.Create(p.CreateRequest{Urn: runURN("c"), Properties: c.Inputs})
			created <- err
		}()
		require.Eventually(t, func() bool { return slices.Contains(prompts(model), "Prompt C") }, 10*time.Second,
			10*time.Millisecond)
		rec.reset()
		deleted := make(chan error, 1)
		go func() { deleted <- s.Delete(p.DeleteRequest{ID: b.ID, Urn: runURN("b"), Properties: b.Properties}) }()
		time.Sleep(200 * time.Millisecond)
		// While B waits, an AgentRun starts in its workingDir (as A, waiting before B, would).
		dir, ok := resolvedDir(b.Properties.Get("workingDir").AsString())
		require.True(t, ok)
		usedDirs.Store(dir, true)
		close(release)
		require.NoError(t, <-created)
		require.NoError(t, <-deleted)
		assert.Zero(t, deletes.Load(), "B's delete prompt did not run")
		assert.Contains(t, strings.Join(rec.messages("warning"), "\n"), "an AgentRun already ran in workingDir")
	})
}

// TestAgentRunStateIsPerCommand: a provider process the engine attaches to
// (PULUMI_DEBUG_PROVIDERS) serves several commands, each from another engine.
func TestAgentRunStateIsPerCommand(t *testing.T) {
	resetRunState(t)
	engineAddress.Lock()
	prev := engineAddress.addr
	engineAddress.addr = ""
	engineAddress.Unlock()
	t.Cleanup(func() {
		engineAddress.Lock()
		engineAddress.addr = prev
		engineAddress.Unlock()
	})
	prov := Provider()
	handshake := func(addr string) {
		t.Helper()
		_, err := prov.Handshake(t.Context(), p.HandshakeRequest{EngineAddress: addr})
		require.NoError(t, err)
	}
	marked := func() (bool, bool) {
		_, dir := usedDirs.Load("/work")
		_, always := alwaysChecked.Load(runURN("a"))
		return dir, always
	}
	handshake("127.0.0.1:40001")
	usedDirs.Store("/work", true)
	alwaysChecked.Store(runURN("a"), true)
	handshake("127.0.0.1:40001") // another provider of the same command
	dir, always := marked()
	assert.True(t, dir && always, "one command keeps its state")
	handshake("127.0.0.1:40002") // the next command
	dir, always = marked()
	assert.False(t, dir || always, "the next command starts without the state of the previous one")
}

// A state-only update keeps an empty `toolCalls`, and a state that lost it (v0.1.1 dropped
// it) still diffs, updates and deletes.
func TestAgentRunEmptyToolCallsSurvive(t *testing.T) {
	resetRunState(t)
	s := runServer(t)
	srv := fakeopenai.New(nil)
	defer srv.Close()
	u := runURN("tools")
	first := runCheckAt(t, s, u, runInputs(srv, "", nil))
	created, err := s.Create(p.CreateRequest{Urn: u, Properties: first.Inputs})
	require.NoError(t, err)
	next := runCheckAt(t, s, u, runInputs(srv, "", map[string]any{"timeoutSeconds": 30}))
	up, err := s.Update(p.UpdateRequest{ID: created.ID, Urn: u, State: created.Properties, Inputs: next.Inputs,
		OldInputs: first.Inputs})
	require.NoError(t, err)
	tools, ok := up.Properties.GetOk("toolCalls")
	require.True(t, ok, "toolCalls is kept")
	assert.Equal(t, 0, tools.AsArray().Len())

	broken := created.Properties.Delete("toolCalls")
	diff, err := s.Diff(p.DiffRequest{ID: created.ID, Urn: u, State: broken, Inputs: first.Inputs, OldInputs: first.Inputs})
	require.NoError(t, err)
	assert.False(t, diff.HasChanges)
	_, err = s.Update(p.UpdateRequest{ID: created.ID, Urn: u, State: broken, Inputs: next.Inputs, OldInputs: first.Inputs})
	require.NoError(t, err)
	_, err = s.Read(p.ReadRequest{ID: created.ID, Urn: u, Properties: broken, Inputs: first.Inputs})
	require.NoError(t, err)
	require.NoError(t, s.Delete(p.DeleteRequest{ID: created.ID, Urn: u, Properties: broken}))
	assert.Len(t, srv.Requests(), 1)
}

// A secret deletePrompt keeps the delete run's tool arguments and messages out of the logs.
func TestAgentRunSecretDeletePrompt(t *testing.T) {
	resetRunState(t)
	s, rec := loggingServer(t)
	srv := loggingModel()
	defer srv.Close()
	in := runInputs(srv, loggingAgent, map[string]any{"logging": "tools"}).
		Set("deletePrompt", property.New("delete the confidential files").WithSecret(true))
	checked := runCheck(t, s, in)
	require.Empty(t, checked.Failures, runFailures(checked.Failures))
	created, err := s.Create(p.CreateRequest{Urn: runURN("secret-delete"), Properties: checked.Inputs})
	require.NoError(t, err)
	require.True(t, created.Properties.Get("deletePrompt").Secret())
	assert.Contains(t, strings.Join(rec.messages("info"), "\n"), "root called think {", "the create run logs arguments")

	rec.reset()
	require.NoError(t, s.Delete(p.DeleteRequest{ID: created.ID, Urn: runURN("secret-delete"), Properties: created.Properties}))
	info := rec.messages("info")
	require.Len(t, info, 2, "only the credential line and the summary: %s", rec.all())
	assert.Equal(t, "models: fake/fake-model (FAKE_API_KEY from env)", info[0])
	assert.Contains(t, info[1], "agent run finished")
}

func TestAgentRunDeleteBeforeReplace(t *testing.T) {
	resetRunState(t)
	s := runServer(t)
	srv := fakeopenai.New(nil)
	defer srv.Close()
	for _, c := range []struct {
		extra map[string]any
		want  bool
	}{{nil, false}, {map[string]any{"deletePrompt": "Clean up."}, true}} {
		checked := runCheck(t, s, runInputs(srv, "", c.extra))
		created, err := s.Create(p.CreateRequest{Urn: runURN("dbr"), Properties: checked.Inputs})
		require.NoError(t, err)
		for _, next := range []map[string]any{c.extra, with(map[string]any{"prompt": "Another task."}, c.extra)} {
			in := runCheck(t, s, runInputs(srv, "", next))
			diff, err := s.Diff(p.DiffRequest{ID: created.ID, Urn: runURN("dbr"), State: created.Properties, Inputs: in.Inputs,
				OldInputs: checked.Inputs})
			require.NoError(t, err)
			assert.Equal(t, c.want, diff.DeleteBeforeReplace, "%v -> %v", c.extra, next)
		}
	}
}

func TestAgentRunUniqueIDs(t *testing.T) {
	resetRunState(t)
	s := runServer(t)
	srv := fakeopenai.New(nil)
	defer srv.Close()
	checked := runCheck(t, s, runInputs(srv, "", nil))
	a, err := s.Create(p.CreateRequest{Urn: runURN("run"), Properties: checked.Inputs})
	require.NoError(t, err)
	b, err := s.Create(p.CreateRequest{Urn: runURN("run"), Properties: checked.Inputs})
	require.NoError(t, err)
	assert.Regexp(t, regexp.MustCompile(`^run-[0-9a-f]{8}$`), a.ID)
	assert.NotEqual(t, a.ID, b.ID, "a replacement and its pending delete differ")

	// A v0.1.1 resource keeps its name as ID: a refresh does not rewrite a stored ID (the
	// other methods do not read it).
	read, err := s.Read(p.ReadRequest{ID: "run", Urn: runURN("run"), Properties: a.Properties, Inputs: checked.Inputs})
	require.NoError(t, err)
	assert.Equal(t, "run", read.ID)
}

// stateValue converts a value of `pulumi stack export --show-secrets` to a property value.
func stateValue(v any) property.Value {
	const sig, secretSig = "4dabf18193072939515e22adb298388d", "1b47061264138c4ac30d75fd1eb44270"
	switch v := v.(type) {
	case map[string]any:
		if v[sig] == secretSig {
			var inner any
			if s, ok := v["plaintext"].(string); ok && json.Unmarshal([]byte(s), &inner) == nil {
				return stateValue(inner).WithSecret(true)
			}
		}
		m := map[string]property.Value{}
		for k, e := range v {
			m[k] = stateValue(e)
		}
		return property.New(property.NewMap(m))
	case []any:
		items := make([]property.Value, 0, len(v))
		for _, e := range v {
			items = append(items, stateValue(e))
		}
		return property.New(items)
	case string:
		return property.New(v)
	case float64:
		return property.New(v)
	case bool:
		return property.New(v)
	}
	return property.Value{}
}

func TestAgentRunUpgradeFromV011(t *testing.T) {
	resetRunState(t)
	b, err := os.ReadFile(filepath.Join("testdata", "agentrun-v0.1.1-state.json"))
	require.NoError(t, err)
	var fixture struct {
		URN     string         `json:"urn"`
		ID      string         `json:"id"`
		Inputs  map[string]any `json:"inputs"`
		Outputs map[string]any `json:"outputs"`
	}
	require.NoError(t, json.Unmarshal(b, &fixture))
	urn := resource.URN(fixture.URN)
	oldInputs, state := stateValue(fixture.Inputs).AsMap(), stateValue(fixture.Outputs).AsMap()
	require.True(t, state.Get("env").Secret())
	for _, k := range []string{"updatePrompt", "deletePrompt", "rerun", "allowHarnessAgents"} {
		_, inOld := oldInputs.GetOk(k)
		require.False(t, inOld, k)
		_, inState := state.GetOk(k)
		require.False(t, inState, k)
	}

	// The unchanged program, as v0.1.2 checks it.
	s := runServer(t)
	program := runProps(map[string]any{"prompt": oldInputs.Get("prompt").AsString(), "yaml": oldInputs.Get("yaml").AsString()}).
		Set("env", oldInputs.Get("env"))
	checked, err := s.Check(p.CheckRequest{Urn: urn, Inputs: program, State: oldInputs})
	require.NoError(t, err)
	require.Empty(t, checked.Failures, runFailures(checked.Failures))
	assert.Equal(t, "onChange", checked.Inputs.Get("rerun").AsString())
	assert.False(t, checked.Inputs.Get("allowHarnessAgents").AsBool())

	req := p.DiffRequest{ID: fixture.ID, Urn: urn, State: state, OldInputs: oldInputs, Inputs: checked.Inputs}
	raw, err := NewAgentRunResource().Diff(t.Context(), req)
	require.NoError(t, err)
	assert.False(t, raw.HasChanges, "%v", raw.DetailedDiff)
	assert.False(t, raw.DeleteBeforeReplace)
	served, err := s.Diff(req)
	require.NoError(t, err)
	assert.False(t, served.HasChanges, "%v", served.DetailedDiff)

	read, err := s.Read(p.ReadRequest{ID: fixture.ID, Urn: urn, Properties: state, Inputs: oldInputs})
	require.NoError(t, err)
	assert.Equal(t, "run", read.ID)
	assert.Equal(t, state.Get("output").AsString(), read.Properties.Get("output").AsString())

	// Nothing listens on the stored model URL: a delete that ran the agent would fail.
	require.NoError(t, s.Delete(p.DeleteRequest{ID: fixture.ID, Urn: urn, Properties: state, OldInputs: oldInputs}))
}

// fakeClaudeOnPath puts a copy of the fake Claude Code CLI (../agentrun/testdata/fake-claude)
// first on PATH, with only the system directories after it, and returns its log file.
func fakeClaudeOnPath(t *testing.T) string {
	t.Helper()
	src := filepath.Join("..", "agentrun", "testdata", "fake-claude")
	dir := t.TempDir()
	entries, err := os.ReadDir(src)
	require.NoError(t, err)
	for _, e := range entries {
		if e.Name() != "claude" && filepath.Ext(e.Name()) != ".jsonl" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(src, e.Name()))
		require.NoError(t, err)
		mode := os.FileMode(0o644)
		if e.Name() == "claude" {
			mode = 0o755
		}
		require.NoError(t, os.WriteFile(filepath.Join(dir, e.Name()), b, mode))
	}
	t.Setenv("PATH", strings.Join([]string{dir, "/usr/bin", "/bin"}, string(os.PathListSeparator)))
	claudeLog := filepath.Join(t.TempDir(), "claude.log")
	t.Setenv("FAKE_CLAUDE_LOG", claudeLog)
	for _, k := range []string{"FAKE_CLAUDE_MODE", "CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN",
		"ANTHROPIC_BASE_URL"} {
		t.Setenv(k, "")
	}
	return claudeLog
}

const harnessYAML = `agents:
  root:
    description: Writes notes with Claude Code
    harness:
      type: claude-code
      model: sonnet
`

func TestAgentRunHarness(t *testing.T) {
	resetRunState(t)
	claudeLog := fakeClaudeOnPath(t)
	const token = "sk-ant-oat01-provider-test-token-0123456789"
	s, rec := loggingServer(t)
	dir := t.TempDir()
	in := runProps(map[string]any{"yaml": harnessYAML, "prompt": "Write NOTES.md", "allowHarnessAgents": true,
		"logging": "full", "workingDir": dir, "deletePrompt": "Delete NOTES.md"}).
		Set("env", property.New(property.NewMap(map[string]property.Value{"CLAUDE_CODE_OAUTH_TOKEN": property.New(token)})).
			WithSecret(true))

	checked := runCheck(t, s, in)
	require.Empty(t, checked.Failures, runFailures(checked.Failures))
	assert.Contains(t, strings.Join(rec.messages("warning"), "\n"),
		"coding-harness agents (agents.root.harness: claude-code) run without tool approval", "Check warns (W1)")

	created, err := s.Create(p.CreateRequest{Urn: runURN("notes"), Properties: checked.Inputs})
	require.NoError(t, err)
	state := created.Properties
	assert.Equal(t, "Done: NOTES.md lists the changes.", state.Get("output").AsString())
	assert.False(t, state.Get("output").Secret())
	assert.True(t, state.Get("env").Secret())
	assert.Equal(t, "Write", state.Get("toolCalls").AsArray().Get(0).AsString())
	assert.Equal(t, 15040.0, state.Get("usage").AsMap().Get("inputTokens").AsNumber())
	assert.Equal(t, 0.0123, state.Get("usage").AsMap().Get("cost").AsNumber())
	assert.FileExists(t, filepath.Join(dir, "NOTES.md"))
	logged, err := os.ReadFile(claudeLog)
	require.NoError(t, err, "the fake claude ran")
	assert.Contains(t, string(logged), "oauth_token=set")
	assert.NotContains(t, string(logged), token)
	assert.NotContains(t, rec.all(), token)

	// The delete prompt of a later command (another provider process) runs in workingDir, and
	// announces the harness: no Check runs in a destroy.
	usedDirs.Clear()
	rec.reset()
	require.NoError(t, s.Delete(p.DeleteRequest{ID: created.ID, Urn: runURN("notes"), Properties: state}))
	assert.NoFileExists(t, filepath.Join(dir, "NOTES.md"))
	assert.Contains(t, strings.Join(rec.messages("warning"), "\n"), "run without tool approval")
	logged, err = os.ReadFile(claudeLog)
	require.NoError(t, err)
	assert.Contains(t, string(logged), "stdin=Delete NOTES.md")

	// An answer that repeats the token is secret, and the progress never shows the token.
	t.Setenv("FAKE_CLAUDE_MODE", "token")
	rec.reset()
	created, err = s.Create(p.CreateRequest{Urn: runURN("token"), Properties: checked.Inputs})
	require.NoError(t, err)
	out := created.Properties.Get("output")
	assert.True(t, out.Secret())
	assert.Equal(t, "The token is "+token+".", out.AsString())
	assert.NotContains(t, rec.all(), token)
	assert.Contains(t, rec.all(), "[secret]")
}

// A configuration pulled by `reference` is not checked in a preview: the run enforces the
// opt-in and announces the harness warnings.
func TestAgentRunHarnessReference(t *testing.T) {
	resetRunState(t)
	fakeClaudeOnPath(t)
	reg := httptest.NewServer(registry.New(registry.Logger(log.New(io.Discard, "", 0))))
	defer reg.Close()
	ref := strings.TrimPrefix(reg.URL, "http://") + "/agents/coder:v1"
	doc := []byte(harnessYAML)
	art, err := ociagent.Build(doc, ociagent.Annotations(doc, ociagent.FormatCreated(time.Now()), nil))
	require.NoError(t, err)
	_, err = ociagent.Push(t.Context(), ref, art, ociagent.Options{})
	require.NoError(t, err)

	s, rec := loggingServer(t)
	in := runProps(map[string]any{"reference": ref, "prompt": "Write NOTES.md"})
	checked := runCheck(t, s, in)
	require.Empty(t, checked.Failures, runFailures(checked.Failures))
	_, err = s.Create(p.CreateRequest{Urn: runURN("ref"), Properties: checked.Inputs})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "coding-harness agents (agents.root.harness: claude-code) run only with "+
		"allowHarnessAgents: true")

	rec.reset()
	allowed := runCheck(t, s, runProps(map[string]any{"reference": ref, "prompt": "Write NOTES.md", "allowHarnessAgents": true}))
	created, err := s.Create(p.CreateRequest{Urn: runURN("ref"), Properties: allowed.Inputs})
	require.NoError(t, err)
	assert.Equal(t, "Done: NOTES.md lists the changes.", created.Properties.Get("output").AsString())
	assert.Contains(t, strings.Join(rec.messages("warning"), "\n"), "run without tool approval", "W1 is announced")
}

// The coding-harness rules are AgentRun's own, not docker-agent's validation: they apply with
// the provider's skipValidation too, to a configuration the pinned docker-agent loads.
func TestAgentRunHarnessCheckWithSkipValidation(t *testing.T) {
	s, rec := loggingServer(t)
	require.NoError(t, s.Configure(p.ConfigureRequest{Args: runProps(map[string]any{"skipValidation": true})}))
	checked := runCheck(t, s, runProps(map[string]any{"yaml": harnessYAML, "prompt": "Write NOTES.md"}))
	assert.Contains(t, runFailures(checked.Failures), "run only with allowHarnessAgents: true", "the opt-in fails the preview")

	rec.reset()
	checked = runCheck(t, s, runProps(map[string]any{"yaml": harnessYAML, "prompt": "Write NOTES.md",
		"allowHarnessAgents": true}))
	require.Empty(t, checked.Failures, runFailures(checked.Failures))
	assert.Contains(t, strings.Join(rec.messages("warning"), "\n"),
		"coding-harness agents (agents.root.harness: claude-code) run without tool approval", "W1")

	// A configuration that the pinned docker-agent cannot load is not checked.
	rec.reset()
	checked = runCheck(t, s, runProps(map[string]any{"yaml": harnessYAML + "  newer_field: [", "prompt": "Write NOTES.md"}))
	require.Empty(t, checked.Failures, runFailures(checked.Failures))
	assert.Empty(t, rec.messages("warning"))
}

// A CLAUDE_CODE_OAUTH_TOKEN of the Pulumi CLI that inheritEnvironment: false keeps from the
// claude CLI is a preview warning, also with skipValidation, unless env sets the token or is
// unknown.
func TestAgentRunWithheldTokenWarning(t *testing.T) {
	const token = "sk-ant-oat01-pulumi-cli-token-0123456789"
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", token)
	const w8 = "CLAUDE_CODE_OAUTH_TOKEN is set in the environment of the Pulumi CLI, but inheritEnvironment: false keeps it " +
		"from the claude CLI"
	in := func(env property.Value) property.Map {
		m := runProps(map[string]any{"yaml": harnessYAML, "prompt": "Write NOTES.md", "allowHarnessAgents": true,
			"inheritEnvironment": false})
		if env.IsNull() {
			return m
		}
		return m.Set("env", env)
	}
	for _, skip := range []bool{false, true} {
		s, rec := loggingServer(t)
		if skip {
			require.NoError(t, s.Configure(p.ConfigureRequest{Args: runProps(map[string]any{"skipValidation": true})}))
		}
		warned := func(env property.Value) bool {
			t.Helper()
			rec.reset()
			checked := runCheck(t, s, in(env))
			require.Empty(t, checked.Failures, runFailures(checked.Failures))
			all := strings.Join(rec.messages("warning"), "\n")
			assert.NotContains(t, all, token)
			return strings.Contains(all, w8)
		}
		assert.True(t, warned(property.Value{}), "no env (skipValidation %v)", skip)
		assert.True(t, warned(property.New(property.NewMap(map[string]property.Value{"OTHER": property.New("x")})).
			WithSecret(true)), "env without the token")
		assert.False(t, warned(property.New(property.NewMap(map[string]property.Value{
			"CLAUDE_CODE_OAUTH_TOKEN": property.New(token)})).WithSecret(true)), "env sets the token")
		assert.False(t, warned(property.New(property.Computed)), "an unknown env")
	}

	// An inheritEnvironment that is unknown during the preview is not false: no W8, and the
	// warning about an inherited API key applies as for the default.
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-api03-inherited-provider-test-0123")
	s, rec := loggingServer(t)
	unknown := in(property.Value{}).Set("inheritEnvironment", property.New(property.Computed))
	checked := runCheck(t, s, unknown)
	require.Empty(t, checked.Failures, runFailures(checked.Failures))
	all := strings.Join(rec.messages("warning"), "\n")
	assert.NotContains(t, all, w8)
	assert.NotContains(t, all, "inheritEnvironment: false keeps", "no warning claims it is false")
	assert.Contains(t, all, "claude-code would use ANTHROPIC_API_KEY (inherited from the environment of the Pulumi CLI)")
}

func TestAgentRunCheckWarnings(t *testing.T) {
	s, rec := loggingServer(t)
	srv := fakeopenai.New(nil)
	defer srv.Close()
	checked := runCheck(t, s, runInputs(srv, "", map[string]any{"deletePrompt": "Clean up."}))
	require.Empty(t, checked.Failures, runFailures(checked.Failures))
	assert.Contains(t, strings.Join(rec.messages("warning"), "\n"), "deletePrompt runs with toolApproval deny", "W7")

	rec.reset()
	runCheck(t, s, runInputs(srv, "", map[string]any{"deletePrompt": "Clean up.", "toolApproval": "allow"}))
	assert.Empty(t, rec.messages("warning"))
}

func TestAgentRunSecretUpdatePrompt(t *testing.T) {
	resetRunState(t)
	s := runServer(t)
	srv := fakeopenai.New(nil)
	defer srv.Close()
	in := runInputs(srv, "", nil).Set("updatePrompt", property.New("a confidential follow-up").WithSecret(true))
	checked := runCheck(t, s, in)
	require.Empty(t, checked.Failures, runFailures(checked.Failures))
	preview, err := s.Create(p.CreateRequest{Urn: runURN("secret"), Properties: checked.Inputs, DryRun: true})
	require.NoError(t, err)
	assert.True(t, preview.Properties.Get("output").Secret(), "the answer can repeat updatePrompt")
	created, err := s.Create(p.CreateRequest{Urn: runURN("secret"), Properties: checked.Inputs})
	require.NoError(t, err)
	assert.True(t, created.Properties.Get("output").Secret())
	assert.True(t, created.Properties.Get("updatePrompt").Secret())
	assert.False(t, created.Properties.Get("usage").Secret())
}
