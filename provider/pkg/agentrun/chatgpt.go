// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package agentrun

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/docker-agent/pkg/atomicfile"
	"github.com/docker/docker-agent/pkg/chatgpt"
	"github.com/docker/docker-agent/pkg/config"
	"github.com/docker/docker-agent/pkg/environment"
	"github.com/docker/docker-agent/pkg/paths"
)

// A ChatGPT subscription (`provider: chatgpt`) authenticates with the login `docker agent setup`
// stores in docker-agent's config directory (~/.config/cagent/chatgpt-auth.json), which a run's
// private config directory (paths.SetRoot) hides. For a configuration that names a chatgpt
// model (also as a fallback, routing target or first_available candidate; model auto does not
// count, see U#2) and whose environment does not set the model's token variable, the parent of
// the run child:
//
//  1. under an advisory lock (chatgpt-auth.json.lock next to the login; it covers AgentRun
//     processes, not the docker agent CLI), lets docker-agent refresh the login when its access
//     token is about to expire (chatgpt.AccessToken, which saves it), and copies the login into
//     the run's config directory (0600);
//  2. adds the login's tokens to the run's redaction set;
//  3. after the run, writes the copy back to the real login, atomically and under the lock,
//     only when the run refreshed it (the copy changed) and the real login did not change
//     meanwhile (compare and swap). Otherwise the real login is kept, with a debug message.
//
// The run child adds docker-agent's ChatGPT login source to the end of its environment chain
// (chatgptEnv), which reads, and refreshes, the copy. chatgpt.SetCredentialsPathForTests is
// never called outside tests (it panics there).

// chatgptAuthFile is the name of the login file in docker-agent's config directory (chatgpt.go).
const chatgptAuthFile = "chatgpt-auth.json"

// chatgptLoginPath returns the ChatGPT login of docker agent setup on this machine. A variable,
// so that tests can point it at a fake login (under go test, the chatgpt package reads only the
// file chatgpt.SetCredentialsPathForTests names).
var chatgptLoginPath = func() string { return filepath.Join(paths.GetConfigDir(), chatgptAuthFile) }

// chatgptRefresh refreshes the login at chatgptLoginPath when its access token is about to
// expire, and saves it. A variable for tests: the token endpoint of the chatgpt package cannot
// be redirected.
var chatgptRefresh = chatgpt.AccessToken

// chatgptLockTimeout bounds the wait for the login's lock, which another AgentRun holds while
// it copies or writes back the login.
var chatgptLockTimeout = 30 * time.Second

// chatgptLogin is the ChatGPT login of one run.
type chatgptLogin struct {
	real, copy string
	// orig is the real login as it was copied, nil when it was not copied.
	orig []byte
	// err is why the login could not be prepared (a failed refresh, a lock that was not freed).
	err error
}

// prepareChatGPTLogin copies the ChatGPT login of this machine into the run's state root when
// the configuration names a chatgpt model whose token variable the run's environment does not
// set. It returns nil when the run does not use the login, and the login's tokens, for the
// redaction set.
func prepareChatGPTLogin(ctx context.Context, req Request, stateRoot string) (*chatgptLogin, []string) {
	cfg, err := config.Load(ctx, config.NewBytesSource(orDefault(req.Name, "agent")+".yaml", req.YAML))
	if err != nil {
		return nil, nil
	}
	v := envView{env: req.Env, inherit: req.InheritEnvironment, provider: providerEnvMap()}
	needed := false
	for _, u := range configModels(cfg) {
		if u.isChatGPT() && !v.set(cmpOrToken(u)) {
			needed = true
		}
	}
	if !needed {
		return nil, nil
	}
	l := &chatgptLogin{real: chatgptLoginPath(), copy: filepath.Join(stateRoot, "config", chatgptAuthFile)}
	if _, err := os.Stat(l.real); err != nil {
		return l, nil // not signed in: docker-agent says so
	}
	unlock, err := lockFile(ctx, l.real+".lock", chatgptLockTimeout)
	if err != nil {
		l.err = fmt.Errorf("locking the ChatGPT login: %w", err)
		return l, nil
	}
	defer unlock()
	if _, err := chatgptRefresh(ctx); err != nil && !errors.Is(err, chatgpt.ErrNotLoggedIn) {
		l.err = err
	}
	data, err := os.ReadFile(l.real)
	if err != nil {
		l.err = errors.Join(l.err, fmt.Errorf("reading the ChatGPT login: %w", err))
		return l, nil
	}
	secrets := loginTokens(data)
	if l.err != nil {
		return l, secrets // an expired login that could not be refreshed is not used
	}
	if err := os.MkdirAll(filepath.Dir(l.copy), 0o700); err != nil {
		l.err = fmt.Errorf("copying the ChatGPT login: %w", err)
		return l, secrets
	}
	if err := os.WriteFile(l.copy, data, 0o600); err != nil { //nolint:gosec // the run's own state root
		l.err = fmt.Errorf("copying the ChatGPT login: %w", err)
		return l, secrets
	}
	l.orig = data
	return l, secrets
}

// loginTokens returns the tokens of a login file.
func loginTokens(data []byte) []string {
	var c chatgpt.Credentials
	if json.Unmarshal(data, &c) != nil {
		return nil
	}
	var out []string
	for _, t := range []string{c.AccessToken, c.RefreshToken, c.IDToken} {
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

// finish writes a login the run refreshed back to the real login, when the real login did not
// change during the run, and returns the copy's tokens (a refreshed access token), for the
// redaction set. debug receives what happened.
func (l *chatgptLogin) finish(ctx context.Context, debug func(string)) []string {
	if l == nil || l.orig == nil {
		return nil
	}
	after, err := os.ReadFile(l.copy)
	if err != nil || bytes.Equal(after, l.orig) {
		return nil
	}
	secrets := loginTokens(after)
	if len(secrets) == 0 {
		debug("the ChatGPT login the run refreshed is not valid, so it was not written back")
		return nil
	}
	unlock, err := lockFile(ctx, l.real+".lock", chatgptLockTimeout)
	if err != nil {
		debug("the ChatGPT login the run refreshed was not written back: " + err.Error())
		return secrets
	}
	defer unlock()
	now, err := os.ReadFile(l.real)
	switch {
	case err != nil:
		debug("the ChatGPT login the run refreshed was not written back: " + err.Error())
	case !bytes.Equal(now, l.orig):
		debug("the ChatGPT login of docker agent setup changed during the run (another docker agent process refreshed " +
			"it), so the login the run refreshed was not written back")
	default:
		if err := atomicfile.Write(l.real, bytes.NewReader(after), 0o600); err != nil {
			debug("the ChatGPT login the run refreshed was not written back: " + err.Error())
		} else {
			debug("wrote the ChatGPT login the run refreshed back to " + l.real)
		}
	}
	return secrets
}

// chatgptRaceHint explains a ChatGPT login that was rejected. Refresh tokens are used once: when
// two processes refresh the login at the same time, the one that comes second fails.
const chatgptRaceHint = "the ChatGPT login of docker agent setup was rejected; when another docker agent process (the " +
	"docker agent CLI, or another Pulumi provider) refreshed the login at the same time, the other refresh fails: run " +
	"the command again, and if it fails again, sign in again with `docker agent setup`"

// errorHint returns a hint for the error of a run that used the login, or "".
func (l *chatgptLogin) errorHint(runErr string) string {
	if l == nil {
		return ""
	}
	var parts []string
	if l.err != nil {
		parts = append(parts, "the ChatGPT login of docker agent setup could not be used: "+l.err.Error())
	}
	if isChatGPTAuthFailure(runErr) || l.err != nil && isChatGPTAuthFailure(l.err.Error()) {
		parts = append(parts, chatgptRaceHint)
	}
	return strings.Join(parts, "; ")
}

// isChatGPTAuthFailure recognizes a rejected ChatGPT login: a refresh the token endpoint refused
// (chatgpt/token.go), and an access token the Codex backend refused.
func isChatGPTAuthFailure(text string) bool {
	return strings.Contains(text, "ChatGPT session is no longer valid") || strings.Contains(text, "refresh token") ||
		isAuthFailure(text)
}

// chatgptEnv adds docker-agent's ChatGPT login source to a run's environment chain when the
// configuration names a chatgpt model: it serves CHATGPT_OAUTH_TOKEN from the login in the run's
// config directory (the copy the parent made), after env and the inherited environment.
func chatgptEnv(env environment.Provider, namesChatGPT bool) environment.Provider {
	if !namesChatGPT {
		return env
	}
	return environment.NewMultiProvider(env, environment.NewChatGPTLoginProvider())
}

// useTestLogin points the chatgpt package of a run child of a test binary at the login copy in
// the run's state root: under go test it reads no login without chatgpt.SetCredentialsPathForTests.
func useTestLogin(stateRoot string) {
	_ = chatgpt.SetCredentialsPathForTests(filepath.Join(stateRoot, "config", chatgptAuthFile))
}
