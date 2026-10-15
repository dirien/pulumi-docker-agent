// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"fmt"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"

	"github.com/dirien/pulumi-docker-agent/provider/pkg/usersettings"
	"github.com/dirien/pulumi-docker-agent/provider/pkg/validate"
)

// UserSettings manages docker-agent's user configuration file
// (`~/.config/cagent/config.yaml`).
type UserSettings struct{}

const userSettingsDoc = `Docker Agent's user configuration: the ` + "`~/.config/cagent/config.yaml`" + ` file with
[user settings](https://docs.docker.com/ai/docker-agent/configuration/user-settings/), aliases, global model
providers, the default model, the board, the credential helper and the sandbox allowlist.

` + "`UserSettings`" + ` renders its typed inputs with docker-agent's own configuration types, exactly as
docker-agent writes the file, and validates them with docker-agent's checks during preview: invalid safety modes,
alias names, global providers, hooks, sandbox hosts and TUI options fail before anything is written.

Without ` + "`path`" + `, the resource only renders: use the ` + "`yaml`" + ` output to bake the file into a container
image or a Docker Sandboxes kit. With ` + "`path`" + `, the provider writes the file there on create and update
(atomically, mode 0600, creating missing directories) and reports changes made outside of Pulumi as drift on
` + "`pulumi refresh`" + `. The path is resolved on the machine that runs Pulumi; ` + "`~`" + ` is that user's home
directory and relative paths are relative to the Pulumi project directory. docker-agent reads
` + "`~/.config/cagent/config.yaml`" + ` unless ` + "`--config-dir`" + ` points elsewhere. A symbolic link at the path (for
example from a dotfile manager) is kept: the file it points to is written.

If a file already exists at the path when the resource is created, the preview fails when its content differs
from the rendered file, so existing aliases and settings are never replaced silently. Copy what you want to keep
into the inputs, or set ` + "`overwrite: true`" + `. Either way the existing file is moved to
` + "`<file>.pulumi-backup`" + ` (output ` + "`backupPath`" + `), and deleting the resource moves it back. Deleting the
resource removes a file that the provider created, and leaves a file adopted with ` + "`pulumi import`" + ` in place.
Set the ` + "`retainOnDelete`" + ` resource option to keep the file.

The resource owns the whole file. Changes docker-agent makes itself (` + "`docker agent alias add`" + `, the TUI's
` + "`/settings`" + ` dialog, ` + "`docker agent sandbox allow`" + `) show up as drift and are reverted by the next
` + "`pulumi up --refresh`" + `, so manage those settings here instead.

The file configures the ` + "`docker agent`" + ` CLI. ` + "`AgentRun`" + ` reads none of it: each run uses private docker-agent
directories, so its aliases, global providers, default model, models gateway and credential helper do not apply there.
Declare providers in the agent configuration and pass keys in the ` + "`env`" + ` input of ` + "`AgentRun`" + ` instead.`

var userSettingsImportDoc = `An existing user configuration can be imported with its path (or ` + "`file://<path>`" + `)
as ID. It is read with docker-agent's own types, and ` + "`pulumi import`" + ` generates the typed inputs,
` + "`path`" + ` included. The file is adopted: the import writes nothing and takes no backup, ` + "`overwrite`" + ` is
not needed, and deleting the resource leaves the file in place (with the content Pulumi last wrote) instead of
removing it. The resource ID is the file's absolute path.` +
	importCommand("docker-agent:index:UserSettings", "settings", "~/.config/cagent/config.yaml")

// Annotate describes the resource.
func (r *UserSettings) Annotate(a infer.Annotator) {
	a.Describe(&r, describe(userSettingsDoc, userSettingsExamples, userSettingsImportDoc))
}

// UserSettingsArgs are the inputs of UserSettings. The `yaml` tags name the docker-agent
// keys (see pkg/usersettings.Encode).
type UserSettingsArgs struct {
	Settings         *UserGlobalSettings     `pulumi:"settings,optional"         yaml:"settings"`
	Aliases          map[string]UserAlias    `pulumi:"aliases,optional"          yaml:"aliases"`
	Providers        map[string]UserProvider `pulumi:"providers,optional"        yaml:"providers"`
	DefaultModel     *UserModel              `pulumi:"defaultModel,optional"     yaml:"default_model"`
	ModelsGateway    *string                 `pulumi:"modelsGateway,optional"    yaml:"models_gateway"`
	Board            *UserBoard              `pulumi:"board,optional"            yaml:"board"`
	CredentialHelper *UserCredentialHelper   `pulumi:"credentialHelper,optional" yaml:"credential_helper"`
	SandboxAllowlist []string                `pulumi:"sandboxAllowlist,optional" yaml:"sandbox_allowlist"`
	Extra            map[string]any          `pulumi:"extra,optional"            yaml:",inline"`
	Path             *string                 `pulumi:"path,optional"             yaml:"-"`
	Overwrite        *bool                   `pulumi:"overwrite,optional"        yaml:"-"`
	SkipValidation   *bool                   `pulumi:"skipValidation,optional"   yaml:"-"`
}

// Annotate describes the inputs.
func (a *UserSettingsArgs) Annotate(an infer.Annotator) {
	an.Describe(&a.Settings, "Global preferences (`settings:`): TUI appearance and behaviour, notifications, the "+
		"default safety mode, global permissions, hooks, keybindings and layout.")
	an.Describe(&a.Aliases, "Agent aliases (`aliases:`), keyed by alias name, as created by "+
		"`docker agent alias add`.")
	an.Describe(&a.Providers, "Global model providers (`providers:`), keyed by name. docker-agent merges them into "+
		"every agent configuration it loads; an agent file's own definition wins on a name conflict.")
	an.Describe(&a.DefaultModel, "The model used when an agent's model is `auto` (`default_model:`).")
	an.Describe(&a.ModelsGateway, "The default models gateway address (`models_gateway:`). The "+
		"`--models-gateway` flag and the `DOCKER_AGENT_MODELS_GATEWAY` environment variable take precedence.")
	an.Describe(&a.Board, "The `docker agent board` Kanban configuration (`board:`).")
	an.Describe(&a.CredentialHelper, "A command that supplies the Docker token (`credential_helper:`).")
	an.Describe(&a.SandboxAllowlist, "Hosts (with an optional `:port`) opened in the sandbox network proxy on "+
		"every sandboxed run (`sandbox_allowlist:`), in addition to the hosts docker-agent opens itself and the "+
		"agent's `runtime.network_allowlist`. Commas and whitespace are rejected.")
	an.Describe(&a.Extra, "Additional top-level keys that this provider version does not model, written "+
		"verbatim (snake_case keys). They must not repeat a typed input; `version` is always `v1`.")
	an.Describe(&a.Path, "Where to write the file, for example `~/.config/cagent/config.yaml`. Resolved on the "+
		"machine that runs Pulumi: `~` is that user's home directory and relative paths are relative to the "+
		"Pulumi project directory. A symbolic link is followed and kept. When unset, nothing is written and the "+
		"resource only renders `yaml`.")
	an.Describe(&a.Overwrite, "Replace a file that already exists at `path` with different content when the "+
		"resource is created or `path` changes. Without it, the preview fails instead, so an existing "+
		"configuration (for example aliases added with `docker agent alias add`) is never lost silently. An "+
		"existing file is always moved to `<file>.pulumi-backup` first and moved back when the resource is "+
		"deleted.")
	an.SetDefault(&a.Overwrite, false)
	an.Describe(&a.SkipValidation, "Skip docker-agent's semantic checks (safety modes, alias names, providers, "+
		"hooks, TUI options). The document must still decode into docker-agent's types. Use it for values "+
		"introduced by a docker-agent release newer than "+DockerAgentVersion+".")
}

// UserSettingsState is the state of UserSettings.
type UserSettingsState struct {
	UserSettingsArgs
	YAML       string  `pulumi:"yaml"`
	SHA256     string  `pulumi:"sha256"`
	FilePath   *string `pulumi:"filePath,optional"`
	BackupPath *string `pulumi:"backupPath,optional"`
}

// Annotate describes the outputs.
func (s *UserSettingsState) Annotate(an infer.Annotator) {
	an.Describe(&s.YAML, "The rendered configuration file, exactly as docker-agent itself writes it. After a "+
		"refresh it holds the file's current content, so drift shows up in the next diff.")
	an.Describe(&s.SHA256, "The hex-encoded SHA-256 of `yaml`.")
	an.Describe(&s.FilePath, "The absolute path of the written file. Unset when `path` is unset.")
	an.Describe(&s.BackupPath, "Where the file that already existed at `path` was moved when the resource took "+
		"it over. Deleting the resource moves it back. Unset when there was no file, and for an imported resource.")
}

// Check validates the inputs and, when they are all known, the rendered document.
func (*UserSettings) Check(ctx context.Context, req infer.CheckRequest) (infer.CheckResponse[UserSettingsArgs], error) {
	args, failures, err := checkInputs(ctx, req.NewInputs, func(a UserSettingsArgs) bool {
		return skipValidation(ctx, a.SkipValidation)
	})
	if err != nil || len(failures) > 0 {
		return infer.CheckResponse[UserSettingsArgs]{Inputs: args, Failures: failures}, err
	}
	if v := req.NewInputs.Get("path"); args.Path != nil && !v.HasComputed() {
		if _, err := resolveTarget(args.Path); err != nil {
			failures = append(failures, p.CheckFailure{Property: "path", Reason: err.Error()})
		}
	}
	if req.NewInputs.HasComputed() {
		return infer.CheckResponse[UserSettingsArgs]{Inputs: args, Failures: failures}, nil
	}
	secrets := secretStrings(req.NewInputs)
	cfgYAML, cfgErrs, warnings := checkUserSettings(args, !skipValidation(ctx, args.SkipValidation))
	for _, e := range cfgErrs {
		failures = append(failures, p.CheckFailure{
			Property: userSettingsProperty(e),
			Reason:   "invalid docker-agent user settings: " + validate.Redact(e, secrets),
		})
	}
	logWarnings(ctx, warnings, secrets)
	if len(failures) == 0 {
		// Surface a conflict with an existing file already in the preview.
		target, err := userSettingsTarget(args)
		old, oldErr := userSettingsTarget(UserSettingsArgs{Path: oldInputString(req.OldInputs, "path")})
		if err == nil && oldErr == nil {
			if reason := checkClaim(target, req.OldInputs, old.path, cfgYAML, isTrue(args.Overwrite)); reason != "" {
				failures = append(failures, p.CheckFailure{Property: "path", Reason: reason})
			}
		}
	}
	return infer.CheckResponse[UserSettingsArgs]{Inputs: args, Failures: failures}, nil
}

// checkUserSettings renders the document and, when strict, validates it with
// docker-agent's checks.
func checkUserSettings(args UserSettingsArgs, strict bool) (yaml string, errs, warnings []string) {
	doc, err := usersettings.Encode(args)
	if err != nil {
		return "", []string{err.Error()}, nil
	}
	cfg, yaml, err := usersettings.Render(doc)
	if err != nil {
		return "", []string{err.Error()}, nil
	}
	if !strict {
		return yaml, nil, nil
	}
	errs, warnings = usersettings.Validate(cfg)
	return yaml, errs, warnings
}

// renderUserSettings returns the document docker-agent would write for args.
func renderUserSettings(args UserSettingsArgs) (string, error) {
	doc, err := usersettings.Encode(args)
	if err != nil {
		return "", err
	}
	_, out, err := usersettings.Render(doc)
	return out, err
}

func userSettingsTarget(args UserSettingsArgs) (localFileTarget, error) {
	path, err := resolveTarget(args.Path)
	return localFileTarget{
		path: path, mode: 0o600, dirMode: 0o700,
		what: "the existing docker-agent user configuration (for example aliases added with `docker agent alias add` " +
			"and settings saved from the TUI)",
	}, err
}

func userSettingsState(args UserSettingsArgs) (UserSettingsState, localFileTarget, error) {
	state := UserSettingsState{UserSettingsArgs: args}
	y, renderErr := renderUserSettings(args)
	if renderErr == nil {
		state.YAML = y
		state.SHA256 = usersettings.SHA256(y)
	}
	target, err := userSettingsTarget(args)
	state.FilePath = strPtrOrNil(target.path)
	if renderErr != nil {
		return state, target, fmt.Errorf("rendering docker-agent user settings: %w", renderErr)
	}
	return state, target, err
}

// Create renders the configuration and writes the file when `path` is set.
func (*UserSettings) Create(
	ctx context.Context, req infer.CreateRequest[UserSettingsArgs],
) (infer.CreateResponse[UserSettingsState], error) {
	state, target, err := userSettingsState(req.Inputs)
	if req.DryRun {
		// Previews never write. Unknown inputs decode to zero values, so rendering errors
		// are ignored here; withLocalFilePreview marks such outputs unknown.
		return infer.CreateResponse[UserSettingsState]{ID: req.Name, Output: state}, nil
	}
	if err != nil {
		return infer.CreateResponse[UserSettingsState]{}, err
	}
	backup, warning, err := target.apply(state.YAML, isTrue(req.Inputs.Overwrite), "", "", false)
	if err != nil {
		return infer.CreateResponse[UserSettingsState]{}, err
	}
	logWarning(ctx, warning)
	state.BackupPath = strPtrOrNil(backup)
	return infer.CreateResponse[UserSettingsState]{ID: req.Name, Output: state}, nil
}

// Update re-renders the configuration and rewrites (or moves) the file.
func (*UserSettings) Update(
	ctx context.Context, req infer.UpdateRequest[UserSettingsArgs, UserSettingsState],
) (infer.UpdateResponse[UserSettingsState], error) {
	state, target, err := userSettingsState(req.Inputs)
	if req.DryRun {
		state.BackupPath = req.State.BackupPath
		return infer.UpdateResponse[UserSettingsState]{Output: state}, nil
	}
	if err != nil {
		return infer.UpdateResponse[UserSettingsState]{}, err
	}
	oldPath := derefStr(req.State.FilePath)
	backup, warning, err := target.apply(state.YAML, isTrue(req.Inputs.Overwrite),
		oldPath, derefStr(req.State.BackupPath), adoptedFile(req.ID, oldPath))
	if err != nil {
		return infer.UpdateResponse[UserSettingsState]{}, err
	}
	logWarning(ctx, warning)
	state.BackupPath = strPtrOrNil(backup)
	return infer.UpdateResponse[UserSettingsState]{Output: state}, nil
}

// Read detects drift between the state and the file. Without a state, it imports (adopts)
// the existing file named by the ID.
func (*UserSettings) Read(
	ctx context.Context, req infer.ReadRequest[UserSettingsArgs, UserSettingsState],
) (infer.ReadResponse[UserSettingsArgs, UserSettingsState], error) {
	state := req.State
	if state.SHA256 == "" {
		return importUserSettings(ctx, req.ID)
	}
	if path := derefStr(state.FilePath); path != "" {
		content, changed, gone, err := refreshContent(path, state.YAML)
		if err != nil {
			return infer.ReadResponse[UserSettingsArgs, UserSettingsState]{}, err
		}
		if gone || changed {
			// A deleted file reads as empty content, so the next `up` writes it again and the
			// backup (if any) stays tracked.
			state.YAML = content
			state.SHA256 = usersettings.SHA256(content)
		}
	}
	return infer.ReadResponse[UserSettingsArgs, UserSettingsState]{ID: req.ID, Inputs: req.Inputs, State: state}, nil
}

// Delete moves the backup of the file that existed before back, or removes the managed
// file. An adopted file (imported with `pulumi import`) is left in place.
func (*UserSettings) Delete(ctx context.Context, req infer.DeleteRequest[UserSettingsState]) (infer.DeleteResponse, error) {
	return infer.DeleteResponse{}, releaseFile(ctx, req.ID, derefStr(req.State.FilePath), derefStr(req.State.BackupPath))
}

// Diff compares the inputs and detects drift: a refreshed file whose content differs from
// the rendered document is an update.
func (*UserSettings) Diff(
	_ context.Context, req infer.DiffRequest[UserSettingsArgs, UserSettingsState],
) (infer.DiffResponse, error) {
	olds, news := req.State.UserSettingsArgs, req.Inputs
	diff := map[string]p.PropertyDiff{}
	fields := []struct {
		name     string
		old, new any
	}{
		{"settings", olds.Settings, news.Settings},
		{"aliases", olds.Aliases, news.Aliases},
		{"providers", olds.Providers, news.Providers},
		{"defaultModel", olds.DefaultModel, news.DefaultModel},
		{"modelsGateway", olds.ModelsGateway, news.ModelsGateway},
		{"board", olds.Board, news.Board},
		{"credentialHelper", olds.CredentialHelper, news.CredentialHelper},
		{"sandboxAllowlist", olds.SandboxAllowlist, news.SandboxAllowlist},
		{"extra", olds.Extra, news.Extra},
		{"path", olds.Path, news.Path},
		{"overwrite", olds.Overwrite, news.Overwrite},
		{"skipValidation", olds.SkipValidation, news.SkipValidation},
	}
	for _, f := range fields {
		if !equalValues(f.old, f.new) {
			diff[f.name] = p.PropertyDiff{Kind: p.Update, InputDiff: true}
		}
	}
	if y, err := renderUserSettings(news); err == nil && y != req.State.YAML {
		// Drift found by a refresh, or a rendering change between provider versions.
		diff["yaml"] = p.PropertyDiff{Kind: p.Update}
	}
	if target, err := userSettingsTarget(news); err == nil && target.path != derefStr(req.State.FilePath) {
		diff["filePath"] = p.PropertyDiff{Kind: p.Update}
	}
	return infer.DiffResponse{HasChanges: len(diff) > 0, DetailedDiff: diff}, nil
}

// WireDependencies declares how secretness flows: `yaml` and `sha256` are secret when any
// input is secret. They are always known because Create and Update compute them during
// previews; withLocalFilePreview marks them unknown when an input is unknown.
func (*UserSettings) WireDependencies(f infer.FieldSelector, args *UserSettingsArgs, state *UserSettingsState) {
	secret := f.InputField(args).Secret()
	for _, out := range []any{&state.YAML, &state.SHA256} {
		f.OutputField(out).AlwaysKnown()
		f.OutputField(out).DependsOn(secret)
	}
	f.OutputField(&state.FilePath).AlwaysKnown()
	f.OutputField(&state.FilePath).DependsOn(f.InputField(&args.Path).Secret())
}
