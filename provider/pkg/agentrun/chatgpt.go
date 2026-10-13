// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package agentrun

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker-agent/pkg/atomicfile"
	"github.com/docker/docker-agent/pkg/chatgpt"
	"github.com/docker/docker-agent/pkg/config"
	"github.com/docker/docker-agent/pkg/paths"
)

// A ChatGPT subscription (`provider: chatgpt`) authenticates with the login `docker agent setup`
// stores in docker-agent's config directory (~/.config/cagent/chatgpt-auth.json), which a run's
// private config directory (paths.SetRoot) hides. A run can use it when its configuration names
// a chatgpt model (also as a fallback, routing target or first_available candidate) or
// DOCKER_AGENT_DEFAULT_MODEL selects one for model auto (model auto's own choice does not count,
// see U#2), and its environment has no CHATGPT_OAUTH_TOKEN (an empty one hides the login, as
// docker-agent's environment chain does). Then:
//
//   - the parent of the run child adds the login's tokens to the run's redaction set: it reads
//     the login, without a lock, a refresh or a copy;
//   - the run child adds a ChatGPT login source to the end of its environment chain
//     (chatgptSource). Only when the run asks for the login's token (an unchosen first_available
//     candidate never does), it copies the login into the run's config directory (0600) under an
//     advisory lock (chatgpt-auth.json.lock next to the login; it covers AgentRun processes, not
//     the docker agent CLI). docker-agent refreshes the copy when its access token is about to
//     expire; before that, under the lock, the source takes the real login again when another
//     process refreshed it. A login the run refreshed goes back to the real one at once (compare
//     and swap: only when the real login is still the one the run copied or wrote last), and its
//     tokens join the run's redaction set;
//   - after the run, the parent writes back what the child could not (compare and swap again,
//     ignoring a cancellation, with a longer wait for the lock). If that fails too, the refreshed
//     login is kept next to the real one (chatgpt-auth.json.agentrun-<time>, 0600) with a
//     warning: a refresh rotates the refresh token, so the real login may no longer refresh.
//
// A config directory that is read-only, or a file system without locks, leaves the login
// unlocked (the compare and swap still keeps another process's write), as docker-agent itself
// uses a login it cannot save. chatgpt.SetCredentialsPathForTests is never called outside tests
// (it panics there).

// chatgptAuthFile is the name of the login file in docker-agent's config directory (chatgpt.go).
const chatgptAuthFile = "chatgpt-auth.json"

// chatgptBaseFile, in the run's state root, holds the SHA-256 of the real login the run's copy
// derives from: what the run copied, or wrote back, last.
const chatgptBaseFile = "chatgpt-login.base"

// chatgptLoginPath returns the ChatGPT login of docker agent setup on this machine. A variable,
// so that tests can point it at a fake login (under go test, the chatgpt package reads only the
// file chatgpt.SetCredentialsPathForTests names).
var chatgptLoginPath = func() string { return filepath.Join(paths.GetConfigDir(), chatgptAuthFile) }

// chatgptAccessToken returns the access token of the run's copy of the login, and refreshes and
// saves it when it is about to expire. A variable for tests: the token endpoint of the chatgpt
// package cannot be redirected.
var chatgptAccessToken = chatgpt.AccessToken

// chatgptLockTimeout bounds the wait for the login's lock, which another AgentRun holds while
// it copies, refreshes or writes back the login. The parent's write-back after the run waits
// twice as long.
var chatgptLockTimeout = 30 * time.Second

// --- the parent -------------------------------------------------------------------------------

// chatgptLogin is the ChatGPT login of one run, as the parent sees it.
type chatgptLogin struct {
	// real is the login of docker agent setup; copy is the run's copy; base holds the hash of
	// the real login the copy derives from (chatgptBaseFile).
	real, copy, base string
}

// planChatGPTLogin returns the ChatGPT login a run may use, nil when it uses none, and the
// login's tokens, for the redaction set. It neither locks, refreshes nor copies the login: the
// run child does that when the run asks for its token.
func planChatGPTLogin(ctx context.Context, req Request, stateRoot string) (*chatgptLogin, []string) {
	cfg, err := config.Load(ctx, config.NewBytesSource(orDefault(req.Name, "agent")+".yaml", req.YAML))
	if err != nil {
		return nil, nil
	}
	v := envView{env: req.Env, inherit: req.InheritEnvironment, provider: providerEnvMap()}
	if v.present(chatgpt.TokenEnvVar) ||
		!slices.ContainsFunc(chatgptUses(ctx, cfg, v.asProvider()), modelUse.usesChatGPTLogin) {
		return nil, nil
	}
	l := &chatgptLogin{real: chatgptLoginPath(), copy: filepath.Join(stateRoot, "config", chatgptAuthFile),
		base: filepath.Join(stateRoot, chatgptBaseFile)}
	data, err := os.ReadFile(l.real)
	if err != nil {
		return l, nil // not signed in: docker-agent says so
	}
	return l, loginTokens(data)
}

// finish writes a login the run refreshed back to the real login when the run child could not,
// and the real login is still the one the copy derives from. It returns the copy's tokens (a
// refreshed access token), for the redaction set, and a warning when the refreshed login had to
// be kept elsewhere. debug receives what happened.
func (l *chatgptLogin) finish(ctx context.Context, debug func(string)) (secrets, warnings []string) {
	if l == nil {
		return nil, nil
	}
	after, err := os.ReadFile(l.copy)
	if err != nil {
		return nil, nil // the run did not use the login
	}
	secrets = loginTokens(after)
	base, err := os.ReadFile(l.base)
	if err != nil || loginHash(after) == string(base) {
		return secrets, nil // nothing the real login lacks
	}
	if len(secrets) == 0 {
		debug("the ChatGPT login the run refreshed is not valid, so it was not written back")
		return nil, nil
	}
	// The run is over: neither its cancellation nor another process's long hold of the lock may
	// lose a refresh token the run rotated.
	unlock, err := lockLogin(context.WithoutCancel(ctx), l.real+".lock", 2*chatgptLockTimeout, debug)
	if err != nil {
		return secrets, l.keep(after, err)
	}
	defer unlock()
	now, err := os.ReadFile(l.real)
	switch {
	case err == nil && bytes.Equal(now, after):
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		return secrets, l.keep(after, err)
	case err != nil || loginHash(now) != string(base):
		debug("the ChatGPT login of docker agent setup changed during the run (another docker agent process refreshed " +
			"it, or signed in again), so the login the run refreshed was not written back")
	default:
		if err := atomicfile.Write(l.real, bytes.NewReader(after), 0o600); err != nil {
			return secrets, l.keep(after, err)
		}
		debug("wrote the ChatGPT login the run refreshed back to " + l.real)
	}
	return secrets, nil
}

// keep saves a refreshed login that could not be written back, next to the real login, or in
// the temporary directory when that fails too, and returns the warning that says where.
func (l *chatgptLogin) keep(data []byte, cause error) []string {
	name := filepath.Base(l.real) + ".agentrun-" + time.Now().UTC().Format("20060102T150405.000Z")
	var errs []error
	for _, dir := range []string{filepath.Dir(l.real), os.TempDir()} {
		path := filepath.Join(dir, name)
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) //nolint:gosec // next to the login
		if err == nil {
			_, err = f.Write(data)
			err = errors.Join(err, f.Close())
			if err != nil {
				_ = os.Remove(path)
			}
		}
		if err == nil {
			return []string{fmt.Sprintf("the ChatGPT login the run refreshed could not be written back to %s (%v), and "+
				"the refresh replaced its refresh token: it is kept in %s (mode 0600); move it into place (mv %s %s) before "+
				"docker agent or AgentRun use the login again, or sign in again with `docker agent setup`", l.real, cause,
				path, path, l.real)}
		}
		errs = append(errs, err)
	}
	return []string{fmt.Sprintf("the ChatGPT login the run refreshed could not be written back to %s (%v), nor kept "+
		"next to it (%v): when the next run fails with a ChatGPT session that is no longer valid, sign in again with "+
		"`docker agent setup`", l.real, cause, errors.Join(errs...))}
}

// loginHash identifies a login file's content.
func loginHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
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

// lockLogin takes the login's lock (lockFile). When the lock file cannot be created or locked
// because the directory is read-only or the file system has no locks (lockUnavailable), it goes
// on without the lock: the compare and swap still keeps a login another process wrote.
func lockLogin(ctx context.Context, path string, timeout time.Duration, debug func(string)) (func(), error) {
	unlock, err := lockFile(ctx, path, timeout)
	if err != nil && lockUnavailable(err) {
		debug("the ChatGPT login of docker agent setup is used without a lock: " + err.Error())
		return func() {}, nil
	}
	return unlock, err
}

// --- the run child ----------------------------------------------------------------------------

// errNotSignedIn: the machine has no ChatGPT login; docker-agent says so itself.
var errNotSignedIn = errors.New("not signed in")

// chatgptSource serves CHATGPT_OAUTH_TOKEN from the run's copy of the ChatGPT login, at the end
// of a run child's environment chain (see the top of this file).
type chatgptSource struct {
	// real is the login of docker agent setup; copy is the run's copy; baseFile holds the hash
	// of base (chatgptBaseFile).
	real, copy, baseFile string
	debug                func(string)
	// addSecrets adds the tokens of the logins the run reads to its redaction set.
	addSecrets func(...string)

	mu sync.Mutex
	// base is the real login as the run copied or wrote it last, nil before the first copy.
	base []byte
	// tried is the hash of the copy the run last tried to write back.
	tried string
	// refused is the token endpoint's refusal to refresh base (a session that is no longer
	// valid): base is not refreshed again.
	refused error
	// err is why the login could not be used the last time it was asked for, used reports that
	// it supplied a token.
	err  error
	used bool
}

// newChatGPTSource returns the ChatGPT login source of a run child whose state root is
// stateRoot, for the login at login.
func newChatGPTSource(login, stateRoot string, debug func(string), addSecrets func(...string)) *chatgptSource {
	return &chatgptSource{real: login, copy: filepath.Join(stateRoot, "config", chatgptAuthFile),
		baseFile: filepath.Join(stateRoot, chatgptBaseFile), debug: debug, addSecrets: addSecrets}
}

func (s *chatgptSource) Get(ctx context.Context, name string) (string, bool) {
	if s == nil || name != chatgpt.TokenEnvVar {
		return "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	token, err := s.token(ctx)
	switch {
	case errors.Is(err, errNotSignedIn):
		return "", false
	case err != nil:
		s.err = err
		s.debug("the ChatGPT login of docker agent setup could not supply an access token: " + err.Error())
		return "", false
	}
	// A login that works again (a lock that was freed) is no reason for a hint.
	s.used, s.err = true, nil
	return token, true
}

// token returns the copy's access token. The first time, and before a refresh, it copies the
// real login under the lock when the real login is not what the run copied or wrote last.
func (s *chatgptSource) token(ctx context.Context) (string, error) {
	if s.base != nil {
		if c, err := chatgpt.Load(); err == nil && !c.Expired() {
			// docker-agent may have refreshed the copy itself (its fallback when this source
			// supplies no token).
			s.writeBack(ctx)
			return c.AccessToken, nil
		}
	}
	unlock, err := lockLogin(ctx, s.real+".lock", chatgptLockTimeout, s.debug)
	if err != nil {
		return "", fmt.Errorf("locking the ChatGPT login: %w", err)
	}
	defer unlock()
	data, err := os.ReadFile(s.real)
	switch {
	case errors.Is(err, fs.ErrNotExist) && s.base == nil:
		return "", errNotSignedIn
	case err != nil && s.base == nil:
		return "", fmt.Errorf("reading the ChatGPT login: %w", err)
	case err == nil && !bytes.Equal(data, s.base):
		// The first copy, or another process refreshed the login (or signed in again) since.
		if err := os.MkdirAll(filepath.Dir(s.copy), 0o700); err != nil {
			return "", fmt.Errorf("copying the ChatGPT login: %w", err)
		}
		if err := atomicfile.Write(s.copy, bytes.NewReader(data), 0o600); err != nil {
			return "", fmt.Errorf("copying the ChatGPT login: %w", err)
		}
		s.synced(data)
		s.refused = nil
	}
	if s.refused != nil {
		return "", s.refused
	}
	token, err := chatgptAccessToken(ctx)
	// A refresh rotated the refresh token: the real login gets it at once.
	s.writeBackLocked()
	if err != nil && refusedRefresh(err.Error()) {
		s.refused = err
		// Without a copy docker-agent's own fallback (chatgpt.AccessToken) finds no login instead
		// of sending the refused refresh token again. A copy the run refreshed earlier and could
		// not write back stays, for the parent's write-back.
		if after, rerr := os.ReadFile(s.copy); rerr == nil && bytes.Equal(after, s.base) {
			_ = os.Remove(s.copy)
		}
	}
	return token, err
}

// synced records that the real login holds data, which the copy derives from.
func (s *chatgptSource) synced(data []byte) {
	s.base = data
	s.addSecrets(loginTokens(data)...)
	if err := os.WriteFile(s.baseFile, []byte(loginHash(data)), 0o600); err != nil { //nolint:gosec // the run's own state root
		s.debug("recording the ChatGPT login of the run: " + err.Error())
	}
}

// writeBack writes a copy that changed back to the real login, under the lock.
func (s *chatgptSource) writeBack(ctx context.Context) {
	after, err := os.ReadFile(s.copy)
	if err != nil || bytes.Equal(after, s.base) || loginHash(after) == s.tried {
		return
	}
	unlock, err := lockLogin(ctx, s.real+".lock", chatgptLockTimeout, s.debug)
	if err != nil {
		s.tried = loginHash(after)
		s.debug("the ChatGPT login the run refreshed was not written back yet: " + err.Error())
		return
	}
	defer unlock()
	s.writeBackLocked()
}

// writeBackLocked writes a copy that changed back to the real login (the lock is held), when the
// real login is still the one the copy derives from. A failure is left to the parent's write-back
// after the run.
func (s *chatgptSource) writeBackLocked() {
	after, err := os.ReadFile(s.copy)
	if err != nil || s.base == nil || bytes.Equal(after, s.base) {
		return
	}
	s.tried = loginHash(after)
	tokens := loginTokens(after)
	if len(tokens) == 0 {
		return
	}
	s.addSecrets(tokens...)
	now, err := os.ReadFile(s.real)
	switch {
	case err == nil && bytes.Equal(now, after):
		s.synced(after)
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		s.debug("the ChatGPT login the run refreshed was not written back yet: " + err.Error())
	case err != nil || !bytes.Equal(now, s.base):
		s.debug("the ChatGPT login of docker agent setup changed during the run (another docker agent process refreshed " +
			"it, or signed in again), so the login the run refreshed was not written back")
	default:
		if err := atomicfile.Write(s.real, bytes.NewReader(after), 0o600); err != nil {
			s.debug("the ChatGPT login the run refreshed was not written back yet: " + err.Error())
			return
		}
		s.synced(after)
		s.debug("wrote the ChatGPT login the run refreshed back to " + s.real)
	}
}

// chatgptRaceHint explains a ChatGPT login that was rejected. Refresh tokens are used once: when
// two processes refresh the login at the same time, the one that comes second fails.
const chatgptRaceHint = "the ChatGPT login of docker agent setup was rejected; when another docker agent process (the " +
	"docker agent CLI, or another Pulumi provider) refreshed the login at the same time, the other refresh fails: run " +
	"the command again, and if it fails again, sign in again with `docker agent setup`"

// errorHint returns a hint for the error of a run that could use the login, or "": why the login
// could not be used, and chatgptRaceHint for a rejected login: a refresh the token endpoint
// refused (in runErr or the login's own error), or a 401 in chatgptErr, the errors of the agents
// whose models are chatgpt models (the Codex backend rejected the access token).
func (s *chatgptSource) errorHint(runErr, chatgptErr string) string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var parts []string
	if s.err != nil {
		parts = append(parts, "the ChatGPT login of docker agent setup could not be used: "+s.err.Error())
	}
	if refusedRefresh(runErr) || s.err != nil && refusedRefresh(s.err.Error()) || chatgpt401.MatchString(chatgptErr) {
		parts = append(parts, chatgptRaceHint)
	}
	return strings.Join(parts, "; ")
}

// unusedWarning is the warning of a successful run that asked for the login but could not use
// it (a first_available selector then took another candidate), or "". line is the run's
// credential line.
func (s *chatgptSource) unusedWarning(line string) string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err == nil || s.used {
		return ""
	}
	msg := "the ChatGPT login of docker agent setup could not be used: " + s.err.Error()
	if line != "" {
		msg += "; the run used " + line
	}
	return msg
}

// chatgpt401 matches an HTTP 401 status in an error.
var chatgpt401 = regexp.MustCompile(`\b401\b`)

// refusedRefresh recognizes a refresh of the ChatGPT login that the token endpoint refused
// (chatgpt/token.go: the session is no longer valid, a refresh token that was used already).
func refusedRefresh(text string) bool {
	return strings.Contains(text, "ChatGPT session is no longer valid") || strings.Contains(text, "refresh_token_reused")
}

// useTestLogin points the chatgpt package of a run child of a test binary at the login copy in
// the run's state root: under go test it reads no login without chatgpt.SetCredentialsPathForTests.
func useTestLogin(stateRoot string) {
	_ = chatgpt.SetCredentialsPathForTests(filepath.Join(stateRoot, "config", chatgptAuthFile))
}
