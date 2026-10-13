// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package agentrun

// The ChatGPT login of docker agent setup (chatgpt.go), against the fake Codex backend of
// providers_fake_test.go (a chatgpt model with a base_url). The chatgpt package's token endpoint
// (authBaseURL in pkg/chatgpt/token.go) is unexported, so a real refresh cannot run here: the
// logins are valid for an hour, and the tests replace chatgptRefresh (the parent's refresh) or
// rewrite the run's copy of the login (a refresh during the run) themselves.

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
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
		assert.Contains(t, all, "no cost estimate (billed to the ChatGPT plan)")
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
			"process refreshed it), so the login the run refreshed was not written back")
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
		assert.Contains(t, err.Error(), chatgptRaceHint, "a rejected login gets the concurrent-refresh hint")
		assert.Contains(t, err.Error(), "the run's models: chatgpt/gpt-5.5 (the ChatGPT login of docker agent setup")

		_, req, _, _ = chatgptRun(t, func(wireReq) wireReply { return wireReply{Text: "my token is " + chatgptAccess} })
		res, err := Run(t.Context(), req)
		require.NoError(t, err)
		assert.True(t, res.OutputContainsSecret)
	})

	t.Run("the parent refreshes an expiring login", func(t *testing.T) {
		const fresh = "fake-chatgpt-fresh-access-0123456789"
		f, req, login, _ := chatgptRun(t, providerScript)
		writeChatGPTLoginExpiring(t, login, chatgptAccess, chatgptRefreshToken, time.Now().Add(time.Minute))
		prev := chatgptRefresh
		chatgptRefresh = func(context.Context) (string, error) {
			// As chatgpt.AccessToken: refresh and save.
			writeChatGPTLogin(t, login, fresh, chatgptRefreshToken)
			return fresh, nil
		}
		t.Cleanup(func() { chatgptRefresh = prev })
		_, err := Run(t.Context(), req)
		require.NoError(t, err)
		assert.Equal(t, fresh, f.requests()[0].credential())
	})

	t.Run("a refresh that failed gets the concurrent-refresh hint", func(t *testing.T) {
		_, req, login, _ := chatgptRun(t, providerScript)
		writeChatGPTLoginExpiring(t, login, chatgptAccess, chatgptRefreshToken, time.Now().Add(-time.Hour))
		prev := chatgptRefresh
		chatgptRefresh = func(context.Context) (string, error) {
			return "", errors.New("the ChatGPT session is no longer valid (HTTP 401: refresh_token_reused); sign in again " +
				"with `docker agent setup`")
		}
		t.Cleanup(func() { chatgptRefresh = prev })
		_, err := Run(t.Context(), req)
		require.Error(t, err)
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
