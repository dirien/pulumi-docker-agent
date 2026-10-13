// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package agentrun

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Codex reports no credential source in its JSONL events (unlike Claude Code's apiKeySource).
// AgentRun therefore works out which login a codex CLI uses the way Codex's load_auth does,
// from the CLI's environment and its CODEX_HOME, read-only:
//
//  1. CODEX_API_KEY (bills API usage);
//  2. CODEX_ACCESS_TOKEN (a personal access token or an agent identity of a ChatGPT workspace);
//  3. the workload identity variables (OIDC federation of a ChatGPT workspace);
//  4. the stored login: $CODEX_HOME/auth.json, or the OS keyring when config.toml's
//     cli_auth_credentials_store says so. Its auth_mode (or the mode Codex infers from its
//     fields) tells a ChatGPT login from an API key, an access token or Amazon Bedrock
//     credentials.
//
// From config.toml (the system file, $CODEX_HOME/config.toml and the managed file, in Codex's
// order) it reads model_provider, model and service_tier (also of the active profile), the
// env_key of a custom model provider, forced_login_method and cli_auth_credentials_store. A
// model_provider other than openai decides the login and the billing itself.
//
// With forced_login_method, Codex logs out (it deletes auth.json and the keyring entry) and
// exits when the credential it would use is of the other kind. AgentRun refuses to start a codex
// CLI in that case (codexAuth.conflict).
//
// From auth.json it keeps only the login's kind, and the ChatGPT plan claimed by the ID token
// (decoded without verification, for display): never the e-mail address or the account ID.
// In a run child, and only there, it also keeps the values of the file's tokens and keys,
// which are secrets of the run (redaction).

// codexSystemConfigDir holds Codex's system config.toml and managed_config.toml; tests change it.
var codexSystemConfigDir = "/etc/codex"

// codexConfig holds the keys of Codex's config.toml layers that decide its login and billing.
type codexConfig struct {
	// modelProvider is the active model_provider; "" means Codex's default, openai.
	modelProvider string
	// envKey is the env_key of a custom model provider, and requiresOpenAIAuth its
	// requires_openai_auth: it authenticates with Codex's own login (a ChatGPT login or an API
	// key) instead.
	envKey             string
	requiresOpenAIAuth bool
	// openAIBaseURL is openai_base_url: the built-in provider, with Codex's login, sends its
	// requests there.
	openAIBaseURL string
	// model and serviceTier are the active model and service tier ("" for Codex's defaults).
	model, serviceTier string
	// forcedLoginMethod is "chatgpt", "api" or ""; forcedIn names the file that sets it.
	forcedLoginMethod, forcedIn string
	// forcedWorkspaces is forced_chatgpt_workspace_id (a string or a list); workspacesIn names
	// the file that sets it.
	forcedWorkspaces []string
	workspacesIn     string
	// credentialsStore is cli_auth_credentials_store: file (""), keyring, auto or ephemeral.
	credentialsStore string
	// problems are the files that could not be read or parsed.
	problems []string
}

// codexProjectDenied are the keys Codex ignores in a project's .codex/config.toml (config
// loader, PROJECT_LOCAL_CONFIG_DENYLIST): repository content must not choose where a user's
// credentials go.
var codexProjectDenied = []string{"openai_base_url", "chatgpt_base_url", "model_provider", "model_providers", "notify",
	"profile", "profiles", "otel"}

// readCodexConfig reads the config.toml layers of a CODEX_HOME ("" for the system files only)
// for a codex CLI working in dir ("" when not known), in Codex's order: the system file, the
// user's $CODEX_HOME/config.toml, the .codex/config.toml of the trusted project directories of
// dir (codexProjectLayers), and the managed file.
func readCodexConfig(home, dir string) codexConfig {
	var c codexConfig
	type layer struct {
		path string
		doc  map[string]any
	}
	var layers []layer // lowest precedence first
	read := func(path string) (map[string]any, bool) {
		b, err := os.ReadFile(path) //nolint:gosec // Codex's own configuration files
		if errors.Is(err, fs.ErrNotExist) {
			return nil, false
		}
		var doc map[string]any
		if err == nil {
			_, err = toml.Decode(string(b), &doc)
		}
		if err != nil {
			// The error can quote the file; only its name is kept.
			c.problems = append(c.problems, path)
			return nil, false
		}
		return doc, true
	}
	paths := []string{filepath.Join(codexSystemConfigDir, "config.toml")}
	if home != "" {
		paths = append(paths, filepath.Join(home, "config.toml"))
	}
	for _, path := range paths {
		if doc, ok := read(path); ok {
			layers = append(layers, layer{path, doc})
		}
	}
	managed := filepath.Join(codexSystemConfigDir, "managed_config.toml")
	managedDoc, hasManaged := read(managed)
	// Trust comes from the layers below the project's, and the managed file.
	trust := slices.Clone(layers)
	if hasManaged {
		trust = append(trust, layer{managed, managedDoc})
	}
	trusted := func(path string) bool {
		for i := len(trust) - 1; i >= 0; i-- {
			projects, _ := trust[i].doc["projects"].(map[string]any)
			if p, ok := projects[path].(map[string]any); ok {
				level, _ := p["trust_level"].(string)
				return level == "trusted"
			}
		}
		return false
	}
	for _, path := range codexProjectLayers(dir, home, trusted) {
		if doc, ok := read(path); ok {
			for _, k := range codexProjectDenied {
				delete(doc, k)
			}
			layers = append(layers, layer{path, doc})
		}
	}
	if hasManaged {
		layers = append(layers, layer{managed, managedDoc})
	}
	// get returns the value of a top-level key in the highest layer that sets it.
	get := func(key string) (string, string) {
		for i := len(layers) - 1; i >= 0; i-- {
			if v, ok := layers[i].doc[key].(string); ok {
				return v, layers[i].path
			}
		}
		return "", ""
	}
	c.modelProvider, _ = get("model_provider")
	c.model, _ = get("model")
	c.serviceTier, _ = get("service_tier")
	c.openAIBaseURL, _ = get("openai_base_url")
	c.forcedLoginMethod, c.forcedIn = get("forced_login_method")
	c.credentialsStore, _ = get("cli_auth_credentials_store")
	for i := len(layers) - 1; i >= 0; i-- {
		switch v := layers[i].doc["forced_chatgpt_workspace_id"].(type) {
		case string:
			c.forcedWorkspaces = []string{v}
		case []any:
			for _, x := range v {
				if id, ok := x.(string); ok {
					c.forcedWorkspaces = append(c.forcedWorkspaces, id)
				}
			}
		default:
			continue
		}
		c.forcedWorkspaces = slices.DeleteFunc(c.forcedWorkspaces, func(id string) bool { return strings.TrimSpace(id) == "" })
		c.workspacesIn = layers[i].path
		break
	}
	// The active profile's values win over the top-level ones.
	if profile, _ := get("profile"); profile != "" {
		for i := len(layers) - 1; i >= 0; i-- {
			profiles, _ := layers[i].doc["profiles"].(map[string]any)
			p, ok := profiles[profile].(map[string]any)
			if !ok {
				continue
			}
			for key, dst := range map[string]*string{"model_provider": &c.modelProvider, "model": &c.model,
				"service_tier": &c.serviceTier} {
				if v, ok := p[key].(string); ok {
					*dst = v
				}
			}
			break
		}
	}
	if c.modelProvider != "" && c.modelProvider != "openai" {
		for i := len(layers) - 1; i >= 0; i-- {
			providers, _ := layers[i].doc["model_providers"].(map[string]any)
			if p, ok := providers[c.modelProvider].(map[string]any); ok {
				c.envKey, _ = p["env_key"].(string)
				c.requiresOpenAIAuth, _ = p["requires_openai_auth"].(bool)
				break
			}
		}
	}
	return c
}

// codexProjectLayers returns the .codex/config.toml files Codex reads as project layers for a
// codex CLI working in dir, lowest precedence first: those of dir and its parents up to the
// project root (the nearest directory with a .git, else dir), as far as trusted reports the
// directory, or the project root, as a trusted project of config.toml ([projects."<path>"]
// trust_level = "trusted"). An untrusted project's files are not loaded, as Codex does.
func codexProjectLayers(dir, home string, trusted func(path string) bool) []string {
	if dir == "" || !filepath.IsAbs(dir) {
		return nil
	}
	dir = filepath.Clean(dir)
	root := dir
	for d := dir; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			root = d
			break
		}
		if filepath.Dir(d) == d {
			break
		}
	}
	trustedDir := func(d string) bool {
		for _, k := range []string{d, evalSymlinks(d), root, evalSymlinks(root)} {
			if trusted(k) {
				return true
			}
		}
		return false
	}
	var dirs []string
	for d := dir; ; d = filepath.Dir(d) {
		dirs = append(dirs, d)
		if d == root || filepath.Dir(d) == d {
			break
		}
	}
	slices.Reverse(dirs)
	var out []string
	for _, d := range dirs {
		// CODEX_HOME itself (~/.codex of a working directory in the home directory) is no project.
		dot := filepath.Join(d, ".codex")
		if home != "" && (dot == filepath.Clean(home) || evalSymlinks(dot) == evalSymlinks(home)) {
			continue
		}
		if trustedDir(d) {
			out = append(out, filepath.Join(dot, "config.toml"))
		}
	}
	return out
}

// customProvider reports whether config.toml selects a model provider other than openai.
func (c codexConfig) customProvider() bool {
	return c.modelProvider != "" && c.modelProvider != "openai"
}

// codexAuthFile is what AgentRun keeps of a $CODEX_HOME/auth.json.
type codexAuthFile struct {
	exists bool
	// invalid: the file could not be read or is not the JSON Codex writes.
	invalid bool
	// mode is auth_mode, or the mode Codex infers from the fields that are set.
	mode string
	// plan is the ChatGPT plan the ID token claims ("plus", "pro", ...), "" when unknown.
	plan string
	// secrets are the values of the file's tokens and keys (only when asked for).
	secrets []string
	// stamp tells whether the file changed since it was read.
	stamp fileStamp
}

// fileStamp is a file's modification time and size.
type fileStamp struct {
	mod  time.Time
	size int64
}

func stampOf(path string) fileStamp {
	fi, err := os.Stat(path)
	if err != nil {
		return fileStamp{}
	}
	return fileStamp{fi.ModTime(), fi.Size()}
}

// authReadRetry is how long readCodexAuthFile waits before it reads a file again that it could
// not parse: Codex rewrites auth.json in place (truncate, then write), so a read can see a part.
var authReadRetry = 50 * time.Millisecond

// readCodexAuthFile reads $CODEX_HOME/auth.json. withSecrets keeps the values of its tokens.
func readCodexAuthFile(home string, withSecrets bool) codexAuthFile {
	path := filepath.Join(home, "auth.json")
	var f codexAuthFile
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			time.Sleep(authReadRetry)
		}
		f = codexAuthFile{stamp: stampOf(path)}
		b, err := os.ReadFile(path)
		if errors.Is(err, fs.ErrNotExist) {
			return f
		}
		f.exists = true
		var doc map[string]json.RawMessage
		if err != nil || json.Unmarshal(b, &doc) != nil || doc == nil {
			f.invalid = true
			continue
		}
		f.mode = codexAuthMode(doc)
		f.plan = codexPlan(doc)
		if withSecrets {
			var v any
			if json.Unmarshal(b, &v) == nil {
				f.secrets = authSecrets(v, "")
			}
		}
		return f
	}
	return f
}

// codexAuthMode returns auth_mode, or the mode Codex infers when it is missing (resolved_mode):
// a personal access token, Bedrock credentials or an API key, else a ChatGPT login.
func codexAuthMode(doc map[string]json.RawMessage) string {
	var mode string
	if json.Unmarshal(doc["auth_mode"], &mode) == nil && mode != "" {
		return mode
	}
	present := func(key string) bool {
		raw, ok := doc[key]
		return ok && strings.TrimSpace(string(raw)) != "null"
	}
	switch {
	case present("personal_access_token"):
		return "personalAccessToken"
	case present("bedrock_api_key"):
		return "bedrockApiKey"
	case present("bedrock_access_keys"):
		return "bedrockAccessKeys"
	case present("OPENAI_API_KEY"):
		return "apikey"
	}
	return "chatgpt"
}

// codexPlan returns the chatgpt_plan_type claim of the ID token of a ChatGPT login.
func codexPlan(doc map[string]json.RawMessage) string {
	var tokens struct {
		IDToken string `json:"id_token"`
	}
	if json.Unmarshal(doc["tokens"], &tokens) != nil {
		return ""
	}
	parts := strings.Split(tokens.IDToken, ".")
	if len(parts) != 3 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return ""
	}
	var claims struct {
		Auth struct {
			Plan any `json:"chatgpt_plan_type"`
		} `json:"https://api.openai.com/auth"`
	}
	if json.Unmarshal(payload, &claims) != nil {
		return ""
	}
	plan, _ := claims.Auth.Plan.(string)
	// The claim is display information: only a plain plan name is shown.
	if len(plan) > 40 || strings.ContainsFunc(plan, func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' && r != '-'
	}) {
		return ""
	}
	return plan
}

// authSecrets returns the string values of auth.json that are credentials: everything but its
// mode, its timestamps and IDs, and short values.
func authSecrets(v any, key string) []string {
	switch v := v.(type) {
	case map[string]any:
		var out []string
		for k, x := range v {
			out = append(out, authSecrets(x, k)...)
		}
		slices.Sort(out)
		return slices.Compact(out)
	case []any:
		var out []string
		for _, x := range v {
			out = append(out, authSecrets(x, key)...)
		}
		return out
	case string:
		switch key {
		case "auth_mode", "last_refresh", "account_id", "chatgpt_account_id", "chatgpt_user_id", "user_id", "region":
			return nil
		}
		if len(strings.TrimSpace(v)) < minInheritedSecret {
			return nil
		}
		return []string{v}
	}
	return nil
}

// codexAuth is the login a codex CLI uses, as AgentRun infers it.
type codexAuth struct {
	home codexHome
	cfg  codexConfig
	// source names the credential for the info line, for example "the ChatGPT login of this
	// machine (/home/u/.codex/auth.json)".
	source string
	// plan is the ChatGPT plan of a ChatGPT login, "" when unknown.
	plan string
	// billing is a preview warning about a credential that bills something other than a
	// ChatGPT plan; billed is what the run reports afterwards. Both are "" for a ChatGPT login.
	billing, billed string
	// conflict is set when config.toml's forced_login_method would make Codex delete the stored
	// login (a problem: the CLI does not start).
	conflict string
	// missing reports that no login was found (no auth.json, and no keyring store).
	missing bool
	// authPath is $CODEX_HOME/auth.json; secrets are its token values (in a run only).
	authPath string
	secrets  []string
	stamp    fileStamp
}

// codexAuthOf infers the login of a codex CLI with the environment e. withSecrets keeps the
// token values of auth.json (in a run child only).
func codexAuthOf(e codexEnvironment, withSecrets bool) codexAuth {
	a := codexAuth{home: e.home()}
	if !a.home.unknown && a.home.dir != "" {
		a.cfg = readCodexConfig(a.home.dir, e.dir)
		a.authPath = filepath.Join(a.home.dir, "auth.json")
	} else {
		a.cfg = readCodexConfig("", e.dir)
	}
	forced := strings.ToLower(a.cfg.forcedLoginMethod)
	conflict := func(what, kind, remedy string) {
		if forced == "" || forced == kind {
			return
		}
		a.conflict = fmt.Sprintf("codex would delete the stored login of its CODEX_HOME (%s): %s sets "+
			"forced_login_method = %q, but codex would use %s, so it logs out (removing auth.json and the keyring "+
			"entry) and exits; %s", a.home.dir, a.cfg.forcedIn, a.cfg.forcedLoginMethod, what, remedy)
	}
	dedicated := ", or give AgentRun a CODEX_HOME of its own (env: {CODEX_HOME: ...}) with its own codex login"

	var stored bool
	switch {
	case e.set(codexAPIKeyVar):
		a.source = codexAPIKeyVar + " (" + e.origin(codexAPIKeyVar) + ")"
		a.billing = fmt.Sprintf("codex would use CODEX_API_KEY (%s) instead of a ChatGPT login and bill API usage to it "+
			"(OPENAI_ORGANIZATION and OPENAI_PROJECT, when set, select the billed organization and project); %s",
			e.origin(codexAPIKeyVar), e.remedy(codexAPIKeyVar))
		a.billed = "codex authenticated with CODEX_API_KEY, which bills API usage, not a ChatGPT plan"
		conflict("CODEX_API_KEY ("+e.origin(codexAPIKeyVar)+")", "api", e.remedy(codexAPIKeyVar)+dedicated)
	case e.set(codexAccessTokenVar):
		a.source = codexAccessTokenVar + " (" + e.origin(codexAccessTokenVar) + ", a ChatGPT workspace)"
		conflict("CODEX_ACCESS_TOKEN ("+e.origin(codexAccessTokenVar)+")", "chatgpt",
			e.remedy(codexAccessTokenVar)+dedicated)
		// Codex checks the token's workspace against forced_chatgpt_workspace_id and logs out when
		// it is another one, or none; the account of a token is not something AgentRun reads.
		if a.conflict == "" && len(a.cfg.forcedWorkspaces) > 0 {
			a.conflict = fmt.Sprintf("codex would delete the stored login of its CODEX_HOME (%s): %s sets "+
				"forced_chatgpt_workspace_id, and codex would use CODEX_ACCESS_TOKEN (%s), whose workspace AgentRun cannot "+
				"check: when the token belongs to another workspace, or names none, codex logs out (removing auth.json and "+
				"the keyring entry) and exits; %s", a.home.dir, a.cfg.workspacesIn, e.origin(codexAccessTokenVar),
				e.remedy(codexAccessTokenVar)+dedicated)
		}
	case slices.ContainsFunc(codexWorkloadIdentityVars, func(k string) bool { _, ok := e.vars[k]; return ok }):
		var k string
		for _, k = range codexWorkloadIdentityVars {
			if _, ok := e.vars[k]; ok {
				break
			}
		}
		// Codex selects it when the variable is set at all, also to an empty value.
		remedy := "set inheritEnvironment: false to use the Codex login of the machine"
		if e.fromEnv[k] {
			remedy = "remove it from env to use the Codex login of the machine"
		}
		a.source = "workload identity (" + k + ", " + e.origin(k) + ", a ChatGPT workspace)"
		a.billing = fmt.Sprintf("%s (%s) selects Codex's workload identity login (a ChatGPT workspace) instead of the "+
			"Codex login of this machine; %s", k, e.origin(k), remedy)
		// codex exec skips its login restrictions for workload identity, so nothing is deleted:
		// it refuses to start instead.
		if forced != "" && forced != "chatgpt" {
			a.conflict = fmt.Sprintf("codex would refuse to start: %s sets forced_login_method = %q, but workload "+
				"identity (%s) needs a login policy that permits ChatGPT authentication; %s", a.cfg.forcedIn,
				a.cfg.forcedLoginMethod, k, remedy+dedicated)
		}
	default:
		stored = true
	}

	if stored {
		a.storedLogin(withSecrets, conflict)
	} else if withSecrets && a.authPath != "" {
		// The file's tokens are secrets whichever login the CLI uses.
		f := readCodexAuthFile(a.home.dir, true)
		a.secrets, a.stamp = f.secrets, f.stamp
	}

	switch {
	case a.cfg.customProvider() && a.cfg.requiresOpenAIAuth:
		// The provider authenticates with Codex's own login, which decides the billing as for
		// the built-in provider.
		a.source += ", through model_provider " + a.cfg.modelProvider + " of config.toml"
	case !a.cfg.customProvider() && a.cfg.openAIBaseURL != "":
		// The built-in provider sends Codex's login to another endpoint (a gateway).
		a.source += ", through openai_base_url " + hostOf(a.cfg.openAIBaseURL) + " of config.toml"
	case a.cfg.customProvider():
		// The provider decides the login and the billing; no plan is claimed.
		via := ""
		if a.cfg.envKey != "" {
			via = ", with the key in " + a.cfg.envKey
		}
		a.source = fmt.Sprintf("model_provider %s of config.toml%s", a.cfg.modelProvider, via)
		a.plan = ""
		a.billing = fmt.Sprintf("codex uses model_provider %q of its config.toml (%s)%s, which decides its login and "+
			"billing instead of a ChatGPT plan", a.cfg.modelProvider, a.home.dir, via)
		a.billed = fmt.Sprintf("codex used model_provider %s of its config.toml, which bills that provider, not a "+
			"ChatGPT plan", a.cfg.modelProvider)
		a.missing = false
	}
	return a
}

// storedLogin infers the stored login: auth.json, or the keyring.
func (a *codexAuth) storedLogin(withSecrets bool, conflict func(what, kind, remedy string)) {
	store := strings.ToLower(a.cfg.credentialsStore)
	relogin := "log in again with the method config.toml requires (codex login, or codex login --with-api-key), or " +
		"remove forced_login_method"
	if a.home.unknown || a.home.dir == "" {
		a.source = "the Codex login of its CODEX_HOME"
		return
	}
	if store == "ephemeral" {
		a.source = "no stored Codex login (cli_auth_credentials_store = \"ephemeral\")"
		a.missing = true
		return
	}
	f := readCodexAuthFile(a.home.dir, withSecrets)
	a.secrets, a.stamp = f.secrets, f.stamp
	switch {
	case !f.exists && (store == "keyring" || store == "auto"):
		a.source = fmt.Sprintf("the Codex login in the OS keyring (cli_auth_credentials_store = %q)", store)
	case !f.exists:
		a.source = "no Codex login (" + a.authPath + " does not exist)"
		a.missing = true
	case f.invalid:
		a.source = "the Codex login of this machine (" + a.authPath + " could not be read)"
	default:
		switch f.mode {
		case "chatgpt", "chatgptAuthTokens":
			a.source = "the ChatGPT login of this machine (" + a.authPath
			if f.plan != "" {
				a.source += ", plan " + f.plan
			}
			a.source += ")"
			a.plan = f.plan
			conflict("the ChatGPT login of "+a.authPath, "chatgpt", relogin)
		case "apikey":
			a.source = "an API key stored by codex login (" + a.authPath + ")"
			a.billing = "codex is logged in with an API key (" + a.authPath + "), which bills API usage, not a ChatGPT " +
				"plan; run codex login to use your ChatGPT plan"
			a.billed = "codex authenticated with an API key stored by codex login, which bills API usage, not a ChatGPT plan"
			conflict("the API key of "+a.authPath, "api", relogin)
		case "personalAccessToken", "agentIdentity", "headers":
			a.source = "a Codex access token stored by codex login (" + a.authPath + ", a ChatGPT workspace)"
			conflict("the access token of "+a.authPath, "chatgpt", relogin)
		case "bedrockApiKey", "bedrockAccessKeys":
			a.source = "Amazon Bedrock credentials stored by codex login (" + a.authPath + ")"
			a.billing = "codex is logged in with Amazon Bedrock credentials (" + a.authPath + "), which bill usage to " +
				"that AWS account, not a ChatGPT plan"
			a.billed = "codex authenticated with Amazon Bedrock credentials, which bill usage to that AWS account, not a " +
				"ChatGPT plan"
			conflict("the Amazon Bedrock credentials of "+a.authPath, "api", relogin)
		default:
			a.source = "the Codex login of this machine (" + a.authPath + ", auth_mode " + f.mode + ")"
		}
	}
}

// refreshSecrets reads auth.json again when it changed since it was read last (Codex rotates
// the tokens of a ChatGPT login), and returns the token values that are new.
func (a *codexAuth) refreshSecrets() []string {
	if a.authPath == "" {
		return nil
	}
	if s := stampOf(a.authPath); s == a.stamp {
		return nil
	}
	f := readCodexAuthFile(a.home.dir, true)
	a.stamp = f.stamp
	var added []string
	for _, s := range f.secrets {
		if !slices.Contains(a.secrets, s) {
			added = append(added, s)
			a.secrets = append(a.secrets, s)
		}
	}
	return added
}
