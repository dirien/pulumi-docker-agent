// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package agentrun

// The ChatGPT login of docker agent setup (chatgpt.go), against the fake Codex backend of
// providers_fake_test.go (a chatgpt model with a base_url). The chatgpt package's token endpoint
// (authBaseURL in pkg/chatgpt/token.go) is unexported, so a real refresh cannot run here: the
// logins are valid for an hour, and the tests replace the run child's refresh
// (useChatGPTRefresh) or rewrite the run's copy of the login themselves.

import (
	"context"
	"encoding/json"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/docker/docker-agent/pkg/chatgpt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	chatgptAccess       = "fake-chatgpt-access-token-0123456789"
	chatgptRefreshToken = "fake-chatgpt-refresh-token-0123456789"
)

// writeChatGPTLogin writes a login file whose access token expires in an hour.
func writeChatGPTLogin(t *testing.T, path, access, refresh string) {
	t.Helper()
	writeChatGPTLoginExpiring(t, path, access, refresh, time.Now().Add(time.Hour))
}

func writeChatGPTLoginExpiring(t *testing.T, path, access, refresh string, expires time.Time) {
	t.Helper()
	data, err := json.MarshalIndent(chatgpt.Credentials{AccessToken: access, RefreshToken: refresh,
		IDToken: "fake-chatgpt-id-token-0123456789", ExpiresAt: expires}, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, data, 0o600))
}

// loginCopies returns the run's copies of the login (in the state roots under tmp).
func loginCopies(tmp string) []string {
	copies, _ := filepath.Glob(filepath.Join(tmp, stateRootPrefix+"*", "config", chatgptAuthFile))
	return copies
}

// chatgptRun prepares a run of a chatgpt model against a fake Codex backend, with the run's
// temporary directories in a directory of the test, and returns the fake, the request, the
// login's path and that directory.
func chatgptRun(t *testing.T, script wireScript) (*fakeModels, Request, string, string) {
	t.Helper()
	isolation := isolateProviderEnv(t)
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	login := fakeChatGPTLogin(t, chatgptAccess)
	f := newFakeModels(t, script)
	c := providerCase{models: func(u string) string { return baseURLModel("chatgpt", "gpt-5.5", u+"/v1") }}
	return f, Request{Name: "chatgpt", YAML: []byte(c.yaml(f.URL, "")), Prompt: "Say hello", Env: isolation}, login, tmp
}

// collectLog collects a request's debug messages.
func collectLog(req *Request) func() string {
	var mu sync.Mutex
	var msgs []string
	req.Log = func(_ slog.Level, msg string) {
		mu.Lock()
		defer mu.Unlock()
		msgs = append(msgs, msg)
	}
	return func() string {
		mu.Lock()
		defer mu.Unlock()
		return strings.Join(msgs, "\n")
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(b)
}

func TestChatGPTLogin(t *testing.T) {
	t.Run("a chatgpt model uses the login", func(t *testing.T) {
		var copies []string
		var mode os.FileMode
		var tmp string
		f, req, login, dir := chatgptRun(t, func(r wireReq) wireReply {
			copies = loginCopies(tmp)
			if len(copies) == 1 {
				if fi, err := os.Stat(copies[0]); err == nil {
					mode = fi.Mode().Perm()
				}
			}
			return providerScript(r)
		})
		tmp = dir
		before := readFile(t, login)
		var progress []string
		req.Progress = func(p Progress) { progress = append(progress, p.Message) }
		res, err := Run(t.Context(), req)
		require.NoError(t, err)
		assert.Equal(t, "Hello from the fake: Say hello", res.Output)
		require.Len(t, f.requests(), 1)
		assert.Equal(t, chatgptAccess, f.requests()[0].credential(), "the login's access token")
		require.Len(t, copies, 1, "the run reads a copy of the login in its state root")
		assert.Equal(t, os.FileMode(0o600), mode)
		assert.Empty(t, loginCopies(dir), "the copy is removed with the state root")
		assert.Equal(t, before, readFile(t, login), "an unchanged copy is not written back")
		all := strings.Join(progress, "\n")
		assert.Contains(t, all, "models: chatgpt/gpt-5.5 (the ChatGPT login of docker agent setup, billed to the ChatGPT plan)")
		assert.Contains(t, all, "; no per-token cost: billed to the ChatGPT plan")
		assert.NotContains(t, all, chatgptAccess)
	})

	t.Run("a login the run refreshed is written back", func(t *testing.T) {
		const refreshed = "fake-chatgpt-refreshed-access-0123456789"
		var tmp string
		_, req, login, dir := chatgptRun(t, func(r wireReq) wireReply {
			for _, c := range loginCopies(tmp) {
				writeChatGPTLogin(t, c, refreshed, "fake-chatgpt-rotated-refresh-0123456789")
			}
			return wireReply{Text: "token " + refreshed}
		})
		tmp = dir
		logs := collectLog(&req)
		res, err := Run(t.Context(), req)
		require.NoError(t, err)
		var c chatgpt.Credentials
		require.NoError(t, json.Unmarshal([]byte(readFile(t, login)), &c))
		assert.Equal(t, refreshed, c.AccessToken, "the refreshed login replaced the real one")
		assert.Equal(t, "fake-chatgpt-rotated-refresh-0123456789", c.RefreshToken)
		fi, err := os.Stat(login)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm())
		assert.Contains(t, logs(), "wrote the ChatGPT login the run refreshed back to "+login)
		assert.True(t, res.OutputContainsSecret, "the refreshed access token is a secret too")
	})

	t.Run("a login another process changed during the run is kept", func(t *testing.T) {
		var tmp, realLogin string
		_, req, login, dir := chatgptRun(t, func(r wireReq) wireReply {
			for _, c := range loginCopies(tmp) {
				writeChatGPTLogin(t, c, "fake-chatgpt-ours-0123456789", "fake-chatgpt-ours-refresh-0123456789")
			}
			writeChatGPTLogin(t, realLogin, "fake-chatgpt-theirs-0123456789", "fake-chatgpt-theirs-refresh-0123456789")
			return providerScript(r)
		})
		tmp, realLogin = dir, login
		logs := collectLog(&req)
		_, err := Run(t.Context(), req)
		require.NoError(t, err)
		assert.Contains(t, readFile(t, login), "fake-chatgpt-theirs-0123456789", "compare and swap: the other login stays")
		assert.Contains(t, logs(), "the ChatGPT login of docker agent setup changed during the run (another docker agent "+
			"process refreshed it, or signed in again), so the login the run refreshed was not written back")
	})

	t.Run("CHATGPT_OAUTH_TOKEN in env wins", func(t *testing.T) {
		var tmp string
		var copies []string
		f, req, login, dir := chatgptRun(t, func(r wireReq) wireReply {
			copies = loginCopies(tmp)
			return providerScript(r)
		})
		tmp = dir
		req.Env["CHATGPT_OAUTH_TOKEN"] = "fake-env-chatgpt-token-0123456789"
		_, err := Run(t.Context(), req)
		require.NoError(t, err)
		assert.Equal(t, "fake-env-chatgpt-token-0123456789", f.requests()[0].credential())
		assert.Empty(t, copies, "the login is not copied")
		assert.NoFileExists(t, login+".lock", "nor locked")
	})

	t.Run("configurations without a chatgpt model do not use the login", func(t *testing.T) {
		isolation := isolateProviderEnv(t)
		tmp := t.TempDir()
		t.Setenv("TMPDIR", tmp)
		login := fakeChatGPTLogin(t, chatgptAccess)
		var copies []string
		f := newFakeModels(t, func(r wireReq) wireReply {
			copies = loginCopies(tmp)
			return providerScript(r)
		})
		// model auto with only GOOGLE_API_KEY: chatgpt comes before google in auto's order, so
		// a login in the run's environment would change what auto picks (U#2).
		env := map[string]string{"GOOGLE_API_KEY": providerKey, "GOOGLE_GEMINI_BASE_URL": f.URL + "/"}
		for k, v := range isolation {
			env[k] = v
		}
		_, err := Run(t.Context(), Request{Name: "auto", YAML: []byte(providerCase{ref: "auto"}.yaml(f.URL, "")),
			Prompt: "Say hello", Env: env})
		require.NoError(t, err)
		require.Len(t, f.requests(), 1)
		assert.Equal(t, protoGemini, f.requests()[0].Proto, "auto stays with Google")
		assert.Empty(t, copies)
		assert.NoFileExists(t, login+".lock")
	})

	t.Run("tokens are redacted", func(t *testing.T) {
		_, req, _, _ := chatgptRun(t, func(wireReq) wireReply {
			return wireReply{Status: 401, Err: "token " + chatgptAccess + " (refresh " + chatgptRefreshToken + ") was rejected"}
		})
		_, err := Run(t.Context(), req)
		require.Error(t, err)
		assert.NotContains(t, err.Error(), chatgptAccess)
		assert.NotContains(t, err.Error(), chatgptRefreshToken)
		assert.Contains(t, err.Error(), chatgptRaceHint, "a 401 of the Codex backend gets the concurrent-refresh hint")
		assert.Contains(t, err.Error(), "the run's models: chatgpt/gpt-5.5 (the ChatGPT login of docker agent setup")

		_, req, _, _ = chatgptRun(t, func(wireReq) wireReply { return wireReply{Text: "my token is " + chatgptAccess} })
		res, err := Run(t.Context(), req)
		require.NoError(t, err)
		assert.True(t, res.OutputContainsSecret)
	})

	t.Run("a 403 is not a rejected login", func(t *testing.T) {
		_, req, _, _ := chatgptRun(t, func(wireReq) wireReply {
			return wireReply{Status: 403, Err: "workspace policy forbids this model"}
		})
		_, err := Run(t.Context(), req)
		require.Error(t, err)
		assert.NotContains(t, err.Error(), chatgptRaceHint, "only a refused refresh or a 401 of the Codex backend")
	})

	t.Run("an expiring login is refreshed under the lock and written back at once", func(t *testing.T) {
		const fresh, rotated = "fake-chatgpt-fresh-access-0123456789", "fake-chatgpt-rotated-refresh-0123456789"
		var realLogin string
		var during string
		f, req, login, _ := chatgptRun(t, func(r wireReq) wireReply {
			during = readFile(t, realLogin)
			return providerScript(r)
		})
		realLogin = login
		writeChatGPTLoginExpiring(t, login, chatgptAccess, chatgptRefreshToken, time.Now().Add(time.Minute))
		calls := filepath.Join(t.TempDir(), "calls")
		useChatGPTRefresh(t, &chatgptRefreshFake{Access: fresh, Refresh: rotated, Calls: calls})
		logs := collectLog(&req)
		res, err := Run(t.Context(), req)
		require.NoError(t, err)
		assert.Equal(t, fresh, f.requests()[0].credential())
		assert.Contains(t, during, fresh, "the real login has the refreshed token while the run still runs")
		assert.Contains(t, during, rotated)
		assert.Equal(t, "refresh\n", readFile(t, calls), "one refresh")
		assert.Contains(t, logs(), "wrote the ChatGPT login the run refreshed back to "+login)
		assert.Contains(t, readFile(t, login), rotated)
		assert.False(t, res.OutputContainsSecret)
	})

	t.Run("a login another process refreshed is taken before a refresh", func(t *testing.T) {
		const theirs = "fake-chatgpt-theirs-access-0123456789"
		var tmp, realLogin string
		requests := 0
		f, req, login, dir := chatgptRun(t, func(r wireReq) wireReply {
			requests++
			if requests == 1 {
				// The run's copy expires, and another process refreshes the real login.
				for _, c := range loginCopies(tmp) {
					writeChatGPTLoginExpiring(t, c, chatgptAccess, chatgptRefreshToken, time.Now().Add(-time.Minute))
				}
				writeChatGPTLogin(t, realLogin, theirs, "fake-chatgpt-theirs-refresh-0123456789")
			}
			return providerScript(r)
		})
		tmp, realLogin = dir, login
		req.YAML = []byte(string(req.YAML) + fsToolset)
		calls := filepath.Join(t.TempDir(), "calls")
		useChatGPTRefresh(t, &chatgptRefreshFake{Err: "the run must not refresh", Calls: calls})
		_, err := Run(t.Context(), req)
		require.NoError(t, err)
		require.Len(t, f.requests(), 2)
		assert.Equal(t, chatgptAccess, f.requests()[0].credential())
		assert.Equal(t, theirs, f.requests()[1].credential(), "the other process's login, read again before refreshing")
		assert.NoFileExists(t, calls, "no refresh of a rotated-out refresh token")
		assert.Contains(t, readFile(t, login), theirs)
	})

	t.Run("a refresh that failed gets the concurrent-refresh hint", func(t *testing.T) {
		_, req, login, _ := chatgptRun(t, providerScript)
		writeChatGPTLoginExpiring(t, login, chatgptAccess, chatgptRefreshToken, time.Now().Add(-time.Hour))
		calls := filepath.Join(t.TempDir(), "calls")
		useChatGPTRefresh(t, &chatgptRefreshFake{Err: "the ChatGPT session is no longer valid (HTTP 401: " +
			"refresh_token_reused); sign in again with `docker agent setup`", Calls: calls})
		_, err := Run(t.Context(), req)
		require.Error(t, err)
		assert.Equal(t, "refresh\n", readFile(t, calls), "a refused refresh token is not sent again")
		assert.Contains(t, err.Error(), "; the ChatGPT login of docker agent setup could not be used: the ChatGPT session is "+
			"no longer valid (HTTP 401: refresh_token_reused)")
		assert.Contains(t, err.Error(), chatgptRaceHint)
	})

	t.Run("the lock of another AgentRun process", func(t *testing.T) {
		f, req, login, _ := chatgptRun(t, providerScript)
		unlock, err := lockFile(t.Context(), login+".lock", time.Second)
		require.NoError(t, err)
		prev := chatgptLockTimeout
		chatgptLockTimeout = 200 * time.Millisecond
		t.Cleanup(func() { chatgptLockTimeout = prev })
		_, err = Run(t.Context(), req)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "the ChatGPT login of docker agent setup could not be used: locking the ChatGPT login: "+
			"another process held "+login+".lock for more than 200ms")
		assert.Empty(t, f.requests())
		unlock()
		_, err = Run(t.Context(), req)
		require.NoError(t, err)
		assert.Len(t, f.requests(), 1)
	})

	t.Run("a read-only config directory", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root writes to a read-only directory")
		}
		const fresh, rotated = "fake-chatgpt-ro-access-0123456789", "fake-chatgpt-ro-rotated-0123456789"
		f, req, login, tmp := chatgptRun(t, providerScript)
		writeChatGPTLoginExpiring(t, login, chatgptAccess, chatgptRefreshToken, time.Now().Add(time.Minute))
		require.NoError(t, os.Chmod(filepath.Dir(login), 0o500))
		t.Cleanup(func() { _ = os.Chmod(filepath.Dir(login), 0o700) })
		useChatGPTRefresh(t, &chatgptRefreshFake{Access: fresh, Refresh: rotated})
		logs := collectLog(&req)
		res, err := Run(t.Context(), req)
		require.NoError(t, err, "no lock file: the run goes on without the lock")
		assert.Equal(t, fresh, f.requests()[0].credential())
		assert.Contains(t, logs(), "the ChatGPT login of docker agent setup is used without a lock: open "+login+".lock")
		assert.NotContains(t, readFile(t, login), rotated, "the directory is read-only")
		kept, _ := filepath.Glob(filepath.Join(tmp, chatgptAuthFile+".agentrun-*"))
		require.Len(t, kept, 1, "the refreshed login is kept in the temporary directory")
		fi, err := os.Stat(kept[0])
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm())
		assert.Contains(t, readFile(t, kept[0]), rotated)
		require.Len(t, res.Warnings, 1, "%v", res.Warnings)
		assert.Contains(t, res.Warnings[0], "the ChatGPT login the run refreshed could not be written back to "+login)
		assert.Contains(t, res.Warnings[0], "it is kept in "+kept[0]+" (mode 0600); move it into place (mv "+kept[0]+" "+
			login+")")
		assert.NotContains(t, res.Warnings[0], fresh)
	})
}

// TestChatGPTLoginWhenUsed: the login is copied only when the run asks for its token: model auto
// with DOCKER_AGENT_DEFAULT_MODEL selecting a chatgpt model uses it, a first_available candidate
// that is not reached does not touch it, and one whose login cannot be used is reported when the
// run succeeds with another candidate.
func TestChatGPTLoginWhenUsed(t *testing.T) {
	setup := func(t *testing.T) (*fakeModels, string, string, map[string]string, *[]string) {
		t.Helper()
		isolation := isolateProviderEnv(t)
		tmp := t.TempDir()
		t.Setenv("TMPDIR", tmp)
		login := fakeChatGPTLogin(t, chatgptAccess)
		var copies []string
		f := newFakeModels(t, func(r wireReq) wireReply {
			copies = loginCopies(tmp)
			return providerScript(r)
		})
		// A token_key: docker-agent asks the environment for the token of a model with a base_url
		// only then (first_available, the preflight).
		models := "providers:\n  sub:\n    provider: chatgpt\n    base_url: " + f.URL + "/v1\n    token_key: " +
			"CHATGPT_OAUTH_TOKEN\n" +
			"  corp:\n    api_type: openai_chatcompletions\n    base_url: " + f.URL + "/v1\n    token_key: FAKE_PROVIDER_KEY\n"
		return f, login, models, maps.Clone(isolation), &copies
	}

	t.Run("DOCKER_AGENT_DEFAULT_MODEL selects a chatgpt model for auto", func(t *testing.T) {
		f, _, models, env, copies := setup(t)
		env["DOCKER_AGENT_DEFAULT_MODEL"] = "sub/gpt-5.5"
		var progress []string
		res, err := Run(t.Context(), Request{Name: "auto", YAML: []byte(models + agentWith("auto")), Prompt: "Say hello",
			Env: env, Progress: func(p Progress) { progress = append(progress, p.Message) }})
		require.NoError(t, err)
		assert.Equal(t, "Hello from the fake: Say hello", res.Output)
		assert.Equal(t, chatgptAccess, f.requests()[0].credential(), "the login's token")
		assert.Len(t, *copies, 1)
		assert.Contains(t, strings.Join(progress, "\n"), "no per-token cost: billed to the ChatGPT plan")
	})

	t.Run("a first_available candidate that is not reached", func(t *testing.T) {
		f, login, models, env, copies := setup(t)
		env["FAKE_PROVIDER_KEY"] = providerKey
		doc := models + "models:\n  pick:\n    first_available: [corp/my-model, sub/gpt-5.5]\n" + agentWith("pick")
		res, err := Run(t.Context(), Request{Name: "fa", YAML: []byte(doc), Prompt: "Say hello", Env: env})
		require.NoError(t, err)
		assert.Equal(t, providerKey, f.requests()[0].credential())
		assert.Empty(t, *copies, "the login is not copied")
		assert.NoFileExists(t, login+".lock", "nor locked")
		assert.Empty(t, res.Warnings)
	})

	t.Run("a first_available candidate whose login cannot be used", func(t *testing.T) {
		f, login, models, env, _ := setup(t)
		env["FAKE_PROVIDER_KEY"] = providerKey
		unlock, err := lockFile(t.Context(), login+".lock", time.Second)
		require.NoError(t, err)
		defer unlock()
		prev := chatgptLockTimeout
		chatgptLockTimeout = 100 * time.Millisecond
		t.Cleanup(func() { chatgptLockTimeout = prev })
		doc := models + "models:\n  pick:\n    first_available: [sub/gpt-5.5, corp/my-model]\n" + agentWith("pick")
		res, err := Run(t.Context(), Request{Name: "fa", YAML: []byte(doc), Prompt: "Say hello", Env: env})
		require.NoError(t, err)
		assert.Equal(t, providerKey, f.requests()[0].credential(), "the next candidate answered")
		assert.Equal(t, []string{"the ChatGPT login of docker agent setup could not be used: locking the ChatGPT login: " +
			"another process held " + login + ".lock for more than 100ms; the run used models: first_available: " +
			"corp/my-model (FAKE_PROVIDER_KEY from env)"}, res.Warnings)
	})
}

// useChatGPTRefresh replaces the refresh of the ChatGPT login in the run children of a test.
func useChatGPTRefresh(t *testing.T, f *chatgptRefreshFake) {
	t.Helper()
	prev := testFaults.chatgptRefresh
	testFaults.chatgptRefresh = f
	t.Cleanup(func() { testFaults.chatgptRefresh = prev })
}

// TestChatGPTLoginFinish: the parent's write-back after the run (finish) never loses a login the
// run refreshed: it ignores the run's cancellation, and keeps the login next to the real one when
// it cannot write it back.
func TestChatGPTLoginFinish(t *testing.T) {
	setup := func(t *testing.T) (*chatgptLogin, string) {
		t.Helper()
		dir := t.TempDir()
		state := t.TempDir()
		l := &chatgptLogin{real: filepath.Join(dir, chatgptAuthFile), copy: filepath.Join(state, "config", chatgptAuthFile),
			base: filepath.Join(state, chatgptBaseFile)}
		writeChatGPTLogin(t, l.real, chatgptAccess, chatgptRefreshToken)
		require.NoError(t, os.WriteFile(l.base, []byte(loginHash([]byte(readFile(t, l.real)))), 0o600))
		writeChatGPTLogin(t, l.copy, "fake-chatgpt-refreshed-0123456789", "fake-chatgpt-rotated-0123456789")
		return l, dir
	}
	var debug []string
	logf := func(m string) { debug = append(debug, m) }

	t.Run("a canceled run still writes back", func(t *testing.T) {
		l, _ := setup(t)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		secrets, warnings := l.finish(ctx, logf)
		assert.Empty(t, warnings)
		assert.Contains(t, secrets, "fake-chatgpt-rotated-0123456789")
		assert.Contains(t, readFile(t, l.real), "fake-chatgpt-rotated-0123456789")
	})

	t.Run("a lock that is not freed keeps the login next to the real one", func(t *testing.T) {
		l, dir := setup(t)
		unlock, err := lockFile(t.Context(), l.real+".lock", time.Second)
		require.NoError(t, err)
		defer unlock()
		prev := chatgptLockTimeout
		chatgptLockTimeout = 50 * time.Millisecond
		t.Cleanup(func() { chatgptLockTimeout = prev })
		_, warnings := l.finish(t.Context(), logf)
		assert.NotContains(t, readFile(t, l.real), "fake-chatgpt-rotated-0123456789")
		kept, _ := filepath.Glob(filepath.Join(dir, chatgptAuthFile+".agentrun-*"))
		require.Len(t, kept, 1)
		assert.Contains(t, readFile(t, kept[0]), "fake-chatgpt-rotated-0123456789")
		require.Len(t, warnings, 1)
		assert.Equal(t, "the ChatGPT login the run refreshed could not be written back to "+l.real+" (another process held "+
			l.real+".lock for more than 100ms), and the refresh replaced its refresh token: it is kept in "+kept[0]+
			" (mode 0600); move it into place (mv "+kept[0]+" "+l.real+") before docker agent or AgentRun use the login "+
			"again, or sign in again with `docker agent setup`", warnings[0])
	})

	t.Run("an unchanged copy is not written", func(t *testing.T) {
		l, _ := setup(t)
		require.NoError(t, os.WriteFile(l.base, []byte(loginHash([]byte(readFile(t, l.copy)))), 0o600))
		before := readFile(t, l.real)
		_, warnings := l.finish(t.Context(), logf)
		assert.Empty(t, warnings)
		assert.Equal(t, before, readFile(t, l.real))
	})
}

func TestLockFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.lock")
	unlock, err := lockFile(t.Context(), path, time.Second)
	require.NoError(t, err)
	_, err = lockFile(t.Context(), path, 100*time.Millisecond)
	require.Error(t, err, "a second lock waits for the first")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = lockFile(ctx, path, time.Minute)
	require.ErrorIs(t, err, context.Canceled)
	unlock()
	again, err := lockFile(t.Context(), path, 100*time.Millisecond)
	require.NoError(t, err)
	again()
}
