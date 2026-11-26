// Copyright 2026, Engin Diri.
// SPDX-License-Identifier: Apache-2.0

package provider

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/pulumi/pulumi-go-provider/infer"

	"github.com/dirien/pulumi-docker-agent/provider/pkg/agentsignore"
	"github.com/dirien/pulumi-docker-agent/provider/pkg/localfile"
	"github.com/dirien/pulumi-docker-agent/provider/pkg/usersettings"
)

// GetUserSettings reads an existing docker-agent user configuration file.
type GetUserSettings struct{}

const getUserSettingsDoc = `Reads an existing Docker Agent
[user configuration](https://docs.docker.com/ai/docker-agent/configuration/user-settings/) file
(` + "`~/.config/cagent/config.yaml`" + ` by default) into the typed fields of ` + "`UserSettings`" + `, without managing it.
Use it to build on settings that ` + "`docker agent alias add`" + `, ` + "`docker agent setup`" + `
or another program wrote, for example to reuse the default model or an alias in an ` + "`AgentConfig`" + `.

The file is read on the machine that runs Pulumi: ` + "`~`" + ` is that user's home directory and relative paths are
relative to the Pulumi project directory. A missing file is not an error: ` + "`exists`" + ` is ` + "`false`" + ` and every
other field is empty, which is what docker-agent uses then. docker-agent's own types parse the file, so a file that
docker-agent cannot load fails the function. The settings are not validated beyond that; ` + "`UserSettings`" + ` reports
values that docker-agent rejects.`

// Annotate describes the function.
func (g *GetUserSettings) Annotate(a infer.Annotator) {
	a.SetToken("index", "getUserSettings")
	a.Describe(&g, describe(getUserSettingsDoc, getUserSettingsExamples, ""))
}

// GetUserSettingsArgs are the inputs of getUserSettings.
type GetUserSettingsArgs struct {
	Path *string `pulumi:"path,optional"`
}

// Annotate describes the inputs.
func (a *GetUserSettingsArgs) Annotate(an infer.Annotator) {
	an.Describe(&a.Path, "The file to read. Defaults to `"+usersettings.DefaultPath+"`, where docker-agent reads its "+
		"user configuration on Linux and macOS. A symbolic link is followed.")
}

// GetUserSettingsResult is the result of getUserSettings.
type GetUserSettingsResult struct {
	Settings         *UserGlobalSettings     `pulumi:"settings,optional"`
	Aliases          map[string]UserAlias    `pulumi:"aliases,optional"`
	Providers        map[string]UserProvider `pulumi:"providers,optional"`
	DefaultModel     *UserModel              `pulumi:"defaultModel,optional"`
	ModelsGateway    *string                 `pulumi:"modelsGateway,optional"`
	Board            *UserBoard              `pulumi:"board,optional"`
	CredentialHelper *UserCredentialHelper   `pulumi:"credentialHelper,optional"`
	SandboxAllowlist []string                `pulumi:"sandboxAllowlist,optional"`
	Extra            map[string]any          `pulumi:"extra,optional" provider:"secret"`
	FilePath         string                  `pulumi:"filePath"`
	Exists           bool                    `pulumi:"exists"`
	YAML             string                  `pulumi:"yaml" provider:"secret"`
	SHA256           string                  `pulumi:"sha256"`
}

// Annotate describes the result.
func (r *GetUserSettingsResult) Annotate(an infer.Annotator) {
	an.Describe(&r.Settings, "Global preferences (`settings:`).")
	an.Describe(&r.Aliases, "Agent aliases (`aliases:`), keyed by alias name.")
	an.Describe(&r.Providers, "Global model providers (`providers:`), keyed by name.")
	an.Describe(&r.DefaultModel, "The model used when an agent's model is `auto` (`default_model:`).")
	an.Describe(&r.ModelsGateway, "The default models gateway address (`models_gateway:`).")
	an.Describe(&r.Board, "The `docker agent board` configuration (`board:`).")
	an.Describe(&r.CredentialHelper, "The command that supplies the Docker token (`credential_helper:`).")
	an.Describe(&r.SandboxAllowlist, "Hosts opened in the sandbox network proxy on every sandboxed run "+
		"(`sandbox_allowlist:`).")
	an.Describe(&r.Extra, "Top-level keys that this provider version does not model, as they are in the file. "+
		"Secret, because the provider cannot tell whether they hold credentials.")
	an.Describe(&r.FilePath, "The absolute path of the file that was read.")
	an.Describe(&r.Exists, "Whether the file exists. When `false`, every other field except `filePath` is empty.")
	an.Describe(&r.YAML, "The file's content, exactly as it is on disk. Secret, because the file can hold "+
		"credentials (hook `env` values, board `headers`), which the typed fields mark secret too.")
	an.Describe(&r.SHA256, "The hex-encoded SHA-256 of `yaml`, empty when the file does not exist.")
}

// Invoke reads the file.
func (*GetUserSettings) Invoke(
	_ context.Context, req infer.FunctionRequest[GetUserSettingsArgs],
) (infer.FunctionResponse[GetUserSettingsResult], error) {
	path := usersettings.DefaultPath
	if p := strings.TrimSpace(derefStr(req.Input.Path)); p != "" {
		path = p
	}
	data, file, exists, err := readLocalFile(path)
	if err != nil || !exists {
		return infer.FunctionResponse[GetUserSettingsResult]{Output: GetUserSettingsResult{FilePath: file}}, err
	}
	doc, _, err := usersettings.Canonical(data)
	if err != nil {
		return infer.FunctionResponse[GetUserSettingsResult]{}, fmt.Errorf("parsing %s: %w", file, err)
	}
	var args UserSettingsArgs
	if err := usersettings.Decode(doc, &args); err != nil {
		return infer.FunctionResponse[GetUserSettingsResult]{}, fmt.Errorf("%s cannot be expressed with the typed "+
			"fields: %w", file, err)
	}
	return infer.FunctionResponse[GetUserSettingsResult]{Output: GetUserSettingsResult{
		Settings:         args.Settings,
		Aliases:          args.Aliases,
		Providers:        args.Providers,
		DefaultModel:     args.DefaultModel,
		ModelsGateway:    args.ModelsGateway,
		Board:            args.Board,
		CredentialHelper: args.CredentialHelper,
		SandboxAllowlist: args.SandboxAllowlist,
		Extra:            args.Extra,
		FilePath:         file,
		Exists:           true,
		YAML:             string(data),
		SHA256:           usersettings.SHA256(string(data)),
	}}, nil
}

// GetAgentsIgnore reads an existing `.agentsignore` file.
type GetAgentsIgnore struct{}

const getAgentsIgnoreDoc = `Reads an existing Docker Agent
[` + "`.agentsignore`" + `](https://docs.docker.com/ai/docker-agent/configuration/agentsignore/) file into the
` + "`patterns`" + ` and ` + "`comment`" + ` of ` + "`AgentsIgnore`" + `, without managing it. Use it to extend a
project's patterns, for example in an ` + "`AgentsIgnore`" + ` of another directory or a container image.

The comment block at the top of the file becomes ` + "`comment`" + ` and every other non-empty line a pattern, in order
(comments between patterns are dropped, as for ` + "`pulumi import`" + `). The patterns are returned as they are:
` + "`AgentsIgnore`" + ` reports patterns that docker-agent reads differently. The file is read on the machine that runs
Pulumi; a missing file is not an error, ` + "`exists`" + ` is then ` + "`false`" + `. Unlike docker-agent, the function does
not look for the nearest ` + "`.agentsignore`" + ` above the directory.`

// Annotate describes the function.
func (g *GetAgentsIgnore) Annotate(a infer.Annotator) {
	a.SetToken("index", "getAgentsIgnore")
	a.Describe(&g, describe(getAgentsIgnoreDoc, getAgentsIgnoreExamples, ""))
}

// GetAgentsIgnoreArgs are the inputs of getAgentsIgnore.
type GetAgentsIgnoreArgs struct {
	Directory *string `pulumi:"directory,optional"`
}

// Annotate describes the inputs.
func (a *GetAgentsIgnoreArgs) Annotate(an infer.Annotator) {
	an.Describe(&a.Directory, "The project directory that holds `.agentsignore`, or the path of a file named `.agentsignore`. "+
		"Defaults to the Pulumi project directory. `~` is the home directory of the user that runs Pulumi.")
}

// GetAgentsIgnoreResult is the result of getAgentsIgnore.
type GetAgentsIgnoreResult struct {
	Patterns []string `pulumi:"patterns"`
	Comment  *string  `pulumi:"comment,optional"`
	FilePath string   `pulumi:"filePath"`
	Exists   bool     `pulumi:"exists"`
	Content  string   `pulumi:"content"`
	SHA256   string   `pulumi:"sha256"`
}

// Annotate describes the result.
func (r *GetAgentsIgnoreResult) Annotate(an infer.Annotator) {
	an.Describe(&r.Patterns, "The patterns, in file order. Empty when the file does not exist.")
	an.Describe(&r.Comment, "The comment block at the top of the file, without the `# ` prefixes.")
	an.Describe(&r.FilePath, "The absolute path of the file that was read.")
	an.Describe(&r.Exists, "Whether the file exists.")
	an.Describe(&r.Content, "The file's content, exactly as it is on disk.")
	an.Describe(&r.SHA256, "The hex-encoded SHA-256 of `content`, empty when the file does not exist.")
}

// Invoke reads the file.
func (*GetAgentsIgnore) Invoke(
	_ context.Context, req infer.FunctionRequest[GetAgentsIgnoreArgs],
) (infer.FunctionResponse[GetAgentsIgnoreResult], error) {
	dir := strings.TrimSpace(derefStr(req.Input.Directory))
	if dir == "" {
		dir = "."
	}
	path := filepath.Join(dir, agentsignore.FileName)
	if filepath.Base(dir) == agentsignore.FileName {
		path = dir
	} else {
		resolved, err := localfile.Resolve(dir)
		if err != nil {
			return infer.FunctionResponse[GetAgentsIgnoreResult]{}, fmt.Errorf("resolving %s: %w", dir, err)
		}
		info, err := os.Stat(resolved)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			// A missing directory has no .agentsignore: exists is false.
		case err != nil:
			return infer.FunctionResponse[GetAgentsIgnoreResult]{}, fmt.Errorf("reading %s: %w", resolved, err)
		case !info.IsDir():
			return infer.FunctionResponse[GetAgentsIgnoreResult]{}, fmt.Errorf("%s is neither a directory nor a %s "+
				"file", resolved, agentsignore.FileName)
		}
	}
	data, file, exists, err := readLocalFile(path)
	out := GetAgentsIgnoreResult{Patterns: []string{}, FilePath: file}
	if err != nil || !exists {
		return infer.FunctionResponse[GetAgentsIgnoreResult]{Output: out}, err
	}
	parsed := agentsignore.Parse(string(data))
	if parsed.Patterns != nil {
		out.Patterns = parsed.Patterns
	}
	out.Comment = strPtrOrNil(parsed.Comment)
	out.Exists = true
	out.Content = string(data)
	out.SHA256 = agentsignore.SHA256(out.Content)
	return infer.FunctionResponse[GetAgentsIgnoreResult]{Output: out}, nil
}

// readLocalFile reads the file at path (as the user wrote it) for a function. It returns the
// content, the absolute path and whether the file exists.
func readLocalFile(path string) (data []byte, file string, exists bool, err error) {
	file, err = localfile.Resolve(path)
	if err != nil {
		return nil, "", false, fmt.Errorf("resolving %s: %w", path, err)
	}
	data, exists, err = localfile.Read(file)
	switch {
	case err != nil:
		return nil, file, false, fmt.Errorf("reading %s: %w", file, err)
	case exists && len(data) > localfile.MaxReadSize:
		return nil, file, false, fmt.Errorf("%s is larger than %d bytes", file, localfile.MaxReadSize)
	}
	return data, file, exists, nil
}
